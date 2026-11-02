package canvas

import (
	"fmt"
	"image/color"
	"math"
	"testing"
)

func TestGradAdd(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	green := color.RGBA{0, 255, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	white := color.RGBA{255, 255, 255, 255}

	tests := []struct {
		name     string
		initial  Grad
		addT     float64
		addColor color.RGBA
		want     Grad
	}{
		{
			name:     "add to empty gradient",
			initial:  Grad{},
			addT:     0.5,
			addColor: red,
			want:     Grad{{0.5, red}},
		},
		{
			name:     "add at end",
			initial:  Grad{{0.0, red}},
			addT:     1.0,
			addColor: blue,
			want:     Grad{{0.0, red}, {1.0, blue}},
		},
		{
			name:     "add at beginning",
			initial:  Grad{{0.5, red}},
			addT:     0.0,
			addColor: blue,
			want:     Grad{{0.0, blue}, {0.5, red}},
		},
		{
			name:     "insert in middle maintains sort order",
			initial:  Grad{{0.0, red}, {1.0, blue}},
			addT:     0.5,
			addColor: green,
			want:     Grad{{0.0, red}, {0.5, green}, {1.0, blue}},
		},
		{
			name:     "replace existing offset",
			initial:  Grad{{0.0, red}, {0.5, green}, {1.0, blue}},
			addT:     0.5,
			addColor: white,
			want:     Grad{{0.0, red}, {0.5, white}, {1.0, blue}},
		},
		{
			name:     "clamp t below 0",
			initial:  Grad{},
			addT:     -0.5,
			addColor: red,
			want:     Grad{{0.0, red}},
		},
		{
			name:     "clamp t above 1",
			initial:  Grad{},
			addT:     1.5,
			addColor: red,
			want:     Grad{{1.0, red}},
		},
		{
			name:     "add multiple maintains order",
			initial:  Grad{{0.2, red}, {0.8, blue}},
			addT:     0.4,
			addColor: green,
			want:     Grad{{0.2, red}, {0.4, green}, {0.8, blue}},
		},
		{
			name:     "add semi-transparent color clips to premultiplied",
			initial:  Grad{},
			addT:     0.5,
			addColor: color.RGBA{255, 0, 0, 128},
			want:     Grad{{0.5, color.RGBA{128, 0, 0, 128}}},
		},
		{
			name:     "replace opaque with semi-transparent clips to premultiplied",
			initial:  Grad{{0.0, red}, {0.5, green}, {1.0, blue}},
			addT:     0.5,
			addColor: color.RGBA{0, 255, 0, 64},
			want:     Grad{{0.0, red}, {0.5, color.RGBA{0, 64, 0, 64}}, {1.0, blue}},
		},
		{
			name:     "fully transparent color",
			initial:  Grad{{0.0, red}},
			addT:     1.0,
			addColor: color.RGBA{0, 0, 0, 0},
			want:     Grad{{0.0, red}, {1.0, color.RGBA{0, 0, 0, 0}}},
		},
		{
			name:     "mixed transparent stops maintain order",
			initial:  Grad{{0.0, color.RGBA{255, 0, 0, 200}}, {1.0, color.RGBA{0, 0, 255, 100}}},
			addT:     0.5,
			addColor: color.RGBA{0, 255, 0, 50},
			want:     Grad{{0.0, color.RGBA{255, 0, 0, 200}}, {0.5, color.RGBA{0, 50, 0, 50}}, {1.0, color.RGBA{0, 0, 255, 100}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := make(Grad, len(tt.initial))
			copy(g, tt.initial)
			g.Add(tt.addT, tt.addColor)

			if len(g) != len(tt.want) {
				t.Fatalf("got %d stops, want %d", len(g), len(tt.want))
			}
			for i := range tt.want {
				if !Equal(g[i].Offset, tt.want[i].Offset) {
					t.Errorf("stop[%d].Offset = %v, want %v", i, g[i].Offset, tt.want[i].Offset)
				}
				if g[i].Color != tt.want[i].Color {
					t.Errorf("stop[%d].Color = %v, want %v", i, g[i].Color, tt.want[i].Color)
				}
			}
		})
	}
}

func TestGradTransparentCarrierIsNotReturnedAsPremultipliedPixel(t *testing.T) {
	g := Grad{{Offset: 0, Color: color.RGBA{R: 255}}, {Offset: 1, Color: color.RGBA{B: 255, A: 255}}}
	for _, position := range []float64{-1, 0, math.NaN()} {
		if got := g.At(position); got != Transparent {
			t.Fatalf("transparent endpoint at %g leaked color: %v", position, got)
		}
	}
	if got := g.At(.5); got != (color.RGBA{R: 63, B: 63, A: 127}) {
		t.Fatalf("carrier midpoint: %v", got)
	}
	for _, position := range []float64{.1, .25, .5, .75, .9} {
		c := g.At(position)
		if c.R > c.A || c.G > c.A || c.B > c.A {
			t.Fatalf("invalid premultiplied pixel at %g: %v", position, c)
		}
	}
	if g[0].Color.R != 255 {
		t.Fatal("sampling mutated the stop carrier")
	}
}

func TestRadialGradientAt(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}

	// Helper to build a radial gradient with red at t=0 and blue at t=1.
	makeGrad := func(c0 Point, r0 float64, c1 Point, r1 float64) *RadialGradient {
		g := NewRadialGradient(c0, r0, c1, r1)
		g.Add(0.0, red)
		g.Add(1.0, blue)
		return g
	}

	tests := []struct {
		name string
		grad *RadialGradient
		x, y float64
		want color.RGBA
	}{
		{
			name: "empty gradient returns transparent",
			grad: NewRadialGradient(Point{0, 0}, 0, Point{0, 0}, 10),
			x:    5,
			y:    0,
			want: Transparent,
		},
		{
			name: "concentric at center returns first stop",
			grad: makeGrad(Point{0, 0}, 0, Point{0, 0}, 10),
			x:    0,
			y:    0,
			want: red,
		},
		{
			name: "concentric at outer edge returns last stop",
			grad: makeGrad(Point{0, 0}, 0, Point{0, 0}, 10),
			x:    10,
			y:    0,
			want: blue,
		},
		{
			name: "concentric beyond outer edge clamps to last stop",
			grad: makeGrad(Point{0, 0}, 0, Point{0, 0}, 10),
			x:    20,
			y:    0,
			want: blue,
		},
		{
			name: "concentric at midpoint interpolates",
			grad: makeGrad(Point{0, 0}, 0, Point{0, 0}, 10),
			x:    5,
			y:    0,
			want: color.RGBA{128, 0, 127, 255},
		},
		{
			name: "concentric along y axis",
			grad: makeGrad(Point{0, 0}, 0, Point{0, 0}, 10),
			x:    0,
			y:    10,
			want: blue,
		},
		{
			name: "concentric with offset center",
			grad: makeGrad(Point{5, 5}, 0, Point{5, 5}, 10),
			x:    5,
			y:    5,
			want: red,
		},
		{
			name: "concentric with offset center at edge",
			grad: makeGrad(Point{5, 5}, 0, Point{5, 5}, 10),
			x:    15,
			y:    5,
			want: blue,
		},
		{
			name: "non-concentric gradient at inner center",
			grad: makeGrad(Point{0, 0}, 0, Point{10, 0}, 10),
			x:    0,
			y:    0,
			want: red,
		},
		{
			name: "single stop gradient returns that stop everywhere",
			grad: func() *RadialGradient {
				g := NewRadialGradient(Point{0, 0}, 0, Point{0, 0}, 10)
				g.Add(0.0, red)
				return g
			}(),
			x:    5,
			y:    0,
			want: red,
		},
		{
			name: "offset centers uses valid root not largest root",
			grad: func() *RadialGradient {
				g := NewRadialGradient(Point{30, 30}, 5, Point{170, 170}, 60)
				g.Add(0.0, color.RGBA{255, 192, 203, 255})
				g.Add(0.5, color.RGBA{255, 255, 255, 255})
				g.Add(1.0, color.RGBA{0, 128, 0, 255})
				return g
			}(),
			x:    160,
			y:    111,
			want: color.RGBA{180, 218, 180, 255},
		},
		{
			name: "offset centers near outer boundary not clamped to last stop",
			grad: func() *RadialGradient {
				g := NewRadialGradient(Point{30, 30}, 5, Point{170, 170}, 60)
				g.Add(0.0, color.RGBA{255, 192, 203, 255})
				g.Add(0.5, color.RGBA{255, 255, 255, 255})
				g.Add(1.0, color.RGBA{0, 128, 0, 255})
				return g
			}(),
			x:    165,
			y:    111,
			want: color.RGBA{164, 210, 164, 255},
		},
		{
			name: "concentric with semi-transparent stops",
			grad: func() *RadialGradient {
				g := NewRadialGradient(Point{0, 0}, 0, Point{0, 0}, 10)
				g.Add(0.0, color.RGBA{128, 0, 0, 128})
				g.Add(1.0, color.RGBA{0, 0, 128, 128})
				return g
			}(),
			x:    0,
			y:    0,
			want: color.RGBA{128, 0, 0, 128},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.grad.At(tt.x, tt.y)
			if !colorApproxEqual(got, tt.want) {
				t.Errorf("At(%v, %v) = %v, want %v (within ±1 per channel)",
					tt.x, tt.y, formatRGBA(got), formatRGBA(tt.want))
			}
		})
	}
}

