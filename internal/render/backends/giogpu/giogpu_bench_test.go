package giogpu

import (
	"bytes"
	"fmt"
	"image"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/op"

	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/testscene"
	tcanvas "github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/internal/thirdparty/canvas/renderers/rasterizer"
)

// benchmarkScene 构造与后端一致性测试共用的 200×140mm 页面：包含背景、
// 图片、曲线描边、EvenOdd 路径、渐变、布尔裁剪、文字轮廓、虚线与离屏图。
func benchmarkScene(b *testing.B) *tcanvas.Canvas {
	b.Helper()
	font := testscene.PickTestFont()
	if font == "" {
		b.Skip("缺少测试字体")
	}
	scene := tcanvas.New(200, 140)
	ctx := tcanvas.NewContext(scene)
	testscene.SceneDirect(b, ctx, font)
	return scene
}

// BenchmarkRasterizeSceneGPUStages 分离 CPU 操作记录、GPU 帧提交、同步回读
// 和 RGBA 归一化。Frame 包含驱动工作，不能把它等同于纯 GPU 内核执行时间。
// readback 复用目标缓冲，排除输出图像分配；总耗时仍以完整 Rasterize 基准为准。
func BenchmarkRasterizeSceneGPUStages(b *testing.B) {
	requireGPU(b)
	scene := benchmarkScene(b)
	for _, dpi := range []float64{96, 150, 300} {
		b.Run(fmt.Sprintf("%.0fdpi", dpi), func(b *testing.B) {
			dpmm := geom.DPI(dpi).DPMM()
			width, height := int(scene.W*dpmm+0.5), int(scene.H*dpmm+0.5)
			gpuMu.Lock()
			defer gpuMu.Unlock()
			window, err := gpuWindows.acquire(width, height, time.Now(), headless.NewWindow)
			if err != nil {
				b.Fatal(err)
			}
			var ops op.Ops
			recordScene(&ops, scene, dpmm, width, height)
			if err := window.Frame(&ops); err != nil {
				b.Fatal(err)
			}
			img := image.NewRGBA(image.Rect(0, 0, width, height))
			if err := window.Screenshot(img); err != nil {
				b.Fatal(err)
			}
			b.Run("record", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					recordScene(&ops, scene, dpmm, width, height)
				}
			})
			b.Run("submit", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if err := window.Frame(&ops); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("readback", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					// Vulkan 在 Frame 时回收暂存区；连续 Screenshot 而不换帧
					// 会累积回读缓冲。帧提交不计入回读阶段耗时。
					b.StopTimer()
					if err := window.Frame(&ops); err != nil {
						b.Fatal(err)
					}
					b.StartTimer()
					if err := window.Screenshot(img); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("normalize", func(b *testing.B) {
				raw := bytes.Clone(img.Pix)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					copy(img.Pix, raw)
					b.StartTimer()
					normalizeReadback(img)
				}
			})
		})
	}
}

// BenchmarkRasterizeSceneCPU 测量 canvas CPU 光栅化同一测试场景的耗时。
func BenchmarkRasterizeSceneCPU(b *testing.B) {
	scene := benchmarkScene(b)
	for _, dpi := range []float64{96, 150, 300} {
		b.Run(fmt.Sprintf("%.0fdpi", dpi), func(b *testing.B) {
			res := tcanvas.Resolution(geom.DPI(dpi))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = rasterizer.Draw(scene, res, tcanvas.DefaultColorSpace)
			}
		})
	}
}

// BenchmarkRasterizeSceneGPU 测量 Gio 无头 GPU 重放同一测试场景的耗时。
// 预热一次使窗口缓存进入对应尺寸，基准反映稳态复用开销，不含首次创建。
func BenchmarkRasterizeSceneGPU(b *testing.B) {
	requireGPU(b)
	scene := benchmarkScene(b)
	for _, dpi := range []float64{96, 150, 300} {
		b.Run(fmt.Sprintf("%.0fdpi", dpi), func(b *testing.B) {
			res := geom.DPI(dpi)
			if _, err := Rasterize(scene, res); err != nil {
				b.Fatalf("GPU 预热失败: %v", err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := Rasterize(scene, res); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
