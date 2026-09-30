package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	json "github.com/goccy/go-json"

	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttputil"

	"github.com/zc310/ofd/internal/allowlist"
	"github.com/zc310/ofd/internal/convertersvc"
	"github.com/zc310/ofd/internal/jobstore"
	"github.com/zc310/ofd/internal/notify"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func newTestServer(t *testing.T) (*Server, *jobstore.Store, *Config) {
	t.Helper()
	s, store, cfg, _ := newTestServerWithList(t, nil)
	return s, store, cfg
}

// newTestServerWithList 允许指定 URL 输入白名单。
// testAPIKey 是测试用的固定令牌。用常量而不是随机值：认证用例要断言
// "错令牌被拒"，随机会让"对令牌通过"和"错令牌被拒"用同一个值而失去意义。
const testAPIKey = "test-token-0123456789abcdef"

func newTestServerWithList(t *testing.T, entries []string) (*Server, *jobstore.Store, *Config, *allowlist.List) {
	t.Helper()
	dir := t.TempDir()
	store, err := jobstore.Open(filepath.Join(dir, "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cfg := &Config{
		OutputDir:      filepath.Join(dir, "out"),
		APIKey:         testAPIKey,
		MaxUploadBytes: 1 << 20,
		MaxStreamBytes: 1 << 20,
		NotifyTargets:  map[string]NotifyTarget{"erp": {URL: "https://erp.example.com/hook"}},
		FastWorkers:    1,
		HeavyWorkers:   1,
		NotifyWorkers:  1,
		Retention:      Duration(time.Hour),
	}
	registry := notify.NewRegistry(map[string]notify.Target{
		"erp": {URL: "https://erp.example.com/hook"},
	})
	list, err := allowlist.Parse(entries)
	if err != nil {
		t.Fatal(err)
	}
	return NewServer(cfg, store, registry, list, &RemoteTargets{}, convertersvc.New(cfg.TempDir, list), testLogger()), store, cfg, list
}

// newPipeServer 起一个内存监听的服务，返回 client 与地址。
func newPipeServer(t *testing.T, handler fasthttp.RequestHandler) *fasthttp.Client {
	t.Helper()
	ln := fasthttputil.NewInmemoryListener()
	server := &fasthttp.Server{Handler: handler}
	go func() { _ = server.Serve(ln) }()
	t.Cleanup(func() {
		_ = server.Shutdown()
	})
	return &fasthttp.Client{
		Dial: func(addr string) (net.Conn, error) { return ln.Dial() },
	}
}

type response struct {
	status int
	body   string
	// contentLength 与 body 实际长度可能不一致：响应被截断时能测出来。
	contentLength int
	// jobID 是 X-OFD-Job-Id 响应头，只有同步转换会带。
	jobID string
	// contentType 是响应的 Content-Type。
	contentType string
	// headers 保留全部响应头，供需要断言单个头的用例使用。
	headers map[string]string
}

// collectHeaders 收集响应头。
func collectHeaders(h *fasthttp.ResponseHeader) map[string]string {
	out := make(map[string]string, 8)
	h.VisitAll(func(key, value []byte) {
		out[string(key)] = string(value)
	})
	return out
}

// header 取响应头，不区分大小写（HTTP 头名本身不敏感）。
func (r response) header(name string) string {
	for k, v := range r.headers {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}

func do(t *testing.T, client *fasthttp.Client, method, path, body string) response {
	t.Helper()
	return doAuth(t, client, method, path, body, testAPIKey)
}

// doAuth 发起带 Bearer 令牌的请求。
//
// 绝大多数用例只关心业务行为，令牌在 do 里统一补上；只有验证认证本身的
// 用例才用 doAuth 传空串或错值——否则每个用例都得先处理 401，认证
// 相关的断言也就淹没在噪音里。
func doAuth(t *testing.T, client *fasthttp.Client, method, path, body, token string) response {
	t.Helper()
	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)
	req.Header.SetMethod(method)
	// Host 必填：fasthttp 的 client 不会替请求补上。
	req.Header.SetHost("ofd-server.test")
	req.SetRequestURI(path)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.SetBodyString(body)
	}
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)
	if err := client.Do(req, resp); err != nil {
		t.Fatalf("%s %s 失败: %v", method, path, err)
	}
	return response{
		status:        resp.StatusCode(),
		body:          string(resp.Body()),
		contentLength: resp.Header.ContentLength(),
		jobID:         string(resp.Header.Peek(jobIDHeader)),
		contentType:   string(resp.Header.ContentType()),
		headers:       collectHeaders(&resp.Header),
	}
}

