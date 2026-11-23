package render

import (
	"bytes"
	"image"
	"image/color"
	"reflect"
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/pkg/spec"
)

type nativePathClipContext struct {
	*clippedPathCaptureContext

	accept bool
	path   *geom.Path
	clip   *geom.Path
	matrix geom.Matrix
}

type nativePathStyleContext struct {
	*nativePathClipContext

	canvas      *canvas.Context
	styles      []canvas.Style
	strokeReads int
}

func (c *nativePathStyleContext) DrawClippedPath(path, clip *geom.Path) bool {
	c.styles = append(c.styles, c.canvas.Style)
	return c.nativePathClipContext.DrawClippedPath(path, clip)
}

func (c *nativePathStyleContext) StrokeWidth() float64 {
	c.strokeReads++
	return c.DrawContext.StrokeWidth()
}

func TestNativePathClipSkipsGeometryAndPreservesSeparatePaints(t *testing.T) {
	for _, accepted := range []bool{true, false} {
		t.Run(map[bool]string{true: "原生裁剪", false: "拒绝后回退"}[accepted], func(t *testing.T) {
			recorder := canvas.NewContext(canvas.New(40, 40))
			ctx := &nativePathStyleContext{
				nativePathClipContext: &nativePathClipContext{
					clippedPathCaptureContext: &clippedPathCaptureContext{DrawContext: newCanvasBackend(recorder)},
					accept:                    accepted,
				},
				canvas: recorder,
			}
			ctx.SetFillColor(color.RGBA{R: 128, A: 128})
			ctx.SetStrokeColor(color.RGBA{B: 64, A: 64})
			ctx.SetStrokeWidth(2)
			ctx.SetDashes(.5, 2, 1)
			ctx.Transform(geom.Identity.Translate(3, 4).Shear(.2, .1))
			beforeStyle, beforeMatrix := recorder.Style, ctx.CurrentMatrix()
			path := geom.Circle(12).Translate(20, 20).Append(geom.Circle(4).Translate(20, 20))
			clip := geom.Rectangle(20, 30).Translate(5, 5)
			beforePath, beforeClip := path.ToSVG(), clip.ToSVG()
			pattern := spec.StArray[float64]{2, 1}
			object := spec.PathObject{}
			object.Fill, object.Rule = true, spec.FillRuleEvenOdd
			object.Stroke.Set(true)
			object.DashOffset, object.DashPattern = .5, &pattern
			(&Document{}).drawClippedPath(ctx, path, clip, object)
			if len(ctx.styles) != 2 {
				t.Fatalf("应分别尝试填充和描边裁剪，实际 %d 次", len(ctx.styles))
			}
			fill, stroke := ctx.styles[0], ctx.styles[1]
			if !fill.HasFill() || fill.HasStroke() || fill.FillRule != geom.EvenOdd {
				t.Fatal("填充没有单独保留 EvenOdd 规则")
			}
			if stroke.HasFill() || !stroke.HasStroke() || !reflect.DeepEqual(stroke.Dashes, beforeStyle.Dashes) {
				t.Fatal("描边混入了填充或丢失虚线样式")
			}
			if accepted && (ctx.strokeReads != 0 || len(ctx.paths) != 0) {
				t.Fatal("原生裁剪成功后仍读取线宽展开描边或输出求交轮廓")
			}
			if !accepted && (ctx.strokeReads == 0 || len(ctx.paths) != 2) {
				t.Fatal("后端拒绝后没有保留填充、描边的求交回退")
			}
			if !reflect.DeepEqual(recorder.Style, beforeStyle) || ctx.CurrentMatrix() != beforeMatrix {
				t.Fatal("裁剪泄漏了画笔、填充规则或矩阵")
			}
			if path.ToSVG() != beforePath || clip.ToSVG() != beforeClip {
				t.Fatal("裁剪修改了共享几何")
			}
		})
	}
}

