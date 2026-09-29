package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/valyala/fasthttp"

	"github.com/zc310/ofd/internal/allowlist"
	"github.com/zc310/ofd/internal/convertersvc"
	"github.com/zc310/ofd/internal/jobstore"
	"github.com/zc310/ofd/internal/notify"
	"github.com/zc310/ofd/internal/runner"
)

// 端到端：真的起 HTTP 端口，走 fasthttp 服务、队列、worker、转换、通知回环。
// 中间任何一环没接上，这个用例都会卡在等待终态上。
func TestEndToEndConvertAndNotify(t *testing.T) {
	// 接收方回调：记录收到的通知。
	hook := make(chan []byte, 4)
	hookSrv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		hook <- body
		w.WriteHeader(http.StatusOK)
	})}
	hookLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = hookSrv.Serve(hookLn) }()
	defer hookSrv.Close()

	dir := t.TempDir()
	store, err := jobstore.Open(filepath.Join(dir, "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	outDir := filepath.Join(dir, "out")
	for _, d := range []string{outDir, filepath.Join(dir, "tmp")} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &Config{
		Listen:         "127.0.0.1:0",
		DBPath:         filepath.Join(dir, "jobs.db"),
		OutputDir:      outDir,
		TempDir:        filepath.Join(dir, "tmp"),
		LogLevel:       "error",
		APIKey:         testAPIKey,
		MaxUploadBytes: 32 << 20,
		MaxStreamBytes: 16 << 20,
		FastWorkers:    2,
		HeavyWorkers:   1,
		NotifyWorkers:  2,
		MaxJobAttempts: 1,
		Retention:      Duration(time.Hour),
		NotifyTargets:  map[string]NotifyTarget{"loop": {URL: "http://" + hookLn.Addr().String() + "/hook", Secret: "k"}},
	}
	registry := cfg.NotifyRegistry()
	list, err := allowlist.Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	convert := convertersvc.New(cfg.TempDir, list)
	deliverer := notify.NewDeliverer(registry, &http.Client{Timeout: 5 * time.Second})
	jobRunner := runner.New(store, convert, deliverer, runner.Config{
		FastWorkers: 2, HeavyWorkers: 1, NotifyWorkers: 2,
		PollInterval: 10 * time.Millisecond, MaxJobAttempts: 1, Retention: time.Hour,
	}, testLogger())
	if err := jobRunner.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer jobRunner.Stop()

	server := NewServer(cfg, store, registry, list, &RemoteTargets{}, convert, testLogger())
	// 真实监听一个空闲端口。
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	probe.Close()
	cfg.Listen = addr

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = server.Start(ctx) }()
	waitForPort(t, addr)

	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		// 走真实 net/http 而非 fasthttp client，所以令牌在传输层统一补，
		// 免得每个请求各写一遍 header 而漏掉某个。
		Transport: bearerTransport{key: testAPIKey, base: http.DefaultTransport},
	}

	// 提交。
	payload := ofdPayload(t)
	body, _ := json.Marshal(map[string]any{
		"input": map[string]any{"kind": "upload", "file_name": "a.ofd", "bytes": payload},
		// 用 dir 而不是 stream：stream 现在是同步语义（响应体直接是产物），
		// 没有 202 可轮询、也不发通知。异步加通知这条路径要单独测。
		"output": map[string]any{"kind": "dir", "format": "pdf"},
		"notify": map[string]any{"target": "loop"},
	})
	resp, err := httpClient.Post("http://"+addr+"/v1/convert", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var submitted map[string]any
	decodeBody(t, resp, &submitted)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("提交 = %d: %v", resp.StatusCode, submitted)
	}
	id := submitted["id"].(string)

	// 轮询到终态。
	var final map[string]any
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := httpClient.Get("http://" + addr + "/v1/jobs/" + id)
		if err != nil {
			t.Fatal(err)
		}
		decodeBody(t, resp, &final)
		resp.Body.Close()
		if state, _ := final["state"].(string); state == "succeeded" || state == "failed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if final["state"] != "succeeded" {
		t.Fatalf("任务未成功: %v", final)
	}
	// 用量字段从 jobstore 直接读，而不是从 HTTP 响应读。
	// jobView 是刻意收窄的白名单投影，统计接口将来同样走 store 而非
	// HTTP 层——所以这里要验证的链路是 runner -> RecordUsage -> jobstore。
	stored, err := store.Get(id)
	if err != nil || stored == nil {
		t.Fatalf("读任务失败: %v", err)
	}
	if stored.FromActual != "ofd" {
		t.Errorf("FromActual = %q，期望 ofd（服务端判定值，不是声明值）", stored.FromActual)
	}
	if stored.InputBytes <= 0 {
		t.Errorf("InputBytes = %d，应为正的落盘字节数", stored.InputBytes)
	}
	output, _ := final["output"].(map[string]any)
	if output["kind"] != "dir" {
		t.Errorf("输出类型 = %v", output["kind"])
	}
	if size, _ := output["size"].(float64); size <= 0 {
		t.Errorf("输出大小 = %v", output["size"])
	}

	// 通知应当送到。
	select {
	case got := <-hook:
		var event map[string]any
		if err := json.Unmarshal(got, &event); err != nil {
			t.Fatalf("通知不是合法 JSON: %q", got)
		}
		if event["job_id"] != id {
			t.Errorf("通知里的 job_id = %v，期望 %s", event["job_id"], id)
		}
		if event["event"] != notify.EventSucceeded {
			t.Errorf("事件 = %v", event["event"])
		}
	case <-time.After(20 * time.Second):
		t.Fatal("通知未送达")
	}

	// 目录输出走另一条路径：确认文件真的落在 output_dir 下。
	dirBody, _ := json.Marshal(map[string]any{
		"input":  map[string]any{"kind": "upload", "file_name": "a.ofd", "bytes": payload},
		"output": map[string]any{"kind": "dir", "format": "pdf"},
	})
	resp, err = httpClient.Post("http://"+addr+"/v1/convert", "application/json", bytes.NewReader(dirBody))
	if err != nil {
		t.Fatal(err)
	}
	var dirJob map[string]any
	decodeBody(t, resp, &dirJob)
	dirID := dirJob["id"].(string)

	deadline = time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = httpClient.Get("http://" + addr + "/v1/jobs/" + dirID)
		if err != nil {
			t.Fatal(err)
		}
		decodeBody(t, resp, &final)
		resp.Body.Close()
		if state, _ := final["state"].(string); state == "succeeded" || state == "failed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if final["state"] != "succeeded" {
		t.Fatalf("目录输出任务未成功: %v", final)
	}
	// 结果落在 outDir/<任务 ID>/ 下，按任务隔离。
	//
	// 只检查这个任务自己的子目录，不统计 outDir 下的总项数——同一个测试里
	// 前面那个任务也留下了目录，计数会随测试顺序变。
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if entry.Name() == dirID {
			if !entry.IsDir() {
				t.Fatalf("%s 不是目录", dirID)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("outDir 下没有任务 %s 的子目录，现有: %v", dirID, names(entries))
	}
	produced, err := os.ReadDir(filepath.Join(outDir, dirID))
	if err != nil {
		t.Fatal(err)
	}
	if len(produced) == 0 {
		t.Fatal("任务子目录里没有产出文件")
	}
	for _, entry := range produced {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() == 0 {
			t.Errorf("%s 是空文件", entry.Name())
		}
	}
	// 任务记录里的路径应指向这个子目录。
	output, _ = final["output"].(map[string]any)
	if output["path"] != filepath.Join(outDir, dirID) {
		t.Errorf("记录里的输出路径 = %v，期望 %s", output["path"], filepath.Join(outDir, dirID))
	}
}