func (r response) decode(t *testing.T) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(r.body), &out); err != nil {
		t.Fatalf("响应不是合法 JSON: %q", r.body)
	}
	return out
}

func ofdPayload(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"sample.ofd", "actions.ofd", "link.ofd"} {
		if data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", name)); err == nil && len(data) > 0 {
			return base64.StdEncoding.EncodeToString(data)
		}
	}
	t.Skip("仓库里没有可用的 OFD 样本")
	return ""
}

// submitBody 构造异步提交（dir 输出）的请求体。
//
// 默认用 dir 而不是 stream：stream 是同步语义，响应体直接是产物字节，没有
// 202 可轮询。需要测 stream 的用例用 submitInlineBody。
func submitBody(t *testing.T, to string) string {
	t.Helper()
	return submitBodyWith(t, to, "dir")
}

// submitInlineBody 构造同步提交（stream 输出）的请求体。
func submitInlineBody(t *testing.T, to string) string {
	t.Helper()
	return submitBodyWith(t, to, "stream")
}

func submitBodyWith(t *testing.T, to, kind string) string {
	t.Helper()
	body := map[string]any{
		"input":  map[string]any{"kind": "upload", "file_name": "a.ofd", "bytes": ofdPayload(t)},
		"output": map[string]any{"kind": kind, "format": to},
	}
	raw, _ := json.Marshal(body)
	return string(raw)
}

func TestSubmitAndQuery(t *testing.T) {
	s, store, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())

	got := do(t, client, fasthttp.MethodPost, "/v1/convert", submitBody(t, "pdf"))
	if got.status != fasthttp.StatusAccepted {
		t.Fatalf("提交应返回 202，实际 %d: %s", got.status, got.body)
	}
	payload := got.decode(t)
	id, _ := payload["id"].(string)
	if id == "" {
		t.Fatalf("响应里没有任务 ID: %s", got.body)
	}
	if payload["state"] != string(jobstore.StateQueued) {
		t.Errorf("初始状态 = %v", payload["state"])
	}
	if payload["status_url"] != "/v1/jobs/"+id {
		t.Errorf("status_url = %v", payload["status_url"])
	}
	// 任务确实入库了。
	if job, err := store.Get(id); err != nil || job == nil {
		t.Errorf("任务未入库: %v", err)
	}

	// 查询。
	got = do(t, client, fasthttp.MethodGet, "/v1/jobs/"+id, "")
	if got.status != fasthttp.StatusOK {
		t.Fatalf("查询应返回 200，实际 %d", got.status)
	}
	view := got.decode(t)
	if view["id"] != id {
		t.Errorf("id = %v", view["id"])
	}
	// 状态视图不该回显原始请求体。
	if _, leaked := view["request"]; leaked {
		t.Error("状态响应不应包含原始请求体")
	}
}

func TestSubmitValidation(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())

	cases := []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{"空请求体", "", fasthttp.StatusBadRequest, "invalid_request"},
		{"非 JSON", "not json", fasthttp.StatusBadRequest, "invalid_request"},
		{"未知输入种类", `{"input":{"kind":"s3","url":"x"},"output":{"format":"pdf"}}`, fasthttp.StatusBadRequest, "invalid_request"},
		{"URL 缺地址", `{"input":{"kind":"url"},"output":{"format":"pdf"}}`, fasthttp.StatusBadRequest, "invalid_request"},
		{"上传内容为空", `{"input":{"kind":"upload","file_name":"a.ofd"},"output":{"format":"pdf"}}`, fasthttp.StatusBadRequest, "invalid_request"},
		{"缺输出格式", `{"input":{"kind":"upload","file_name":"a.ofd","bytes":"eA=="}}`, fasthttp.StatusBadRequest, "invalid_request"},
		{"未注册通知目标", `{"input":{"kind":"upload","file_name":"a.ofd","bytes":"eA=="},"output":{"format":"pdf"},"notify":{"target":"nope"}}`, fasthttp.StatusBadRequest, "unknown_notify_target"},
		// 输出目录必须由服务端决定，不能让调用方指定任意路径。
		{"越界的输出目录", `{"input":{"kind":"upload","file_name":"a.ofd","bytes":"eA=="},"output":{"format":"pdf","kind":"dir","dir":"/etc"}}`, fasthttp.StatusBadRequest, "invalid_request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := do(t, client, fasthttp.MethodPost, "/v1/convert", tc.body)
			if got.status != tc.status {
				t.Fatalf("状态 = %d，期望 %d: %s", got.status, tc.status, got.body)
			}
			if code := got.decode(t)["code"]; code != tc.code {
				t.Errorf("code = %v，期望 %s", code, tc.code)
			}
		})
	}
	// output_dir 之下的相对子目录是允许的。
	body := `{"input":{"kind":"upload","file_name":"a.ofd","bytes":"` + ofdPayload(t) +
		`"},"output":{"format":"pdf","kind":"dir","dir":"a/b/c"}}`
	got := do(t, client, fasthttp.MethodPost, "/v1/convert", body)
	if got.status != fasthttp.StatusAccepted {
		t.Errorf("output_dir 之下的相对子目录应被接受，实际 %d: %s", got.status, got.body)
	}
}