func TestNativeClippedFillMatchesRasterFallbackAndPDFPaintOrder(t *testing.T) {
	path := geom.Rectangle(30, 30).Translate(5, 5).
		Append(geom.Rectangle(8, 8).Translate(16, 16))
	clip := geom.Rectangle(20, 30).Translate(10, 5).
		Append(geom.Rectangle(4, 4).Translate(12, 12)).Settle(geom.EvenOdd)
	object := spec.PathObject{}
	object.Fill, object.Rule = true, spec.FillRuleEvenOdd
	object.Stroke.Set(true)
	draw := func(ctx DrawContext) {
		ctx.SetFillColor(color.RGBA{R: 128, A: 128})
		ctx.SetStrokeColor(color.RGBA{B: 128, A: 128})
		ctx.SetStrokeWidth(2)
		(&Document{}).drawClippedPath(ctx, path, clip, object)
		// 后续图元必须不受裁剪或半透明画笔影响。
		ctx.SetFillColor(canvas.Green)
		ctx.ClearStroke()
		ctx.DrawPath(0, 0, geom.Rectangle(3, 3).Translate(35, 35))
	}
	native := clipTestSurface(t, draw)
	fallback := clipTestSurface(t, func(ctx DrawContext) {
		// 隐藏可选原生裁剪能力，独立走核心的几何求交流程。
		draw(struct{ DrawContext }{ctx})
	})
	first, second := native.Rasterize(geom.DPMM(10), nil), fallback.Rasterize(geom.DPMM(10), nil)
	if !bytes.Equal(first.Pix, second.Pix) {
		t.Fatal("延迟裁剪改变了光栅回放结果")
	}
	check := func(t *testing.T, img image.Image) {
		t.Helper()
		for _, tc := range []struct {
			x, y float64
			rgb  [3]uint8
		}{
			{11, 20, [3]uint8{255, 127, 127}},   // 仅有填充。
			{16.5, 20, [3]uint8{127, 127, 255}}, // 填充洞内仅有描边。
			{15.5, 20, [3]uint8{127, 63, 191}},  // 先填充再叠加描边。
			{14, 14, [3]uint8{255, 255, 255}},   // 裁剪区的洞。
			{20, 20, [3]uint8{255, 255, 255}},   // EvenOdd 填充洞。
			{8, 20, [3]uint8{255, 255, 255}},    // 裁剪区外。
			{36, 36, [3]uint8{0, 128, 0}},       // 后续不透明图元。
		} {
			// 透明光栅图叠到白底后与 PDF 页面比较。
			c, _ := color.RGBAModel.Convert(img.At(int(tc.x*10), img.Bounds().Dy()-int(tc.y*10))).(color.RGBA)
			got := [3]uint8{c.R + 255 - c.A, c.G + 255 - c.A, c.B + 255 - c.A}
			for i, value := range got {
				if delta := int(value) - int(tc.rgb[i]); delta < -1 || delta > 1 {
					t.Fatalf("像素(%g,%g)=%v，预期 %v", tc.x, tc.y, got, tc.rgb)
				}
			}
		}
	}
	check(t, first)
	t.Run("PDF", func(t *testing.T) { check(t, clipPDFImage(t, native, 254)) })
}

func (c *nativePathClipContext) DrawClippedPath(path, clip *geom.Path) bool {
	c.path, c.clip, c.matrix = path, clip, c.CurrentMatrix()
	return c.accept
}

func TestNativePathClipKeepsExactRegionCurveAndFallback(t *testing.T) {
	for _, accepted := range []bool{true, false} {
		t.Run(map[bool]string{true: "原生裁剪", false: "后端拒绝后求交"}[accepted], func(t *testing.T) {
			path := geom.MustParseSVGPath("M0 5 C5 20 15 -10 20 5")
			clip := geom.Rectangle(10, 10).Translate(5, 0)
			beforePath, beforeClip := path.ToSVG(), clip.ToSVG()
			ctx := &nativePathClipContext{
				clippedPathCaptureContext: &clippedPathCaptureContext{DrawContext: newCanvasBackend(canvas.NewContext(canvas.New(40, 40)))},
				accept:                    accepted,
			}
			ctx.SetStrokeWidth(2)
			ctx.SetStrokeColor(canvas.Black)
			matrix := geom.Identity.Translate(3, 4).Shear(.2, .1)
			ctx.Transform(matrix)
			object := spec.PathObject{CtPath: spec.CtPath{Stroke: spec.NewBool(true)}}
			(&Document{}).drawClippedPath(ctx, path, clip, object)
			if ctx.path != path || ctx.clip != clip || ctx.matrix != matrix {
				t.Fatal("原生裁剪改变了曲线、精确裁剪区或当前矩阵")
			}
			if accepted && len(ctx.paths) != 0 {
				t.Fatal("原生裁剪成功后不应再次绘制展开轮廓")
			}
			if !accepted && (len(ctx.paths) != 1 || !clip.Bounds().Contains(ctx.paths[0].Bounds())) {
				t.Fatal("后端拒绝原生裁剪时丢失了求交回退")
			}
			if path.ToSVG() != beforePath || clip.ToSVG() != beforeClip {
				t.Fatal("裁剪改写了只读几何")
			}
		})
	}
}
