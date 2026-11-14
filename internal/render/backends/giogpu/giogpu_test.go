package giogpu

import (
	"errors"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
	tcanvas "github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/internal/thirdparty/canvas/renderers/rasterizer"
)

const (
	testPageWidth  = 20.0
	testPageHeight = 10.0
)

func TestBackendRegistered(t *testing.T) {
	if _, ok := drawing.LookupBackend(BackendName); !ok {
		t.Fatalf("后端 %q 未注册，已注册: %v", BackendName, drawing.BackendNames())
	}
}

func TestRasterizeSolidSceneMatchesCPU(t *testing.T) {
	requireGPU(t)
	scene := solidScene(t)
	gpu, err := Rasterize(scene, geom.DPMM(10))
	if err != nil {
		t.Fatalf("GPU 渲染失败: %v", err)
	}
	cpu := rasterizer.Draw(scene, tcanvas.Resolution(geom.DPMM(10)), tcanvas.DefaultColorSpace)

	if gpu.Bounds() != cpu.Bounds() {
		t.Fatalf("GPU 输出尺寸 %v 与 CPU %v 不一致", gpu.Bounds(), cpu.Bounds())
	}
	// 页面高 10mm、分辨率 10px/mm，红色矩形位于 (5,2)-(15,8)mm。
	// 设备坐标 y 向下，红色中心在 (100, 50)，四角为白色。
	probes := []struct {
		point image.Point
		want  color.RGBA
	}{
		{image.Point{X: 100, Y: 50}, color.RGBA{R: 220, G: 30, B: 30, A: 255}},
		{image.Point{X: 20, Y: 20}, color.RGBA{R: 255, G: 255, B: 255, A: 255}},
		{image.Point{X: 180, Y: 80}, color.RGBA{R: 255, G: 255, B: 255, A: 255}},
	}
	for _, probe := range probes {
		gpuColor := gpu.RGBAAt(probe.point.X, probe.point.Y)
		if !colorNear(gpuColor, probe.want, 2) {
			t.Errorf("GPU 在 %v 的颜色 %v，期望接近 %v", probe.point, gpuColor, probe.want)
		}
		cpuColor := cpu.RGBAAt(probe.point.X, probe.point.Y)
		if !colorNear(gpuColor, cpuColor, 2) {
			t.Errorf("GPU 在 %v 的颜色 %v 与 CPU %v 差异过大", probe.point, gpuColor, cpuColor)
		}
	}
}

func TestRasterizeConcurrent(t *testing.T) {
	requireGPU(t)
	scene := solidScene(t)
	if _, err := Rasterize(scene, geom.DPMM(2)); err != nil {
		t.Fatalf("GPU 渲染失败: %v", err)
	}
	const workers = 4
	errs := make(chan error, workers)
	for range workers {
		go func() {
			img, err := Rasterize(scene, geom.DPMM(2))
			if err == nil && !colorNear(img.RGBAAt(20, 10), color.RGBA{R: 220, G: 30, B: 30, A: 255}, 2) {
				err = errors.New("并发渲染的红色区域颜色错误")
			}
			errs <- err
		}()
	}
	for range workers {
		if err := <-errs; err != nil {
			t.Errorf("并发 GPU 渲染失败: %v", err)
		}
	}
}

func TestRasterizeReusesWindow(t *testing.T) {
	requireGPU(t)
	scene := solidScene(t)
	if _, err := Rasterize(scene, geom.DPMM(2)); err != nil {
		t.Fatalf("GPU 渲染失败: %v", err)
	}
	gpuMu.Lock()
	first := gpuWindows.window
	gpuMu.Unlock()
	if first == nil {
		t.Fatal("成功渲染后应缓存离屏窗口")
	}

	empty := tcanvas.New(testPageWidth, testPageHeight)
	img, err := Rasterize(empty, geom.DPMM(2))
	if err != nil {
		t.Fatalf("复用窗口渲染失败: %v", err)
	}
	// 复用窗口后帧内容必须被清空并重绘，不能残留上一帧。
	if got := img.RGBAAt(20, 10); got != (color.RGBA{}) {
		t.Errorf("空场景残留上一帧内容: %v", got)
	}
	gpuMu.Lock()
	reused := gpuWindows.window == first
	gpuMu.Unlock()
	if !reused {
		t.Error("相同像素尺寸的连续渲染应复用离屏窗口")
	}
}