func TestLaneAssignment(t *testing.T) {
	s, store, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())

	// 纯 OFD→PDF 走快通道。
	got := do(t, client, fasthttp.MethodPost, "/v1/convert", submitBody(t, "pdf"))
	fast := got.decode(t)["lane"]

	// HTML 输出要拉起 Chrome，走重通道。内容仍用 OFD，
	// 格式判定只看 input/output 组合，不看内容。
	body := `{"input":{"kind":"upload","file_name":"a.ofd","bytes":"` + ofdPayload(t) + `"},"output":{"kind":"dir","format":"html"}}`
	got = do(t, client, fasthttp.MethodPost, "/v1/convert", body)
	heavy := got.decode(t)["lane"]

	if fast != "fast" {
		t.Errorf("OFD→PDF 应走 fast，实际 %v", fast)
	}
	if heavy != "heavy" {
		t.Errorf("→HTML 应走 heavy，实际 %v", heavy)
	}
	// URL 输入走重通道：耗时由对端决定，不该占住快速通道。
	s2, _, _, _ := newTestServerWithList(t, []string{"example.com"})
	client2 := newPipeServer(t, s2.Handler())
	urlBody := `{"input":{"kind":"url","url":"https://example.com/a.ofd","format":"ofd"},"output":{"kind":"dir","format":"pdf"}}`
	got = do(t, client2, fasthttp.MethodPost, "/v1/convert", urlBody)
	if lane := got.decode(t)["lane"]; lane != "heavy" {
		t.Errorf("URL 输入应走 heavy，实际 %v", lane)
	}

	// 通道是服务端按格式判定的。请求里的 lane 会被拒绝而不是被忽略：
	// 静默忽略会让调用方以为自己拿到了 heavy 优先级、实际走了 fast，
	// 这种"看起来生效了"的偏差比直接报错难查得多。
	body = `{"input":{"kind":"upload","file_name":"a.ofd","bytes":"` + ofdPayload(t) +
		`"},"output":{"kind":"dir","format":"pdf"},"lane":"heavy"}`
	got = do(t, client, fasthttp.MethodPost, "/v1/convert", body)
	if got.status != fasthttp.StatusBadRequest {
		t.Errorf("请求里带 lane 应被拒绝，实际 %d: %s", got.status, got.body)
	}
	_ = store
}

func TestCancelJob(t *testing.T) {
	s, store, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())

	// 取走一个任务让它处于运行中，取消应当被拒。
	got := do(t, client, fasthttp.MethodPost, "/v1/convert", submitBody(t, "pdf"))
	id := got.decode(t)["id"].(string)
	if _, err := store.Claim(jobstore.LaneFast); err != nil {
		t.Fatal(err)
	}
	got = do(t, client, fasthttp.MethodPost, "/v1/jobs/"+id+"/cancel", "")
	if got.status != fasthttp.StatusConflict {
		t.Errorf("运行中任务取消应返回 409，实际 %d: %s", got.status, got.body)
	}
	if got.decode(t)["code"] != "not_cancellable" {
		t.Errorf("code = %v", got.decode(t)["code"])
	}

	// 排队中的可以取消。
	got = do(t, client, fasthttp.MethodPost, "/v1/convert", submitBody(t, "pdf"))
	queued := got.decode(t)["id"].(string)
	got = do(t, client, fasthttp.MethodPost, "/v1/jobs/"+queued+"/cancel", "")
	if got.status != fasthttp.StatusOK {
		t.Fatalf("取消排队任务应返回 200，实际 %d: %s", got.status, got.body)
	}
	if got.decode(t)["state"] != string(jobstore.StateCancelled) {
		t.Errorf("state = %v", got.decode(t)["state"])
	}
	// 重复取消应报冲突。
	if got := do(t, client, fasthttp.MethodPost, "/v1/jobs/"+queued+"/cancel", ""); got.status != fasthttp.StatusConflict {
		t.Errorf("重复取消应返回 409，实际 %d", got.status)
	}
	// 不存在的任务。
	got = do(t, client, fasthttp.MethodPost, "/v1/jobs/nope/cancel", "")
	if got.status != fasthttp.StatusNotFound {
		t.Errorf("不存在的任务应返回 404，实际 %d", got.status)
	}
}