func formatRGBA(c color.RGBA) string {
	return fmt.Sprintf("RGBA{%d, %d, %d, %d}", c.R, c.G, c.B, c.A)
}

// colorApproxEqual allows small rounding differences from interpolation.
func colorApproxEqual(a, b color.RGBA) bool {
	diff := func(x, y uint8) int {
		d := int(x) - int(y)
		if d < 0 {
			return -d
		}
		return d
	}
	return diff(a.R, b.R) <= 1 && diff(a.G, b.G) <= 1 && diff(a.B, b.B) <= 1 && diff(a.A, b.A) <= 1
}

func TestLinearGradientExtend(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}

	// Horizontal gradient from x=0 (red) to x=10 (blue).
	makeGrad := func() *LinearGradient {
		g := NewLinearGradient(Point{0, 0}, Point{10, 0})
		g.Add(0.0, red)
		g.Add(1.0, blue)
		return g
	}

	tests := []struct {
		name   string
		extend [2]bool
		x, y   float64
		want   color.RGBA
	}{
		{name: "both sides extend beyond start", extend: [2]bool{true, true}, x: -5, y: 0, want: red},
		{name: "both sides extend beyond end", extend: [2]bool{true, true}, x: 20, y: 0, want: blue},
		{name: "no extend leaves region before start unpainted", extend: [2]bool{false, true}, x: -5, y: 0, want: Transparent},
		{name: "no extend leaves region after end unpainted", extend: [2]bool{true, false}, x: 20, y: 0, want: Transparent},
		{name: "neither side extends", extend: [2]bool{false, false}, x: 5, y: 0, want: color.RGBA{128, 0, 127, 255}},
		{name: "inside range is painted when both sides closed", extend: [2]bool{false, false}, x: -5, y: 0, want: Transparent},
		{name: "inside range is painted when only end closed", extend: [2]bool{true, false}, x: 5, y: 0, want: color.RGBA{128, 0, 127, 255}},
		{name: "exact start point", extend: [2]bool{false, false}, x: 0, y: 0, want: red},
		{name: "exact end point", extend: [2]bool{false, false}, x: 10, y: 0, want: blue},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := makeGrad()
			g.Extend = tt.extend
			got := g.At(tt.x, tt.y)
			if !colorApproxEqual(got, tt.want) {
				t.Errorf("At(%v, %v) = %v, want %v (within ±1 per channel)",
					tt.x, tt.y, formatRGBA(got), formatRGBA(tt.want))
			}
		})
	}
}

