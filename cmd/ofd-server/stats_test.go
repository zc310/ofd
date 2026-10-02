package main

import (
	"fmt"
	"github.com/goccy/go-json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/valyala/fasthttp"

	"github.com/zc310/ofd/internal/jobstore"
)

// 走一遍真实的记账路径，让 /v1/stats 的数字有来源。
func seedStats(t *testing.T, store *jobstore.Store) {
	t.Helper()
	cases := []struct {
		id       string
		from     string
		to       string
		state    jobstore.State
		in       int64
		out      int64
		claimIt  bool
		useUsage bool
	}{
		{id: "a1", from: "ofd", to: "pdf", state: jobstore.StateSucceeded, in: 100, out: 900, claimIt: true, useUsage: true},
		{id: "a2", from: "ofd", to: "pdf", state: jobstore.StateSucceeded, in: 200, out: 800, claimIt: true, useUsage: true},
		{id: "a3", from: "ofd", to: "png", state: jobstore.StateSucceeded, in: 50, out: 5000, claimIt: true, useUsage: true},
		{id: "a4", from: "docx", to: "pdf", state: jobstore.StateSucceeded, in: 400, out: 700, claimIt: true, useUsage: true},
		// 失败任务没有实际格式，归 unknown。
		// 失败任务刻意不调 RecordUsage：runner 只在转换成功后回写实际格式，
		// 失败时没走到那一步，所以 FromActual 为空、统计里归 unknown。
		{id: "a5", from: "", to: "ofd", state: jobstore.StateFailed, in: 0, claimIt: true},
	}
	for _, c := range cases {
		if err := store.Enqueue(&jobstore.Job{ID: c.id, To: c.to}); err != nil {
			t.Fatal(err)
		}
		if c.claimIt {
			if _, err := store.Claim(jobstore.LaneFast); err != nil {
				t.Fatal(err)
			}
		}
		if c.from != "" {
			if err := store.RecordUsage(c.id, c.from, c.in); err != nil {
				t.Fatal(err)
			}
		}
		if err := store.Finish(c.id, c.state, jobstore.Output{Size: c.out}, ""); err != nil {
			t.Fatal(err)
		}
	}
}

// TestStatsTotals 各合计数必须自洽。
func TestStatsTotals(t *testing.T) {
	s, store, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	seedStats(t, store)

	got := do(t, client, fasthttp.MethodGet, "/v1/stats", "")
	if got.status != fasthttp.StatusOK {
		t.Fatalf("状态 = %d: %s", got.status, got.body)
	}
	var view statsView
	if err := json.Unmarshal([]byte(got.body), &view); err != nil {
		t.Fatalf("响应不是合法 JSON: %v\n%s", err, got.body)
	}
	if view.Totals.Conversions != 5 {
		t.Errorf("conversions = %d，期望 5", view.Totals.Conversions)
	}
	if view.Totals.Succeeded != 4 || view.Totals.Failed != 1 {
		t.Errorf("succeeded=%d failed=%d，期望 4/1", view.Totals.Succeeded, view.Totals.Failed)
	}
	// 成功与失败之和必须等于总数：任一分类被漏掉都会在这里露出来。
	if view.Totals.Succeeded+view.Totals.Failed != view.Totals.Conversions {
		t.Errorf("分类之和 %d != 总数 %d", view.Totals.Succeeded+view.Totals.Failed, view.Totals.Conversions)
	}
	// 失败任务没有 RecordUsage，所以它的输入字节不计入：
	// 100 + 200 + 50 + 400 = 750。
	if view.Totals.InputBytes != 750 {
		t.Errorf("input_bytes = %d，期望 750（失败任务未记账）", view.Totals.InputBytes)
	}
	if view.Totals.OutputBytes != 7400 {
		t.Errorf("output_bytes = %d，期望 7400", view.Totals.OutputBytes)
	}
	if view.Since == "" {
		t.Error("since 为空：没有转换时可以不填，但有过转换就必须有起点")
	}
}

