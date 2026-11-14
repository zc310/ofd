package giogpu

import (
	"log/slog"
	"sync"
	"time"
)

// Statistics 是进程级诊断快照。计数从进程启动累计，Close 不清零。
// 并发任务共享计数，不能用两个快照的差值归属某个特定任务。
type Statistics struct {
	// GPUFrames 是 Rasterize 成功完成回读的次数，含后端和直接调用。
	GPUFrames uint64
	// CPUFallbacks 是注册后端因 GPU 错误改用 canvas CPU 的次数。
	CPUFallbacks uint64
	// LastFallback 是最近一次 CPU 回退原因；没有回退时为 nil。
	LastFallback error
}

const retryInterval = 5 * time.Second

type diagnosticsState struct {
	sync.Mutex

	statistics  Statistics
	lastWarning time.Time
}

var diagnostics diagnosticsState

// Stats 返回并发安全的诊断快照，不输出日志、不重置计数。
func Stats() Statistics {
	diagnostics.Lock()
	defer diagnostics.Unlock()
	return diagnostics.statistics
}

func recordGPUFrame() {
	diagnostics.Lock()
	defer diagnostics.Unlock()
	diagnostics.statistics.GPUFrames++
}

// fallback 为每次回退记账，但限制警告频率，避免无 GPU 环境下多页导出刷屏。
func (d *diagnosticsState) fallback(err error, now time.Time) bool {
	d.Lock()
	defer d.Unlock()
	d.statistics.CPUFallbacks++
	d.statistics.LastFallback = err
	if !d.lastWarning.IsZero() && now.Sub(d.lastWarning) < retryInterval {
		return false
	}
	d.lastWarning = now
	return true
}

func recordFallback(err error) {
	if diagnostics.fallback(err, time.Now()) {
		slog.Warn("giogpu 光栅化失败，回退 canvas CPU 后端", "error", err)
	}
}