func TestLinearGradientVerticalAndDiagonalExtend(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}

	vertical := NewLinearGradient(Point{0, 0}, Point{0, 10})
	vertical.Add(0.0, red)
	vertical.Add(1.0, blue)
	vertical.Extend = [2]bool{true, false}
	if got := vertical.At(0, 20); got != Transparent {
		t.Errorf("vertical At(0, 20) = %v, want %v", formatRGBA(got), formatRGBA(Transparent))
	}
	if got := vertical.At(0, -20); got != red {
		t.Errorf("vertical At(0, -20) = %v, want %v", formatRGBA(got), formatRGBA(red))
	}

	// The d.X == 0 && d.Y == 0 branch divides by d2, which is now guarded.
	diagonal := NewLinearGradient(Point{0, 0}, Point{10, 10})
	diagonal.Add(0.0, red)
	diagonal.Add(1.0, blue)
	diagonal.Extend = [2]bool{false, false}
	if got := diagonal.At(-1, -1); got != Transparent {
		t.Errorf("diagonal At(-1, -1) = %v, want %v", formatRGBA(got), formatRGBA(Transparent))
	}
	if got := diagonal.At(20, 20); got != Transparent {
		t.Errorf("diagonal At(20, 20) = %v, want %v", formatRGBA(got), formatRGBA(Transparent))
	}

	// Grad.At(NaN) falls through to the last stop, so at() has to reject NaN
	// coordinates itself rather than letting them reach the interpolation.
	nan := NewLinearGradient(Point{0, 0}, Point{10, 0})
	nan.Add(0.0, red)
	nan.Add(1.0, blue)
	nan.Extend = [2]bool{true, true}
	if got := nan.At(math.NaN(), 0); got != Transparent {
		t.Errorf("At(NaN, 0) = %v, want %v", formatRGBA(got), formatRGBA(Transparent))
	}
	// A horizontal gradient only reads p.X, so a NaN y is legitimately ignored
	// there; a diagonal one divides p.Dot(d) by d2 and does propagate it.
	nanDiagonal := NewLinearGradient(Point{0, 0}, Point{10, 10})
	nanDiagonal.Add(0.0, red)
	nanDiagonal.Add(1.0, blue)
	nanDiagonal.Extend = [2]bool{true, true}
	if got := nanDiagonal.At(5, math.NaN()); got != Transparent {
		t.Errorf("diagonal At(5, NaN) = %v, want %v", formatRGBA(got), formatRGBA(Transparent))
	}

	// A zero-length gradient divides by d2 == 0, which is now guarded by
	// falling back to t = 0. Since 0 is inside [0,1] the gradient paints the
	// first stop everywhere; what matters is that it no longer yields the
	// NaN-derived colours the unguarded division produced.
	degenerate := NewLinearGradient(Point{5, 5}, Point{5, 5})
	degenerate.Add(0.0, red)
	degenerate.Add(1.0, blue)
	degenerate.Extend = [2]bool{false, false}
	if got := degenerate.At(5, 5); !colorApproxEqual(got, red) {
		t.Errorf("degenerate At(5, 5) = %v, want %v", formatRGBA(got), formatRGBA(red))
	}
	if got := degenerate.At(50, 50); !colorApproxEqual(got, red) {
		t.Errorf("degenerate At(50, 50) = %v, want %v", formatRGBA(got), formatRGBA(red))
	}
}

