package main

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/goccy/go-json"
	"github.com/valyala/fasthttp"

	"github.com/zc310/ofd/internal/server/convertersvc"
	"github.com/zc310/ofd/internal/server/jobstore"
	"github.com/zc310/ofd/internal/server/observe"
)

func doRequest(t *testing.T, client *testHTTPClient, method, path string, body io.Reader, headers map[string]string) response {
	t.Helper()
	req, err := http.NewRequest(method, client.baseURL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s 失败: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response{
		status:        resp.StatusCode,
		body:          string(raw),
		contentLength: int(resp.ContentLength),
		jobID:         resp.Header.Get(jobIDHeader),
		contentType:   resp.Header.Get("Content-Type"),
		headers:       collectHeaders(resp.Header),
	}
}

func TestRequestIDHeaderAndValidation(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())

	got := doAuth(t, client, fasthttp.MethodGet, "/healthz", "", "")
	if got.header(requestIDHeader) == "" {
		t.Fatal("响应缺少 X-Request-Id")
	}
	got = doRequest(t, client, http.MethodGet, "/healthz", nil, map[string]string{requestIDHeader: "trace-123"})
	if got.header(requestIDHeader) != "trace-123" {
		t.Errorf("合法请求 ID 应被复用，实际 %q", got.header(requestIDHeader))
	}
	for _, invalid := range []string{"a b", "a\nb", strings.Repeat("x", 129)} {
		if validRequestID(invalid) {
			t.Errorf("无效请求 ID %q 被接受", invalid)
		}
	}
	if !validRequestID("abc.DEF_123:456-789") {
		t.Error("合法字符集被拒")
	}
}

func TestListJobsEndpoint(t *testing.T) {
	s, store, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	for _, to := range []string{"pdf", "png", "svg"} {
		got := do(t, client, fasthttp.MethodPost, "/v1/convert", submitBody(t, to))
		if got.status != fasthttp.StatusAccepted {
			t.Fatalf("提交失败: %d %s", got.status, got.body)
		}
	}
	// 让其中一个任务进入终态，验证状态过滤。
	job, err := store.Claim(jobstore.LaneFast)
	if err != nil || job == nil {
		t.Fatalf("Claim = %v, %v", job, err)
	}
	if err := store.Finish(job.ID, jobstore.StateSucceeded, jobstore.Output{}, ""); err != nil {
		t.Fatal(err)
	}

	got := do(t, client, fasthttp.MethodGet, "/v1/jobs?limit=2", "")
	if got.status != http.StatusOK {
		t.Fatalf("列表失败: %d %s", got.status, got.body)
	}
	payload := got.decode(t)
	jobs, _ := payload["jobs"].([]any)
	if len(jobs) != 2 {
		t.Fatalf("jobs 长度 = %d: %s", len(jobs), got.body)
	}
	if payload["count"] != float64(2) {
		t.Errorf("count = %v", payload["count"])
	}
	next, _ := payload["next_cursor"].(string)
	if next == "" {
		t.Fatalf("缺少 next_cursor: %s", got.body)
	}
	second := do(t, client, fasthttp.MethodGet, "/v1/jobs?limit=2&cursor="+next, "")
	if second.status != http.StatusOK {
		t.Fatalf("第二页失败: %d %s", second.status, second.body)
	}
	if got, want := len(second.decode(t)["jobs"].([]any)), 1; got != want {
		t.Errorf("第二页数量 = %d，期望 %d", got, want)
	}

	filtered := do(t, client, fasthttp.MethodGet, "/v1/jobs?state=succeeded", "")
	if filtered.status != http.StatusOK {
		t.Fatalf("状态过滤失败: %d %s", filtered.status, filtered.body)
	}
	filteredJobs, _ := filtered.decode(t)["jobs"].([]any)
	if len(filteredJobs) != 1 {
		t.Errorf("succeeded 任务数 = %d", len(filteredJobs))
	}
	for _, path := range []string{
		"/v1/jobs?state=bogus",
		"/v1/jobs?lane=bogus",
		"/v1/jobs?limit=0",
		"/v1/jobs?limit=201",
	} {
		if got := do(t, client, fasthttp.MethodGet, path, ""); got.status != http.StatusBadRequest {
			t.Errorf("%s 状态 = %d，期望 400", path, got.status)
		}
	}
}