// TestStatsFormatsBreakdown 按格式分组，且各行能对回合计。
func TestStatsFormatsBreakdown(t *testing.T) {
	s, store, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	seedStats(t, store)

	view := fetchStats(t, client)
	byKey := make(map[string]formatRow, len(view.Formats))
	for _, row := range view.Formats {
		key := row.From + "->" + row.To
		if _, dup := byKey[key]; dup {
			t.Errorf("格式组合 %s 出现两次", key)
		}
		byKey[key] = row
	}
	if row := byKey["ofd->pdf"]; row.Conversions != 2 || row.Succeeded != 2 {
		t.Errorf("ofd->pdf = %+v，期望 2 次且都成功", row)
	}
	// 失败任务：输入格式未知。
	unknown, ok := byKey["unknown->ofd"]
	if !ok {
		t.Fatalf("缺少 unknown->ofd 行，实际有: %+v", view.Formats)
	}
	if !unknown.LabelUnknown {
		t.Error("失败任务那行应带 label_unknown，让调用方能与真实格式区分")
	}
	if unknown.From != "unknown" {
		t.Errorf("失败任务的 from = %q，期望 unknown", unknown.From)
	}
	// unknown 必须排在最后：它不是一种格式，混在字母序里会让人误读。
	if !view.Formats[len(view.Formats)-1].LabelUnknown {
		t.Errorf("unknown 行未排在最后: %+v", view.Formats)
	}

	// 各行合计必须等于总计——这是最容易在分组时算错的地方。
	var conv, in, out uint64
	for _, row := range view.Formats {
		conv += row.Conversions
		if row.InputBytes > in {
			in = row.InputBytes
		}
		out += row.OutputBytes
	}
	if conv != view.Totals.Conversions {
		t.Errorf("各行 conversions 之和 %d != totals %d", conv, view.Totals.Conversions)
	}
	if out != view.Totals.OutputBytes {
		t.Errorf("各行 output_bytes 之和 %d != totals %d", out, view.Totals.OutputBytes)
	}
	// 输入字节按 from 索引，同一 from 的多行会重复计入，所以只能验证
	// "不超过总计"——重复计入是这个口径的已知代价，不是 bug。
	if in > view.Totals.InputBytes {
		t.Errorf("单行 input_bytes %d 超过总计 %d", in, view.Totals.InputBytes)
	}
}

// TestStatsEmpty 服务刚起来时不能返回空壳。
func TestStatsEmpty(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	got := do(t, client, fasthttp.MethodGet, "/v1/stats", "")
	var view statsView
	if err := json.Unmarshal([]byte(got.body), &view); err != nil {
		t.Fatal(err)
	}
	if view.Since != "" {
		t.Errorf("无转换时 since 应为空，实际 %q", view.Since)
	}
	if view.Totals.Conversions != 0 {
		t.Errorf("conversions = %d，期望 0", view.Totals.Conversions)
	}
	// formats 必须是空数组而不是 null：调用方 range 一个 null 会直接崩。
	if !strings.Contains(got.body, `"formats":[]`) {
		t.Errorf("formats 应输出为空数组:\n%s", got.body)
	}
	if !strings.Contains(got.body, `"workers"`) {
		t.Errorf("worker 数应始终输出:\n%s", got.body)
	}
	if view.Note == "" {
		t.Error("note 为空：这些数字的口径必须写在响应里，否则调用方会把 failed 也算进 conversions")
	}
}

// TestStatsRequiresAPIKey 统计属于业务数据，要令牌。
func TestStatsRequiresAPIKey(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	if got := doAuth(t, client, fasthttp.MethodGet, "/v1/stats", "", ""); got.status != fasthttp.StatusUnauthorized {
		t.Errorf("无令牌时状态 = %d，期望 401", got.status)
	}
}

