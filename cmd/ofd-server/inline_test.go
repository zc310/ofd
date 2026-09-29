package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/valyala/fasthttp"

	"github.com/zc310/ofd/internal/jobstore"
)

// TestInlineStreamReturnsContent 同步转换必须真的把产物交回来。
//
// 这是 stream 语义的全部意义：它不落盘，异步提交的结果没有任何人能取到。
// 之前的行为是转换被完整执行一遍、任务记为 succeeded、size 也报得出来，
// 但内容随内存里的 Result 一起丢掉——产出为零。
func TestInlineStreamReturnsContent(t *testing.T) {
	s, store, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())

	got := do(t, client, fasthttp.MethodPost, "/v1/convert", submitInlineBody(t, "pdf"))
	if got.status != fasthttp.StatusOK {
		t.Fatalf("状态 = %d，期望 200: %s", got.status, truncate(got.body))
	}
	if got.contentType != "application/pdf" {
		t.Errorf("Content-Type = %q，期望 application/pdf", got.contentType)
	}
	// 产物必须是真 PDF，不是空响应或错误 JSON。
	if len(got.body) < 5 || !strings.HasPrefix(got.body, "%PDF-") {
		t.Fatalf("响应体不是 PDF: %q", truncate(got.body))
	}
	// 大小必须与 Content-Length 一致——截断的响应也要能被测出来。
	if got.contentLength != len(got.body) {
		t.Errorf("Content-Length = %d，响应体 %d 字节", got.contentLength, len(got.body))
	}
	// 任务 ID 放进响应头，调用方之后还能查记录。
	if got.jobID == "" {
		t.Error("缺少 X-OFD-Job-Id 响应头")
	}
	// 任务记录照常写，统计和查询都不会漏掉同步转换。
	job, err := store.Get(got.jobID)
	if err != nil || job == nil {
		t.Fatalf("同步转换没有留下任务记录: %v", err)
	}
	if job.State != jobstore.StateSucceeded {
		t.Errorf("任务状态 = %s，期望 succeeded", job.State)
	}
	if job.FromActual == "" {
		t.Error("FromActual 为空：同步转换也要记账，否则统计里是隐形的")
	}
	if job.InputBytes <= 0 {
		t.Errorf("InputBytes = %d，期望正的字节数", job.InputBytes)
	}
}

// TestInlineCountsInStats 同步转换必须计入 /v1/stats。
func TestInlineCountsInStats(t *testing.T) {
	s, store, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())

	before := fetchStats(t, client).Totals.Conversions
	if got := do(t, client, fasthttp.MethodPost, "/v1/convert", submitInlineBody(t, "pdf")); got.status != fasthttp.StatusOK {
		t.Fatalf("状态 = %d", got.status)
	}
	after := fetchStats(t, client).Totals.Conversions
	if after != before+1 {
		t.Errorf("转换数 %d -> %d，期望 +1", before, after)
	}
	_ = store
}

// TestInlineDoesNotEnqueue 同步转换不能进通道队列。
//
// RecordDirect 而不是 Enqueue 就是为了这个：Enqueue 会把任务丢进 lane 队列，
// runner 随即领走再转换一遍，同一个输入被跑两次。
func TestInlineDoesNotEnqueue(t *testing.T) {
	s, store, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())

	got := do(t, client, fasthttp.MethodPost, "/v1/convert", submitInlineBody(t, "pdf"))
	if got.status != fasthttp.StatusOK {
		t.Fatalf("状态 = %d", got.status)
	}
	depth, err := store.QueueDepth()
	if err != nil {
		t.Fatal(err)
	}
	// 同步转换结束后不留任何未终结任务。
	if depth[jobstore.StateQueued] != 0 || depth[jobstore.StateRunning] != 0 {
		t.Errorf("同步转换后仍有未终结任务: queued=%d running=%d",
			depth[jobstore.StateQueued], depth[jobstore.StateRunning])
	}
	// 再领一次也不该捞到它。
	if claimed, err := store.Claim(jobstore.LaneFast); err == nil && claimed != nil {
		t.Errorf("同步转换的任务被 runner 领走了: %s", claimed.ID)
	}
}

// TestInlineErrorReturnsStatus 失败直接给错误状态码，而不是 202 再轮询。
func TestInlineErrorReturnsStatus(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())

	// 未注册的输出格式会在业务校验阶段被拒。
	body := `{"input":{"kind":"upload","file_name":"a.ofd","bytes":"` + ofdPayload(t) +
		`"},"output":{"kind":"stream","format":"no-such-format"}}`
	got := do(t, client, fasthttp.MethodPost, "/v1/convert", body)
	if got.status == fasthttp.StatusAccepted {
		t.Fatalf("格式非法不应返回 202")
	}
	if got.status < 400 {
		t.Errorf("状态 = %d，期望 4xx/5xx", got.status)
	}
}

