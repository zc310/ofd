package render

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	imagelayout "github.com/zc310/ofd/internal/render/layout/image"
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
	object := models.PathObject{CtPath: models.CtPath{Fill: true, Rule: "Even-Odd"}}
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
	object := models.PathObject{CtPath: models.CtPath{Fill: true, Stroke: models.NewOptionalBool(true)}}
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
	object := models.PathObject{CtPath: models.CtPath{Fill: true, Stroke: models.NewOptionalBool(false)}}
	(&Document{}).drawClippedPath(ctx, path, clip, object)
	img := rasterize(scene, canvas.DPMM(10), canvas.DefaultColorSpace)
	if pixel := img.RGBAAt(100, 100); pixel.A != 0 {
		t.Fatalf("裁剪孔洞被包围盒快路径填实：%v", pixel)
	}
	if pixel := img.RGBAAt(55, 145); pixel.R != 255 || pixel.A != 255 {
		t.Fatalf("孔洞外的填充丢失：%v", pixel)
	}
}

func TestCoveredEvenOddFillAndClippedStrokeMatchPDF(t *testing.T) {
	path := geom.Rectangle(20, 20).Translate(10, 10).Append(geom.Rectangle(8, 8).Translate(16, 16))
	clip := geom.Rectangle(20, 20).Translate(10, 10)
	object := models.PathObject{CtPath: models.CtPath{Fill: true, Stroke: models.NewOptionalBool(true), Rule: "Even-Odd"}}
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
	check(t, surface.Rasterize(geom.DPMM(10)))
	t.Run("PDF像素", func(t *testing.T) { check(t, clipPDFImage(t, surface, 254)) })
}

func TestGraphicRenderingSkipsNonFiniteCTM(t *testing.T) {
	bad := models.CTM{1, 0, 0, 1, math.NaN(), 0}
	document := &Document{}
	ctx := canvas.NewContext(canvas.New(100, 100))

	document.Path(newCanvasBackend(ctx), models.PathObject{CtPath: models.CtPath{
		CTGraphicUnit: models.CTGraphicUnit{CTM: &bad},
	}}, nil, models.StBox{Width: 100, Height: 100})
	document.Image(newCanvasBackend(ctx), models.ImageObject{CtImage: models.CtImage{
		CTGraphicUnit: models.CTGraphicUnit{CTM: &bad},
	}}, nil, models.StBox{Width: 100, Height: 100})
	document.Text(newCanvasBackend(ctx), models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{CTM: &bad},
	}}, nil, models.StBox{Width: 100, Height: 100})
	document.Composite(newCanvasBackend(ctx), models.CompositeObject{CtComposite: models.CtComposite{
		CTGraphicUnit: models.CTGraphicUnit{CTM: &bad},
	}}, nil, models.StBox{Width: 100, Height: 100})
}

func TestImageMatrixRejectsOverflowedResult(t *testing.T) {
	matrix := imagelayout.Matrix(
		models.StBox{X: math.MaxFloat64},
		image.NewRGBA(image.Rect(0, 0, 1, 1)),
		models.CTM{1, 0, 0, 1, math.MaxFloat64, 0},
		0,
	)
	if finiteMatrix(matrix) {
		t.Fatal("image matrix with overflowed translation should be rejected")
	}
}
