package canvasconv

import (
	"image/color"
	"math"
	"testing"

	"github.com/tdewolff/canvas"

	"github.com/zc310/ofd/internal/render/geom"
)

const boundsTol = 1e-6

func assertBounds(t *testing.T, label string, got geom.Rect, want canvas.Rect) {
	t.Helper()
	if math.Abs(got.X0-want.X0) > boundsTol || math.Abs(got.Y0-want.Y0) > boundsTol ||
		math.Abs(got.X1-want.X1) > boundsTol || math.Abs(got.Y1-want.Y1) > boundsTol {
		t.Fatalf("%s bounds = %v, want (%g,%g)-(%g,%g)", label, got, want.X0, want.Y0, want.X1, want.Y1)
	}
}

func TestPathRoundTripBounds(t *testing.T) {
	src := &canvas.Path{}
	src.MoveTo(0, 0)
	src.LineTo(10, 0)
	src.QuadTo(12, 2, 10, 4)
	src.CubeTo(8, 6, 2, 6, 0, 4)
	src.ArcTo(3, 3, 0, false, true, 0, 0)
	src.Close()

	want := src.Bounds()
	got := ToCanvasPath(FromCanvasPath(src)).Bounds()
	assertBounds(t, "round trip", geom.Rect{X0: got.X0, Y0: got.Y0, X1: got.X1, Y1: got.Y1}, want)

	gfxBounds := FromCanvasPath(src).Bounds()
	assertBounds(t, "gfx bounds", gfxBounds, want)
}

func TestPathArcPreserved(t *testing.T) {
	src := &canvas.Path{}
	src.MoveTo(0, 0)
	src.ArcTo(5, 5, 45, true, false, 10, 0)

	converted := FromCanvasPath(src)
	foundArc := false
	sc := converted.Scanner()
	for sc.Scan() {
		if sc.Cmd() != geom.ArcToCmd {
			continue
		}
		foundArc = true
		rx, ry, _, large, sweep := sc.Arc()
		if !geom.Equal(rx, 5) || !geom.Equal(ry, 5) || !large || sweep {
			t.Fatalf("弧参数不一致: rx=%g ry=%g large=%v sweep=%v", rx, ry, large, sweep)
		}
	}
	if !foundArc {
		t.Fatal("弧段未保留")
	}
	assertBounds(t, "arc bounds", converted.Bounds(), src.Bounds())
}

func TestMatrixConversion(t *testing.T) {
	cm := canvas.Identity.Translate(3, 4).Rotate(30).Scale(2, 2)
	gm := FromCanvasMatrix(cm)
	if !gm.Equals(geom.Matrix(cm)) {
		t.Fatal("矩阵转换不一致")
	}
	if ToCanvasMatrix(gm) != cm {
		t.Fatal("矩阵往返不一致")
	}
}

func TestGradientConversionSampling(t *testing.T) {
	g := canvas.Grad{}
	g.Add(0, color.RGBA{0, 0, 0, 255})
	g.Add(1, color.RGBA{255, 0, 0, 255})
	cg := g.ToLinear(canvas.Point{X: 0, Y: 0}, canvas.Point{X: 10, Y: 0})

	gg := FromCanvasGradient(cg)
	for _, x := range []float64{0, 2.5, 5, 7.5, 10} {
		if got, want := gg.At(x, 0), cg.At(x, 0); got != want {
			t.Fatalf("x=%g: gfx 渐变 %v != canvas %v", x, got, want)
		}
	}

	back := ToCanvasGradient(gg)
	if back.At(5, 0) != cg.At(5, 0) {
		t.Fatalf("渐变往返采样不一致: %v != %v", back.At(5, 0), cg.At(5, 0))
	}
}
