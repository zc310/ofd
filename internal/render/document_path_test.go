package render

import (
	"image"
	"math"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/models"
)

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

	document.Path(ctx, models.PathObject{CtPath: models.CtPath{
		CTGraphicUnit: models.CTGraphicUnit{CTM: &bad},
	}}, nil, models.StBox{Width: 100, Height: 100})
	document.Image(ctx, models.ImageObject{CtImage: models.CtImage{
		CTGraphicUnit: models.CTGraphicUnit{CTM: &bad},
	}}, nil, models.StBox{Width: 100, Height: 100})
	document.Text(ctx, models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{CTM: &bad},
	}}, nil, models.StBox{Width: 100, Height: 100})
	document.Composite(ctx, models.CompositeObject{CtComposite: models.CtComposite{
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
