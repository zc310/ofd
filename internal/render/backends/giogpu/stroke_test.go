package giogpu

import (
	"math"
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

// GB/T 33190—2016 表21规定正线宽的设备下限；低 DPI 的参考设备换算是项目预览策略。
// 严格调用 GPU 入口，避免自动 CPU 回退掩盖细线丢失。
func TestGPUMinimumPositiveStrokePreservesCoverageAcrossTransforms(t *testing.T) {
	requireGPU(t)
	for _, dpi := range []float64{96, 150, 300, 600} {
		for _, scale := range []float64{0.3528, 1, 9} {
			scene := canvas.New(20, 10)
			ctx := canvas.NewContext(scene)
			ctx.SetFillColor(canvas.Transparent)
			ctx.SetStrokeColor(canvas.Black)
			ctx.SetStrokeCapper(canvas.ButtCap)
			ctx.SetStrokeWidth(0.002)
			ctx.Scale(scale, scale)
			path := &canvas.Path{}
			path.MoveTo(2/scale, 5/scale)
			path.LineTo(18/scale, 5/scale)
			ctx.DrawPath(0, 0, path)
			res := geom.DPI(dpi)
			img, err := Rasterize(scene, res)
			if err != nil {
				t.Fatalf("DPI=%g 缩放=%g：GPU 渲染失败: %v", dpi, scale, err)
			}
			var alpha int
			for y := range img.Bounds().Dy() {
				alpha += int(img.RGBAAt(int(10*res.DPMM()), y).A)
			}
			got, want := float64(alpha)/255, math.Min(2, dpi/300)
			if math.Abs(got-want) > 0.25 {
				t.Errorf("DPI=%g 缩放=%g：描边覆盖 %.3f 像素，期望 %.3f", dpi, scale, got, want)
			}
		}
	}
}

// 线宽下限不能改变虚线的绝对长度、相位或共享页面记录。
func TestGPUStrokeMinimumKeepsDashPatternAndSourceStyle(t *testing.T) {
	requireGPU(t)
	scene := canvas.New(20, 10)
	style := canvas.DefaultStyle
	style.Fill = canvas.Paint{}
	style.Stroke = canvas.Paint{Color: canvas.Black}
	style.StrokeWidth = 0.002
	style.StrokeCapper = canvas.ButtCap
	style.Dashes = []float64{1000, 1000}
	path := &canvas.Path{}
	path.MoveTo(2, 5)
	path.LineTo(18, 5)
	scene.RenderPath(path, style, canvas.Identity)
	for _, res := range []geom.Resolution{geom.DPMM(10), geom.DPI(600)} {
		img, err := Rasterize(scene, res)
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range []float64{3, 7, 11, 15} {
			var alpha int
			for y := range img.Bounds().Dy() {
				alpha += int(img.RGBAAt(int(x*res.DPMM()), y).A)
			}
			want := math.Min(2, float64(res.DPMM())*2/geom.DPI(600).DPMM())
			if got := float64(alpha) / 255; math.Abs(got-want) > 0.25 {
				t.Errorf("分辨率=%v x=%g：实线覆盖 %.3f，期望 %.3f", res, x, got, want)
			}
		}
		for _, x := range []float64{5, 9, 13, 17} {
			var alpha int
			for y := range img.Bounds().Dy() {
				alpha += int(img.RGBAAt(int(x*res.DPMM()), y).A)
			}
			if alpha != 0 {
				t.Errorf("分辨率=%v x=%g：虚线间隙被覆盖", res, x)
			}
		}
	}
	if style.StrokeWidth != 0.002 || style.DashOffset != 0 || len(style.Dashes) != 2 || style.Dashes[0] != 1000 {
		t.Fatal("GPU 栅格化不应修改原始线宽或虚线样式")
	}
	// 相位偏移半个周期后，原来的实线段与间隙应互换。
	phaseScene := canvas.New(20, 10)
	style.DashOffset = 1000
	phaseScene.RenderPath(path, style, canvas.Identity)
	img, err := Rasterize(phaseScene, geom.DPMM(10))
	if err != nil {
		t.Fatal(err)
	}
	for _, probe := range []struct {
		x       int
		visible bool
	}{{30, false}, {50, true}} {
		var alpha int
		for y := range img.Bounds().Dy() {
			alpha += int(img.RGBAAt(probe.x, y).A)
		}
		if (alpha > 0) != probe.visible {
			t.Errorf("虚线相位 x=%d：覆盖=%d，期望可见=%v", probe.x, alpha, probe.visible)
		}
	}
}

// 表格内部的正常细线不能因预览下限被额外加粗。
func TestGPUStrokeAboveMinimumKeepsOriginalWidth(t *testing.T) {
	requireGPU(t)
	for _, dpi := range []float64{96, 150, 300, 600} {
		scene := canvas.New(20, 10)
		ctx := canvas.NewContext(scene)
		ctx.SetFillColor(canvas.Transparent)
		ctx.SetStrokeColor(canvas.Black)
		ctx.SetStrokeCapper(canvas.ButtCap)
		ctx.SetStrokeWidth(0.1154)
		path := &canvas.Path{}
		path.MoveTo(2, 5)
		path.LineTo(18, 5)
		ctx.DrawPath(0, 0, path)
		res := geom.DPI(dpi)
		img, err := Rasterize(scene, res)
		if err != nil {
			t.Fatal(err)
		}
		var alpha int
		for y := range img.Bounds().Dy() {
			alpha += int(img.RGBAAt(int(10*res.DPMM()), y).A)
		}
		if got, want := float64(alpha)/255, 0.1154*res.DPMM(); math.Abs(got-want) > 0.25 {
			t.Errorf("DPI=%g：表格细线覆盖 %.3f 像素，期望 %.3f", dpi, got, want)
		}
	}
}