func TestGradientExtendDefaultsToPad(t *testing.T) {
	if got := NewLinearGradient(Point{0, 0}, Point{1, 0}).Extend; got != [2]bool{true, true} {
		t.Errorf("NewLinearGradient Extend = %v, want [true true]", got)
	}
	if got := NewRadialGradient(Point{0, 0}, 0, Point{0, 0}, 1).Extend; got != [2]bool{true, true} {
		t.Errorf("NewRadialGradient Extend = %v, want [true true]", got)
	}
}

func TestRadialGradientExtend(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}

	// Concentric gradient, radius 0 to 10.
	makeGrad := func() *RadialGradient {
		g := NewRadialGradient(Point{0, 0}, 0, Point{0, 0}, 10)
		g.Add(0.0, red)
		g.Add(1.0, blue)
		return g
	}

	tests := []struct {
		name   string
		extend [2]bool
		x, y   float64
		want   color.RGBA
	}{
		{name: "both sides extend beyond outer circle", extend: [2]bool{true, true}, x: 20, y: 0, want: blue},
		{name: "no extend beyond outer circle is unpainted", extend: [2]bool{true, false}, x: 20, y: 0, want: Transparent},
		{name: "no extend on either side", extend: [2]bool{false, false}, x: 20, y: 0, want: Transparent},
		{name: "inside the outer circle is still painted", extend: [2]bool{false, false}, x: 5, y: 0, want: color.RGBA{128, 0, 127, 255}},
		{name: "at the outer circle", extend: [2]bool{false, false}, x: 10, y: 0, want: blue},
		{name: "extend beyond inner circle is painted", extend: [2]bool{true, true}, x: 0, y: 0, want: red},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := makeGrad()
			g.Extend = tt.extend
			got := g.At(tt.x, tt.y)
			if !colorApproxEqual(got, tt.want) {
				t.Errorf("At(%v, %v) = %v, want %v (within ±1 per channel)",
					tt.x, tt.y, formatRGBA(got), formatRGBA(tt.want))
			}
		})
	}
}

