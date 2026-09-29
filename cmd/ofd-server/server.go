package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	json "github.com/goccy/go-json"

	"github.com/zc310/ofd/internal/allowlist"

	"github.com/fasthttp/router"
	"github.com/valyala/fasthttp"

	"github.com/zc310/ofd/internal/convertersvc"
	"github.com/zc310/ofd/internal/jobstore"
	"github.com/zc310/ofd/internal/notify"
	"github.com/zc310/ofd/internal/transfer"
)

// RemoteTargets 是各类远程输出目标的注册表。
//
// 请求方能给的只有目标名，地址与凭据全在这里——这是"让调用方指定 URL 就能
// 往任意主机上传数据"的最后一道防线。
type RemoteTargets struct {
	FTP    map[string]*transfer.FTPSink
	S3     map[string]*transfer.MinioSink
	WebDAV map[string]*transfer.WebDAVSink
	SFTP   map[string]*transfer.SFTPSink
}

// Server 是 HTTP 层。
//
// 路由手写而不是引入 fasthttp/router：接口只有 5 个，引入一个额外模块换不来
// 什么，而依赖越少这个服务越好审计。
type Server struct {
	cfg       *Config
	store     *jobstore.Store
	registry  *notify.Registry
	allowlist *allowlist.List
	ftp       map[string]*transfer.FTPSink
	s3        map[string]*transfer.MinioSink
	webdav    map[string]*transfer.WebDAVSink
	sftp      map[string]*transfer.SFTPSink
	log       *slog.Logger
	ready     func() bool
	router    *router.Router
	// startedAt 是服务对象创建时刻，用于 /v1/stats 的启动时间与运行时长。
	//
	// 记在 NewServer 而不是 Start：测试里普遍直接用 Handler() 而不启动
	// 监听器，记在 Start 会让绝大多数用例的启动时间恒为零。NewServer 与
	// Start 之间只有准备步骤，差值在毫秒级。
	startedAt time.Time
	// apiKey 是访问 /v1/* 的令牌，按字节保存以配合定长比较。
	apiKey []byte
	// convert 供 stream 输出的同步转换使用；异步路径用的是 runner 手上的
	// 那一个，两者是不同的 Service 实例但共用同一个 TempDir。
	convert *convertersvc.Service
	// inlineFast 与 inlineHeavy 限制同步转换的并发，按通道分开。
	inlineFast, inlineHeavy chan struct{}
	inlineOnce              sync.Once
}

// NewServer 构造 HTTP 层。
func NewServer(cfg *Config, store *jobstore.Store, registry *notify.Registry, list *allowlist.List, targets *RemoteTargets, convert *convertersvc.Service, log *slog.Logger) *Server {
	if targets == nil {
		targets = &RemoteTargets{}
	}
	if convert == nil {
		// 同步转换没有可用的转换器时，stream 请求会明确报错而不是崩在空指针上。
		convert = convertersvc.New("", list)
	}
	s := &Server{cfg: cfg, store: store, registry: registry, allowlist: list,
		ftp: targets.FTP, s3: targets.S3, webdav: targets.WebDAV,
		sftp: targets.SFTP, convert: convert, log: log}
	s.apiKey = []byte(cfg.APIKey)
	s.startedAt = time.Now().UTC()
	s.router = s.routes()
	return s
}

// SetReadyFunc 注入就绪探针。
func (s *Server) SetReadyFunc(fn func() bool) { s.ready = fn }

// Handler 返回 fasthttp 请求处理函数。
//
// 在路由外面包一层做两件事：限制请求体大小，以及兜住处理函数里的 panic。
// 两者都放在这里而不是各自分发器里，是因为它们对所有端点一视同仁。
func (s *Server) Handler() fasthttp.RequestHandler {
	routed := s.router.Handler
	limit := s.cfg.MaxUploadBytes
	return func(ctx *fasthttp.RequestCtx) {
		defer func() {
			// 一个畸形请求的边界问题应该表现为 500，而不是整个进程退出。
			if rec := recover(); rec != nil {
				s.log.Error("请求处理 panic", "path", string(ctx.Path()), "panic", rec)
				writeError(ctx, fasthttp.StatusInternalServerError, "internal_error", "服务内部错误")
			}
		}()
		// fasthttp 的 MaxRequestBodySize 也会拦，但它返回的是纯文本；
		// 这里先拦一次，好让错误响应保持统一的 JSON 形状。
		if int64(len(ctx.Request.Body())) > limit {
			writeError(ctx, fasthttp.StatusRequestEntityTooLarge, "payload_too_large",
				fmt.Sprintf("请求体超过 %d 字节", limit))
			return
		}
		routed(ctx)
	}
}

