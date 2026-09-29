package main

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/valyala/fasthttp"

	"github.com/zc310/ofd/internal/jobstore"
)

// Prometheus 文本格式版本。改动这个值意味着所有采集端要同步升级解析器。
const prometheusTextVersion = "0.0.4"

// promContentType 是 Prometheus 抓取时默认接受的类型。
//
// 不能用 text/plain：采集端会先按 Content-Type 判断，`text/plain` 不在白名单里
// 就直接拒绝这个目标，而不是降级解析。charset 参数同样要带上。
const promContentType = "text/plain; version=" + prometheusTextVersion + "; charset=utf-8"

// handleMetrics 输出 Prometheus 抓取用的累计计数。
//
// 标签只有 from、to、state 三个有限集合值，是刻意的约束。基数一旦随任务数
// 或文件名增长，就是几百万条 series——那是把 Prometheus 和本服务一起拖垮的
// 标准做法。单任务的明细在 jobstore 里，/metrics 只负责聚合。
func (s *Server) handleMetrics(ctx *fasthttp.RequestCtx) {
	snap, err := s.store.Snapshot()
	if err != nil {
		s.log.Error("读取统计失败", "err", err)
		writeError(ctx, fasthttp.StatusInternalServerError, "internal_error", "读取统计失败")
		return
	}
	// 队列深度是 gauge，每次抓取现算。读失败时仍然输出累计计数与运行时指标：
	// 少几个 gauge 比整个端点 500 更有用——采集端能拿到历史数据补上这段时间，
	// 端点失败则是一次空洞。
	depth, err := s.store.QueueDepth()
	if err != nil {
		s.log.Error("读取队列深度失败", "err", err)
		depth = nil
	}
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetContentType(promContentType)
	ctx.SetBody(renderMetrics(snap, depth, s.cfg.FastWorkers, s.cfg.HeavyWorkers, s.cfg.NotifyWorkers))
}

// renderMetrics 渲染 Prometheus 文本格式。
//
// 格式要求先声明 HELP/TYPE 再输出样本，且同名指标的 HELP/TYPE 只能出现一次。
// 样本行按标签值排序输出，让输出稳定可 diff——不稳定的顺序会让每次抓取的
// 字节级对比、缓存与人工排查都变得困难。
func renderMetrics(snap jobstore.StatsSnapshot, depth map[jobstore.State]int, fast, heavy, notifyWorkers int) []byte {
	var b strings.Builder
	b.Grow(1024)

	b.WriteString("# HELP ofd_conversions_total 到达终态的转换任务数，按实际输入格式、输出格式与终态分类。\n")
	b.WriteString("# TYPE ofd_conversions_total counter\n")
	for _, t := range sortedTriples(snap.Jobs) {
		count := snap.Jobs[t]
		fmt.Fprintf(&b, "ofd_conversions_total{from=%s,to=%s,state=%s} %d\n",
			promLabel(t.From), promLabel(t.To), promLabel(t.State), count)
	}

	b.WriteString("# HELP ofd_input_bytes_total 输入字节总量，按实际输入格式分类。\n")
	b.WriteString("# TYPE ofd_input_bytes_total counter\n")
	for _, from := range sortedKeys(snap.InputBytes) {
		fmt.Fprintf(&b, "ofd_input_bytes_total{from=%s} %d\n",
			promLabel(from), snap.InputBytes[from])
	}

	b.WriteString("# HELP ofd_output_bytes_total 输出字节总量，按实际输入格式与输出格式分类。\n")
	b.WriteString("# TYPE ofd_output_bytes_total counter\n")
	for _, p := range sortedPairs(snap.OutputBytes) {
		fmt.Fprintf(&b, "ofd_output_bytes_total{from=%s,to=%s} %d\n",
			promLabel(p.From), promLabel(p.To), snap.OutputBytes[p])
	}

	b.WriteString("# HELP ofd_queue_depth 未终结的任务数量。queued 包含处于重试退避中的任务。\n")
	b.WriteString("# TYPE ofd_queue_depth gauge\n")
	// depth 为 nil 表示这次抓取读队列深度失败，整组 gauge 不输出。
	// 输出缺失与 0 在 Prometheus 里是两件事：前者显示 no data，后者会被
	// 读成"队列是空的"，在告警里等于谎报服务健康。
	if depth != nil {
		for _, state := range []jobstore.State{jobstore.StateQueued, jobstore.StateRunning} {
			fmt.Fprintf(&b, "ofd_queue_depth{state=%s} %d\n", promLabel(string(state)), depth[state])
		}
	}

	b.WriteString("# HELP ofd_workers 各通道的 worker 数量。用于把 ofd_queue_depth 换算成利用率。\n")
	b.WriteString("# TYPE ofd_workers gauge\n")
	for _, w := range []struct {
		lane  string
		count int
	}{{"fast", fast}, {"heavy", heavy}, {"notify", notifyWorkers}} {
		fmt.Fprintf(&b, "ofd_workers{lane=%s} %d\n", promLabel(w.lane), w.count)
	}

	b.WriteString(renderRuntimeMetrics())
	return []byte(b.String())
}