func TestIdempotencyKey(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	body := submitBody(t, "pdf")

	first := doRequest(t, client, http.MethodPost, "/v1/convert", strings.NewReader(body), map[string]string{idempotencyHeader: "idem-1"})
	if first.status != http.StatusAccepted {
		t.Fatalf("首次提交 = %d %s", first.status, first.body)
	}
	firstID := first.decode(t)["id"].(string)
	second := doRequest(t, client, http.MethodPost, "/v1/convert", strings.NewReader(body), map[string]string{idempotencyHeader: "idem-1"})
	if second.status != http.StatusAccepted {
		t.Fatalf("重放 = %d %s", second.status, second.body)
	}
	if second.decode(t)["id"].(string) != firstID {
		t.Error("重放应返回同一个任务 ID")
	}
	if second.header("Idempotent-Replayed") != "true" {
		t.Error("重放应带 Idempotent-Replayed: true")
	}

	conflict := doRequest(t, client, http.MethodPost, "/v1/convert", strings.NewReader(submitBody(t, "png")), map[string]string{idempotencyHeader: "idem-1"})
	if conflict.status != http.StatusConflict || conflict.decode(t)["code"] != "idempotency_conflict" {
		t.Errorf("同键不同请求 = %d %s", conflict.status, conflict.body)
	}

	sync := doRequest(t, client, http.MethodPost, "/v1/convert", strings.NewReader(submitInlineBody(t, "pdf")), map[string]string{idempotencyHeader: "idem-2"})
	if sync.status != http.StatusBadRequest {
		t.Errorf("同步转换带幂等键 = %d，期望 400", sync.status)
	}
}

func TestMultipartAndRawUpload(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	data := helperOFDBytes(t)
	requestBody := map[string]any{
		"output": map[string]any{"kind": "dir", "format": "pdf", "file_name": "out.pdf"},
	}
	requestJSON, _ := json.Marshal(requestBody)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("request", string(requestJSON)); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("file", "sample.ofd")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	got := doRequest(t, client, http.MethodPost, "/v1/convert", &body, map[string]string{"Content-Type": writer.FormDataContentType()})
	if got.status != http.StatusAccepted {
		t.Fatalf("multipart 上传 = %d %s", got.status, got.body)
	}
	if got.decode(t)["id"] == "" {
		t.Error("multipart 上传响应缺少任务 ID")
	}

	got = doRequest(t, client, http.MethodPost, "/v1/convert?to=pdf&kind=dir&file_name=raw.pdf", bytes.NewReader(data), map[string]string{"Content-Type": "application/octet-stream"})
	if got.status != http.StatusAccepted {
		t.Fatalf("raw 上传 = %d %s", got.status, got.body)
	}
	missingFormat := doRequest(t, client, http.MethodPost, "/v1/convert", bytes.NewReader(data), map[string]string{"Content-Type": "application/octet-stream"})
	if missingFormat.status != http.StatusBadRequest {
		t.Errorf("raw 上传缺 to = %d，期望 400", missingFormat.status)
	}
}