// routes 构造路由表。
func (s *Server) routes() *router.Router {
	r := router.New()
	// 关掉路径自动纠正：默认的 RedirectTrailingSlash 对 GET 无害，但
	// 对 POST 是危险的——重定向既可能变成 GET，也可能丢掉请求体，
	// 调用方只会看到一句莫名其妙的 301。
	r.RedirectTrailingSlash = false
	r.RedirectFixedPath = false
	// 未知路径与错误方法由我们统一给 JSON 响应，而不是 fasthttp 的纯文本。
	r.NotFound = func(ctx *fasthttp.RequestCtx) {
		writeError(ctx, fasthttp.StatusNotFound, "not_found", "未知路径")
	}
	r.MethodNotAllowed = func(ctx *fasthttp.RequestCtx) {
		writeError(ctx, fasthttp.StatusMethodNotAllowed, "method_not_allowed", "方法不允许")
	}

	r.GET("/healthz", s.handleHealth)
	r.GET("/readyz", s.handleReady)
	// /v1/* 全部要令牌。探针不设限：编排系统的 kubelet 与负载均衡器
	// 不会也不该持有业务令牌，把它们挡在门外只会让健康检查一直失败。
	r.POST("/v1/convert", s.requireAPIKey(s.handleSubmit))
	r.GET("/v1/jobs/{id}", s.requireAPIKey(s.handleGetJob))
	r.GET("/v1/jobs/{id}/content", s.requireAPIKey(s.handleContent))
	r.POST("/v1/jobs/{id}/cancel", s.requireAPIKey(s.handleCancelJob))
	// /metrics 要令牌：它暴露转换量与格式分布，属于运营数据，而默认监听
	// 所有网卡。Prometheus 支持在 scrape_configs 里配 bearer_token_file，
	// 采集端多一份配置，好过把数据敞着。
	r.GET("/metrics", s.requireAPIKey(s.handleMetrics))
	// 转换量统计的 JSON 视图，与 /metrics 同源不同形。放在 /v1 下是因为
	// 它是业务数据（转换量与格式分布），且沿用同一套令牌。
	r.GET("/v1/stats", s.requireAPIKey(s.handleStats))
	return r
}

// requireAPIKey 校验 Bearer 令牌，通过后转交 next。
//
// 令牌从两个位置取，缺一不可：
//
//	Authorization: Bearer <key>    标准形式，命令行与脚本用这个
//	X-OFD-Api-Key: <key>           给浏览器页面用——fetch 也能设请求头，
//	                               但一旦将来要支持 Cookie 会话或表单
//	                               提交，自定义头是比自定义鉴权更省事的方向
//
// 比较用 subtle.ConstantTimeCompare：逐字节提前返回的写法会把密钥前缀
// 的信息泄进响应时间，足够被多次采样还原出整个令牌。
func (s *Server) requireAPIKey(next fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		presented := ctx.Request.Header.Peek("Authorization")
		if token, ok := bytes.CutPrefix(presented, []byte("Bearer ")); ok {
			if s.apiKeyMatches(token) {
				next(ctx)
				return
			}
		}
		if direct := ctx.Request.Header.Peek(apiKeyHeader); s.apiKeyMatches(direct) {
			next(ctx)
			return
		}
		ctx.Response.Header.Set("WWW-Authenticate", `Bearer realm="ofd-server"`)
		writeError(ctx, fasthttp.StatusUnauthorized, "unauthorized", "缺少或无效的 API 令牌")
	}
}

