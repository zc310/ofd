package render

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
	imagelayout "github.com/zc310/ofd/internal/render/layout/image"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/pkg/spec"
)

func TestDrawClippedPathKeepsEvenOddHole(t *testing.T) {
	// Path.And 固定按 NonZero 求交，会把 Even-Odd 的内圈洞当作填充区域。
	// 带裁剪的 Even-Odd 路径（例如注解外观流中"整页减洞"的背景）必须保留洞。
	doc := &Document{}
	c := canvas.New(100, 100)
	backend := newCanvasBackend(canvas.NewContext(c))
	backend.SetFillColor(color.RGBA{A: 255})

	path := &geom.Path{}
	path.MoveTo(0, 0)
	path.LineTo(100, 0)
	path.LineTo(100, 100)
	path.LineTo(0, 100)
	path.Close()
	path.MoveTo(40, 40)
	path.LineTo(60, 40)
	path.LineTo(60, 60)
	path.LineTo(40, 60)
	path.Close()
	clip := geom.Rectangle(100, 100)
	object := spec.PathObject{CtPath: spec.CtPath{Fill: true, Rule: "Even-Odd"}}
	doc.drawClippedPath(backend, path, clip, object)

	out := rasterize(c, canvas.DPI(72), canvas.DefaultColorSpace)
	dpmm := canvas.DPI(72).DPMM()
	if _, _, _, a := out.RGBAAt(int(50*dpmm), out.Bounds().Dy()-int(50*dpmm)).RGBA(); a>>8 != 0 {
		t.Fatalf("hole alpha = %d, want 0", a>>8)
	}
	if _, _, _, a := out.RGBAAt(int(10*dpmm), out.Bounds().Dy()-int(10*dpmm)).RGBA(); a>>8 != 255 {
		t.Fatalf("fill alpha = %d, want 255", a>>8)
	}
}

type clippedPathCaptureContext struct {
	DrawContext

	paths []*geom.Path
}

func (c *clippedPathCaptureContext) DrawPath(_, _ float64, path *geom.Path) {
	c.paths = append(c.paths, path)
	c.DrawContext.DrawPath(0, 0, path)
}

func TestDrawClippedPathReusesCoveredFillAndStillClipsStroke(t *testing.T) {
	scene := canvas.New(40, 40)
	ctx := &clippedPathCaptureContext{DrawContext: newCanvasBackend(canvas.NewContext(scene))}
	ctx.SetFillColor(canvas.Red)
	ctx.SetStrokeColor(canvas.Black)
	ctx.SetStrokeWidth(4)
	path := geom.Circle(10).Translate(20, 20)
	before := path.ToSVG()
	clip := geom.Rectangle(20, 20).Translate(10, 10)
	object := spec.PathObject{CtPath: spec.CtPath{Fill: true, Stroke: spec.NewBool(true)}}
	(&Document{}).drawClippedPath(ctx, path, clip, object)
	if len(ctx.paths) != 2 || ctx.paths[0] != path {
		t.Fatal("完全覆盖的填充不应展开或求交，描边仍须独立裁剪")
	}
	if !clip.Bounds().Contains(ctx.paths[1].Bounds()) {
		t.Fatalf("描边越出裁剪区：%v", ctx.paths[1].Bounds())
	}
	if path.ToSVG() != before {
		t.Fatal("裁剪修改了共享路径")
	}
}

func TestDrawClippedPathDoesNotBypassClipHoles(t *testing.T) {
	scene := canvas.New(20, 20)
	ctx := newCanvasBackend(canvas.NewContext(scene))
	ctx.SetFillColor(canvas.Red)
	path := geom.Rectangle(10, 10).Translate(5, 5)
	clip := geom.Rectangle(20, 20).Append(geom.Rectangle(8, 8).Translate(6, 6)).Settle(geom.EvenOdd)
	object := spec.PathObject{CtPath: spec.CtPath{Fill: true, Stroke: spec.NewBool(false)}}
	(&Document{}).drawClippedPath(ctx, path, clip, object)
	img := rasterize(scene, canvas.DPMM(10), canvas.DefaultColorSpace)
	if pixel := img.RGBAAt(100, 100); pixel.A != 0 {
		t.Fatalf("裁剪孔洞被包围盒快路径填实：%v", pixel)
	}
	if pixel := img.RGBAAt(55, 145); pixel.R != 255 || pixel.A != 255 {
		t.Fatalf("孔洞外的填充丢失：%v", pixel)
	}
}