func TestNotFoundAndMethodNotAllowed(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())

	got := do(t, client, fasthttp.MethodGet, "/nope", "")
	if got.status != fasthttp.StatusNotFound {
		t.Errorf("未知路径应返回 404，实际 %d", got.status)
	}
	if got.decode(t)["code"] != "not_found" {
		t.Errorf("code = %v", got.decode(t)["code"])
	}
	// 方法不匹配。
	got = do(t, client, fasthttp.MethodGet, "/v1/convert", "")
	if got.status != fasthttp.StatusMethodNotAllowed {
		t.Errorf("GET /v1/convert 应返回 405，实际 %d", got.status)
	}
	got = do(t, client, fasthttp.MethodDelete, "/v1/jobs/abc", "")
	if got.status != fasthttp.StatusMethodNotAllowed {
		t.Errorf("DELETE 任务应返回 405，实际 %d", got.status)
	}
	// 尾斜杠不该被重定向：POST 重定向会丢请求体。
	got = do(t, client, fasthttp.MethodPost, "/v1/convert/", submitBody(t, "pdf"))
	if got.status == fasthttp.StatusMovedPermanently || got.status == fasthttp.StatusTemporaryRedirect {
		t.Errorf("尾斜杠不应触发重定向，实际 %d", got.status)
	}
}

func TestHealthAndReady(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())

	for _, path := range []string{"/healthz", "/readyz"} {
		got := do(t, client, fasthttp.MethodGet, path, "")
		if got.status != fasthttp.StatusOK {
			t.Errorf("%s = %d: %s", path, got.status, got.body)
		}
	}
	// 未注入就绪函数时按就绪处理。
	if got := do(t, client, fasthttp.MethodGet, "/readyz", ""); got.status != fasthttp.StatusOK {
		t.Errorf("readyz = %d", got.status)
	}
	// 注入为不就绪时应返回 503。
	s.SetReadyFunc(func() bool { return false })
	if got := do(t, client, fasthttp.MethodGet, "/readyz", ""); got.status != fasthttp.StatusServiceUnavailable {
		t.Errorf("未就绪时 readyz 应为 503，实际 %d", got.status)
	}
}

func TestBodySizeLimit(t *testing.T) {
	s, _, cfg := newTestServer(t)
	cfg.MaxUploadBytes = 100
	client := newPipeServer(t, s.Handler())

	big := `{"input":{"kind":"upload","file_name":"a.ofd","bytes":"` + strings.Repeat("A", 500) + `"}}`
	got := do(t, client, fasthttp.MethodPost, "/v1/convert", big)
	if got.status != fasthttp.StatusRequestEntityTooLarge {
		t.Errorf("超限请求应返回 413，实际 %d: %s", got.status, got.body)
	}
	if got.decode(t)["code"] != "payload_too_large" {
		t.Errorf("code = %v", got.decode(t)["code"])
	}
	// 超限的请求不应留下任务。
	counts, _ := storeCount(s)
	total := 0
	for _, n := range counts {
		total += n
	}
	if total != 0 {
		t.Errorf("超限请求不应入队，实际 %d 个任务", total)
	}
}

// 任务总数，用于确认被拒绝的请求没有留下记录。
func storeCount(s *Server) (map[jobstore.State]int, error) {
	return s.store.Count()
}

