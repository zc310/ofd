package render

import (
	"image"
	"image/color"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/models"
)

func TestPatternMatrix(t *testing.T) {
	pattern := &models.CtPattern{CTM: models.StArray{"2", "3", "4", "5", "6", "7"}}
	matrix, ok := patternMatrix(pattern)
	if !ok {
		t.Fatal("expected valid pattern CTM")
	}
	got := matrix.Dot(canvas.Point{X: 1, Y: 2})
	if got.X != 16 || got.Y != 20 {
		t.Fatalf("unexpected transformed point: %+v", got)
	}
}

func TestPatternMatrixInvalid(t *testing.T) {
	pattern := &models.CtPattern{CTM: models.StArray{"1", "0", "0"}}
	if _, ok := patternMatrix(pattern); ok {
		t.Fatal("expected invalid pattern CTM")
	}
}

func TestPatternTileReflect(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.SetRGBA(0, 0, color.RGBA{R: 10, A: 255})
	src.SetRGBA(1, 0, color.RGBA{B: 20, A: 255})

	got := patternTile(src, "RowAndColumn", 1, 1)
	left := color.RGBAModel.Convert(got.At(0, 0)).(color.RGBA)
	right := color.RGBAModel.Convert(got.At(1, 0)).(color.RGBA)
	if left.B != 20 || right.R != 10 {
		t.Fatalf("unexpected reflected tile: left=%v right=%v", left, right)
	}
}