// apiKeyHeader 是浏览器友好的令牌头名。
// 注意：fasthttp 会把自定义头名规范化成 X-Ofd-*（每个连字符后首字母大写，
// 不保留缩写全大写）。HTTP 头名大小写不敏感，所以 Peek 与 Set 都能对上，
// 但抓包看到的是 X-Ofd-Job-Id 而不是 X-OFD-Job-Id，文档按前者写。
const apiKeyHeader = "X-OFD-Api-Key"

func (s *Server) apiKeyMatches(presented []byte) bool {
	// 长度不等时 ConstantTimeCompare 直接返回 0 且耗时与内容无关，
	// 所以长度本身没有被泄露；但空令牌必须先挡掉——否则配置成空值时
	// 空请求头就能通过。
	if len(presented) == 0 || len(presented) != len(s.apiKey) {
		return false
	}
	return subtle.ConstantTimeCompare(presented, s.apiKey) == 1
}

func (s *Server) handleHealth(ctx *fasthttp.RequestCtx) {
	writeJSON(ctx, fasthttp.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleReady(ctx *fasthttp.RequestCtx) {
	if s.ready != nil && !s.ready() {
		writeError(ctx, fasthttp.StatusServiceUnavailable, "not_ready", "尚未就绪")
		return
	}
	writeJSON(ctx, fasthttp.StatusOK, map[string]any{"status": "ready"})
}

// submitRequest 是提交转换的请求体。
type submitRequest struct {
	Input  convertersvc.Input  `json:"input"`
	Output convertersvc.Output `json:"output"`
	// Notify 只允许引用已注册的目标名，不接受 URL 或密钥。
	Notify *submitNotify `json:"notify,omitempty"`
	// HighPriority 让任务插到普通任务之前。
	HighPriority bool `json:"high_priority,omitempty"`
}

type submitNotify struct {
	// Target 是已注册的目标名。
	Target string `json:"target"`
	// Events 限定事件，空表示目标的默认集合。
	Events []string `json:"events,omitempty"`
}

func (s *Server) handleSubmit(ctx *fasthttp.RequestCtx) {
	body := ctx.Request.Body()
	if len(body) == 0 {
		writeError(ctx, fasthttp.StatusBadRequest, "invalid_request", "请求体为空")
		return
	}
	var request submitRequest
	// 未知字段报错，与配置文件同一套。请求体被静默忽略未知字段时代价特别大：
	// 写错 file_name 会安静地退回默认产物名 output.pdf，调用方拿到的是
	// "转换成功但名字不对"，排查起来比直接报错费时间得多。
	if err := decodeStrictJSON(body, &request); err != nil {
		writeError(ctx, fasthttp.StatusBadRequest, "invalid_request", "解析请求体失败: "+err.Error())
		return
	}
	// 决定同步还是异步之前先规范化 output.kind。
	//
	// 空值按 stream 处理，判定必须发生在规范化之后：直接拿字面量比较
	// `kind == "stream"`，省略字段的请求会被送进异步队列，随后按 stream 处理、
	// 产物只留在内存里随 Result 丢弃——任务报 succeeded，而调用方拿不到任何
	// 东西。顺带把未知值也从"入队后在 worker 里失败"提前到提交阶段报 400：
	// 那种失败要等几秒后才出现在任务日志里，而日志里看不出是 kind 写错了。
	kind, err := convertersvc.NormalizeOutputKind(request.Output.Kind)
	if err != nil {
		writeError(ctx, fasthttp.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	request.Output.Kind = kind

	if request.Output.Kind == convertersvc.OutputStream {
		s.handleConvertInline(ctx, &request)
		return
	}

	job, err := s.buildJob(&request)
	if err != nil {
		writeError(ctx, statusForRequestError(err), codeForRequestError(err), err.Error())
		return
	}
	if err := s.store.Enqueue(job); err != nil {
		s.log.Error("入队失败", "err", err)
		writeError(ctx, fasthttp.StatusInternalServerError, "internal_error", "入队失败")
		return
	}
	s.log.Info("已受理", "job", job.ID, "lane", job.Lane, "to", job.To)
	// 202：任务已受理但尚未开始。同步返回结果需要另一个端点，
	// 而不是让这个请求挂着等——那会把 HTTP 连接数和队列深度绑在一起。
	writeJSON(ctx, fasthttp.StatusAccepted, map[string]any{
		"id":         job.ID,
		"state":      job.State,
		"lane":       job.Lane,
		"to":         job.To,
		"created_at": job.CreatedAt,
		"status_url": "/v1/jobs/" + job.ID,
	})
}

// buildJob 校验请求并生成任务记录。
func (s *Server) buildJob(request *submitRequest) (*jobstore.Job, error) {
	if request.Input.Kind != "" && request.Input.Kind != convertersvc.InputUpload && request.Input.Kind != convertersvc.InputURL {
		return nil, &requestError{kind: "invalid_request", status: fasthttp.StatusBadRequest,
			msg: fmt.Sprintf("未知输入种类 %q", request.Input.Kind)}
	}
	if request.Input.Kind == convertersvc.InputURL && request.Input.URL == "" {
		return nil, &requestError{kind: "invalid_request", status: fasthttp.StatusBadRequest, msg: "input.url 不能为空"}
	}
	if request.Input.Kind == convertersvc.InputURL {
		if err := s.checkInputURL(request.Input.URL); err != nil {
			return nil, err
		}
	}
	if request.Input.Kind == convertersvc.InputUpload && len(request.Input.Bytes) == 0 {
		return nil, &requestError{kind: "invalid_request", status: fasthttp.StatusBadRequest, msg: "input.bytes 不能为空"}
	}
	if request.Output.Format == "" {
		return nil, &requestError{kind: "invalid_request", status: fasthttp.StatusBadRequest, msg: "output.format 不能为空"}
	}
	// 文件名在这里就拒掉，而不是等转换跑完才失败：非法文件名是请求本身的问题，
	// 让它占一个队列位置再报 failed 是白排队。完整规则见 convertersvc。
	if err := convertersvc.ValidateOutputFileName(request.Output.FileName); err != nil {
		return nil, &requestError{kind: "invalid_request", status: fasthttp.StatusBadRequest, msg: err.Error()}
	}
	// 任务 ID 先定下来：输出目录要用它做隔离。
	id := newJobID()

	// output.dir 是 output_dir 之下的**相对**子路径，由调用方指定结果落点。
	//
	// 只接受相对路径：让调用方写绝对路径等于要求它知道服务端 output_dir 配成了
	// 什么，那是服务端自己的布局，不该成为接口契约的一部分。
	//
	// 未指定时用任务 ID 作子目录，每个任务独占一个目录。不隔离的话，所有 dir
	// 输出的任务都往同一个目录写 "output.pdf"，而 DirSink 默认不允许覆盖：
	// 第二个任务（哪怕顺序执行）必然以"输出文件已存在"失败。
	//
	// 显式指定了就不加任务 ID —— 调用方写了什么就落在什么位置。代价是并发
	// 任务若写同一目录且同名文件会撞：后者失败，错误是明确的"文件已存在"，
	// 不是静默覆盖。要避免就在 dir 或 filename 里带上区分。
	relative := strings.TrimSpace(request.Output.Dir)
	if relative == "" {
		relative = id
	} else if err := validateRelativeSubdir(relative); err != nil {
		return nil, &requestError{kind: "invalid_request", status: fasthttp.StatusBadRequest,
			msg: "output.dir 非法: " + err.Error()}
	}
	request.Output.Dir = filepath.Join(s.cfg.OutputDir, relative)
	request.Output.MaxStreamBytes = s.cfg.MaxStreamBytes

	notifyTarget := ""
	var notifyEvents []string
	if request.Notify != nil && request.Notify.Target != "" {
		if _, ok := s.registry.Lookup(request.Notify.Target); !ok {
			// 未注册的目标名直接拒绝，而不是接受后在投递时才失败：
			// 调用方需要当场知道通知不会送达。
			return nil, &requestError{kind: "unknown_notify_target", status: fasthttp.StatusBadRequest,
				msg: fmt.Sprintf("未注册的通知目标 %q", request.Notify.Target)}
		}
		notifyTarget = request.Notify.Target
		notifyEvents = request.Notify.Events
		// 事件名在这里校验，而不是留着到投递时静默过滤。拼错的后果是该任务
		// 一条通知都收不到，而调用方能观察到的只有"没收到"——那是最难查的
		// 失败形态。事件集合随请求演进，旧的拼法今天合法不代表一直合法。
		if err := validateNotifyEvents(notifyEvents); err != nil {
			return nil, &requestError{kind: "invalid_request", status: fasthttp.StatusBadRequest,
				msg: "notify.events 非法: " + err.Error()}
		}
	}

	if err := s.checkRemoteTarget(&request.Output); err != nil {
		return nil, err
	}

	// 通道由服务端按格式判定，不接受调用方指定。
	lane := jobstore.LaneFast
	if convertersvc.IsHeavy(request.Input, request.Output.Format) {
		lane = jobstore.LaneHeavy
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	order := jobstore.OrderNormal
	if request.HighPriority {
		order = jobstore.OrderHigh
	}
	return &jobstore.Job{
		ID:    id,
		Lane:  lane,
		Order: order,
		From:  request.Input.Format,
		// 转换成功后会用实际落盘尺寸覆盖。URL 输入在这里只能是 0：
		// 内容要到 workers 手上才拉取，那时才知道有多大。
		InputBytes:   int64(len(request.Input.Bytes)),
		To:           request.Output.Format,
		Request:      raw,
		NotifyTarget: notifyTarget,
		NotifyEvents: notifyEvents,
		CreatedAt:    time.Now().UTC(),
	}, nil
}

// validateNotifyEvents 校验请求里的 notify.events。
//
// 接受 succeeded、failed 与通配符 *，与配置侧 notify_targets[].events 同一套
// 名字（notify.KnownEvent 是唯一的判定处）。空列表合法，表示不收窄、沿用目标
// 自己的订阅集合。
func validateNotifyEvents(events []string) error {
	for _, name := range events {
		if !notify.KnownEvent(name) {
			return fmt.Errorf("未知事件 %q，可用 %s / %s / %s",
				name, notify.EventSucceeded, notify.EventFailed, notify.EventWildcard)
		}
	}
	return nil
}

// validateRelativeSubdir 校验 output_dir 之下的相对子路径。
//
// 判据是"必须是干净的相对路径"：不是绝对路径、不含 `..` 段、不含 NUL 与
// 控制字符、长度受限。满足这些时 filepath.Join(root, rel) 落在 root 之内是
// 结构性成立的，不依赖逐个排除。
//
// 逐段检查 `..` 而不是对整个路径做一次 Rel 比较，是因为 Rel 的结果在
// Windows 上还受盘符与大小写影响，而"任何一段是 .. 就拒绝"这条规则在
// 两个平台上的行为完全一致。
func validateRelativeSubdir(rel string) error {
	if filepath.IsAbs(rel) {
		return fmt.Errorf("必须是相对路径，不能是绝对路径: %q", rel)
	}
	if strings.ContainsRune(rel, 0) {
		return fmt.Errorf("不能包含空字符")
	}
	for _, r := range rel {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("不能包含控制字符")
		}
	}
	if len(rel) > maxSubdirBytes {
		return fmt.Errorf("长度 %d 字节，超过 %d 上限", len(rel), maxSubdirBytes)
	}
	// 统一分隔符后再逐段判断，Windows 上传 a\b\..\c 也要挡住。
	normalized := strings.ReplaceAll(rel, `\`, "/")
	if normalized == "" {
		return fmt.Errorf("不能为空")
	}
	for _, seg := range strings.Split(normalized, "/") {
		if seg == ".." {
			return fmt.Errorf("不能包含 .. 段: %q", rel)
		}
	}
	return nil
}

// maxSubdirBytes 是 output.dir 的字节上限，给 output_dir 与文件名留余量。
const maxSubdirBytes = 400

// checkInputURL 在提交阶段就校验 URL 输入。
//
// 这不是对 transfer 里拨号校验的替代：那边才是权威的（校验解析后的 IP，防 DNS
// rebinding）。这里只是提前给出反馈——让调用方当场看到"这台服务不拉外部地址"，
// 而不是提交成功、轮询一圈才发现任务失败。
func (s *Server) checkInputURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return &requestError{kind: "invalid_request", status: fasthttp.StatusBadRequest, msg: "input.url 不合法: " + err.Error()}
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return &requestError{kind: "invalid_request", status: fasthttp.StatusBadRequest,
			msg: "input.url 只支持 http 与 https"}
	}
	host := parsed.Hostname()
	if host == "" {
		return &requestError{kind: "invalid_request", status: fasthttp.StatusBadRequest, msg: "input.url 缺少主机名"}
	}
	if s.allowlist == nil || s.allowlist.Empty() {
		return &requestError{kind: "url_input_disabled", status: fasthttp.StatusForbidden,
			msg: "本服务未启用 URL 输入（allow_input_url_hosts 为空），请改为上传内容"}
	}
	// 字面量 IP 走 AllowAddr，白名单条目是 CIDR 时它才认；主机名走 AllowedHost。
	// 分开是因为这两个方法的适用对象不同：对主机名做 CIDR 匹配没有意义，
	// 而 AllowedHost 对字面量 IP 一律返回 false——直接拿它判断会让
	// "allow 10.1.0.0/16" 这类受控内网配置在提交阶段就被误拒。
	if addr, err := netip.ParseAddr(host); err == nil {
		if err := s.allowlist.AllowAddr(addr); err != nil {
			return &requestError{kind: "url_not_allowed", status: fasthttp.StatusForbidden,
				msg: fmt.Sprintf("地址 %s 不可访问: %v", host, err)}
		}
		return nil
	}
	if !s.allowlist.AllowedHost(host) {
		return &requestError{kind: "url_not_allowed", status: fasthttp.StatusForbidden,
			msg: fmt.Sprintf("主机 %q 不在允许列表内", host)}
	}
	return nil
}

// handleGetJob 返回任务状态。
func (s *Server) handleGetJob(ctx *fasthttp.RequestCtx) {
	id, ok := pathParam(ctx, "id")
	if !ok {
		writeError(ctx, fasthttp.StatusNotFound, "not_found", "任务 ID 不合法")
		return
	}
	job, err := s.store.Get(id)
	if err != nil {
		s.log.Error("查询任务失败", "job", id, "err", err)
		writeError(ctx, fasthttp.StatusInternalServerError, "internal_error", "查询失败")
		return
	}
	if job == nil {
		writeError(ctx, fasthttp.StatusNotFound, "job_not_found", "任务不存在")
		return
	}
	writeJSON(ctx, fasthttp.StatusOK, jobView(job))
}

// handleCancelJob 取消仍在排队的任务。
func (s *Server) handleCancelJob(ctx *fasthttp.RequestCtx) {
	id, ok := pathParam(ctx, "id")
	if !ok {
		writeError(ctx, fasthttp.StatusNotFound, "not_found", "任务 ID 不合法")
		return
	}
	if err := s.store.Cancel(id); err != nil {
		if strings.Contains(err.Error(), "任务不存在") {
			writeError(ctx, fasthttp.StatusNotFound, "job_not_found", "任务不存在")
			return
		}
		// 运行中或已终结的任务取消不了，属于调用方的状态判断问题。
		writeError(ctx, fasthttp.StatusConflict, "not_cancellable", err.Error())
		return
	}
	writeJSON(ctx, fasthttp.StatusOK, map[string]any{"id": id, "state": jobstore.StateCancelled})
}

// pathParam 读取路径参数。
func pathParam(ctx *fasthttp.RequestCtx, name string) (string, bool) {
	value := ctx.UserValue(name)
	if value == nil {
		return "", false
	}
	text := fmt.Sprint(value)
	if text == "" {
		return "", false
	}
	return text, true
}

// jobView 是对外的任务视图。不直接吐 jobstore.Job：那条记录里有原始请求体，
// 里面可能含调用方上传的内容，不该在状态查询时原样回显。
func jobView(job *jobstore.Job) map[string]any {
	view := map[string]any{
		"id":         job.ID,
		"state":      job.State,
		"lane":       job.Lane,
		"to":         job.To,
		"created_at": job.CreatedAt,
		"attempt":    job.Attempt,
	}
	if !job.StartedAt.IsZero() {
		view["started_at"] = job.StartedAt
	}
	if !job.FinishedAt.IsZero() {
		view["finished_at"] = job.FinishedAt
	}
	if job.State.Terminal() {
		view["output"] = job.Output
		if job.Error != "" {
			view["error"] = job.Error
		}
	}
	return view
}

// requestError 携带 HTTP 状态与错误码的请求错误。
type requestError struct {
	kind   string
	status int
	msg    string
}

func (e *requestError) Error() string { return e.msg }

func statusForRequestError(err error) int {
	var reqErr *requestError
	if errors.As(err, &reqErr) {
		return reqErr.status
	}
	return fasthttp.StatusBadRequest
}

func codeForRequestError(err error) string {
	var reqErr *requestError
	if errors.As(err, &reqErr) {
		return reqErr.kind
	}
	return "invalid_request"
}

func writeJSON(ctx *fasthttp.RequestCtx, status int, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetContentType("application/json; charset=utf-8")
		ctx.SetBodyString(`{"code":"internal_error","message":"响应序列化失败"}`)
		return
	}
	ctx.SetStatusCode(status)
	ctx.SetContentType("application/json; charset=utf-8")
	ctx.SetBody(raw)
}

func writeError(ctx *fasthttp.RequestCtx, status int, code, message string) {
	writeJSON(ctx, status, map[string]string{"code": code, "message": message})
}

// jobSeq 为任务 ID 提供单调递增的来源。
//
// 用时间戳而不是随机 UUID：任务 ID 会出现在日志、通知回调和调用方的状态查询
// 里，排查时能直接看出大致提交时间。发现时间戳不前进时 +1 而不是放弃，
// 保证同一进程内的 ID 严格递增，排序结果才有意义。
var jobSeq atomic.Uint64

func newJobID() string {
	now := uint64(time.Now().UnixNano())
	for {
		prev := jobSeq.Load()
		next := now
		if next <= prev {
			next = prev + 1
		}
		if jobSeq.CompareAndSwap(prev, next) {
			return "job_" + strconv.FormatUint(next, 36)
		}
	}
}

// bodySizeLimit 把 int64 的上限转成 fasthttp 用的 int。
//
// 32 位构建下 int 是 32 位，直接强转会溢出成负数，配置里写了个大值反而让所有
// 请求都被拒。这里钳到当前平台的最大值。
func bodySizeLimit(limit int64) int {
	maxInt := int64(^uint(0) >> 1)
	if limit > maxInt {
		return int(maxInt)
	}
	return int(limit)
}

// Start 监听并服务，直到 ctx 取消或监听失败。
//
// 停止时先停止接收新请求，再等在途请求结束，最后由调用方停 runner——
// 顺序反过来会让正在转换的任务被硬生生掐断。
func (s *Server) Start(ctx context.Context) error {
	server := &fasthttp.Server{
		// 不设 Name：fasthttp 会把它当 Server 头发出去，暴露服务名没有收益。
		Handler:            s.Handler(),
		MaxRequestBodySize: bodySizeLimit(s.cfg.MaxUploadBytes),
		ReadTimeout:        30 * time.Second,
		WriteTimeout:       60 * time.Second,
		IdleTimeout:        120 * time.Second,
		// 不发默认 Server 头：暴露实现细节没有收益。
		NoDefaultServerHeader: true,
	}
	errCh := make(chan error, 1)
	go func() {
		s.log.Info("开始监听", "addr", s.cfg.Listen)
		if err := server.ListenAndServe(s.cfg.Listen); err != nil && !errors.Is(err, net.ErrClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	select {
	case <-ctx.Done():
		s.log.Info("开始优雅退出")
		// 给在途请求留出完成时间。
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := server.ShutdownWithContext(shutdownCtx); err != nil {
			// 超时说明还有长请求没收尾。进程本来就要退出，没有更温和的手段，
			// 记下来让部署侧知道这次是硬退。
			s.log.Error("优雅退出未完成，将强制退出", "err", err)
		}
		return <-errCh
	case err := <-errCh:
		return err
	}
}

// checkRemoteTarget 校验远程输出目标。
//
// 两条规则：目标名必须已注册（否则是拿它当任意主机上传的通道），子目录不能带
// ".." 或绝对路径（否则能写到配置范围之外）。
func (s *Server) checkRemoteTarget(out *convertersvc.Output) error {
	remoteKinds := map[string]bool{
		convertersvc.OutputFTP:    true,
		convertersvc.OutputS3:     true,
		convertersvc.OutputWebDAV: true,
		convertersvc.OutputSFTP:   true,
	}
	isRemote := remoteKinds[out.Kind]
	if !isRemote {
		// 本地落点不该带 remote：给了也没地方用，静默忽略会让调用方以为自己
		// 传了远程目标、产物其实落在了本地。
		if out.Remote.Target != "" || out.Remote.Path != "" {
			return &requestError{kind: "invalid_request", status: fasthttp.StatusBadRequest,
				msg: fmt.Sprintf("output.remote 只在 output.kind 为 ftp/s3/webdav/sftp 时有效，当前是 %s", out.Kind)}
		}
		return nil
	}
	if out.Remote.Target == "" {
		return &requestError{kind: "invalid_request", status: fasthttp.StatusBadRequest,
			msg: "output.remote.target 不能为空"}
	}
	if !s.remoteTargetRegistered(out.Kind, out.Remote.Target) {
		return &requestError{kind: "unknown_remote_target", status: fasthttp.StatusBadRequest,
			msg: fmt.Sprintf("未注册的 %s 目标 %q", out.Kind, out.Remote.Target)}
	}
	if path := out.Remote.Path; path != "" {
		if err := validateRemotePath(path); err != nil {
			return &requestError{kind: "invalid_request", status: fasthttp.StatusBadRequest,
				msg: "output.remote.path 非法: " + err.Error()}
		}
	}
	return nil
}

// validateRemotePath 校验远端目标下的相对子路径。
//
// 逐段判断 `..` 而不是对整串做 Contains：`a..b/c` 是合法路径名，含 `..` 的
// 子串却会被误杀。而 ".." 作为**一段**才是穿越——这一点上 Contains 与分段
// 判断的结论正好相反，误杀比漏判更常见（文件名里带两个点的到处都是）。
//
// 远端路径不套用 validateRelativeSubdir 的单组件限制：S3 的 prefix 天然就是
// 多段的（incoming/2026/08），限制成一段会让它没法用。分段穿越与绝对路径
// 才是要挡的。
func validateRemotePath(path string) error {
	if strings.ContainsRune(path, 0) {
		return fmt.Errorf("不能包含空字符")
	}
	for _, r := range path {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("不能包含控制字符")
		}
	}
	if len(path) > maxRemotePathBytes {
		return fmt.Errorf("长度 %d 字节，超过 %d 上限", len(path), maxRemotePathBytes)
	}
	// 统一分隔符后逐段判断，两个平台上行为一致。
	normalized := strings.ReplaceAll(path, `\`, "/")
	if strings.HasPrefix(normalized, "/") {
		return fmt.Errorf("不能是绝对路径: %q", path)
	}
	for _, seg := range strings.Split(normalized, "/") {
		if seg == ".." {
			return fmt.Errorf("不能包含 .. 段: %q", path)
		}
	}
	return nil
}

// maxRemotePathBytes 是 output.remote.path 的字节上限。
const maxRemotePathBytes = 500

// remoteTargetRegistered 报告目标名是否在对应注册表里。
func (s *Server) remoteTargetRegistered(kind, name string) bool {
	switch kind {
	case convertersvc.OutputFTP:
		_, ok := s.ftp[name]
		return ok
	case convertersvc.OutputS3:
		_, ok := s.s3[name]
		return ok
	case convertersvc.OutputWebDAV:
		_, ok := s.webdav[name]
		return ok
	case convertersvc.OutputSFTP:
		_, ok := s.sftp[name]
		return ok
	}
	return false
}