func helperOFDBytes(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(ofdPayload(t))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestFormatsEndpoint(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	got := do(t, client, fasthttp.MethodGet, "/v1/formats", "")
	if got.status != http.StatusOK {
		t.Fatalf("/v1/formats = %d %s", got.status, got.body)
	}
	var payload formatsView
	if err := json.Unmarshal([]byte(got.body), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Input) == 0 || len(payload.Output) == 0 {
		t.Fatalf("格式列表为空: %s", got.body)
	}
	var hasOFD, hasPDF bool
	for _, format := range payload.Output {
		if format.Name == "ofd" {
			hasOFD = true
		}
		if format.Name == "pdf" {
			hasPDF = true
			if !format.Stream {
				t.Error("pdf 应支持 stream")
			}
		}
	}
	if !hasOFD || !hasPDF {
		t.Errorf("输出格式缺少 ofd/pdf: %s", got.body)
	}
}

func TestDeleteJobEndpointRemovesArtifacts(t *testing.T) {
	s, store, cfg := newTestServer(t)
	client := newPipeServer(t, s.Handler())

	queued := do(t, client, fasthttp.MethodPost, "/v1/convert", submitBody(t, "pdf"))
	id := queued.decode(t)["id"].(string)
	if got := do(t, client, fasthttp.MethodDelete, "/v1/jobs/"+id, ""); got.status != http.StatusConflict {
		t.Errorf("删除排队任务 = %d，期望 409", got.status)
	}

	dir := filepath.Join(cfg.OutputDir, id)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(dir, "result.pdf")
	if err := os.WriteFile(artifact, []byte("pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	if job, err := store.Claim(jobstore.LaneFast); err != nil || job == nil {
		t.Fatalf("Claim = %v, %v", job, err)
	}
	if err := store.Finish(id, jobstore.StateSucceeded, jobstore.Output{
		Kind: convertersvc.OutputDir, Path: dir, Size: 3, Files: []string{"result.pdf"},
	}, ""); err != nil {
		t.Fatal(err)
	}

	got := do(t, client, fasthttp.MethodDelete, "/v1/jobs/"+id, "")
	if got.status != http.StatusNoContent {
		t.Fatalf("删除终态任务 = %d %s", got.status, got.body)
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Errorf("产物仍存在: %v", err)
	}
	if got := do(t, client, fasthttp.MethodGet, "/v1/jobs/"+id, ""); got.status != http.StatusNotFound {
		t.Errorf("删除后查询 = %d，期望 404", got.status)
	}
	if got := do(t, client, fasthttp.MethodDelete, "/v1/jobs/"+id, ""); got.status != http.StatusNotFound {
		t.Errorf("重复删除 = %d，期望 404", got.status)
	}
}

func TestMetricsIncludesServerInfoAndObservations(t *testing.T) {
	s, _, _ := newTestServer(t)
	registry := s.metrics
	registry.ObserveConversion(observe.Conversion{Lane: "fast", From: "ofd", To: "pdf", State: "succeeded"}, time.Second)
	registry.ObserveNotification(observe.Notification{Target: "erp", Event: "succeeded", Result: observe.ResultSuccess})
	client := newPipeServer(t, s.Handler())
	got := do(t, client, fasthttp.MethodGet, "/metrics", "")
	if got.status != http.StatusOK {
		t.Fatalf("/metrics = %d", got.status)
	}
	for _, want := range []string{
		"ofd_server_info{",
		"go_version=",
		`ofd_conversion_duration_seconds_count{lane="fast",from="ofd",to="pdf",state="succeeded"} 1`,
		`ofd_notifications_total{target="erp",event="succeeded",result="success"} 1`,
	} {
		if !strings.Contains(got.body, want) {
			t.Errorf("缺少 %s:\n%s", want, got.body)
		}
	}
}

func TestQueueLimitEndpoint(t *testing.T) {
	s, _, cfg := newTestServer(t)
	cfg.MaxQueueDepth = 1
	client := newPipeServer(t, s.Handler())
	if got := do(t, client, fasthttp.MethodPost, "/v1/convert", submitBody(t, "pdf")); got.status != http.StatusAccepted {
		t.Fatalf("首个任务 = %d %s", got.status, got.body)
	}
	got := do(t, client, fasthttp.MethodPost, "/v1/convert", submitBody(t, "pdf"))
	if got.status != http.StatusTooManyRequests {
		t.Fatalf("超过队列上限 = %d %s", got.status, got.body)
	}
	if got.decode(t)["code"] != "queue_full" {
		t.Errorf("code = %v", got.decode(t)["code"])
	}
	if got.header("Retry-After") == "" {
		t.Error("429 应带 Retry-After")
	}
}
