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
	// apiKey 是访问 /v1/* 的令牌，按字节保存以配合定长比较。
	apiKey []byte
}

// NewServer 构造 HTTP 层。
func NewServer(cfg *Config, store *jobstore.Store, registry *notify.Registry, list *allowlist.List, targets *RemoteTargets, log *slog.Logger) *Server {
	if targets == nil {
		targets = &RemoteTargets{}
	}
	s := &Server{cfg: cfg, store: store, registry: registry, allowlist: list,
		ftp: targets.FTP, s3: targets.S3, webdav: targets.WebDAV,
		sftp: targets.SFTP, log: log}
	s.apiKey = []byte(cfg.APIKey)
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
	r.POST("/v1/jobs/{id}/cancel", s.requireAPIKey(s.handleCancelJob))
	// /metrics 要令牌：它暴露转换量与格式分布，属于运营数据，而默认监听
	// 所有网卡。Prometheus 支持在 scrape_configs 里配 bearer_token_file，
	// 采集端多一份配置，好过把数据敞着。
	r.GET("/metrics", s.requireAPIKey(s.handleMetrics))
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
	if err := json.Unmarshal(body, &request); err != nil {
		writeError(ctx, fasthttp.StatusBadRequest, "invalid_request", "请求体不是合法 JSON: "+err.Error())
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
	// 任务 ID 先定下来：输出目录要用它做隔离。
	id := newJobID()

	// 输出根目录由服务端决定：让调用方传任意路径等于让它往任何位置写文件。
	// 只允许在配置的 output_dir 之下。
	outputRoot := request.Output.Dir
	if outputRoot == "" {
		outputRoot = s.cfg.OutputDir
	} else if !isUnderRoot(s.cfg.OutputDir, outputRoot) {
		return nil, &requestError{kind: "invalid_request", status: fasthttp.StatusBadRequest,
			msg: "output.dir 必须位于服务配置的 output_dir 之下"}
	}
	// 每个任务独占一个子目录。
	//
	// 不隔离的话，所有 dir 输出的任务都往同一个目录写 "output.pdf"，
	// 而 DirSink 默认不允许覆盖：第二个任务（哪怕是顺序执行的）必然以
	// "输出文件已存在" 失败。并发时更糟，两个任务会互相抢同一个文件名。
	request.Output.Dir = filepath.Join(outputRoot, id)
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

// isUnderRoot 判断 path 是否位于 root 之下。
//
// 用 filepath.Rel 而不是前缀比较：前缀比较会被 "/data/../etc" 与 "/data-other"
// 这类输入绕过，而 Clean 后的相对路径不会。root 为空时视为不限制，交由上层
// 保证 root 已被配置。
func isUnderRoot(root, path string) bool {
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

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
	type spec struct {
		kind      string
		target    string
		dir       string
		targetKey string
		dirKey    string
	}
	specs := []spec{
		{convertersvc.OutputFTP, out.FTPTarget, out.FTPDir, "ftp_target", "ftp_dir"},
		{convertersvc.OutputS3, out.S3Target, out.S3Prefix, "s3_target", "s3_prefix"},
		{convertersvc.OutputWebDAV, out.WebDAVTarget, out.WebDAVDir, "webdav_target", "webdav_dir"},
		{convertersvc.OutputSFTP, out.SFTPTarget, out.SFTPDir, "sftp_target", "sftp_dir"},
	}
	for _, sp := range specs {
		isRemote := sp.kind != convertersvc.OutputStream && sp.kind != convertersvc.OutputDir
		if out.Kind != sp.kind {
			if sp.target != "" {
				return &requestError{kind: "invalid_request", status: fasthttp.StatusBadRequest,
					msg: fmt.Sprintf("output.%s 只在 output.kind 为 %s 时有效", sp.targetKey, sp.kind)}
			}
			continue
		}
		if !isRemote {
			continue
		}
		if sp.target == "" {
			return &requestError{kind: "invalid_request", status: fasthttp.StatusBadRequest,
				msg: fmt.Sprintf("output.%s 不能为空", sp.targetKey)}
		}
		if !s.remoteTargetRegistered(sp.kind, sp.target) {
			return &requestError{kind: "unknown_remote_target", status: fasthttp.StatusBadRequest,
				msg: fmt.Sprintf("未注册的 %s 目标 %q", sp.kind, sp.target)}
		}
		if dir := sp.dir; dir != "" {
			if strings.Contains(dir, "..") || strings.HasPrefix(dir, "/") ||
				strings.ContainsAny(dir, "\\") || strings.ContainsRune(dir, 0) {
				return &requestError{kind: "invalid_request", status: fasthttp.StatusBadRequest,
					msg: fmt.Sprintf("output.%s 不能是绝对路径或包含 ..", sp.dirKey)}
			}
		}
	}
	return nil
}

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
