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
