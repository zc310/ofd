package canvasconv

import (
	"image/color"
	"math"
	"reflect"
	"testing"

	"github.com/tdewolff/canvas"

	"github.com/zc310/ofd/internal/render/geom"
)

// 仅用于验证未知类型兜底的占位实现。
type unknownGeomCapper struct{}

func (unknownGeomCapper) Cap(*geom.Path, float64, geom.Point, geom.Point) {}

type unknownCanvasCapper struct{}

func (unknownCanvasCapper) Cap(*canvas.Path, float64, canvas.Point, canvas.Point) {}

type unknownGeomJoiner struct{}

func (unknownGeomJoiner) Join(*geom.Path, *geom.Path, float64, geom.Point, geom.Point, geom.Point, float64, float64) {
}

type unknownCanvasJoiner struct{}

func (unknownCanvasJoiner) Join(*canvas.Path, *canvas.Path, float64, canvas.Point, canvas.Point, canvas.Point, float64, float64) {
}

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

func TestCapperRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		g    geom.Capper
		c    canvas.Capper
	}{
		{"Round", geom.RoundCapper{}, canvas.RoundCapper{}},
		{"Square", geom.SquareCapper{}, canvas.SquareCapper{}},
		{"Butt", geom.ButtCapper{}, canvas.ButtCapper{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ToCanvasCapper(tc.g); reflect.TypeOf(got) != reflect.TypeOf(tc.c) {
				t.Errorf("ToCanvasCapper(%T) = %T, want %T", tc.g, got, tc.c)
			}
			if got := FromCanvasCapper(tc.c); reflect.TypeOf(got) != reflect.TypeOf(tc.g) {
				t.Errorf("FromCanvasCapper(%T) = %T, want %T", tc.c, got, tc.g)
			}
		})
	}
}

func TestCapperDefaultFallback(t *testing.T) {
	if got := FromCanvasCapper(unknownCanvasCapper{}); reflect.TypeOf(got) != reflect.TypeOf(geom.ButtCapper{}) {
		t.Errorf("未知 canvas 线帽 → %T, want ButtCapper", got)
	}
	if got := ToCanvasCapper(unknownGeomCapper{}); reflect.TypeOf(got) != reflect.TypeOf(canvas.ButtCapper{}) {
		t.Errorf("未知 geom 线帽 → %T, want canvas.ButtCapper", got)
	}
}

func TestJoinerRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		g    geom.Joiner
		c    canvas.Joiner
	}{
		{"Round", geom.RoundJoiner{}, canvas.RoundJoiner{}},
		{"Bevel", geom.BevelJoiner{}, canvas.BevelJoiner{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ToCanvasJoiner(tc.g); reflect.TypeOf(got) != reflect.TypeOf(tc.c) {
				t.Errorf("ToCanvasJoiner(%T) = %T, want %T", tc.g, got, tc.c)
			}
			if got := FromCanvasJoiner(tc.c); reflect.TypeOf(got) != reflect.TypeOf(tc.g) {
				t.Errorf("FromCanvasJoiner(%T) = %T, want %T", tc.c, got, tc.g)
			}
		})
	}
}

func TestMiterJoinerPreservesGapAndLimit(t *testing.T) {
	g := geom.MiterJoiner{GapJoiner: geom.RoundJoiner{}, Limit: 7.5}
	cg, ok := ToCanvasJoiner(g).(canvas.MiterJoiner)
	if !ok {
		t.Fatalf("ToCanvasJoiner 未还原为 canvas.MiterJoiner: %T", ToCanvasJoiner(g))
	}
	if cg.Limit != g.Limit {
		t.Errorf("Limit = %g, want %g", cg.Limit, g.Limit)
	}
	if _, ok := cg.GapJoiner.(canvas.RoundJoiner); !ok {
		t.Errorf("GapJoiner = %T, want canvas.RoundJoiner", cg.GapJoiner)
	}

	back, ok := FromCanvasJoiner(cg).(geom.MiterJoiner)
	if !ok {
		t.Fatalf("往返后 = %T, want geom.MiterJoiner", FromCanvasJoiner(cg))
	}
	if back.Limit != g.Limit || reflect.TypeOf(back.GapJoiner) != reflect.TypeOf(geom.RoundJoiner{}) {
		t.Errorf("往返后 = %+v, want %+v", back, g)
	}

	// canvas → geom 方向同样保 GapJoiner 与 Limit。
	c := canvas.MiterJoiner{GapJoiner: canvas.BevelJoiner{}, Limit: 2.5}
	gj, ok := FromCanvasJoiner(c).(geom.MiterJoiner)
	if !ok {
		t.Fatalf("FromCanvasJoiner = %T, want geom.MiterJoiner", FromCanvasJoiner(c))
	}
	if gj.Limit != c.Limit {
		t.Errorf("Limit = %g, want %g", gj.Limit, c.Limit)
	}
	if _, ok := gj.GapJoiner.(geom.BevelJoiner); !ok {
		t.Errorf("GapJoiner = %T, want geom.BevelJoiner", gj.GapJoiner)
	}
	if _, ok := ToCanvasJoiner(gj).(canvas.MiterJoiner); !ok {
		t.Errorf("ToCanvasJoiner(FromCanvasJoiner(%v)) = %T, want canvas.MiterJoiner", c, ToCanvasJoiner(gj))
	}
}

