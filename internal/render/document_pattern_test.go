package render

import (
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

func TestPatternStepsDefaultToCellSize(t *testing.T) {
	pattern := &models.CtPattern{Width: 20, Height: 12}
	xStep, yStep := patternSteps(pattern)
	if xStep != 20 || yStep != 12 {
		t.Fatalf("steps = (%v, %v), want (20, 12)", xStep, yStep)
	}

	pattern.XStep = 8
	pattern.YStep = 6
	xStep, yStep = patternSteps(pattern)
	if xStep != 20 || yStep != 12 {
		t.Fatalf("undersized steps = (%v, %v), want (20, 12)", xStep, yStep)
	}
}

func TestPatternReflection(t *testing.T) {
	tests := []struct {
		name   string
		method string
		ix, iy int
		point  canvas.Point
		want   canvas.Point
	}{
		{"normal", "Normal", 0, 0, canvas.Point{X: 3, Y: 4}, canvas.Point{X: 3, Y: 4}},
		{"row", "Row", 0, 1, canvas.Point{X: 3, Y: 4}, canvas.Point{X: 17, Y: 4}},
		{"column", "Column", 1, 0, canvas.Point{X: 3, Y: 4}, canvas.Point{X: 17, Y: 4}},
		{"row and column", "RowAndColumn", 1, 1, canvas.Point{X: 3, Y: 4}, canvas.Point{X: 17, Y: 16}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reflection := patternReflection(tt.method, 20, 20, tt.ix, tt.iy)
			gx, gy := reflection.TransformPoint(models.StPos{X: tt.point.X, Y: tt.point.Y})
			if gx != tt.want.X || gy != tt.want.Y {
				t.Fatalf("unexpected reflected point: got=(%v,%v), want=%v", gx, gy, tt.want)
			}
		})
	}
}

func TestInvertCTM(t *testing.T) {
	matrix := models.CTM{2, 1, 0.5, 3, 10, 20}
	inverse, ok := invertCTM(matrix)
	if !ok {
		t.Fatal("expected invertible matrix")
	}
	point := models.StPos{X: 7, Y: 11}
	x, y := inverse.TransformPoint(models.StPos{X: matrix[0]*point.X + matrix[2]*point.Y + matrix[4], Y: matrix[1]*point.X + matrix[3]*point.Y + matrix[5]})
	if x < point.X-1e-9 || x > point.X+1e-9 || y < point.Y-1e-9 || y > point.Y+1e-9 {
		t.Fatalf("inverse did not restore point: got=(%v,%v), want=%v", x, y, point)
	}
}