func TestRasterizeRecreatesWindowForNewSize(t *testing.T) {
	requireGPU(t)
	scene := solidScene(t)
	if _, err := Rasterize(scene, geom.DPMM(2)); err != nil {
		t.Fatalf("GPU 渲染失败: %v", err)
	}
	gpuMu.Lock()
	first := gpuWindows.window
	gpuMu.Unlock()

	if _, err := Rasterize(scene, geom.DPMM(4)); err != nil {
		t.Fatalf("新尺寸渲染失败: %v", err)
	}
	gpuMu.Lock()
	second := gpuWindows.window
	gpuMu.Unlock()
	if second == nil {
		t.Fatal("新尺寸渲染后应缓存离屏窗口")
	}
	if second == first {
		t.Error("页面像素尺寸变化时应重建离屏窗口")
	}
}

func TestCloseReleasesWindow(t *testing.T) {
	requireGPU(t)
	scene := solidScene(t)
	if _, err := Rasterize(scene, geom.DPMM(2)); err != nil {
		t.Fatalf("GPU 渲染失败: %v", err)
	}
	Close()
	gpuMu.Lock()
	window := gpuWindows.window
	gpuMu.Unlock()
	if window != nil {
		t.Fatal("Close 后不应保留缓存的离屏窗口")
	}
	// Close 之后仍应能重建并继续渲染。
	if _, err := Rasterize(scene, geom.DPMM(2)); err != nil {
		t.Fatalf("Close 后重新渲染失败: %v", err)
	}
}

func TestBackendDrawAndRaster(t *testing.T) {
	t.Cleanup(Close)
	factory, ok := drawing.LookupBackend(BackendName)
	if !ok {
		t.Fatalf("后端 %q 未注册", BackendName)
	}
	backend, err := factory(testPageWidth, testPageHeight, geom.DPMM(10))
	if err != nil {
		t.Fatalf("创建 giogpu 后端失败: %v", err)
	}
	backend.SetFillColor(color.White)
	backend.DrawPath(0, 0, geom.Rectangle(testPageWidth, testPageHeight))
	backend.SetFillColor(color.RGBA{R: 220, G: 30, B: 30, A: 255})
	backend.DrawPath(5, 2, geom.Rectangle(10, 6))

	img := backend.Raster()
	if got, want := img.Bounds(), image.Rect(0, 0, 200, 100); got != want {
		t.Fatalf("输出尺寸 %v，期望 %v", got, want)
	}
	if got := img.RGBAAt(100, 50); !colorNear(got, color.RGBA{R: 220, G: 30, B: 30, A: 255}, 2) {
		t.Errorf("矩形中心颜色 %v，期望红色", got)
	}
	if got := img.RGBAAt(20, 20); !colorNear(got, color.RGBA{R: 255, G: 255, B: 255, A: 255}, 2) {
		t.Errorf("页面左上角颜色 %v，期望白色", got)
	}
}

// 只允许最小窗口创建失败时跳过；帧执行、回读及实际场景的失败必须暴露。
func requireGPU(tb testing.TB) {
	tb.Helper()
	tb.Cleanup(Close)
	if _, err := Rasterize(tcanvas.New(1, 1), geom.DPMM(1)); err != nil {
		if errors.Is(err, ErrGPUUnavailable) {
			tb.Skipf("无可用 GPU 环境: %v", err)
		}
		tb.Fatalf("GPU 探测帧失败: %v", err)
	}
}

func TestRasterizeRejectsInvalidInput(t *testing.T) {
	if _, err := Rasterize(nil, geom.DPMM(1)); err == nil {
		t.Error("空页面记录应返回错误")
	}
	if _, err := Rasterize(tcanvas.New(10, 10), 0); err == nil {
		t.Error("零分辨率应返回错误")
	}
	if _, err := Rasterize(tcanvas.New(0.01, 0.01), geom.DPMM(1)); err == nil {
		t.Error("不足 1 像素的页面应返回错误")
	}
}

func solidScene(tb testing.TB) *tcanvas.Canvas {
	tb.Helper()
	scene := tcanvas.New(testPageWidth, testPageHeight)
	ctx := tcanvas.NewContext(scene)
	ctx.SetFillColor(color.White)
	ctx.DrawPath(0, 0, tcanvas.Rectangle(testPageWidth, testPageHeight))
	ctx.SetFillColor(color.RGBA{R: 220, G: 30, B: 30, A: 255})
	ctx.DrawPath(5, 2, tcanvas.Rectangle(10, 6))
	return scene
}

func colorNear(a, b color.RGBA, tolerance uint8) bool {
	diff := func(x, y uint8) int {
		return int(math.Abs(float64(x) - float64(y)))
	}
	return diff(a.R, b.R) <= int(tolerance) &&
		diff(a.G, b.G) <= int(tolerance) &&
		diff(a.B, b.B) <= int(tolerance) &&
		diff(a.A, b.A) <= int(tolerance)
}