// URL 输入的准入：白名单为空时一律拒绝，配了白名单则只放行名单内的主机。
// 这些检查在提交阶段做，调用方当场拿到 403，而不是提交成功、轮询一圈才发现失败。
func TestURLInputAdmission(t *testing.T) {
	cases := []struct {
		name    string
		entries []string
		url     string
		status  int
		code    string
	}{
		{"未启用 URL 输入", nil, "https://example.com/a.ofd", fasthttp.StatusForbidden, "url_input_disabled"},
		{"主机在名单内", []string{"example.com"}, "https://example.com/a.ofd", fasthttp.StatusAccepted, ""},
		{"主机不在名单内", []string{"example.com"}, "https://evil.test/a.ofd", fasthttp.StatusForbidden, "url_not_allowed"},
		{"子域通配放行", []string{"*.example.com"}, "https://files.example.com/a.ofd", fasthttp.StatusAccepted, ""},
		{"子域通配不匹配", []string{"*.example.com"}, "https://example.org/a.ofd", fasthttp.StatusForbidden, "url_not_allowed"},
		// 网段白名单。
		{"网段内", []string{"10.1.0.0/16"}, "http://10.1.2.3/a.ofd", fasthttp.StatusAccepted, ""},
		{"网段外", []string{"10.1.0.0/16"}, "http://10.2.0.1/a.ofd", fasthttp.StatusForbidden, "url_not_allowed"},
		// 元数据地址不在名单网段内时按链路本地拒掉。
		{"元数据地址", []string{"10.0.0.0/8"}, "http://169.254.169.254/latest/", fasthttp.StatusForbidden, "url_not_allowed"},
		// 但显式写出 0.0.0.0/0 就是运维自己的决定：allowlist 让显式网段优先于
		// 敏感网段黑名单，这是"受控内网放行"的一部分。这里把它固化成用例，
		// 免得有人当成 bug 顺手"修掉"。
		{"显式全网段网段放行元数据", []string{"0.0.0.0/0"}, "http://169.254.169.254/latest/", fasthttp.StatusAccepted, ""},
		{"回环地址", []string{"10.0.0.0/8"}, "http://127.0.0.1:8080/a.ofd", fasthttp.StatusForbidden, "url_not_allowed"},
		// 协议限制。
		{"非 http 协议", []string{"example.com"}, "file:///etc/passwd", fasthttp.StatusBadRequest, "invalid_request"},
		{"缺主机名", []string{"example.com"}, "http:///a.ofd", fasthttp.StatusBadRequest, "invalid_request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _, _ := newTestServerWithList(t, tc.entries)
			client := newPipeServer(t, s.Handler())
			// 显式写 kind=dir：本表断言的是 URL 白名单的准入结果，要的是 202 与
			// 任务 ID。省略 kind 等同 stream，会走同步路径返回 200，测不到准入。
			body := fmt.Sprintf(`{"input":{"kind":"url","url":%q,"format":"ofd"},"output":{"kind":"dir","format":"pdf"}}`, tc.url)
			got := do(t, client, fasthttp.MethodPost, "/v1/convert", body)
			if got.status != tc.status {
				t.Fatalf("状态 = %d，期望 %d: %s", got.status, tc.status, got.body)
			}
			if tc.code != "" && got.decode(t)["code"] != tc.code {
				t.Errorf("code = %v，期望 %s", got.decode(t)["code"], tc.code)
			}
		})
	}
}

func decodeBody(t *testing.T, resp *http.Response, out *map[string]any) {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("响应不是 JSON (状态 %d): %q", resp.StatusCode, raw)
	}
}

func waitForPort(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("端口 %s 未就绪", addr)
}

// bearerTransport 给每个请求补上 Bearer 令牌。
type bearerTransport struct {
	key  string
	base http.RoundTripper
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+t.key)
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}