// promLabel 转义标签值。
//
// 格式要求值内的 \、换行与双引号必须转义。没做这一步的话，一个含反斜杠的
// 文件名就能把样本行截断，Prometheus 会把后面的内容当成新指标解析——报出来的
// 错误往往指向完全无关的指标名。
func promLabel(value string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		"\n", `\n`,
		`"`, `\"`,
	)
	return `"` + replacer.Replace(value) + `"`
}

func sortedTriples(m map[jobstore.StatsTriple]uint64) []jobstore.StatsTriple {
	keys := make([]jobstore.StatsTriple, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].From != keys[j].From {
			return keys[i].From < keys[j].From
		}
		if keys[i].To != keys[j].To {
			return keys[i].To < keys[j].To
		}
		return keys[i].State < keys[j].State
	})
	return keys
}

func sortedKeys(m map[string]uint64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedPairs(m map[jobstore.StatsPair]uint64) []jobstore.StatsPair {
	keys := make([]jobstore.StatsPair, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].From != keys[j].From {
			return keys[i].From < keys[j].From
		}
		return keys[i].To < keys[j].To
	})
	return keys
}

// renderRuntimeMetrics 输出进程自身的运行时指标。
//
// 不用 client_golang：那会拉进 prometheus/common、client_model 与 protobuf
// 一整条传递依赖，而这个服务只需要十几个数。指标名刻意与 client_golang 的
// go collector 完全一致（go_memstats_*、go_goroutines、process_*），这样
// 现成的 Grafana Go 面板可以直接用，不用改查询。
//
// 这些指标在容器里比裸机更有价值：node_exporter 给的是宿主机视角，而容器
// 被 OOM kill 看的是 cgroup 限额对应的那个进程的 RSS。宿主机监控再完整也
// 看不到单个容器进程占了多少堆。
func renderRuntimeMetrics() string {
	var b strings.Builder
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)

	// 计数类。
	b.WriteString("# HELP go_goroutines 当前 goroutine 数量。\n")
	b.WriteString("# TYPE go_goroutines gauge\n")
	fmt.Fprintf(&b, "go_goroutines %d\n", runtime.NumGoroutine())

	// 线程数从 /proc 读而不是用 GOMAXPROCS：后者是调度器的并行度上限，
	// 与操作系统线程数不是一回事。取错这个值会让面板上的线程数一直是个
	// 固定常数，看着像没泄漏。
	if threads, ok := procThreads(); ok {
		b.WriteString("# HELP go_threads 当前 OS 线程数量。\n")
		b.WriteString("# TYPE go_threads gauge\n")
		fmt.Fprintf(&b, "go_threads %d\n", threads)
	}

	// 内存。用 gauge 而非 counter：这些是当前水位，不是累计量。
	// 命名与 client_golang 对齐，便于复用既有面板。
	mem := []struct {
		name  string
		help  string
		value uint64
	}{
		{"go_memstats_heap_alloc_bytes", "已分配且仍在使用的堆字节数。", stats.HeapAlloc},
		{"go_memstats_heap_inuse_bytes", "已分配且正在使用的堆 span 字节数。", stats.HeapInuse},
		{"go_memstats_heap_sys_bytes", "从操作系统申请的堆字节数。", stats.HeapSys},
		{"go_memstats_stack_inuse_bytes", "goroutine 栈占用的字节数。", stats.StackInuse},
		{"go_memstats_sys_bytes", "从操作系统申请的总字节数。", stats.Sys},
	}
	for _, m := range mem {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n%s %d\n", m.name, m.help, m.name, m.name, m.value)
	}

	// 累计分配量：GC 压力与内存泄漏的观察点。配合 heap_inuse 一起看——
	// heap 平稳而 alloc_total 猛涨，是分配churn 而非泄漏。
	b.WriteString("# HELP go_memstats_alloc_bytes_total 进程启动以来累计分配的堆字节数。\n")
	b.WriteString("# TYPE go_memstats_alloc_bytes_total counter\n")
	fmt.Fprintf(&b, "go_memstats_alloc_bytes_total %d\n", stats.TotalAlloc)

	b.WriteString("# HELP go_memstats_gc_count 垃圾回收总次数。\n")
	b.WriteString("# TYPE go_memstats_gc_count counter\n")
	fmt.Fprintf(&b, "go_memstats_gc_count %d\n", stats.NumGC)

	b.WriteString("# HELP go_gc_pause_seconds 最近一次垃圾回收的暂停秒数。\n")
	b.WriteString("# TYPE go_gc_pause_seconds gauge\n")
	// PauseNs 是环形缓冲，索引 0 是最近一次。
	fmt.Fprintf(&b, "go_gc_pause_seconds %g\n", float64(stats.PauseNs[0])/1e9)

	b.WriteString("# HELP go_info Go 运行时版本信息，值恒为 1。\n")
	b.WriteString("# TYPE go_info gauge\n")
	fmt.Fprintf(&b, "go_info{version=%s} 1\n", promLabel(runtime.Version()))

	b.WriteString(renderProcessMetrics())
	return b.String()
}

