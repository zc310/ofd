package render

import (
	"image/color"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

func TestApplyFillDefaultsToTransparent(t *testing.T) {
	ctx := canvas.NewContext(canvas.New(10, 10))
	ctx.SetFillColor(color.RGBA{R: 255, A: 255})
	var document Document
	document.applyFill(newCanvasBackend(ctx), nil, nil)
	if ctx.Style.Fill.Color != geom.Transparent {
		t.Fatalf("expected transparent default fill, got %v", ctx.Style.Fill.Color)
	}
}

func TestApplyFillKeepsExplicitColor(t *testing.T) {
	ctx := canvas.NewContext(canvas.New(10, 10))
	var document Document
	document.applyFill(newCanvasBackend(ctx), &CTColor{Value: color.RGBA{R: 10, G: 20, B: 30, A: 255}, HasValue: true}, nil)
	if ctx.Style.Fill.Color != (color.RGBA{R: 10, G: 20, B: 30, A: 255}) {
		t.Fatalf("expected explicit fill color, got %v", ctx.Style.Fill.Color)
	}
}

func TestApplyStrokeDefaultsToBlack(t *testing.T) {
	ctx := canvas.NewContext(canvas.New(10, 10))
	var document Document
	document.applyStroke(newCanvasBackend(ctx), nil, &models.CtPath{})
	if ctx.Style.Stroke.Color != geom.Black {
		t.Fatalf("expected black default stroke, got %v", ctx.Style.Stroke.Color)
	}
}

func TestPathStyleObjectPropertiesOverrideDrawParam(t *testing.T) {
	ctx := canvas.NewContext(canvas.New(10, 10))
	var document Document
	object := &models.CtPath{
		CTGraphicUnit: models.CTGraphicUnit{
			LineWidth: 2,
			Cap:       "Butt",
			Join:      "Bevel",
		},
	}
	dp := &models.DrawParam{
		LineWidth: 4,
		Cap:       "Round",
		Join:      "Round",
	}

	document.updateCtPathStyle(newCanvasBackend(ctx), object, dp)

	if ctx.Style.StrokeWidth != 2 {
		t.Fatalf("stroke width = %g, want 2", ctx.Style.StrokeWidth)
	}
	if ctx.Style.StrokeCapper != canvas.ButtCap {
		t.Fatalf("stroke cap = %v, want butt", ctx.Style.StrokeCapper)
	}
	if ctx.Style.StrokeJoiner != canvas.BevelJoin {
		t.Fatalf("stroke join = %v, want bevel", ctx.Style.StrokeJoiner)
	}
}

func TestMiterLimitUsesAbsoluteMillimetres(t *testing.T) {
	ctx := canvas.NewContext(canvas.New(10, 10))
	var document Document
	document.updateCtPathStyle(newCanvasBackend(ctx), &models.CtPath{
		CTGraphicUnit: models.CTGraphicUnit{
			LineWidth:  3,
			Join:       "Miter",
			MiterLimit: 2,
		},
	}, nil)

	joiner, ok := ctx.Style.StrokeJoiner.(canvas.MiterJoiner)
	if !ok {
		t.Fatalf("stroke joiner = %T, want canvas.MiterJoiner", ctx.Style.StrokeJoiner)
	}
	if joiner.Limit != 2.0/1.5 {
		t.Fatalf("miter limit ratio = %g, want %g", joiner.Limit, 2.0/1.5)
	}
	geomJoiner := geom.MiterJoiner{GapJoiner: geom.BevelJoin, Limit: joiner.Limit}
	path := geom.MustParseSVGPath("M20 35L40 5L60 35")
	clipped := path.Stroke(3, geom.ButtCap, geomJoiner, geom.Tolerance).ToSVG()
	unclipped := path.Stroke(3, geom.ButtCap,
		geom.MiterJoiner{GapJoiner: geom.BevelJoin, Limit: 10.0 / 1.5}, geom.Tolerance).ToSVG()
	if clipped == unclipped {
		t.Fatal("different MiterLimit values produced identical stroked paths")
	}
}

func TestStrokeParametersRejectInvalidValues(t *testing.T) {
	ctx := canvas.NewContext(canvas.New(10, 10))
	var document Document
	document.updateCtPathStyle(newCanvasBackend(ctx), &models.CtPath{
		CTGraphicUnit: models.CTGraphicUnit{
			LineWidth:   -1,
			MiterLimit:  -1,
			DashOffset:  -1,
			DashPattern: &models.StArrayF{0, 0},
		},
	}, nil)

	if ctx.Style.StrokeWidth != defaultLineWidth {
		t.Fatalf("stroke width = %g, want default %g", ctx.Style.StrokeWidth, defaultLineWidth)
	}
	if len(ctx.Style.Dashes) != 0 || ctx.Style.DashOffset != 0 {
		t.Fatalf("invalid dash pattern was retained: offset=%g dashes=%v", ctx.Style.DashOffset, ctx.Style.Dashes)
	}
}

func TestValidDashPattern(t *testing.T) {
	pattern := models.StArrayF{2, 1}
	if !validDashPattern(0, &pattern) {
		t.Fatal("valid dash pattern was rejected")
	}
	for _, invalid := range []models.StArrayF{{-1, 1}, {0, 0}} {
		if validDashPattern(0, &invalid) {
			t.Fatalf("invalid dash pattern was accepted: %v", invalid)
		}
	}
}