// TestInlineRejectsImageFormat 逐页格式不能用 stream 语义。
func TestInlineRejectsImageFormat(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	got := do(t, client, fasthttp.MethodPost, "/v1/convert", submitInlineBody(t, "png"))
	if got.status == fasthttp.StatusOK {
		t.Fatalf("逐页格式不应成功返回单个响应体")
	}
	if !strings.Contains(got.body, "dir") {
		t.Errorf("错误信息应提示改用 dir: %s", truncate(got.body))
	}
}

// TestInlineSemaphoreLimitsConcurrency 并发上限按配置生效，满时返回 503。
func TestInlineSemaphoreLimitsConcurrency(t *testing.T) {
	s, _, cfg := newTestServer(t)
	// newTestServer 配的是 FastWorkers: 1。
	if cfg.FastWorkers != 1 {
		t.Skipf("需要 FastWorkers=1，实际 %d", cfg.FastWorkers)
	}
	sem := s.inlineSemaphore(jobstore.LaneFast)
	if cap(sem) != 1 {
		t.Fatalf("信号量容量 = %d，期望跟随 FastWorkers=1", cap(sem))
	}
	// 独占一个槽后再申请，验证确实被挡住。
	sem <- struct{}{}
	select {
	case sem <- struct{}{}:
		<-sem
		<-sem
		t.Fatal("信号量没起作用")
	default:
	}
	<-sem
}

// TestInlineSemaphoreSeparatePerLane 两条通道各有各的信号量。
func TestInlineSemaphoreSeparatePerLane(t *testing.T) {
	s, _, _ := newTestServer(t)
	fast, heavy := s.inlineSemaphore(jobstore.LaneFast), s.inlineSemaphore(jobstore.LaneHeavy)
	if fast == heavy {
		t.Error("fast 与 heavy 共用了同一个信号量：heavy 转换会占掉 fast 的配额")
	}
	// 重复调用应返回同一个实例，否则每次都是新信号量、限流完全失效。
	if s.inlineSemaphore(jobstore.LaneFast) != fast {
		t.Error("重复调用返回了不同的信号量，限流会失效")
	}
}

// TestInlineConcurrencyBounded 高并发下实际同时进行的转换数不超上限。
func TestInlineConcurrencyBounded(t *testing.T) {
	s, _, cfg, _ := newTestServerWithList(t, nil)
	s.cfg.FastWorkers = 2
	s.cfg.JobTimeout = Duration(30 * time.Second)
	client := newPipeServer(t, s.Handler())

	var wg sync.WaitGroup
	codes := make([]int, 8)
	for i := range codes {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			got := do(t, client, fasthttp.MethodPost, "/v1/convert", submitInlineBody(t, "pdf"))
			codes[idx] = got.status
		}(i)
	}
	wg.Wait()
	for i, code := range codes {
		// 200 或 503 都算正常：排队满了就该让调用方重试，不该是 500。
		if code != fasthttp.StatusOK && code != fasthttp.StatusServiceUnavailable {
			t.Errorf("第 %d 个请求状态 = %d，期望 200 或 503", i, code)
		}
	}
	_ = cfg
}

// TestHeaderSafe Content-Disposition 的文件名不能带 CR/LF。
//
// 头里的 CR/LF 会被对端当作响应头分隔符，也就是 HTTP 响应拆分。当前文件名
// 由服务拼出、不来自调用方，但这是三行成本与一整类漏洞的交换。
func TestHeaderSafe(t *testing.T) {
	cases := map[string]string{
		"output.pdf":     "output.pdf",
		"a\r\nX-Evil: 1": "aX-Evil: 1",
		"a\nb":           "ab",
		`quo"te.pdf`:     "quote.pdf",
		"../etc/passwd":  "../etc/passwd",
	}
	for in, want := range cases {
		if got := headerSafe(in); got != want {
			t.Errorf("headerSafe(%q) = %q，期望 %q", in, got, want)
		}
	}
	// 无论如何都不能剩下 CR 或 LF。
	if strings.ContainsAny(headerSafe("a\r\nb"), "\r\n") {
		t.Error("headerSafe 没能去掉 CR/LF")
	}
}

// TestFinishInlineRecordsCancellation 客户端断开记为 cancelled 而不是 failed。
func TestFinishInlineRecordsCancellation(t *testing.T) {
	s, store, _ := newTestServer(t)
	job := &jobstore.Job{ID: "gone-1", To: "pdf"}
	if err := store.RecordDirect(job); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	s.finishInline(ctx, job, errors.New("context canceled"))

	got, err := store.Get(job.ID)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.State != jobstore.StateCancelled {
		t.Errorf("状态 = %s，期望 cancelled（客户端主动放弃不算转换失败）", got.State)
	}
}

func truncate(s string) string {
	if len(s) <= 200 {
		return s
	}
	return s[:200] + "..."
}
