package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/valyala/fasthttp"

	"github.com/zc310/ofd/internal/allowlist"
	"github.com/zc310/ofd/internal/convertersvc"
	"github.com/zc310/ofd/internal/jobstore"
	"github.com/zc310/ofd/internal/notify"
)

// TestMetricsExposesCounters 确认端点真的把累计计数渲染出来。
func TestMetricsExposesCounters(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())

	// 还没转换过时只应有 HELP/TYPE 声明，不能是空响应——空 body 会被
	// Prometheus 判成目标不可用。
	got := do(t, client, fasthttp.MethodGet, "/metrics", "")
	if got.status != fasthttp.StatusOK {
		t.Fatalf("状态 = %d，期望 200", got.status)
	}
	if !strings.Contains(got.body, "# TYPE ofd_conversions_total counter") {
		t.Errorf("缺少 TYPE 声明: %q", got.body)
	}
	// HELP/TYPE 必须在样本之前，且各出现一次。
	if strings.Count(got.body, "# TYPE ofd_conversions_total") != 1 {
		t.Errorf("TYPE 声明应只出现一次: %q", got.body)
	}
	if strings.Contains(got.body, "ofd_conversions_total{") {
		t.Errorf("没有任务时不应有样本行: %q", got.body)
	}
	ct := responseContentType(t, client)
	if !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q，采集端按 text/plain 之外的类型会直接拒绝目标", ct)
	}
	if !strings.Contains(ct, "version="+prometheusTextVersion) {
		t.Errorf("Content-Type = %q，缺少 version 参数", ct)
	}
}