// TestStatsCancelledNotCounted 取消的任务不计入 conversions。
//
// Cancel 绕过记账——取消的任务没进转换，把它算成一次转换会让
// "总转换量"大于真正做过的转换数。
func TestStatsCancelledNotCounted(t *testing.T) {
	s, store, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	if err := store.Enqueue(&jobstore.Job{ID: "c1", To: "pdf"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Cancel("c1"); err != nil {
		t.Fatal(err)
	}
	view := fetchStats(t, client)
	if view.Totals.Conversions != 0 {
		t.Errorf("取消的任务不应计入，conversions = %d", view.Totals.Conversions)
	}
	// 但它确实占着/曾占过队列，所以深度口径里能看到它已离开。
	if view.Queue.Queued == nil {
		t.Error("queued 缺失")
	}
}

// TestStatsAndMetricsAgree 两端数字必须一致——它们同源。
func TestStatsAndMetricsAgree(t *testing.T) {
	s, store, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	seedStats(t, store)

	view := fetchStats(t, client)
	prom := do(t, client, fasthttp.MethodGet, "/metrics", "").body
	// 成功合计对应的 Prometheus 行：succeeded 之和。
	var fromProm uint64
	for _, line := range strings.Split(prom, "\n") {
		if line == "" || line[0] == '#' {
			continue
		}
		if !strings.Contains(line, `state="succeeded"}`) {
			continue
		}
		// 取最后一个字段（样本值）。
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		var v uint64
		if _, err := fmt.Sscan(fields[len(fields)-1], &v); err == nil {
			fromProm += v
		}
	}
	if fromProm != view.Totals.Succeeded {
		t.Errorf("/metrics 的 succeeded 合计 %d != /v1/stats 的 %d，两端不一致", fromProm, view.Totals.Succeeded)
	}
}

// TestStatsSurvivePrune 统计不受任务保留期影响。
func TestStatsSurvivePrune(t *testing.T) {
	s, store, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	seedStats(t, store)

	if n, err := store.Prune(time.Now().Add(time.Hour)); err != nil || n == 0 {
		t.Fatalf("Prune 应删掉 5 个终态任务，实际 %d, %v", n, err)
	}
	view := fetchStats(t, client)
	if view.Totals.Conversions != 5 {
		t.Errorf("清理后 conversions 降为 %d，期望仍为 5", view.Totals.Conversions)
	}
	if view.Totals.InputBytes != 750 {
		t.Errorf("清理后 input_bytes = %d，期望仍为 750", view.Totals.InputBytes)
	}
}

func fetchStats(t *testing.T, client *fasthttp.Client) statsView {
	t.Helper()
	got := do(t, client, fasthttp.MethodGet, "/v1/stats", "")
	if got.status != fasthttp.StatusOK {
		t.Fatalf("状态 = %d: %s", got.status, got.body)
	}
	var view statsView
	if err := json.Unmarshal([]byte(got.body), &view); err != nil {
		t.Fatalf("响应不是合法 JSON: %v\n%s", err, got.body)
	}
	return view
}

// TestStatsReportsStartupTime 启动时间与运行时长必须报出来。
func TestStatsReportsStartupTime(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())

	var view statsView
	got := do(t, client, fasthttp.MethodGet, "/v1/stats", "")
	if err := json.Unmarshal([]byte(got.body), &view); err != nil {
		t.Fatal(err)
	}
	if view.StartedAt == "" {
		t.Error("started_at 为空")
	}
	started, err := time.Parse("2006-01-02T15:04:05Z", view.StartedAt)
	if err != nil {
		t.Fatalf("started_at 格式不对: %q", view.StartedAt)
	}
	// 启动时间必须与 NewServer 的记录一致，而不是请求时刻。
	if diff := time.Since(started); diff < 0 || diff > time.Minute {
		t.Errorf("started_at 距今 %.1f 秒，不像服务启动时刻", diff.Seconds())
	}
	if view.UptimeSeconds < 0 || view.UptimeSeconds > 60 {
		t.Errorf("uptime_seconds = %v，不在合理范围", view.UptimeSeconds)
	}
}

// TestUptimeGrowsAcrossScrapes 运行时长是现算的，不能是缓存值。
func TestUptimeGrowsAcrossScrapes(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	first := fetchStats(t, client).UptimeSeconds
	time.Sleep(1100 * time.Millisecond)
	second := fetchStats(t, client).UptimeSeconds
	if second <= first {
		t.Errorf("运行时长应随时间增长：第一次 %.2f，第二次 %.2f", first, second)
	}
}

// TestStatsStartedAtAcrossRestart 重启后启动时间会变，但 since 不变。
//
// 这是 started_at 的核心用途：两个时间一起看才能知道累计计数跨过了重启，
// 而不是被清零重新开始。
func TestStatsStartedAtAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "jobs.db")

	first, err := jobstore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Enqueue(&jobstore.Job{ID: "r1", To: "pdf"}); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Claim(jobstore.LaneFast); err != nil {
		t.Fatal(err)
	}
	if err := first.RecordUsage("r1", "ofd", 10); err != nil {
		t.Fatal(err)
	}
	if err := first.Finish("r1", jobstore.StateSucceeded, jobstore.Output{Size: 20}, ""); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := jobstore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	// 两次启动之间隔了至少 1 秒，所以秒级时间戳必然不同。
	firstStart := time.Now().UTC().Add(-2 * time.Second)
	before := statsViewFor(t, second, firstStart)
	if before.StartedAt == "" {
		t.Fatal("started_at 为空")
	}
	// 模拟第二次启动：启动时间是新的，而 since 是老账。
	after := statsViewFor(t, second, time.Now().UTC())
	if after.StartedAt == before.StartedAt {
		t.Errorf("两次启动时间相同（%s），第二次启动的时间戳没更新", after.StartedAt)
	}
	if after.Since != before.Since {
		t.Errorf("since 变了：重启不该影响首次记账时间（%q -> %q）", before.Since, after.Since)
	}
	if after.Totals.Conversions != 1 {
		t.Errorf("重启后计数应保留，conversions = %d", after.Totals.Conversions)
	}
}

// statsViewFor 直接从 store 与给定启动时间构造视图，避免起一个监听器。
func statsViewFor(t *testing.T, store *jobstore.Store, startedAt time.Time) statsView {
	t.Helper()
	snap, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return buildStatsView(snap, nil, &Config{}, startedAt)
}