// radialAtPadReference is the pre-Extend implementation of RadialGradient.At,
// kept here so the default path can be checked against it.
// radialAtPadReference mirrors RadialGradient.At for the default Extend =
// [true, true] case, so that a refactor cannot change the default result without
// the sweep below noticing. It intentionally repeats the implementation instead
// of calling At: sharing code would make the comparison vacuous.
func radialAtPadReference(g *RadialGradient, x, y float64) color.RGBA {
	if len(g.Grad) == 0 {
		return Transparent
	}
	pd := Point{x, y}.Sub(g.C0)
	b := pd.Dot(g.cd) + g.R0*g.dr
	c := pd.Dot(pd) - g.R0*g.R0
	// Points off every circle of the family (negative discriminant) are outside
	// the shading and stay transparent; see the comment in At.
	if b*b-g.a*c < 0 {
		return Transparent
	}
	t0, t1 := solveQuadraticFormula(g.a, -2.0*b, c)

	valid := func(t float64) bool {
		return !math.IsNaN(t) && t >= 0 && t <= 1 && g.R0+g.dr*t >= 0
	}
	hasPositive := func(t float64) bool {
		return !math.IsNaN(t) && t > 0 && g.R0+g.dr*t >= 0
	}
	if valid(t1) {
		return g.Grad.At(t1)
	}
	if valid(t0) {
		return g.Grad.At(t0)
	}
	if hasPositive(t0) || hasPositive(t1) {
		return g.Grad.At(1)
	}
	return g.Grad.At(0)
}

func TestRadialGradientDefaultExtendMatchesReference(t *testing.T) {
	grads := []*RadialGradient{
		NewRadialGradient(Point{0, 0}, 0, Point{0, 0}, 10),
		NewRadialGradient(Point{0, 0}, 2, Point{0, 0}, 10),
		NewRadialGradient(Point{30, 30}, 5, Point{170, 170}, 60),
		NewRadialGradient(Point{0, 0}, 0, Point{10, 0}, 10),
		NewRadialGradient(Point{10, 10}, 10, Point{10, 10}, 0),
		NewRadialGradient(Point{5, 5}, 0, Point{5, 5}, 0),
	}
	colors := []color.RGBA{
		{255, 192, 203, 255},
		{255, 255, 255, 255},
		{0, 128, 0, 255},
		{128, 0, 0, 128},
		{0, 0, 128, 128},
	}
	for i, g := range grads {
		g.Add(0.0, colors[i%len(colors)])
		g.Add(0.5, colors[(i+1)%len(colors)])
		g.Add(1.0, colors[(i+2)%len(colors)])
	}

	for i, g := range grads {
		t.Run(fmt.Sprintf("grad%d", i), func(t *testing.T) {
			if g.Extend != [2]bool{true, true} {
				t.Fatalf("Extend = %v, want [true true]", g.Extend)
			}
			for x := -20.0; x <= 200.0; x += 7.5 {
				for y := -20.0; y <= 200.0; y += 7.5 {
					got := g.At(x, y)
					want := radialAtPadReference(g, x, y)
					if !colorApproxEqual(got, want) {
						t.Fatalf("At(%v, %v) = %v, want %v", x, y,
							formatRGBA(got), formatRGBA(want))
					}
				}
			}
		})
	}
}