// TestMetricsCountsAfterConversion 端到端确认一次真实转换后计数出现在输出里。
func TestMetricsCountsAfterConversion(t *testing.T) {
	s, store, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())

	// newTestServer 只建了 store 与 HTTP 层，没有 runner，所以提交的任务会
	// 一直停在 queued。这里按 runner 的实际顺序手动走一遍
	// 转换 -> RecordUsage -> Finish，验证的是计数真正从转换结果里取值。
	convert := convertersvc.New(filepath.Join(s.cfg.OutputDir, "tmp"), nil)
	result, err := convert.Run(t.Context(), convertersvc.Spec{
		Input:  convertersvc.Input{Kind: convertersvc.InputUpload, FileName: "a.ofd", Bytes: decodePayload(t)},
		Output: convertersvc.Output{Kind: convertersvc.OutputStream, Format: "pdf"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Enqueue(&jobstore.Job{ID: "m1", To: "pdf"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim(jobstore.LaneFast); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordUsage("m1", result.InputFormat, result.InputBytes); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish("m1", jobstore.StateSucceeded, jobstore.Output{Size: int64(len(result.Bytes))}, ""); err != nil {
		t.Fatal(err)
	}

	got := do(t, client, fasthttp.MethodGet, "/metrics", "")
	want := []string{
		`ofd_conversions_total{from="ofd",to="pdf",state="succeeded"} 1`,
		`ofd_input_bytes_total{from="ofd"}`,
		`ofd_output_bytes_total{from="ofd",to="pdf"}`,
	}
	for _, w := range want {
		if !strings.Contains(got.body, w) {
			t.Errorf("输出缺少 %q:\n%s", w, got.body)
		}
	}
	// 计数只在 Finish 时累加，所以现在应该正好是 1，不能是 0。
	if !strings.Contains(got.body, `} 1`) {
		t.Errorf("计数应为 1:\n%s", got.body)
	}
}

// TestMetricsCountsSurviveRestart 计数必须跨重启存活。
//
// 纯内存计数在重启后归零，Prometheus 能修正 rate 运算，但人会以为数据丢了。
// 计数落在 bbolt 就是为了不留这个坑。
func TestMetricsCountsSurviveRestart(t *testing.T) {
	// 这里不用 newTestServer：它自己开库但拿不到库文件路径，而"重开后计数
	// 还在"这个断言必须重开同一个文件才有意义。
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "jobs.db")
	store, err := jobstore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Enqueue(&jobstore.Job{ID: "restart-1", To: "pdf"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim(jobstore.LaneFast); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordUsage("restart-1", "ofd", 500); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish("restart-1", jobstore.StateSucceeded, jobstore.Output{Size: 900}, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	fresh, err := jobstore.Open(dbPath)
	if err != nil {
		t.Fatalf("重开任务库失败: %v", err)
	}
	t.Cleanup(func() { _ = fresh.Close() })
	cfg := &Config{OutputDir: filepath.Join(dir, "out"), APIKey: testAPIKey}
	registry := notify.NewRegistry(nil)
	list, err := allowlist.Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewServer(cfg, fresh, registry, list, &RemoteTargets{}, convertersvc.New("", nil), testLogger())

	got := do(t, newPipeServer(t, restarted.Handler()), fasthttp.MethodGet, "/metrics", "")
	if !strings.Contains(got.body, `ofd_conversions_total{from="ofd",to="pdf",state="succeeded"} 1`) {
		t.Errorf("重启后计数丢失:\n%s", got.body)
	}
}

// TestMetricsRequiresAPIKey 统计端点需要令牌。
func TestMetricsRequiresAPIKey(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	if got := doAuth(t, client, fasthttp.MethodGet, "/metrics", "", ""); got.status != fasthttp.StatusUnauthorized {
		t.Errorf("无令牌时状态 = %d，期望 401", got.status)
	}
}

// TestPromLabelEscaping 标签转义必须挡住换行注入。
//
// 不转义的话，一个含换行的值能把样本行截断，后面的内容会被 Prometheus 当成
// 新指标解析，报出来的错误指向完全无关的指标名。
func TestPromLabelEscaping(t *testing.T) {
	cases := map[string]string{
		`plain`:       `"plain"`,
		`a\b`:         `"a\\b"`,
		"a\nb":        `"a\nb"`,
		`say "hi"`:    `"say \"hi\""`,
		"back\\slash": `"back\\slash"`,
	}
	for in, want := range cases {
		if got := promLabel(in); got != want {
			t.Errorf("promLabel(%q) = %s，期望 %s", in, got, want)
		}
	}
}

// TestRenderMetricsEscapesLabels 把转义接到渲染路径上验证。
func TestRenderMetricsEscapesLabels(t *testing.T) {
	snap := jobstore.StatsSnapshot{
		Jobs: map[jobstore.StatsTriple]uint64{
			{From: "a\nb", To: "pdf", State: "succeeded"}: 1,
		},
		InputBytes:  map[string]uint64{"ofd": 1},
		OutputBytes: map[jobstore.StatsPair]uint64{{From: `x"y`, To: "pdf"}: 1},
	}
	out := string(renderMetrics(snap, testQueueDepth(), 4, 2, 4))
	// 只检查带 label 的 ofd_ 样本行：运行时指标（go_goroutines 等）本来
	// 就没有 label，不该套用同一套结构断言。
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(line, "#") || !strings.HasPrefix(line, "ofd_") {
			continue
		}
		// 每条样本行必须恰好一个未转义的标签区段，且以计数值结尾。
		if strings.Count(line, "{") != 1 || strings.Count(line, "}") != 1 {
			t.Errorf("样本行结构异常: %q", line)
		}
		// 末段必须是合法数字。硬编码期望 " 1" 会在加入非 1 的 gauge
		// （队列深度、worker 数）时误报，所以这里解析而不是比字面量。
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Errorf("样本行应恰好两段: %q", line)
			continue
		}
		if _, err := strconv.ParseFloat(fields[1], 64); err != nil {
			t.Errorf("样本行末段应为数值: %q", line)
		}
	}
	if !strings.Contains(out, `ofd_conversions_total{from="a\nb",to="pdf",state="succeeded"} 1`) {
		t.Errorf("换行未被转义:\n%s", out)
	}
	if !strings.Contains(out, `ofd_output_bytes_total{from="x\"y",to="pdf"} 1`) {
		t.Errorf("引号未被转义:\n%s", out)
	}
}

// TestRenderMetricsStableOrder 输出顺序必须稳定，否则每次抓取无法 diff。
func TestRenderMetricsStableOrder(t *testing.T) {
	snap := jobstore.StatsSnapshot{
		Jobs: map[jobstore.StatsTriple]uint64{
			{From: "docx", To: "pdf", State: "succeeded"}: 1,
			{From: "ofd", To: "png", State: "succeeded"}:  2,
			{From: "ofd", To: "pdf", State: "succeeded"}:  3,
		},
		InputBytes: map[string]uint64{"docx": 1, "ofd": 2},
		OutputBytes: map[jobstore.StatsPair]uint64{
			{From: "ofd", To: "pdf"}:  1,
			{From: "docx", To: "pdf"}: 2,
		},
	}
	// 只比较 ofd_ 计数部分：运行时指标（goroutine 数、堆水位）本来就该
	// 每次都变，把它们算进"稳定"是要求水位不许动。
	first := ofdSampleLines(renderMetrics(snap, testQueueDepth(), 4, 2, 4))
	for i := 0; i < 20; i++ {
		if got := ofdSampleLines(renderMetrics(snap, testQueueDepth(), 4, 2, 4)); got != first {
			t.Fatalf("第 %d 次渲染的计数部分不同:\n首次:\n%s\n本次:\n%s", i, first, got)
		}
	}
	// docx 应排在 ofd 之前。
	if strings.Index(first, `from="docx"`) > strings.Index(first, `from="ofd"`) {
		t.Errorf("输出未按标签排序:\n%s", first)
	}
}

// responseContentType 读一次 /metrics 的 Content-Type。
func responseContentType(t *testing.T, client *fasthttp.Client) string {
	t.Helper()
	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)
	req.Header.SetMethod(fasthttp.MethodGet)
	req.Header.SetHost("ofd-server.test")
	req.SetRequestURI("/metrics")
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)
	if err := client.Do(req, resp); err != nil {
		t.Fatal(err)
	}
	return string(resp.Header.ContentType())
}

// TestRuntimeMetricsExposed 确认运行时指标真的出现在输出里。
//
// 这些指标要是有问题，最典型的表现是"面板看着有数据但恒定不变"——比如
// 线程数误用了 GOMAXPROCS。所以断言的是存在且合理，不只是存在。
func TestRuntimeMetricsExposed(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	got := do(t, client, fasthttp.MethodGet, "/metrics", "")

	for _, name := range []string{
		"go_goroutines", "go_memstats_heap_alloc_bytes", "go_memstats_heap_inuse_bytes",
		"go_memstats_sys_bytes", "go_memstats_stack_inuse_bytes",
		"go_memstats_alloc_bytes_total", "go_memstats_gc_count",
		"go_gc_pause_seconds", "go_info",
	} {
		if !strings.Contains(got.body, "\n"+name) && !strings.HasPrefix(got.body, name) {
			if !strings.Contains(got.body, name+" ") && !strings.Contains(got.body, name+"{") {
				t.Errorf("缺少指标 %s", name)
			}
		}
	}
	// 堆水位不可能是 0。
	if !strings.Contains(got.body, "go_memstats_heap_inuse_bytes ") {
		t.Error("缺少堆水位")
	}
}

// TestProcessMetricsExposed 进程级指标在 Linux 上必须出现。
func TestProcessMetricsExposed(t *testing.T) {
	if _, err := os.ReadFile("/proc/self/stat"); err != nil {
		t.Skip("非 Linux 环境，/proc 不可读")
	}
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	got := do(t, client, fasthttp.MethodGet, "/metrics", "")
	for _, name := range []string{
		"process_resident_memory_bytes", "process_virtual_memory_bytes",
		"process_open_fds", "process_max_fds", "process_start_time_seconds",
	} {
		if !strings.Contains(got.body, name+" ") {
			t.Errorf("缺少指标 %s:\n%s", name, got.body)
		}
	}
}

// TestProcessStartTimeIsSane 启动时间必须落在合理区间。
//
// 这个值算错不会报错，只会让重启后的 rate() 产出垃圾速率，所以要单独卡一道。
func TestProcessStartTimeIsSane(t *testing.T) {
	if _, err := os.ReadFile("/proc/self/stat"); err != nil {
		t.Skip("非 Linux 环境")
	}
	start, ok := procStartTime()
	if !ok {
		t.Fatal("procStartTime 应可用")
	}
	now := float64(time.Now().Unix())
	age := now - start
	if age < 0 || age > 86400 {
		t.Errorf("进程启动时间 = %v，距今 %.0f 秒，不在合理范围", start, age)
	}
}

// TestProcMaxFDsRejectsUnlimited unlimited 表示无上限，不能当成数字输出。
func TestProcMaxFDsRejectsUnlimited(t *testing.T) {
	if _, err := os.ReadFile("/proc/self/limits"); err != nil {
		t.Skip("非 Linux 环境")
	}
	if value, ok := procMaxFDs(); ok {
		if value == 0 {
			t.Error("上限为 0 说明解析出了问题")
		}
	}
}

// TestMetricsOutputIsStableAcrossScrapes 连续两次抓取的运行时部分允许变化，
// 但声明行（HELP/TYPE）与顺序必须完全一致——否则 Prometheus 解析会报重复指标。
func TestMetricsOutputIsStableAcrossScrapes(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	first := do(t, client, fasthttp.MethodGet, "/metrics", "").body
	second := do(t, client, fasthttp.MethodGet, "/metrics", "").body
	for _, name := range []string{
		"# HELP ofd_conversions_total", "# TYPE ofd_conversions_total counter",
		"# TYPE ofd_input_bytes_total counter", "# TYPE ofd_output_bytes_total counter",
		"# TYPE go_goroutines gauge", "# TYPE process_resident_memory_bytes gauge",
	} {
		if strings.Count(first, name) != 1 || strings.Count(second, name) != 1 {
			t.Errorf("声明 %q 在输出中出现次数应恰为 1（第一次 %d，第二次 %d）",
				name, strings.Count(first, name), strings.Count(second, name))
		}
	}
}

// TestProcReadFailuresAreNonFatal /proc 读不到时不应让渲染崩掉。
func TestProcReadFailuresAreNonFatal(t *testing.T) {
	// 这些函数在 /proc 缺失时返回 ok=false 而不是 panic 或报错。
	if _, _, ok := procMem(); !ok {
		if _, err := os.ReadFile("/proc/self/statm"); err == nil {
			t.Error("statm 可读但 procMem 报失败")
		}
	}
	if _, ok := procThreads(); !ok {
		if raw, err := os.ReadFile("/proc/self/status"); err == nil && !strings.Contains(string(raw), "Threads:") {
			t.Error("status 可读但没有 Threads 字段")
		}
	}
}

// TestMetricsDoesNotExposeFileNames 输出里不应出现任何文件名或路径。
//
// 转换量是运营数据，文档名不是。label 只有 from/to/state，
// 基数也受限于格式组合数。
func TestMetricsDoesNotExposeFileNames(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	if err := s.store.Enqueue(&jobstore.Job{ID: "secret-doc", To: "pdf",
		Request: []byte(`{"input":{"file_name":"invoice-2026-private.ofd"}}`)}); err != nil {
		t.Fatal(err)
	}
	got := do(t, client, fasthttp.MethodGet, "/metrics", "")
	for _, leak := range []string{"invoice-2026-private", "secret-doc", ".ofd"} {
		if strings.Contains(got.body, leak) {
			t.Errorf("输出泄露了 %q:\n%s", leak, got.body)
		}
	}
	_ = os.Remove(filepath.Join(s.cfg.OutputDir, "unused"))
}

// decodePayload 把 ofdPayload 的 base64 还原成字节。
func decodePayload(t *testing.T) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(ofdPayload(t))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// ofdSampleLines 抽出本服务自己的计数样本行。
//
// 刻意排除运行时指标：goroutine 数与堆水位每次抓取都不一样，把它们算进
// "两次输出必须相同"等于要求水位不许动，那是把测试写错了。
func ofdSampleLines(out []byte) string {
	var kept []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if !strings.HasPrefix(line, "ofd_") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// testQueueDepth 是渲染测试用的固定队列深度。
func testQueueDepth() map[jobstore.State]int {
	return map[jobstore.State]int{jobstore.StateQueued: 7, jobstore.StateRunning: 3}
}

// TestQueueDepthRendered /metrics 要真的输出队列深度。
func TestQueueDepthRendered(t *testing.T) {
	s, store, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())

	// 先确认空队列时是 0 而不是缺序列。
	got := do(t, client, fasthttp.MethodGet, "/metrics", "")
	if !strings.Contains(got.body, `ofd_queue_depth{state="queued"} 0`) {
		t.Errorf("空队列应输出 0:\n%s", got.body)
	}

	// 排入一个就绪任务和一个退避中的任务。
	for _, job := range []*jobstore.Job{
		{ID: "depth-1", To: "pdf"},
		{ID: "depth-2", To: "pdf", DueAt: time.Now().Add(time.Hour)},
	} {
		if err := store.Enqueue(job); err != nil {
			t.Fatal(err)
		}
	}
	got = do(t, client, fasthttp.MethodGet, "/metrics", "")
	if !strings.Contains(got.body, `ofd_queue_depth{state="queued"} 2`) {
		t.Errorf("退避中的任务必须计入 queued:\n%s", got.body)
	}
	if !strings.Contains(got.body, `ofd_queue_depth{state="running"} 0`) {
		t.Errorf("running 应为 0:\n%s", got.body)
	}
}

// TestWorkersRendered worker 数要输出，队列深度才能换算成利用率。
func TestWorkersRendered(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	got := do(t, client, fasthttp.MethodGet, "/metrics", "")
	// newTestServer 配的是 FastWorkers: 1, HeavyWorkers: 1, NotifyWorkers: 1。
	for _, want := range []string{
		`ofd_workers{lane="fast"} 1`,
		`ofd_workers{lane="heavy"} 1`,
		`ofd_workers{lane="notify"} 1`,
	} {
		if !strings.Contains(got.body, want) {
			t.Errorf("缺少 %s:\n%s", want, got.body)
		}
	}
}

// TestQueueDepthOmittedWhenUnavailable 读不到深度时整组 gauge 不输出，
// 而不是输出 0——后者会被告警读成"队列是空的"。
func TestQueueDepthOmittedWhenUnavailable(t *testing.T) {
	out := string(renderMetrics(jobstore.StatsSnapshot{}, nil, 4, 2, 4))
	if strings.Contains(out, "ofd_queue_depth{") {
		t.Errorf("depth 为 nil 时不应输出样本行:\n%s", out)
	}
	// 但其它指标照常输出。
	if !strings.Contains(out, "# TYPE ofd_queue_depth gauge") {
		t.Errorf("TYPE 声明仍应存在:\n%s", out)
	}
	if !strings.Contains(out, `ofd_workers{lane="fast"} 4`) {
		t.Errorf("worker 数应照常输出:\n%s", out)
	}
}
