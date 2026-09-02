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