// renderProcessMetrics 从 /proc 读进程级指标。
//
// 全部包在容错里：这些是 Linux 专有的，/proc 读不到时直接不输出该指标，而不是
// 让整个 /metrics 失败。输出缺失的指标与值为 0 在 Prometheus 里是两件事，
// 前者会被面板显示成 "no data" 而不是"资源用光了"。
func renderProcessMetrics() string {
	var b strings.Builder

	if rss, vsize, ok := procMem(); ok {
		b.WriteString("# HELP process_resident_memory_bytes 进程常驻内存字节数。容器被 OOM kill 判的就是这个值。\n")
		b.WriteString("# TYPE process_resident_memory_bytes gauge\n")
		fmt.Fprintf(&b, "process_resident_memory_bytes %d\n", rss)
		b.WriteString("# HELP process_virtual_memory_bytes 进程虚拟内存字节数。\n")
		b.WriteString("# TYPE process_virtual_memory_bytes gauge\n")
		fmt.Fprintf(&b, "process_virtual_memory_bytes %d\n", vsize)
	}

	// 打开的 fd 数：每个 FTP/S3/WebDAV/SFTP 连接占一个，持续上涨说明连接
	// 没被关闭。配合 process_max_fds 看比值，比看绝对值更容易发现泄漏。
	if open, ok := procOpenFDs(); ok {
		b.WriteString("# HELP process_open_fds 进程当前打开的文件描述符数量。\n")
		b.WriteString("# TYPE process_open_fds gauge\n")
		fmt.Fprintf(&b, "process_open_fds %d\n", open)
	}
	if maxFDs, ok := procMaxFDs(); ok {
		b.WriteString("# HELP process_max_fds 进程允许打开的文件描述符上限。\n")
		b.WriteString("# TYPE process_max_fds gauge\n")
		fmt.Fprintf(&b, "process_max_fds %d\n", maxFDs)
	}

	// 启动时间：没有它，重启之后 process_resident_memory_bytes 这类 gauge
	// 的 rate() 会把重启前后拼成一段，算出根本不存在的速率。
	if start, ok := procStartTime(); ok {
		b.WriteString("# HELP process_start_time_seconds 进程启动的 Unix 时间戳。\n")
		b.WriteString("# TYPE process_start_time_seconds gauge\n")
		fmt.Fprintf(&b, "process_start_time_seconds %g\n", start)
	}
	return b.String()
}

// clockTicks 是 Linux 的 USER_HZ。
//
// /proc/self/stat 的 starttime 字段以时钟 tick 为单位，而 Go 没有不依赖 cgo
// 的 CLK_TCK 查询接口。Linux 上除少数老架构外 USER_HZ 恒为 100，
// client_golang 的 process collector 也用同一个常数。
const clockTicks = 100

// pageSize 是 /proc/self/statm 换算字节数用的页大小。
const pageSize = 4096

func procMem() (rss, vsize uint64, ok bool) {
	raw, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, 0, false
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 2 {
		return 0, 0, false
	}
	pages, err1 := strconv.ParseUint(fields[0], 10, 64)
	resident, err2 := strconv.ParseUint(fields[1], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return resident * pageSize, pages * pageSize, true
}

func procOpenFDs() (uint64, bool) {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return 0, false
	}
	return uint64(len(entries)), true
}

func procThreads() (uint64, bool) {
	raw, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "Threads:") {
			continue
		}
		value, err := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, "Threads:")), 10, 64)
		if err != nil {
			return 0, false
		}
		return value, true
	}
	return 0, false
}

func procMaxFDs() (uint64, bool) {
	raw, err := os.ReadFile("/proc/self/limits")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		// 形如 "Max open files            1048576              1048576              files"
		if !strings.HasPrefix(line, "Max open files") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			// "unlimited" 形式的软限制只有一个值列，此时视为无上限。
			return 0, false
		}
		value, err := strconv.ParseUint(fields[3], 10, 64)
		if err != nil {
			return 0, false
		}
		return value, true
	}
	return 0, false
}

func procStartTime() (float64, bool) {
	raw, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, false
	}
	// comm 字段（进程名）可能含空格与括号，不能按空白简单切分。要从
	// 最后一个 ')' 之后开始数：第 22 个字段 starttime 对应切分后的第 20 项。
	text := string(raw)
	end := strings.LastIndex(text, ")")
	if end < 0 {
		return 0, false
	}
	fields := strings.Fields(text[end+1:])
	// fields[0] 是 state，其后 starttime 是第 20 项（1-based 第 22 项）。
	if len(fields) < 20 {
		return 0, false
	}
	ticks, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0, false
	}
	raw2, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(raw2), "\n") {
		if !strings.HasPrefix(line, "btime ") {
			continue
		}
		btime, err := strconv.ParseUint(strings.TrimPrefix(line, "btime "), 10, 64)
		if err != nil {
			return 0, false
		}
		return float64(btime) + float64(ticks)/clockTicks, true
	}
	return 0, false
}
