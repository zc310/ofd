// Package observe 收集转换耗时与通知投递结果，并渲染成 Prometheus 文本。
//
// 这里没有引入 client_golang：服务只需要两组有界基数的指标，而 client_golang
// 会带来 protobuf 与整套 collector 依赖。标签值全部来自有限集合（通道、格式、
// 终态、目标名），不会随任务数增长。
package observe

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// 通知投递结果标签。
const (
	ResultSuccess = "success"
	ResultFailure = "failure"
)

// Conversion 是一次转换的标签集合。
type Conversion struct {
	Lane  string
	From  string
	To    string
	State string
}

// Notification 是一次通知投递尝试的标签集合。
type Notification struct {
	Target string
	Event  string
	Result string
}

// durationBuckets 与 Prometheus 常用的 http 请求耗时桶对齐，覆盖毫秒级
// 文本转换到几分钟的 Office/Chrome 转换。
var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300}

type histogram struct {
	counts []uint64
	count  uint64
	sum    float64
}

// Registry 并发安全地保存两类指标。
type Registry struct {
	mu            sync.Mutex
	conversions   map[Conversion]*histogram
	notifications map[Notification]uint64
}

// NewRegistry 构造空指标注册表。
func NewRegistry() *Registry {
	return &Registry{
		conversions:   make(map[Conversion]*histogram),
		notifications: make(map[Notification]uint64),
	}
}

// ObserveConversion 记录一次已到达终态的转换耗时。
func (r *Registry) ObserveConversion(labels Conversion, d time.Duration) {
	if r == nil {
		return
	}
	if d < 0 {
		d = 0
	}
	seconds := d.Seconds()
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.conversions[labels]
	if h == nil {
		h = &histogram{counts: make([]uint64, len(durationBuckets)+1)}
		r.conversions[labels] = h
	}
	h.count++
	h.sum += seconds
	h.counts[bucketIndex(seconds)]++
}

func bucketIndex(seconds float64) int {
	index := sort.SearchFloat64s(durationBuckets, seconds)
	if index >= len(durationBuckets) {
		return len(durationBuckets)
	}
	return index
}

// ObserveNotification 记录一次通知投递尝试的结果。
func (r *Registry) ObserveNotification(labels Notification) {
	if r == nil {
		return
	}
	if labels.Result == "" {
		labels.Result = ResultFailure
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notifications[labels]++
}

// WritePrometheus 把指标写入 b。
func (r *Registry) WritePrometheus(b *strings.Builder) {
	if r == nil {
		return
	}
	r.mu.Lock()
	conversions := make(map[Conversion]histogram, len(r.conversions))
	for labels, h := range r.conversions {
		copied := histogram{counts: append([]uint64(nil), h.counts...), count: h.count, sum: h.sum}
		conversions[labels] = copied
	}
	notifications := make(map[Notification]uint64, len(r.notifications))
	for labels, count := range r.notifications {
		notifications[labels] = count
	}
	r.mu.Unlock()

	b.WriteString("# HELP ofd_conversion_duration_seconds 转换耗时（秒），按通道、实际输入格式、输出格式与终态分类。\n")
	b.WriteString("# TYPE ofd_conversion_duration_seconds histogram\n")
	keys := make([]Conversion, 0, len(conversions))
	for labels := range conversions {
		keys = append(keys, labels)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.Lane != b.Lane {
			return a.Lane < b.Lane
		}
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.State < b.State
	})
	for _, labels := range keys {
		h := conversions[labels]
		cumulative := uint64(0)
		for i, bound := range durationBuckets {
			cumulative += h.counts[i]
			fmt.Fprintf(b, "ofd_conversion_duration_seconds_bucket{%s,le=%s} %d\n",
				conversionLabels(labels), quote(formatFloat(bound)), cumulative)
		}
		cumulative += h.counts[len(durationBuckets)]
		fmt.Fprintf(b, "ofd_conversion_duration_seconds_bucket{%s,le=\"+Inf\"} %d\n",
			conversionLabels(labels), cumulative)
		fmt.Fprintf(b, "ofd_conversion_duration_seconds_sum{%s} %g\n", conversionLabels(labels), h.sum)
		fmt.Fprintf(b, "ofd_conversion_duration_seconds_count{%s} %d\n", conversionLabels(labels), h.count)
	}

	b.WriteString("# HELP ofd_notifications_total 通知投递尝试次数，按目标、事件与结果分类。\n")
	b.WriteString("# TYPE ofd_notifications_total counter\n")
	notifyKeys := make([]Notification, 0, len(notifications))
	for labels := range notifications {
		notifyKeys = append(notifyKeys, labels)
	}
	sort.Slice(notifyKeys, func(i, j int) bool {
		a, b := notifyKeys[i], notifyKeys[j]
		if a.Target != b.Target {
			return a.Target < b.Target
		}
		if a.Event != b.Event {
			return a.Event < b.Event
		}
		return a.Result < b.Result
	})
	for _, labels := range notifyKeys {
		fmt.Fprintf(b, "ofd_notifications_total{target=%s,event=%s,result=%s} %d\n",
			quote(labels.Target), quote(labels.Event), quote(labels.Result), notifications[labels])
	}
}

func conversionLabels(labels Conversion) string {
	return fmt.Sprintf("lane=%s,from=%s,to=%s,state=%s",
		quote(labels.Lane), quote(labels.From), quote(labels.To), quote(labels.State))
}

func quote(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`)
	return `"` + replacer.Replace(value) + `"`
}

func formatFloat(value float64) string {
	return fmt.Sprintf("%g", value)
}
