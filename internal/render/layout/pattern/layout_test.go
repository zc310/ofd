package pattern

import (
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

func TestPatternCTMTransformsPoint(t *testing.T) {
	pattern := &models.CtPattern{CTM: &models.CTM{2, 3, 4, 5, 6, 7}}
	matrix, ok := patternCTM(pattern)
	if !ok {
		t.Fatal("expected valid pattern CTM")
	}
	x, y := matrix.TransformPoint(models.StPos{X: 1, Y: 2})
	if x != 16 || y != 20 {
		t.Fatalf("unexpected transformed point: (%v, %v)", x, y)
	}
}

func TestPatternCTMRejectsSingularMatrix(t *testing.T) {
	// 奇异矩阵无法用于图案平铺，应判为无效。
	pattern := &models.CtPattern{CTM: &models.CTM{1, 2, 2, 4, 0, 0}}
	if _, ok := patternCTM(pattern); ok {
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
		point  geom.Point
		want   geom.Point
	}{
		{"normal", "Normal", 0, 0, geom.Point{X: 3, Y: 4}, geom.Point{X: 3, Y: 4}},
		{"row", "Row", 0, 1, geom.Point{X: 3, Y: 4}, geom.Point{X: 3, Y: 16}},
		{"column", "Column", 1, 0, geom.Point{X: 3, Y: 4}, geom.Point{X: 17, Y: 4}},
		{"row and column", "RowAndColumn", 1, 1, geom.Point{X: 3, Y: 4}, geom.Point{X: 17, Y: 16}},
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

func TestPatternCTM(t *testing.T) {
	tests := []struct {
		name   string
		ctm    *models.CTM
		want   models.CTM
		wantOK bool
	}{
		{
			name:   "identity",
			ctm:    &models.CTM{1, 0, 0, 1, 0, 0},
			want:   models.CTM{1, 0, 0, 1, 0, 0},
			wantOK: true,
		},
		{
			name:   "general",
			ctm:    &models.CTM{2, 1, 0.5, 3, 10, 20},
			want:   models.CTM{2, 1, 0.5, 3, 10, 20},
			wantOK: true,
		},
		{
			name:   "singular",
			ctm:    &models.CTM{1, 2, 2, 4, 0, 0},
			wantOK: false,
		},
		{
			name:   "nil defaults to identity",
			ctm:    nil,
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
		for ix := range 3 {
			for iy := range 3 {
				reflection := patternReflection(method, 20, 20, ix, iy)
				if reflection != models.IdentityMatrix {
					t.Fatalf("method=%q ix=%d iy=%d: reflection = %v, want identity", method, ix, iy, reflection)
				}
			}
		}
	}
}

func TestPatternReflectionRow(t *testing.T) {
	const width, height = 15.0, 20.0
	for iy := range 6 {
		reflection := patternReflection("Row", width, height, 0, iy)
		// Row 模式：iy 为奇数时把单元上下镜像。
		if iy%2 == 0 {
			// 偶数行：恒等变换。
			if reflection != models.IdentityMatrix {
				t.Fatalf("Row iy=%d: want identity, got %v", iy, reflection)
			}
			continue
		}
		// 奇数行：上下镜像，X 不变、Y 取 height-y。
		gotX, gotY := reflection.TransformPoint(models.StPos{X: 3, Y: 7})
		if gotX != 3 || gotY != height-7 {
			t.Fatalf("Row iy=%d: flipY(3,7) = (%v,%v), want (3,%v)", iy, gotX, gotY, height-7)
		}
	}
}

func TestPatternReflectionColumn(t *testing.T) {
	const width, height = 20.0, 25.0
	for ix := range 6 {
		reflection := patternReflection("Column", width, height, ix, 0)
		// Column 模式：ix 为奇数时把单元左右镜像。
		if ix%2 == 0 {
			if reflection != models.IdentityMatrix {
				t.Fatalf("Column ix=%d: want identity, got %v", ix, reflection)
			}
			continue
		}
		// 奇数列：左右镜像，X 取 width-x、Y 不变。
		gotX, gotY := reflection.TransformPoint(models.StPos{X: 5, Y: 17})
		if gotX != width-5 || gotY != 17 {
			t.Fatalf("Column ix=%d: flipX(5,17) = (%v,%v), want (%v,17)", ix, gotX, gotY, width-5)
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
		// 先进行正向变换再进行逆变换，应恢复原始点。
		for _, pt := range []models.StPos{{X: 0, Y: 0}, {X: 5, Y: 7}, {X: -3, Y: 12}} {
			fx, fy := matrix.TransformPoint(pt)
			gotX, gotY := inverse.TransformPoint(models.StPos{X: fx, Y: fy})
			if gotX < pt.X-1e-9 || gotX > pt.X+1e-9 || gotY < pt.Y-1e-9 || gotY > pt.Y+1e-9 {
				t.Fatalf("matrix %d: round-trip failed for %v: got=(%v,%v)", i, pt, gotX, gotY)
			}
		}
		// 先进行逆变换再进行正向变换，也应恢复原始点。
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
	// Translate(30,104) * Scale(2,2) * Translate(5,5) * Translate(20,40)
	// 将图块 (1,2) 的单元原点映射到 (80,194)。
	base := *translationMatrix(30, 104).Multiply(&models.CTM{2, 0, 0, 2, 0, 0})
	base = *base.Multiply(&models.CTM{1, 0, 0, 1, 5, 5})
	tile := patternTileTransform(base, models.IdentityMatrix, 20, 40)
	gotX, gotY := tile.Transform(0, 0)
	if gotX != 80 || gotY != 194 {
		t.Fatalf("图块原点=(%v,%v)，期望(80,194)", gotX, gotY)
	}
}