// TestJobIDShape 锁住任务 ID 的形状：不带前缀、base36、长度稳定。
//
// 长度稳定是 jobDirLevels 切目录的前提——切出固定宽度才能让 output_dir 下的
// 扇出保持均匀。长度一变，尾部位数跟着变，目录分布就跟着漂。
func TestJobIDShape(t *testing.T) {
	lengths := map[int]int{}
	for i := 0; i < 500; i++ {
		id := newJobID()
		if strings.HasPrefix(id, "job_") {
			t.Fatalf("任务 ID 不该带 job_ 前缀: %s", id)
		}
		for _, r := range id {
			if !strings.ContainsRune("0123456789abcdefghijklmnopqrstuvwxyz", r) {
				t.Fatalf("任务 ID 含非 base36 字符 %q: %s", r, id)
			}
		}
		lengths[len(id)]++
	}
	// 当前量级固定 12 位（36^12 约 4.7e18，UnixNano 约 1.8e18）。真要变了，
	// 这条断言会失败并指出需要重新评估目录切分——那是应该被看到的变更。
	if len(lengths) != 1 {
		t.Fatalf("任务 ID 长度不稳定: %v", lengths)
	}
	if lengths[12] != 500 {
		t.Errorf("任务 ID 长度 = %v，期望 500 个 12 位", lengths)
	}
}

// TestJobDirLevelsFanout 验证目录切分真的能扇开。
//
// 保护的是"取尾部而不是头部"这个决定：头部是时间戳高位，20 万个连续任务里
// 几乎不变，拿它分目录等于所有任务落进同一处。尾部逐任务变化，应铺满 36^2。
func TestJobDirLevelsFanout(t *testing.T) {
	shards := map[string]bool{}
	const n = 20000
	for i := 0; i < n; i++ {
		levels := jobDirLevels(newJobID())
		if len(levels) != 2 {
			t.Fatalf("目录层级数 = %d，期望 2（%v）", len(levels), levels)
		}
		// 叶子目录名应能拼回完整 ID——否则产物目录和 ID 对不上，排查时
		// 拿 ID 找不到目录。
		if levels[0]+levels[1] == "" {
			t.Fatal("切分后丢字符")
		}
		shards[levels[0]] = true
	}
	// 20000 个任务至少铺开几百个一级目录。头部切法在这里会给出 1。
	if len(shards) < 200 {
		t.Errorf("%d 个任务只铺开 %d 个一级目录，扇出异常", n, len(shards))
	}
}

// TestJobDirLevelsShortID ID 异常短时不切出空目录名。
func TestJobDirLevelsShortID(t *testing.T) {
	for _, id := range []string{"", "a", "ab"} {
		levels := jobDirLevels(id)
		for _, l := range levels {
			if l == "" {
				t.Errorf("ID %q 切出空目录名: %v", id, levels)
			}
		}
		if len(levels) == 2 {
			t.Errorf("ID %q 不该被切成两级: %v", id, levels)
		}
	}
}

func TestJobIDMonotonic(t *testing.T) {
	seen := map[string]bool{}
	previous := ""
	for i := 0; i < 200; i++ {
		id := newJobID()
		if seen[id] {
			t.Fatalf("任务 ID 重复: %s", id)
		}
		seen[id] = true
		if previous != "" && id <= previous {
			t.Fatalf("任务 ID 应单调递增: %s 不大于 %s", id, previous)
		}
		previous = id
	}
}

