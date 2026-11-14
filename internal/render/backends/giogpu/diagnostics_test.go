package giogpu

import (
	"errors"
	"image"
	"sync"
	"testing"
	"time"

	"gioui.org/gpu/headless"

	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
)

func TestWindowCreationFailureIsRetriedAfterCooldownAndPreservesCause(t *testing.T) {
	var cache windowCache
	cause := errors.New("测试窗口创建失败")
	attempts := 0
	create := func(int, int) (*headless.Window, error) {
		attempts++
		return nil, cause
	}
	now := time.Now()
	for _, at := range []time.Time{now, now.Add(retryInterval / 2), now.Add(retryInterval)} {
		if _, err := cache.acquire(20, 10, at, create); !errors.Is(err, cause) || !errors.Is(err, ErrGPUUnavailable) {
			t.Fatalf("窗口创建错误应保留底层原因和类型: %v", err)
		}
	}
	if attempts != 2 {
		t.Fatalf("冷却期应复用失败，过期后重试，实际尝试 %d 次", attempts)
	}
	// 尺寸变化应立即重试，不能让大页面的资源限制阻止正常页面。
	_, _ = cache.acquire(10, 5, now.Add(retryInterval), create)
	if attempts != 3 {
		t.Fatalf("新尺寸未立即重试，实际尝试 %d 次", attempts)
	}
	cache.release()
	if cache.failure != nil || !cache.retryAt.IsZero() || cache.size != (image.Point{}) {
		t.Fatalf("释放后应清除失败缓存: %+v", cache)
	}
	_, _ = cache.acquire(10, 5, now.Add(retryInterval), create)
	if attempts != 4 {
		t.Fatalf("释放后应立即重试，实际尝试 %d 次", attempts)
	}
}

func TestFallbackCountsEveryFailureButRateLimitsWarnings(t *testing.T) {
	var state diagnosticsState
	now := time.Now()
	first := errors.New("第一次失败")
	latest := errors.New("最近一次失败")
	if !state.fallback(first, now) {
		t.Error("首次回退应输出警告")
	}
	if state.fallback(latest, now.Add(time.Second)) {
		t.Error("冷却期内不应重复警告")
	}
	if !state.fallback(latest, now.Add(retryInterval)) {
		t.Error("冷却期结束后应重新警告")
	}
	if state.statistics.CPUFallbacks != 3 || !errors.Is(state.statistics.LastFallback, latest) {
		t.Fatalf("每次回退都应计数并记录最新原因: %+v", state.statistics)
	}
}

func TestBackendFallbackReportsCauseWithoutRequiringGPU(t *testing.T) {
	factory, ok := drawing.LookupBackend(drawing.BackendCanvas)
	if !ok {
		t.Fatal("canvas 后端未注册")
	}
	base, err := factory(10, 10, geom.DPMM(1))
	if err != nil {
		t.Fatal(err)
	}
	// 故意不给页面记录，严格入口应失败，而注册后端仍能 CPU 回退。
	backend := &gpuBackend{Backend: base, resolution: geom.DPMM(1)}
	before := Stats()
	img := backend.Raster()
	if img == nil || img.Bounds() != image.Rect(0, 0, 10, 10) {
		t.Fatal("回退未返回正确的 CPU 图像")
	}
	after := Stats()
	if after.CPUFallbacks != before.CPUFallbacks+1 || after.GPUFrames != before.GPUFrames || !errors.Is(after.LastFallback, errEmptyScene) {
		t.Fatalf("回退诊断错误: before=%+v after=%+v", before, after)
	}
	Close()
	if closed := Stats(); closed.CPUFallbacks != after.CPUFallbacks || !errors.Is(closed.LastFallback, errEmptyScene) {
		t.Fatalf("Close 不应清除累计诊断: %+v", closed)
	}
}

func TestStatsSupportsConcurrentFallbacksAndSnapshots(t *testing.T) {
	before := Stats()
	cause := errors.New("并发回退测试")
	const workers, calls = 8, 20
	var group sync.WaitGroup
	for range workers {
		group.Go(func() {
			for range calls {
				recordFallback(cause)
				_ = Stats()
			}
		})
	}
	group.Wait()
	after := Stats()
	if after.CPUFallbacks != before.CPUFallbacks+workers*calls || !errors.Is(after.LastFallback, cause) {
		t.Fatalf("并发回退丢失诊断记录: before=%+v after=%+v", before, after)
	}
}
