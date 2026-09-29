package main

import (
	"math"
	"sort"
	"time"

	"github.com/valyala/fasthttp"

	"github.com/zc310/ofd/internal/jobstore"
)

// handleStats 输出转换量统计的 JSON 视图。
//
// 与 /metrics 同源不同形：/metrics 是给 Prometheus 抓的，格式必须稳定；
// 这个是给人和 API 用的，可读性与易解析优先。两边读的是同一份 jobstore
// 计数，所以数字必然一致——想对账时两边比即可。
//
// 计数是累计值，不受 retention 影响：任务记录会被 Prune 清理，而这些计数
// 独立存在，因此"总转换量"真的是自服务启动以来的总量，不是"最近 N 天"。
func (s *Server) handleStats(ctx *fasthttp.RequestCtx) {
	snap, err := s.store.Snapshot()
	if err != nil {
		s.log.Error("读取统计失败", "err", err)
		writeError(ctx, fasthttp.StatusInternalServerError, "internal_error", "读取统计失败")
		return
	}
	depth, err := s.store.QueueDepth()
	if err != nil {
		// 队列深度是附加信息，读不到不该让转换量统计整个失败——
		// 调用方要的是那些累计数字，它们已经在手上了。
		s.log.Error("读取队列深度失败", "err", err)
		depth = nil
	}
	writeJSON(ctx, fasthttp.StatusOK, buildStatsView(snap, depth, s.cfg, s.startedAt))
}

// statsView 是 /v1/stats 的响应体。
type statsView struct {
	// StartedAt 是服务启动时刻，UptimeSeconds 是到本次抓取的运行时长。
	//
	// 两者和 Since 一起看才能判断计数的连续性：服务重启过但 Since 远早于
	// StartedAt，说明这些累计数字跨过了重启——不是清零的，也不是新的一段。
	StartedAt     string  `json:"started_at"`
	UptimeSeconds float64 `json:"uptime_seconds"`
	// Since 是第一次记账的时间。零值表示还没有过任何转换。
	Since string `json:"since,omitempty"`
	// Note 说明口径，避免调用方把这些数字当成别的东西。
	Note    string      `json:"note"`
	Totals  statsTotals `json:"totals"`
	Queue   statsQueue  `json:"queue"`
	Formats []formatRow `json:"formats"`
}

// statsTotals 是全部转换的合计。
type statsTotals struct {
	// Conversions 是到达终态且被计入的任务数。
	//
	// 不含被取消的任务：取消的任务没进转换，Cancel 也确实绕过了记账。
	// 所以 conversions + cancelled 不等于提交总数，提交总数服务端不留存。
	Conversions uint64 `json:"conversions"`
	Succeeded   uint64 `json:"succeeded"`
	Failed      uint64 `json:"failed"`
	// InputBytes 是输入字节总量。
	//
	// URL 输入的转换如果失败，这部分记为 0——失败时确实没拿到完整内容，
	// 所以这个数是"实际读入的字节"，不是"提交的字节"。
	InputBytes uint64 `json:"input_bytes"`
	// OutputBytes 是输出字节总量，多文件结果按各文件累加。
	OutputBytes uint64 `json:"output_bytes"`
}

// statsQueue 是抓取瞬间的队列状态。它是瞬时值，与上面的累计值不是一回事。
type statsQueue struct {
	// Queued 含处于重试退避中的任务：那些任务状态是 queued，但还没进通道队列。
	Queued  *int `json:"queued,omitempty"`
	Running *int `json:"running,omitempty"`
	// Workers 是各通道配置的并发数，用来把 Running 换算成利用率。
	Workers map[string]int `json:"workers"`
}

// formatRow 是一组 输入格式 -> 输出格式 的统计。
type formatRow struct {
	From string `json:"from"`
	To   string `json:"to"`
	// LabelUnknown 为真时 From 是 "unknown"，即失败任务——转换没跑到判定
	// 格式那步，因此不知道真实输入是什么。调用方需要能把它与真实格式区分开。
	LabelUnknown bool   `json:"label_unknown,omitempty"`
	Conversions  uint64 `json:"conversions"`
	Succeeded    uint64 `json:"succeeded"`
	Failed       uint64 `json:"failed"`
	InputBytes   uint64 `json:"input_bytes"`
	OutputBytes  uint64 `json:"output_bytes"`
}

func buildStatsView(snap jobstore.StatsSnapshot, depth map[jobstore.State]int, cfg *Config, startedAt time.Time) statsView {
	view := statsView{
		Note: "计数为累计值，不受任务保留期影响；from 用服务端判定的实际格式，" +
			"失败任务记为 unknown；已取消的任务不计入 conversions。",
		Totals:  statsTotals{},
		Queue:   statsQueue{Workers: map[string]int{}},
		Formats: []formatRow{},
	}
	// 运行时长在每次抓取时现算，不缓存：缓存下来的数字在响应里就是错的。
	if !startedAt.IsZero() {
		view.StartedAt = startedAt.Format("2006-01-02T15:04:05Z")
		if uptime := time.Since(startedAt).Seconds(); uptime >= 0 {
			view.UptimeSeconds = math.Round(uptime*100) / 100
		}
	}
	if !snap.Since.IsZero() {
		view.Since = snap.Since.UTC().Format("2006-01-02T15:04:05Z")
	}
	for _, bytes := range snap.InputBytes {
		view.Totals.InputBytes += bytes
	}
	for _, bytes := range snap.OutputBytes {
		view.Totals.OutputBytes += bytes
	}

	// 按 (from, to) 归并。Jobs 的三个标签里 state 只有 succeeded 与 failed
	// 两种（取消的不记账），所以行内是二者之和。
	rows := make(map[jobstore.StatsPair]*formatRow)
	for triple, count := range snap.Jobs {
		pair := jobstore.StatsPair{From: triple.From, To: triple.To}
		row, ok := rows[pair]
		if !ok {
			row = &formatRow{From: triple.From, To: triple.To,
				LabelUnknown: triple.From == "unknown"}
			rows[pair] = row
		}
		row.Conversions += count
		view.Totals.Conversions += count
		switch triple.State {
		case string(jobstore.StateFailed):
			row.Failed += count
			view.Totals.Failed += count
		default:
			row.Succeeded += count
			view.Totals.Succeeded += count
		}
	}
	view.Formats = make([]formatRow, 0, len(rows))
	for _, row := range rows {
		// 输入字节只按 from 索引。把它摊到该 from 的每一行上会让多输出
		// 格式的行重复计数，各行相加对不上 totals 里的合计数——
		// 这一列要理解成"该输入格式的总输入量"，不是这一条转换路径的量。
		row.InputBytes = snap.InputBytes[row.From]
		row.OutputBytes = snap.OutputBytes[jobstore.StatsPair{From: row.From, To: row.To}]
		view.Formats = append(view.Formats, *row)
	}
	// 排序：未知的排最后（它不是一种格式），其余按 from、to 字典序。
	sort.Slice(view.Formats, func(i, j int) bool {
		a, b := view.Formats[i], view.Formats[j]
		if a.LabelUnknown != b.LabelUnknown {
			return b.LabelUnknown
		}
		if a.From != b.From {
			return a.From < b.From
		}
		return a.To < b.To
	})

	if depth != nil {
		queued, running := depth[jobstore.StateQueued], depth[jobstore.StateRunning]
		view.Queue.Queued = &queued
		view.Queue.Running = &running
	}
	if cfg != nil {
		view.Queue.Workers = map[string]int{
			"fast": cfg.FastWorkers, "heavy": cfg.HeavyWorkers, "notify": cfg.NotifyWorkers,
		}
	}
	return view
}