// 默认值是接口契约的一部分：改了端口号，部署侧的探测脚本就要跟着改。
func TestConfigDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	// 只给必填项，其余走默认值。
	if err := os.WriteFile(path, []byte(`{"db_path":"/tmp/j.db","output_dir":"/tmp/o","api_key":"k"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	// 用 any 装值是刻意的：几处默认值类型不同（time.Duration、int、string、
	// 布尔），这样一张表能覆盖全部。Duration 要显式转成 time.Duration，
	// 否则跨类型比较永远不等。
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"默认端口", cfg.Listen, ":9705"},
		{"默认临时目录非空", cfg.TempDir != "", true},
		{"默认日志级别", cfg.LogLevel, "info"},
		{"默认上传上限", cfg.MaxUploadBytes, int64(64 << 20)},
		{"默认输出上限", cfg.MaxStreamBytes, int64(64 << 20)},
		{"默认快通道并发", cfg.FastWorkers, 4},
		// 重通道默认串行：每个 heavy 任务都会拉起外部进程。
		{"默认重通道并发", cfg.HeavyWorkers, 1},
		{"默认通知并发", cfg.NotifyWorkers, 4},
		{"默认保留时长", cfg.Retention.Duration(), 7 * 24 * time.Hour},
		{"默认 URL 超时", cfg.URLTimeout.Duration(), 30 * time.Second},
		// 没配重试次数就不自动重试：转换大多��定性的。
		{"默认重试次数", cfg.MaxJobAttempts, 0},
	}
	for _, tc := range checks {
		if tc.got != tc.want {
			t.Errorf("%s = %v，期望 %v", tc.name, tc.got, tc.want)
		}
	}
}

func TestConfigValidation(t *testing.T) {
	base := map[string]any{
		"db_path":      "/tmp/jobs.db",
		"output_dir":   "/tmp/out",
		"listen":       ":0",
		"temp_dir":     "/tmp/tmp",
		"log_level":    "info",
		"fast_workers": 2,
	}
	write := func(t *testing.T, override map[string]any) string {
		t.Helper()
		merged := map[string]any{}
		for k, v := range base {
			merged[k] = v
		}
		for k, v := range override {
			merged[k] = v
		}
		merged["api_key"] = testAPIKey
		raw, _ := json.Marshal(merged)
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	if _, err := LoadConfig(write(t, nil)); err != nil {
		t.Fatalf("基础配置应有效: %v", err)
	}
	cases := []struct {
		name     string
		override map[string]any
		wantErr  bool
	}{
		{"缺 db_path", map[string]any{"db_path": ""}, true},
		{"缺 output_dir", map[string]any{"output_dir": ""}, true},
		{"通知目标缺 url", map[string]any{"notify_targets": map[string]any{"a": map[string]any{"secret": "s"}}}, true},
		{"通知用明文 http", map[string]any{"notify_targets": map[string]any{"a": map[string]any{"url": "http://evil.example.com"}}}, true},
		{"通知用 https", map[string]any{"notify_targets": map[string]any{"a": map[string]any{"url": "https://ok.example.com"}}}, false},
		{"通知用回环 http", map[string]any{"notify_targets": map[string]any{"a": map[string]any{"url": "http://127.0.0.1:9000"}}}, false},
		// 事件名拼错会让该目标一条通知都不收，唯一能观察到的现象是"没收到"。
		{"通知 events 拼错", map[string]any{"notify_targets": map[string]any{"a": map[string]any{
			"url": "https://ok.example.com", "events": []string{"succeded"}}}}, true},
		{"通知 events 大小写不对", map[string]any{"notify_targets": map[string]any{"a": map[string]any{
			"url": "https://ok.example.com", "events": []string{"Succeeded"}}}}, true},
		{"通知 events 合法", map[string]any{"notify_targets": map[string]any{"a": map[string]any{
			"url": "https://ok.example.com", "events": []string{"succeeded", "failed", "*"}}}}, false},
		{"通知 events 留空", map[string]any{"notify_targets": map[string]any{"a": map[string]any{
			"url": "https://ok.example.com"}}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadConfig(write(t, tc.override))
			if tc.wantErr && err == nil {
				t.Error("应报错")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("不应报错: %v", err)
			}
		})
	}
}

// 时长字段两种写法都要收：配置文件是人读的，"168h" 必须是 168 小时，
// 而不是被当成 168 纳秒，或直接解析失败。
func TestConfigDurationParsing(t *testing.T) {
	const week = 7 * 24 * time.Hour
	type durationCase struct {
		field string
		raw   any
		want  time.Duration
	}
	cases := []durationCase{
		{"retention", "168h", 168 * time.Hour},
		{"retention", "1h30m", 90 * time.Minute},
		{"retention", "45s", 45 * time.Second},
		// 裸数字按纳秒，与 time.Duration 本身一致。
		{"retention", int64(time.Hour), time.Hour},
		{"job_timeout", "30s", 30 * time.Second},
		{"job_timeout", int64(2 * time.Minute), 2 * time.Minute},
		{"url_timeout", "5m", 5 * time.Minute},
		{"url_timeout", "1m30s", 90 * time.Second},
		// 空值与缺省一致：走默认值。0 与"未设"无法区分，而保留 0 也没有意义
		//（retention=0 会让终态任务刚写完就被删掉）。
		{"retention", "", week},
		{"retention", nil, week},
	}
	for _, tc := range cases {
		t.Run(tc.field+"="+fmt.Sprint(tc.raw), func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{
				"db_path": "/tmp/j.db", "output_dir": "/tmp/o",
				"api_key": testAPIKey, tc.field: tc.raw,
			})
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			var got time.Duration
			switch tc.field {
			case "retention":
				got = cfg.Retention.Duration()
			case "job_timeout":
				got = cfg.JobTimeout.Duration()
			case "url_timeout":
				got = cfg.URLTimeout.Duration()
			}
			if got != tc.want {
				t.Errorf("%s = %v，期望 %v", tc.field, got, tc.want)
			}
		})
	}

	// 非法写法必须报错，而不是悄悄退回默认值——那会表现为"超时没生效"。
	for _, bad := range []any{"abc", "10x", true, []int{1}, map[string]any{}} {
		raw, _ := json.Marshal(map[string]any{
			"db_path": "/tmp/j.db", "output_dir": "/tmp/o", "retention": bad,
		})
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(path); err == nil {
			t.Errorf("retention = %v 应报错", bad)
		}
	}
}

// 配置里的拼写错误必须报错，否则表现是"服务起来了但用了默认值"。
func TestConfigRejectsUnknownField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"db_path":"/tmp/j.db","output_dir":"/tmp/o","worker":4}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Error("未知字段应报错")
	}
}

func TestConfigEnvOverride(t *testing.T) {
	t.Setenv("OFD_SERVER_LISTEN", ":9999")
	t.Setenv("OFD_SERVER_DB", "/tmp/env.db")
	t.Setenv("OFD_SERVER_OUTPUT_DIR", "/tmp/envout")
	t.Setenv("OFD_SERVER_API_KEY", "env-token")
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"db_path":"/tmp/file.db","output_dir":"/tmp/fileout","listen":":1","api_key":"file-token"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":9999" || cfg.DBPath != "/tmp/env.db" || cfg.OutputDir != "/tmp/envout" {
		t.Errorf("环境变量未覆盖: %+v", cfg)
	}
	// 令牌同样要能被环境变量覆盖：容器部署靠它传密钥，不进配置文件。
	if cfg.APIKey != "env-token" {
		t.Errorf("APIKey = %q，期望被环境变量覆盖为 env-token", cfg.APIKey)
	}
}

func TestParseArgs(t *testing.T) {
	if _, err := parseArgs([]string{"--bogus"}); err == nil {
		t.Error("未知参数应报错")
	}
	if _, err := parseArgs([]string{"--config"}); err == nil {
		t.Error("缺参数应报错")
	}
	opts, err := parseArgs([]string{"-c", "x.json", "--check-config"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.configPath != "x.json" || !opts.checkConfig {
		t.Errorf("解析结果 = %+v", opts)
	}
	if opts, err := parseArgs([]string{"--help"}); err != nil || !opts.help {
		t.Errorf("--help = %+v, %v", opts, err)
	}
}

func TestStartAndShutdown(t *testing.T) {
	s, store, cfg, list := newTestServerWithList(t, nil)
	cfg.Listen = "127.0.0.1:0"
	s.SetReadyFunc(func() bool { return true })

	// 直接验证 fasthttp 服务器能起能停，不去猜端口。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	cfg.Listen = addr
	s2 := NewServer(cfg, store, s.registry, list, &RemoteTargets{}, s.convert, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s2.Start(ctx) }()

	// 等端口可用。
	deadline := time.Now().Add(5 * time.Second)
	var client *http.Client
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	client = &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("健康检查失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz = %d", resp.StatusCode)
	}
	// 不该暴露 Server 头。
	if got := resp.Header.Get("Server"); got != "" {
		t.Errorf("不应返回 Server 头，实际 %q", got)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("退出应无错误: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Error("优雅退出超时")
	}
}

// TestAPIKeyRequired 守住"接口不能匿名开放"这条不变量。
//
// 这组断言的价值不在于自己能不能跑通，而在于把移除认证会造成的失败固定
// 在测试里：把 requireAPIKey 摘掉，下面每个 wantStatus 都会变。
func TestAPIKeyRequired(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	// 请求体故意不合法（upload 缺 bytes）。这样"通过认证"的证据不是
	// 202，而是 400 + invalid_request：说明令牌过了、请求进了业务校验。
	// 用合法请求体的话，202 既能表示认证通过也能表示别的因素，不够干净。
	body := `{"input":{"kind":"upload","file_name":"a.ofd"},"output":{"format":"pdf"}}`

	cases := []struct {
		name       string
		token      string
		wantStatus int
		wantCode   string
	}{
		{"无令牌", "", fasthttp.StatusUnauthorized, "unauthorized"},
		{"错令牌", "wrong-token", fasthttp.StatusUnauthorized, "unauthorized"},
		// 前缀正确、整体错误：定长比较必须挡住它。用逐字节提前返回的实现
		// 会因为先匹配上公共前缀而放行。
		{"令牌前缀相同", testAPIKey[:len(testAPIKey)-1], fasthttp.StatusUnauthorized, "unauthorized"},
		{"令牌多一个字符", testAPIKey + "x", fasthttp.StatusUnauthorized, "unauthorized"},
		{"Bearer 前缀拼错", "bearer " + testAPIKey, fasthttp.StatusUnauthorized, "unauthorized"},
		{"正确令牌", testAPIKey, fasthttp.StatusBadRequest, "invalid_request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := doAuth(t, client, fasthttp.MethodPost, "/v1/convert", body, tc.token)
			if got.status != tc.wantStatus {
				t.Fatalf("状态 = %d，期望 %d: %s", got.status, tc.wantStatus, got.body)
			}
			if got.decode(t)["code"] != tc.wantCode {
				t.Fatalf("错误码 = %v，期望 %s: %s", got.decode(t)["code"], tc.wantCode, got.body)
			}
		})
	}
}

// TestAPIKeyHeaderVariants 确认两种令牌传递方式等价。
func TestAPIKeyHeaderVariants(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())

	req := fasthttp.AcquireRequest()
	req.Header.SetMethod(fasthttp.MethodGet)
	req.Header.SetHost("ofd-server.test")
	req.SetRequestURI("/v1/jobs/does-not-exist")
	req.Header.Set(apiKeyHeader, testAPIKey)
	resp := fasthttp.AcquireResponse()
	if err := client.Do(req, resp); err != nil {
		t.Fatal(err)
	}
	// 令牌通过了，落到"任务不存在"而不是 401。
	if resp.StatusCode() == fasthttp.StatusUnauthorized {
		t.Fatalf("X-OFD-Api-Key 未被接受: %d", resp.StatusCode())
	}
	fasthttp.ReleaseRequest(req)
	fasthttp.ReleaseResponse(resp)
}

// TestProbesDoNotRequireKey 探针必须保持免鉴权。
//
// kubelet 与负载均衡器不持有业务令牌，挡掉它们只会让健康检查一直失败、
// 容器被反复重启。这是可用性问题，不是安全权衡。
func TestProbesDoNotRequireKey(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	for _, path := range []string{"/healthz", "/readyz"} {
		if got := doAuth(t, client, fasthttp.MethodGet, path, "", ""); got.status != fasthttp.StatusOK {
			t.Errorf("%s = %d，期望 %d: %s", path, got.status, fasthttp.StatusOK, got.body)
		}
	}
}

// TestAPIKeyRequiredByConfig 空令牌必须拒绝启动。
func TestAPIKeyRequiredByConfig(t *testing.T) {
	for _, token := range []string{"", "   "} {
		raw, _ := json.Marshal(map[string]any{
			"db_path": "/tmp/j.db", "output_dir": "/tmp/o", "api_key": token,
		})
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(path); err == nil {
			t.Errorf("api_key = %q 时应拒绝启动", token)
		}
	}
}

// TestUnknownRequestFieldsRejected 请求体里的未知字段必须报错。
//
// 静默忽略未知字段在提交接口上代价特别大：把 file_name 拼错，任务照样成功、
// 产物名安静地退回 output.pdf，调用方拿到的是"成功但名字不对"——比直接 400
// 难查得多。配置文件那边早就开了严格解码，请求体这里以前漏了。
func TestUnknownRequestFieldsRejected(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	payload := ofdPayload(t)

	cases := []struct {
		name string
		body string
	}{
		// 用旧字段名 filename：改名之后老调用方会直接收到明确的错误，
		// 而不是悄悄丢掉这个值。
		{"旧的文件名字段", `{"input":{"kind":"upload","filename":"a.ofd","bytes":"` + payload +
			`"},"output":{"kind":"dir","format":"pdf"}}`},
		{"拼错的文件名字段", `{"input":{"kind":"upload","file_nmae":"a.ofd","bytes":"` + payload +
			`"},"output":{"kind":"dir","format":"pdf"}}`},
		{"拼错的输出字段", `{"input":{"kind":"upload","file_name":"a.ofd","bytes":"` + payload +
			`"},"output":{"kind":"dir","format":"pdf","file_nmae":"x"}}`},
		{"旧的远端字段", `{"input":{"kind":"upload","file_name":"a.ofd","bytes":"` + payload +
			`"},"output":{"kind":"s3","s3_target":"minio"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := do(t, client, fasthttp.MethodPost, "/v1/convert", tc.body)
			if got.status != fasthttp.StatusBadRequest {
				t.Errorf("状态 = %d，期望 400: %s", got.status, truncate(got.body))
			}
		})
	}
}
