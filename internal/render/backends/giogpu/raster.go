package giogpu

import (
	"errors"
	"fmt"
	"image"
	"sync"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	tcanvas "github.com/zc310/ofd/internal/thirdparty/canvas"
	giocanvas "github.com/zc310/ofd/internal/thirdparty/canvas/renderers/gio"

	"github.com/zc310/ofd/internal/render/geom"
)

var (
	// ErrGPUUnavailable 表示离屏窗口创建失败；保留底层错误供诊断。
	// 同一像素尺寸短暂缓存该错误，五秒后重试；Close 可立即清除失败缓存。
	ErrGPUUnavailable = errors.New("GPU 离屏窗口不可用")

	errEmptyScene        = errors.New("页面记录为空")
	errInvalidResolution = errors.New("无效的渲染分辨率")
	errSceneTooSmall     = errors.New("页面尺寸不足 1 像素")
)

// gpuMu 串行化离屏上下文的使用；在本机并发创建和释放 EGL 窗口曾导致
// 回读时 eglMakeCurrent 失败。
//
// gpuWindows 缓存最近一次成功使用的无头窗口。导出多页时页面尺寸通常不变，
// 尺寸变化时释放旧窗口后重建，缓存至多保留一个窗口。
// 同尺寸失败创建只短暂缓存，避免每页都尝试不可用的 GPU；换尺寸可立即重试，
// 不让超大页面的资源限制影响正常页面。帧执行和回读错误不缓存。
// gpuWindows 只在持有 gpuMu 时访问。
var (
	gpuMu      sync.Mutex
	gpuWindows windowCache
)

type windowCache struct {
	window  *headless.Window
	size    image.Point
	failure error
	retryAt time.Time
}

// acquire 返回指定像素尺寸的离屏窗口。进程级缓存的调用方必须持有 gpuMu。
// 工厂与时间显式传入，使失败重试可在无 GPU 环境中确定性验证。
func (c *windowCache) acquire(width, height int, now time.Time, create func(int, int) (*headless.Window, error)) (*headless.Window, error) {
	size := image.Pt(width, height)
	if c.failure != nil && c.size == size && now.Before(c.retryAt) {
		return nil, c.failure
	}
	if c.window != nil && c.size == size {
		return c.window, nil
	}
	c.release()
	window, err := create(width, height)
	if err != nil {
		c.size = size
		c.failure = fmt.Errorf("%w: %w", ErrGPUUnavailable, err)
		c.retryAt = now.Add(retryInterval)
		return nil, c.failure
	}
	c.window, c.size = window, size
	return window, nil
}

// discardWindow 丢弃执行失败的上下文，使下次调用重建，避免继续复用损坏的
// 窗口。调用方必须持有 gpuMu。
func discardWindow(window *headless.Window) {
	if gpuWindows.window == window {
		gpuWindows.release()
		return
	}
	window.Release()
}

// release 释放窗口并清除失败缓存。进程级缓存的调用方必须持有 gpuMu。
func (c *windowCache) release() {
	if c.window != nil {
		c.window.Release()
	}
	*c = windowCache{}
}

// Close 释放缓存的离屏上下文。之后仍可继续调用 Rasterize 重建窗口，
// 主要用于测试或需要显式归还 GPU 资源的长时间运行进程。
// 它同时清除窗口创建失败缓存，但不会清零 Stats 的累计诊断。
func Close() {
	gpuMu.Lock()
	defer gpuMu.Unlock()
	gpuWindows.release()
}

// Rasterize 用 Gio 无头 GPU 把 canvas 页面记录渲染为 RGBA。
//
// 输出像素尺寸与 canvas CPU 后端一致：宽高按毫米乘以分辨率后四舍五入。
// GPU 上下文创建、帧执行或回读失败时返回错误，调用方可以回退 CPU 后端。
// 窗口按像素尺寸复用，调用 Close 可释放缓存；并发调用会串行执行。
// 返回值遵循 Go 的 sRGB 预乘 RGBA 约定，可直接编码 PNG 或交给 image/draw。
func Rasterize(scene *tcanvas.Canvas, resolution geom.Resolution) (*image.RGBA, error) {
	if scene == nil {
		return nil, errEmptyScene
	}
	if resolution <= 0 {
		return nil, fmt.Errorf("%w: %v", errInvalidResolution, resolution)
	}
	dpmm := resolution.DPMM()
	width := int(scene.W*dpmm + 0.5)
	height := int(scene.H*dpmm + 0.5)
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("%w: %g×%gmm @ %v", errSceneTooSmall, scene.W, scene.H, resolution)
	}

	gpuMu.Lock()
	defer gpuMu.Unlock()

	window, err := gpuWindows.acquire(width, height, time.Now(), headless.NewWindow)
	if err != nil {
		return nil, fmt.Errorf("创建 GPU 离屏上下文失败: %w", err)
	}

	var ops op.Ops
	recordScene(&ops, scene, dpmm, width, height)

	if err := window.Frame(&ops); err != nil {
		discardWindow(window)
		return nil, fmt.Errorf("执行 GPU 帧失败: %w", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	if err := window.Screenshot(img); err != nil {
		discardWindow(window)
		return nil, fmt.Errorf("回读 GPU 像素失败: %w", err)
	}
	normalizeReadback(img)
	recordGPUFrame()
	return img, nil
}

// recordScene 每次重新记录只读页面，不按场景地址缓存，避免调用方修改场景后
// 重放旧内容。独立入口供基准拆分操作记录、帧提交和回读的耗时。
func recordScene(ops *op.Ops, scene *tcanvas.Canvas, dpmm float64, width, height int) {
	ops.Reset()
	gtx := layout.Context{
		Ops:         ops,
		Constraints: layout.Exact(image.Pt(width, height)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
	scale := float32(dpmm)
	transform := op.Affine(f32.NewAffine2D(scale, 0, 0, 0, scale, 0)).Push(ops)
	renderer := giocanvas.New(gtx, scene.W, scene.H)
	// giocanvas 以 1 画布单位（毫米）对应 1 像素记录；外层缩放负责映射到
	// 目标 DPI。原生渐变的色轴不会跟随外层缩放，需要显式补偿。
	renderer.SetGradientScale(scale)
	renderer.SetRasterResolution(tcanvas.Resolution(dpmm))
	scene.RenderTo(renderer)
	// 必须在提交帧之前弹出变换，否则 Pop 操作不会进入本帧记录。
	transform.Pop()
}
