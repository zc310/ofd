package render

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
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

func TestNewPathSkipsMalformedCommands(t *testing.T) {
	path := (&Document{}).newPath(&models.CtPath{
		AbbreviatedData: models.SVGPath{
			{Type: models.MoveTo},
			{Type: models.LineTo},
			{Type: models.QuadTo, Points: []models.StPos{{X: 1, Y: 1}}},
			{Type: models.CubicBezier, Points: []models.StPos{{X: 1, Y: 1}, {X: 2, Y: 2}}},
			{Type: models.ArcTo},
			{Type: models.MoveTo, Points: []models.StPos{{X: 0, Y: 0}}},
			{Type: models.LineTo, Points: []models.StPos{{X: 10, Y: 10}}},
		},
	}, func(point models.StPos) (float64, float64) {
		return point.X, point.Y
	})

	if path.Empty() {
		t.Fatal("expected valid commands to remain after malformed commands were skipped")
	}
	if bounds := path.Bounds(); bounds.X0 != 0 || bounds.Y0 != 0 || bounds.X1 != 10 || bounds.Y1 != 10 {
		t.Fatalf("path bounds = %+v, want (0,0)-(10,10)", bounds)
	}
}

func TestNewPathHandlesNilInputs(t *testing.T) {
	document := &Document{}
	if path := document.newPath(nil, func(point models.StPos) (float64, float64) {
		return point.X, point.Y
	}); !path.Empty() {
		t.Fatal("nil path should produce an empty path")
	}
	if path := document.newPath(&models.CtPath{}, nil); !path.Empty() {
		t.Fatal("nil transform should produce an empty path")
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

func TestBuildObjectPathSkipsOverflowedCoordinates(t *testing.T) {
	path := (&Document{}).buildObjectPath(models.PathObject{
		CtPath: models.CtPath{
			CTGraphicUnit: models.CTGraphicUnit{
				Boundary: models.StBox{X: math.MaxFloat64, Y: math.MaxFloat64},
				CTM:      &models.CTM{1, 0, 0, 1, 0, 0},
			},
			AbbreviatedData: models.SVGPath{{
				Type:   models.MoveTo,
				Points: []models.StPos{{X: math.MaxFloat64, Y: math.MaxFloat64}},
			}},
		},
	}, math.MaxFloat64)

	if !path.Empty() {
		t.Fatal("path with overflowed transformed coordinates should be empty")
	}
}

func TestImageMatrixRejectsOverflowedResult(t *testing.T) {
	matrix := imageMatrix(
		models.StBox{X: math.MaxFloat64},
		image.NewRGBA(image.Rect(0, 0, 1, 1)),
		models.CTM{1, 0, 0, 1, math.MaxFloat64, 0},
		0,
	)
	if finiteMatrix(matrix) {
		t.Fatal("image matrix with overflowed translation should be rejected")
	}
}
