package render

import (
	"image/color"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/models"
)

func TestApplyFillDefaultsToTransparent(t *testing.T) {
	ctx := canvas.NewContext(canvas.New(10, 10))
	ctx.SetFillColor(color.RGBA{R: 255, A: 255})
	var document Document
	document.applyFill(ctx, nil, nil)
	if ctx.Style.Fill.Color != canvas.Transparent {
		t.Fatalf("expected transparent default fill, got %v", ctx.Style.Fill.Color)
	}
}

func TestApplyFillKeepsExplicitColor(t *testing.T) {
	ctx := canvas.NewContext(canvas.New(10, 10))
	var document Document
	document.applyFill(ctx, &CTColor{Value: &color.RGBA{R: 10, G: 20, B: 30, A: 255}}, nil)
	if ctx.Style.Fill.Color != (color.RGBA{R: 10, G: 20, B: 30, A: 255}) {
		t.Fatalf("expected explicit fill color, got %v", ctx.Style.Fill.Color)
	}
}

func TestApplyStrokeDefaultsToBlack(t *testing.T) {
	ctx := canvas.NewContext(canvas.New(10, 10))
	var document Document
	document.applyStroke(ctx, nil, &models.CtPath{})
	if ctx.Style.Stroke.Color != canvas.Black {
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

	document.updateCtPathStyle(ctx, object, dp)

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
	document.updateCtPathStyle(ctx, &models.CtPath{
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
	path := canvas.MustParseSVGPath("M20 35L40 5L60 35")
	clipped := path.Stroke(3, canvas.ButtCap, joiner, canvas.Tolerance).ToSVG()
	unclipped := path.Stroke(3, canvas.ButtCap,
		canvas.MiterJoiner{GapJoiner: canvas.BevelJoin, Limit: 10.0 / 1.5}, canvas.Tolerance).ToSVG()
	if clipped == unclipped {
		t.Fatal("different MiterLimit values produced identical stroked paths")
	}
}