func TestJoinerDefaultFallback(t *testing.T) {
	want := geom.MiterJoiner{GapJoiner: geom.BevelJoiner{}, Limit: 4.0}
	got, ok := FromCanvasJoiner(unknownCanvasJoiner{}).(geom.MiterJoiner)
	if !ok {
		t.Fatalf("未知 canvas 连接器 → %T, want geom.MiterJoiner", FromCanvasJoiner(unknownCanvasJoiner{}))
	}
	if got.Limit != want.Limit || reflect.TypeOf(got.GapJoiner) != reflect.TypeOf(want.GapJoiner) {
		t.Errorf("未知 canvas 连接器 → %+v, want %+v", got, want)
	}
	if got := ToCanvasJoiner(unknownGeomJoiner{}); reflect.TypeOf(got) != reflect.TypeOf(canvas.MiterJoiner{}) {
		t.Errorf("未知 geom 连接器 → %T, want canvas.MiterJoiner", got)
	}
}

func TestPaintSolidRoundTrip(t *testing.T) {
	c := color.RGBA{R: 10, G: 20, B: 30, A: 255}

	cp := ToCanvasPaint(geom.SolidPaint(c))
	if !cp.IsColor() || cp.Color != c {
		t.Errorf("ToCanvasPaint(solid) = %+v, want color %v", cp, c)
	}
	gp := FromCanvasPaint(canvas.Paint{Color: c})
	if !gp.IsColor() || !reflect.DeepEqual(gp.Color, c) {
		t.Errorf("FromCanvasPaint(solid) = %+v, want color %v", gp, c)
	}

	back := FromCanvasPaint(ToCanvasPaint(geom.SolidPaint(c)))
	if !back.IsColor() || !reflect.DeepEqual(back.Color, c) {
		t.Errorf("纯色 paint 往返 = %+v, want color %v", back, c)
	}
}

func TestPaintGradientRoundTrip(t *testing.T) {
	g := geom.NewLinearGradient(geom.Point{X: 0, Y: 0}, geom.Point{X: 10, Y: 0})
	g.Add(0, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	g.Add(1, color.RGBA{R: 255, G: 0, B: 0, A: 255})

	cp := ToCanvasPaint(geom.GradientPaint(g))
	if !cp.IsGradient() {
		t.Fatalf("ToCanvasPaint(gradient) 不是渐变: %+v", cp)
	}

	gp := FromCanvasPaint(cp)
	if !gp.IsGradient() {
		t.Fatalf("渐变 paint 往返丢失渐变: %+v", gp)
	}

	// 往返后回到 canvas 侧采样，应与源渐变在同一逻辑点同色。
	back := ToCanvasGradient(gp.Gradient)
	for _, x := range []float64{0, 2.5, 5, 7.5, 10} {
		if got, want := back.At(x, 0), g.At(x, 0); got != want {
			t.Errorf("渐变往返采样 @x=%g = %v, want %v", x, got, want)
		}
	}
}

func TestPaintEmptyRoundTrip(t *testing.T) {
	if p := ToCanvasPaint(geom.Paint{}); p.Has() {
		t.Errorf("ToCanvasPaint(empty) = %+v, want empty", p)
	}
	if p := FromCanvasPaint(canvas.Paint{}); p.Has() {
		t.Errorf("FromCanvasPaint(empty) = %+v, want empty", p)
	}
}

func TestFillRuleRoundTrip(t *testing.T) {
	// 转换依赖“两者常量顺序一致”的底层值直接映射，这里逐值对比，任何一端
	// 调整枚举顺序都会使断言失败。
	tests := []struct {
		name string
		g    geom.FillRule
		c    canvas.FillRule
	}{
		{"NonZero", geom.NonZero, canvas.NonZero},
		{"EvenOdd", geom.EvenOdd, canvas.EvenOdd},
		{"Positive", geom.Positive, canvas.Positive},
		{"Negative", geom.Negative, canvas.Negative},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ToCanvasFillRule(tc.g); got != tc.c {
				t.Errorf("ToCanvasFillRule(%v) = %v, want %v", tc.g, got, tc.c)
			}
			if got := FromCanvasFillRule(tc.c); got != tc.g {
				t.Errorf("FromCanvasFillRule(%v) = %v, want %v", tc.c, got, tc.g)
			}
		})
	}
}