// A disjoint eccentric shading has a region that lies on no circle of the
// interpolated family (negative discriminant). Such points must stay
// transparent, and in particular must not change with the Extend bits: the same
// point used to come out transparent for one-sided extend and painted with the
// start colour when both ends extended.
func TestRadialGradientDisjointGapStaysTransparent(t *testing.T) {
	// Circles are far apart: centres 34 apart with radii 8 and 18.
	newGradient := func() *RadialGradient {
		g := NewRadialGradient(Point{18, 35}, 8, Point{52, 35}, 18)
		g.Add(0.0, color.RGBA{255, 0, 0, 255})
		g.Add(1.0, color.RGBA{0, 0, 255, 255})
		return g
	}
	// Off-family points: to the side of the start circle, away from the end
	// circle, where the two-circle sweep has no solution at all (negative
	// discriminant). Each pair was picked from a grid scan of that region.
	gap := []Point{{X: 4, Y: 27}, {X: 4, Y: 43}, {X: 2, Y: 30}, {X: 4, Y: 24}}
	for _, extend := range [][2]bool{{true, false}, {false, true}, {true, true}} {
		g := newGradient()
		g.Extend = extend
		for _, pt := range gap {
			if got := g.At(pt.X, pt.Y); got.A != 0 {
				t.Errorf("Extend = %v, At(%v, %v) = %v, want transparent",
					extend, pt.X, pt.Y, formatRGBA(got))
			}
		}
	}

	// Points that are on the family keep their colour, so the check above is not
	// passing simply because everything went transparent.
	g := newGradient()
	g.Extend = [2]bool{true, true}
	if got := g.At(18, 35); got.A == 0 {
		t.Errorf("start circle centre = %v, want opaque", formatRGBA(got))
	}
	if got := g.At(70, 35); got.A == 0 {
		t.Errorf("past the end circle = %v, want opaque", formatRGBA(got))
	}
}

func TestRadialParameter(t *testing.T) {
	// r(t) = 10t, so every point on the +x axis at distance d has the single
	// root t = d, and any t < 0 would have a negative radius.
	concentric := NewRadialGradient(Point{0, 0}, 0, Point{0, 0}, 10)
	// r(t) = 10 + 10t, so t < 0 still has a positive radius and extend bit 0
	// becomes meaningful.
	growing := NewRadialGradient(Point{0, 0}, 10, Point{0, 0}, 20)
	// r(t) = -10t: the root with the larger t is the one with the negative
	// radius and must be rejected in favour of the smaller root.
	shrinking := NewRadialGradient(Point{0, 0}, 0, Point{0, 0}, -10)
	// cd.Dot(cd) - dr*dr == 0, so the quadratic degenerates to the linear
	// fallback in RadialParameter.
	degenerate := NewRadialGradient(Point{0, 0}, 0, Point{10, 0}, 10)
	// Both a and b vanish, so no solution exists at any point.
	collapsed := NewRadialGradient(Point{0, 0}, 0, Point{0, 0}, 0)

	tests := []struct {
		name   string
		grad   *RadialGradient
		x, y   float64
		extend int
		wantT  float64
		wantOK bool
	}{
		{name: "concentric midpoint", grad: concentric, x: 5, y: 0, extend: 0, wantT: 0.5, wantOK: true},
		{name: "concentric beyond end needs extend bit 1", grad: concentric, x: 20, y: 0, extend: 0, wantOK: false},
		{name: "concentric beyond end allowed by extend bit 1", grad: concentric, x: 20, y: 0, extend: 2, wantT: 2, wantOK: true},
		{name: "root with negative radius is rejected", grad: concentric, x: -5, y: 0, extend: 0, wantT: 0.5, wantOK: true},
		{name: "inside the inner circle has no solution", grad: growing, x: 0, y: 0, extend: 0, wantOK: false},
		{name: "t below zero with positive radius needs extend bit 0", grad: growing, x: 5, y: 0, extend: 0, wantOK: false},
		{name: "t below zero with positive radius allowed by extend bit 0", grad: growing, x: 5, y: 0, extend: 1, wantT: -0.5, wantOK: true},
		{name: "inside the gradient range", grad: growing, x: 12, y: 0, extend: 0, wantT: 0.2, wantOK: true},
		{name: "larger root has negative radius so the smaller one is used", grad: shrinking, x: 5, y: 0, extend: 0, wantOK: false},
		{name: "smaller root is used once extend bit 0 is set", grad: shrinking, x: 5, y: 0, extend: 1, wantT: -0.5, wantOK: true},
		{name: "degenerate linear family", grad: degenerate, x: 5, y: 0, extend: 0, wantT: 0.25, wantOK: true},
		{name: "degenerate linear family beyond end", grad: degenerate, x: 25, y: 0, extend: 0, wantOK: false},
		{name: "degenerate linear family beyond end allowed by extend", grad: degenerate, x: 25, y: 0, extend: 2, wantT: 1.25, wantOK: true},
		{name: "degenerate family has no solution behind the start", grad: degenerate, x: -5, y: 0, extend: 3, wantOK: false},
		{name: "vanishing a and b have no solution", grad: collapsed, x: 5, y: 0, extend: 3, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.grad.RadialParameter(tt.x, tt.y, tt.extend)
			if ok != tt.wantOK {
				t.Fatalf("RadialParameter(%v, %v, %d) ok = %v, want %v", tt.x, tt.y, tt.extend, ok, tt.wantOK)
			}
			if ok && !Equal(got, tt.wantT) {
				t.Errorf("RadialParameter(%v, %v, %d) = %v, want %v",
					tt.x, tt.y, tt.extend, got, tt.wantT)
			}
		})
	}
}