func TestCoveredStrokeKeepsNativeCurveAndDashStyle(t *testing.T) {
	for _, dashed := range []bool{false, true} {
		t.Run(map[bool]string{false: "实线", true: "虚线"}[dashed], func(t *testing.T) {
			path := geom.Circle(10).Translate(20, 20)
			before := path.ToSVG()
			clip := geom.Rectangle(22, 22).Translate(9, 9)
			var style spec.CTGraphicUnit
			if dashed {
				pattern := spec.StArray[float64]{2, 1}
				style.DashPattern, style.DashOffset = &pattern, 0.5
			}
			setup := func(ctx DrawContext) {
				ctx.ClearFill()
				ctx.SetStrokeColor(canvas.Black)
				ctx.SetStrokeWidth(1)
				ctx.SetStrokeJoiner(geom.MiterJoiner{Limit: 10})
				if style.DashPattern != nil {
					ctx.SetDashes(style.DashOffset, (*style.DashPattern)...)
				}
			}
			actual := clipTestSurface(t, func(ctx DrawContext) {
				setup(ctx)
				if clipContainsRect(clip, strokeBounds(ctx, path.Bounds())) {
					t.Fatal("测试必须使保守斜接包围盒越出裁剪区")
				}
				capture := &clippedPathCaptureContext{DrawContext: ctx}
				object := spec.PathObject{CtPath: spec.CtPath{Stroke: spec.NewBool(true)}}
				(&Document{}).drawClippedPathStyle(capture, path, clip, object, style)
				if len(capture.paths) != 1 || capture.paths[0] != path {
					t.Fatal("已覆盖实际描边时不应输出展开轮廓")
				}
			})
			reference := clipTestSurface(t, func(ctx DrawContext) {
				setup(ctx)
				ctx.DrawPath(0, 0, path)
			})
			first, second := actual.Rasterize(geom.DPMM(10), nil), reference.Rasterize(geom.DPMM(10), nil)
			if !bytes.Equal(first.Pix, second.Pix) {
				t.Fatal("保留原生描边后线宽或虚线相位发生变化")
			}
			t.Run("PDF", func(t *testing.T) {
				first, second := clipPDFImage(t, actual, 254), clipPDFImage(t, reference, 254)
				for y := first.Bounds().Min.Y; y < first.Bounds().Max.Y; y++ {
					for x := first.Bounds().Min.X; x < first.Bounds().Max.X; x++ {
						if first.At(x, y) != second.At(x, y) {
							t.Fatalf("PDF像素(%d,%d)与源曲线描边样式不一致", x, y)
						}
					}
				}
			})
			if path.ToSVG() != before {
				t.Fatal("描边范围检查修改了共享路径")
			}
		})
	}
}

func TestStrokeCoverageKeepsOutlineToleranceMargin(t *testing.T) {
	scene := canvas.New(40, 40)
	ctx := &clippedPathCaptureContext{DrawContext: newCanvasBackend(canvas.NewContext(scene))}
	ctx.ClearFill()
	ctx.SetStrokeColor(canvas.Black)
	ctx.SetStrokeWidth(1)
	path := &geom.Path{}
	path.MoveTo(5, 20)
	path.LineTo(35, 20)
	// 实际描边上沿20.5，只余半个展开容差，不把近似边界当成精确覆盖。
	clip := geom.Rectangle(40, 20.505)
	object := spec.PathObject{CtPath: spec.CtPath{Stroke: spec.NewBool(true)}}
	(&Document{}).drawClippedPath(ctx, path, clip, object)
	if len(ctx.paths) != 1 || ctx.paths[0] == path {
		t.Fatal("靠近裁剪边缘的轮廓仍应保留求交回退")
	}
}