func TestPatternCTMParse(t *testing.T) {
	tests := []struct {
		name   string
		ctm    models.StArray
		want   models.CTM
		wantOK bool
	}{
		{
			name:   "identity",
			ctm:    models.StArray{"1", "0", "0", "1", "0", "0"},
			want:   models.CTM{1, 0, 0, 1, 0, 0},
			wantOK: true,
		},
		{
			name:   "translation",
			ctm:    models.StArray{"1", "0", "0", "1", "10", "20"},
			want:   models.CTM{1, 0, 0, 1, 10, 20},
			wantOK: true,
		},
		{
			name:   "scale",
			ctm:    models.StArray{"2", "0", "0", "3", "0", "0"},
			want:   models.CTM{2, 0, 0, 3, 0, 0},
			wantOK: true,
		},
		{
			name:   "general",
			ctm:    models.StArray{"2", "1", "0.5", "3", "10", "20"},
			want:   models.CTM{2, 1, 0.5, 3, 10, 20},
			wantOK: true,
		},
		{
			name:   "singular",
			ctm:    models.StArray{"1", "2", "2", "4", "0", "0"},
			wantOK: false,
		},
		{
			name:   "wrong count",
			ctm:    models.StArray{"1", "0", "0"},
			wantOK: false,
		},
		{
			name:   "non-numeric",
			ctm:    models.StArray{"1", "0", "0", "x", "0", "0"},
			wantOK: false,
		},
		{
			name:   "nil",
			ctm:    nil,
			want:   models.IdentityMatrix,
			wantOK: true,
		},
		{
			name:   "empty",
			ctm:    models.StArray{},
			want:   models.IdentityMatrix,
			wantOK: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := &models.CtPattern{CTM: tt.ctm}
			got, ok := patternCTM(pattern)
			if ok != tt.wantOK {
				t.Fatalf("patternCTM ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Fatalf("patternCTM = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPatternStepsNil(t *testing.T) {
	x, y := patternSteps(nil)
	if x != 0 || y != 0 {
		t.Fatalf("patternSteps(nil) = (%v, %v), want (0, 0)", x, y)
	}
}

func TestPatternStepsZeroAndNegative(t *testing.T) {
	tests := []struct {
		name         string
		w, h, xs, ys float64
		wantX, wantY float64
	}{
		{"zero xstep", 20, 20, 0, 20, 20, 20},
		{"zero ystep", 20, 20, 20, 0, 20, 20},
		{"negative xstep", 20, 20, -5, 20, 20, 20},
		{"negative ystep", 20, 20, 20, -5, 20, 20},
		{"both zero", 20, 20, 0, 0, 20, 20},
		{"both negative", 20, 20, -1, -1, 20, 20},
		{"xstep less than width", 20, 20, 10, 20, 20, 20},
		{"ystep less than height", 20, 20, 20, 10, 20, 20},
		{"both less", 20, 20, 10, 10, 20, 20},
		{"equal to size", 20, 20, 20, 20, 20, 20},
		{"larger than size", 20, 20, 30, 40, 30, 40},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := &models.CtPattern{Width: tt.w, Height: tt.h, XStep: tt.xs, YStep: tt.ys}
			gotX, gotY := patternSteps(pattern)
			if gotX != tt.wantX || gotY != tt.wantY {
				t.Fatalf("patternSteps = (%v, %v), want (%v, %v)", gotX, gotY, tt.wantX, tt.wantY)
			}
		})
	}
}

func TestPatternReflectionNormal(t *testing.T) {
	for _, method := range []string{"", "Normal", "normal", "NORMAL"} {
		for ix := 0; ix < 3; ix++ {
			for iy := 0; iy < 3; iy++ {
				reflection := patternReflection(method, 20, 20, ix, iy)
				if reflection != models.IdentityMatrix {
					t.Fatalf("method=%q ix=%d iy=%d: reflection = %v, want identity", method, ix, iy, reflection)
				}
			}
		}
	}
}

func TestPatternReflectionRow(t *testing.T) {
	width := 15.0
	for iy := 0; iy < 6; iy++ {
		reflection := patternReflection("Row", width, 20, 0, iy)
		// Row reflection: flipX for odd iy
		if iy%2 == 0 {
			// Even row: identity
			if reflection != models.IdentityMatrix {
				t.Fatalf("Row iy=%d: want identity, got %v", iy, reflection)
			}
		} else {
			// Odd row: horizontal flip around width
			gotX, _ := reflection.TransformPoint(models.StPos{X: 3, Y: 7})
			if gotX != width-3 {
				t.Fatalf("Row iy=%d: flipX(3) = %v, want %v", iy, gotX, width-3)
			}
		}
	}
}

func TestPatternReflectionColumn(t *testing.T) {
	height := 25.0
	for ix := range 6 {
		reflection := patternReflection("Column", 20, height, ix, 0)
		if ix%2 == 0 {
			if reflection != models.IdentityMatrix {
				t.Fatalf("Column ix=%d: want identity, got %v", ix, reflection)
			}
		} else {
			_, gotY := reflection.TransformPoint(models.StPos{X: 5, Y: 17})
			if gotY != height-8 {
				t.Fatalf("Column ix=%d: flipY(8) = %v, want %v", ix, gotY, height-8)
			}
		}
	}
}

func TestPatternReflectionRowAndColumn(t *testing.T) {
	width, height := 20.0, 20.0
	for ix := range 4 {
		for iy := range 4 {
			reflection := patternReflection("RowAndColumn", width, height, ix, iy)
			flipX := ix%2 != 0
			flipY := iy%2 != 0

			gotX, gotY := reflection.TransformPoint(models.StPos{X: 5, Y: 8})
			wantX := 5.0
			if flipX {
				wantX = width - 5
			}
			wantY := 8.0
			if flipY {
				wantY = height - 8
			}
			if gotX != wantX || gotY != wantY {
				t.Fatalf("RowAndColumn ix=%d iy=%d: got=(%v,%v), want=(%v,%v)", ix, iy, gotX, gotY, wantX, wantY)
			}
		}
	}
}

func TestInvertCTMSingular(t *testing.T) {
	tests := []struct {
		name   string
		matrix models.CTM
	}{
		{"zero det", models.CTM{1, 2, 2, 4, 0, 0}},
		{"identity-like singular", models.CTM{0, 0, 0, 0, 5, 6}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := invertCTM(tt.matrix)
			if ok {
				t.Fatal("expected singular matrix to fail inversion")
			}
		})
	}
}

func TestInvertCTMRoundTrip(t *testing.T) {
	matrices := []models.CTM{
		{1, 0, 0, 1, 0, 0},           // 恒等变换
		{2, 0, 0, 3, 10, 20},         // 缩放 + 平移
		{0.5, 0.3, -0.2, 1.5, 7, -3}, // 一般仿射变换
		{1, 0, 0, 1, 100, 200},       // 纯平移
		{0.5, 0, 0, 0.5, 0, 0},       // 等比缩放
	}
	for i, matrix := range matrices {
		inverse, ok := invertCTM(matrix)
		if !ok {
			t.Fatalf("matrix %d: failed to invert", i)
		}
		// Forward then inverse should restore original point
		for _, pt := range []models.StPos{{X: 0, Y: 0}, {X: 5, Y: 7}, {X: -3, Y: 12}} {
			fx, fy := matrix.TransformPoint(pt)
			gotX, gotY := inverse.TransformPoint(models.StPos{X: fx, Y: fy})
			if gotX < pt.X-1e-9 || gotX > pt.X+1e-9 || gotY < pt.Y-1e-9 || gotY > pt.Y+1e-9 {
				t.Fatalf("matrix %d: round-trip failed for %v: got=(%v,%v)", i, pt, gotX, gotY)
			}
		}
		// Inverse then forward should also restore
		for _, pt := range []models.StPos{{X: 1, Y: 2}, {X: 8, Y: -4}} {
			ix, iy := inverse.TransformPoint(pt)
			gotX, gotY := matrix.TransformPoint(models.StPos{X: ix, Y: iy})
			if gotX < pt.X-1e-9 || gotX > pt.X+1e-9 || gotY < pt.Y-1e-9 || gotY > pt.Y+1e-9 {
				t.Fatalf("matrix %d: inverse round-trip failed for %v: got=(%v,%v)", i, pt, gotX, gotY)
			}
		}
	}
}

func TestPatternTileCTMOrder(t *testing.T) {
	// Verify that the tile CTM composition matches the expected order:
	// Translate(origin) * parentCTM * objectCTM * patternCTM * tileOffset * reflection

	originX, originY := 30.0, 104.0
	objectCTM := models.CTM{2, 0, 0, 2, 0, 0}  // scale 2x
	patternCTM := models.CTM{1, 0, 0, 1, 5, 5} // translate (5,5)
	xStep, yStep := 20.0, 20.0

	// Build the expected CTM manually
	base := objectCTM
	base = *translationMatrix(originX, originY).Multiply(&base)
	base = *base.Multiply(&patternCTM)

	// Tile (1, 2) with no reflection
	tile := *base.Multiply(&models.CTM{
		1, 0, 0, 1,
		float64(1) * xStep,
		float64(2) * yStep,
	})

	// A cell content point at (0,0) should map to:
	// 1. patternCTM: (0,0) → (5,5)
	// 2. objectCTM: (5,5) → (10,10)
	// 3. origin: (10,10) → (40,114)
	// 4. tile offset: (40,114) → (60,154)
	gotX, gotY := tile.TransformPoint(models.StPos{X: 0, Y: 0})
	wantX := originX + 0*xStep + 5 + 0 // origin + tileOffset + patternTranslate + objectCTM(0,0).x
	wantY := originY + 2*yStep + 5 + 0
	// Full computation:
	// base = Translate(30,104) * Scale(2,2) * Translate(5,5)
	// base matrix:
	// a = 1*2 = 2, b = 0*2 = 0, c = 0*2 = 0, d = 1*2 = 2
	// e = 1*0 + 0*0 + 30 = 30, f = 0*0 + 1*0 + 104 = 104
	// Wait, let me compute more carefully.
	// Scale(2,2) = {2,0,0,2,0,0}
	// Translate(30,104) = {1,0,0,1,30,104}
	// Translate(30,104).Multiply(Scale(2,2)):
	//   a = 1*2 + 0*0 = 2
	//   b = 0*2 + 1*0 = 0
	//   c = 1*0 + 0*2 = 0
	//   d = 0*0 + 1*2 = 2
	//   e = 1*0 + 0*0 + 30 = 30
	//   f = 0*0 + 1*0 + 104 = 104
	// So intermediate = {2,0,0,2,30,104}
	//
	// Multiply by Translate(5,5):
	//   a = 2*1 + 0*0 = 2
	//   b = 0*1 + 2*0 = 0
	//   c = 2*0 + 0*1 = 0
	//   d = 0*0 + 2*1 = 2
	//   e = 2*5 + 0*5 + 30 = 40
	//   f = 0*5 + 2*5 + 104 = 114
	// So base = {2,0,0,2,40,114}
	//
	// Tile (1,2): base * Translate(20,40)
	//   e = 2*20 + 0*40 + 40 = 80
	//   f = 0*20 + 2*40 + 114 = 194
	// So tile = {2,0,0,2,80,194}
	//
	// TransformPoint(0,0) = (80, 194)

	wantX = 80.0
	wantY = 194.0
	if gotX != wantX || gotY != wantY {
		t.Fatalf("tile CTM: TransformPoint(0,0) = (%v, %v), want (%v, %v)", gotX, gotY, wantX, wantY)
	}
}