// TestRadialParameterPrefersLargestT covers the two-circle case. With a focal
// family the quadratic has two roots that are both in range and both have a
// non-negative radius, so the choice between them is observable and the
// larger one has to win.
func TestRadialParameterPrefersLargestT(t *testing.T) {
	// r(t) = 10t, centre moving from (0,0) to (30,0). cd.Dot(cd) - dr*dr is
	// 800, so the quadratic is genuinely non-degenerate and (20,0) yields the
	// roots 0.5 and 1.0, both admissible.
	g := NewRadialGradient(Point{0, 0}, 0, Point{30, 0}, 10)
	got, ok := g.RadialParameter(20, 0, 0)
	if !ok || !Equal(got, 1.0) {
		t.Errorf("RadialParameter(20, 0, 0) = %v, %v; want 1, true", got, ok)
	}
	// The smaller root must never be preferred, whichever extend bits are set.
	for _, extend := range []int{1, 2, 3} {
		got, ok := g.RadialParameter(20, 0, extend)
		if !ok || !Equal(got, 1.0) {
			t.Errorf("RadialParameter(20, 0, %d) = %v, %v; want 1, true", extend, got, ok)
		}
	}
	// At (24,0) only the smaller root stays inside the sweep. The point is well
	// within the t=1 circle (6 units from its centre against radius 10), so the
	// sweep colour must win no matter which extend bits are set: an end-side
	// extension only paints points the sweep misses, it must not overwrite the
	// sweep with the end colour.
	if got, ok := g.RadialParameter(24, 0, 0); !ok || !Equal(got, 0.6) {
		t.Errorf("RadialParameter(24, 0, 0) = %v, %v; want 0.6, true", got, ok)
	}
	for _, extend := range []int{1, 2, 3} {
		if got, ok := g.RadialParameter(24, 0, extend); !ok || !Equal(got, 0.6) {
			t.Errorf("RadialParameter(24, 0, %d) = %v, %v; want 0.6, true", extend, got, ok)
		}
	}
	// (45,0) is past the outer circle, so both roots exceed 1 and there is no
	// sweep value: only extend bit 1 paints it, and then the largest root wins.
	if got, ok := g.RadialParameter(45, 0, 2); !ok || !Equal(got, 2.25) {
		t.Errorf("RadialParameter(45, 0, 2) = %v, %v; want 2.25, true", got, ok)
	}
	for _, extend := range []int{0, 1} {
		if got, ok := g.RadialParameter(45, 0, extend); ok {
			t.Errorf("RadialParameter(45, 0, %d) = %v, %v; want _, false", extend, got, ok)
		}
	}
	// Concentric growing family, r(t) = 10+10t: at (5,0) both roots are
	// negative, so only extend bit 0 paints the point.
	growing := NewRadialGradient(Point{0, 0}, 10, Point{0, 0}, 20)
	if got, ok := growing.RadialParameter(5, 0, 1); !ok || !Equal(got, -0.5) {
		t.Errorf("RadialParameter(5, 0, 1) = %v, %v; want -0.5, true", got, ok)
	}
	for _, extend := range []int{0, 2} {
		if got, ok := growing.RadialParameter(5, 0, extend); ok {
			t.Errorf("RadialParameter(5, 0, %d) = %v, %v; want _, false", extend, got, ok)
		}
	}
}