func TestDrawClippedStrokeDoesNotBypassClipHoles(t *testing.T) {
	path := &geom.Path{}
	path.MoveTo(5, 20)
	path.LineTo(35, 20)
	clip := geom.Rectangle(40, 40).Append(geom.Rectangle(8, 8).Translate(16, 16)).Settle(geom.EvenOdd)
	surface := clipTestSurface(t, func(ctx DrawContext) {
		ctx.ClearFill()
		ctx.SetStrokeColor(canvas.Black)
		ctx.SetStrokeWidth(2)
		object := spec.PathObject{CtPath: spec.CtPath{Stroke: spec.NewBool(true)}}
		(&Document{}).drawClippedPath(ctx, path, clip, object)
	})
	check := func(t *testing.T, img image.Image) {
		t.Helper()
		insideR, _, _, inside := img.At(100, 200).RGBA()
		r, _, _, hole := img.At(200, 200).RGBA()
		if inside == 0 || insideR>>8 > 10 || (hole != 0 && r>>8 < 245) {
			t.Fatal("描边丢失或裁剪孔洞内仍有描边")
		}
	}
	check(t, surface.Rasterize(geom.DPMM(10), nil))
	t.Run("PDF", func(t *testing.T) { check(t, clipPDFImage(t, surface, 254)) })
}

func TestCoveredEvenOddFillAndClippedStrokeMatchPDF(t *testing.T) {
	path := geom.Rectangle(20, 20).Translate(10, 10).Append(geom.Rectangle(8, 8).Translate(16, 16))
	clip := geom.Rectangle(20, 20).Translate(10, 10)
	object := spec.PathObject{CtPath: spec.CtPath{Fill: true, Stroke: spec.NewBool(true), Rule: "Even-Odd"}}
	surface := clipTestSurface(t, func(ctx DrawContext) {
		ctx.SetFillColor(canvas.Red)
		ctx.SetStrokeColor(canvas.Black)
		ctx.SetStrokeWidth(2)
		(&Document{}).drawClippedPath(ctx, path, clip, object)
	})
	check := func(t *testing.T, img image.Image) {
		t.Helper()
		for _, tc := range []struct {
			x, y int
			want color.NRGBA
		}{
			{120, 200, color.NRGBA{R: 255, A: 255}},
			{105, 200, color.NRGBA{A: 255}},
			{200, 200, color.NRGBA{R: 255, G: 255, B: 255, A: 255}},
			{95, 200, color.NRGBA{R: 255, G: 255, B: 255, A: 255}},
		} {
			got, _ := color.NRGBAModel.Convert(img.At(tc.x, tc.y)).(color.NRGBA)
			if got.A == 0 && tc.want.R == 255 && tc.want.G == 255 {
				continue
			}
			if got != tc.want {
				t.Fatalf("像素(%d,%d)=%v，期望%v", tc.x, tc.y, got, tc.want)
			}
		}
	}
	check(t, surface.Rasterize(geom.DPMM(10), nil))
	t.Run("PDF像素", func(t *testing.T) { check(t, clipPDFImage(t, surface, 254)) })
}

func TestGraphicRenderingSkipsNonFiniteCTM(t *testing.T) {
	bad := spec.CTM{1, 0, 0, 1, math.NaN(), 0}
	document := &Document{}
	ctx := canvas.NewContext(canvas.New(100, 100))

	document.Path(newCanvasBackend(ctx), spec.PathObject{CtPath: spec.CtPath{
		CTGraphicUnit: spec.CTGraphicUnit{CTM: &bad},
	}}, nil, spec.StBox{Width: 100, Height: 100})
	document.Image(newCanvasBackend(ctx), spec.ImageObject{CtImage: spec.CtImage{
		CTGraphicUnit: spec.CTGraphicUnit{CTM: &bad},
	}}, nil, spec.StBox{Width: 100, Height: 100})
	document.Text(newCanvasBackend(ctx), spec.TextObject{CtText: spec.CtText{
		CTGraphicUnit: spec.CTGraphicUnit{CTM: &bad},
	}}, nil, spec.StBox{Width: 100, Height: 100})
	document.Composite(newCanvasBackend(ctx), spec.CompositeObject{CtComposite: spec.CtComposite{
		CTGraphicUnit: spec.CTGraphicUnit{CTM: &bad},
	}}, nil, spec.StBox{Width: 100, Height: 100})
}

func TestImageMatrixRejectsOverflowedResult(t *testing.T) {
	matrix := imagelayout.Matrix(
		spec.StBox{X: math.MaxFloat64},
		image.NewRGBA(image.Rect(0, 0, 1, 1)),
		spec.CTM{1, 0, 0, 1, math.MaxFloat64, 0},
	)
	if finiteMatrix(matrix) {
		t.Fatal("image matrix with overflowed translation should be rejected")
	}
}
