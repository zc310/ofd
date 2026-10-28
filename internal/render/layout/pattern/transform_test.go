package pattern

import (
	"fmt"
	"math"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

func TestPreparedPatternTileTransformKeepsReflectionAndMatrixOrder(t *testing.T) {
	base := models.CTM{1.2, -.3, .4, 2.1, 7, -9}
	for _, method := range []string{"Normal", "Row", "Column", "RowAndColumn"} {
		for ix := -3; ix <= 3; ix++ {
			for iy := -3; iy <= 3; iy++ {
				x, y := float64(ix)*2.3, float64(iy)*3.7
				reflection := patternReflection(method, 2, 3, ix, iy)
				want := *base.Multiply(&models.CTM{1, 0, 0, 1, x, y})
				want = *want.Multiply(&reflection)
				got := patternTileTransform(base, patternReflection(method, 2, 3, ix&1, iy&1), x, y)
				for i := range got {
					if math.Abs(got[i]-want[i]) > 1e-12 {
						t.Fatalf("%s(%d,%d) 第%d项=%g，期望%g", method, ix, iy, i, got[i], want[i])
					}
				}
			}
		}
	}
}

func BenchmarkPatternTileTransform(b *testing.B) {
	base := models.CTM{1.2, -.3, .4, 2.1, 7, -9}
	for _, prepared := range []bool{false, true} {
		b.Run(fmt.Sprintf("prepared=%v", prepared), func(b *testing.B) {
			var reflections [2][2]models.CTM
			for x := range 2 {
				for y := range 2 {
					reflections[x][y] = patternReflection("RowAndColumn", 2, 3, x, y)
				}
			}
			b.ReportAllocs()
			var tile models.CTM
			index := 0
			for b.Loop() {
				x, y := index%101, index/101%103
				if prepared {
					tile = patternTileTransform(base, reflections[x&1][y&1], float64(x)*2.3, float64(y)*3.7)
				} else {
					tile = *base.Multiply(&models.CTM{1, 0, 0, 1, float64(x) * 2.3, float64(y) * 3.7})
					reflection := patternReflection("RowAndColumn", 2, 3, x, y)
					tile = *tile.Multiply(&reflection)
				}
				index++
			}
			if !tile.IsFinite() {
				b.Fatal("图块矩阵非法")
			}
		})
	}
}

func TestPatternTileRangeRejectsOverflowAndKeepsNormalMargins(t *testing.T) {
	if start, count, ok := patternTileRange(-2, 10, 2); !ok || start != -2 || count != 9 {
		t.Fatalf("普通图块范围=%d,%d,%v", start, count, ok)
	}
	maximum := float64(int(^uint(0) >> 1))
	for _, tc := range [][3]float64{
		{1e300, 1e300, math.SmallestNonzeroFloat64}, {maximum, maximum, 1},
		{-maximum, maximum, 1}, {math.NaN(), 1, 1}, {0, 1, 0},
	} {
		if _, _, ok := patternTileRange(tc[0], tc[1], tc[2]); ok {
			t.Fatalf("越界索引未被拒绝：%v", tc)
		}
	}
}

func TestPlanPreservesObjectOriginParentTransformAndNegativeTileParity(t *testing.T) {
	parent := models.CTM{1, 0, 0, 1, 7, 9}
	object := models.CTGraphicUnit{Boundary: models.StBox{X: 30, Y: 40}, CTM: &models.CTM{2, 0, 0, 2, 0, 0}}
	pattern := &models.CtPattern{Width: 2, Height: 3, ReflectMethod: "RowAndColumn", CTM: &models.CTM{1, 0, 0, 1, 5, 6}}
	plan, status := New(pattern, object, models.StBox{Width: 100, Height: 100}, geom.Rect{X0: 40, Y0: 30, X1: 60, Y1: 50}, &parent)
	if status != Ready {
		t.Fatalf("有效平铺计划被拒绝: %v", status)
	}
	for col := range plan.Columns() {
		for row := range plan.Rows() {
			tile, ok := plan.Tile(col, row)
			if !ok {
				t.Fatal("有效图块矩阵被拒绝")
			}
			ix, iy := plan.startX+col, plan.startY+row
			// 从单元点 (0.5,1) 出发，先镜像和平铺，再作 pattern、object、parent 变换及 Boundary 平移。
			x, y := 0.5, 1.0
			if ix&1 != 0 {
				x = 2 - x
			}
			if iy&1 != 0 {
				y = 3 - y
			}
			wantX := 30 + 7 + 2*(5+float64(ix)*2+x)
			wantY := 40 + 9 + 2*(6+float64(iy)*3+y)
			gotX, gotY := tile.Transform(0.5, 1)
			if gotX != wantX || gotY != wantY {
				t.Fatalf("图块变换=(%g,%g)，期望(%g,%g)", gotX, gotY, wantX, wantY)
			}
		}
	}
}

func TestPlanRejectsOutOfRangeIndexesBeforeIndexArithmetic(t *testing.T) {
	plan, status := New(&models.CtPattern{Width: 2, Height: 3}, models.CTGraphicUnit{},
		models.StBox{Width: 100, Height: 100}, geom.Rect{X0: -10, Y0: 80, X1: 10, Y1: 100}, nil)
	if status != Ready {
		t.Fatalf("有效平铺计划被拒绝: %v", status)
	}
	for _, index := range [][2]int{{0, 0}, {plan.Columns() - 1, plan.Rows() - 1}} {
		if _, ok := plan.Tile(index[0], index[1]); !ok {
			t.Errorf("拒绝了有效边界索引 %v", index)
		}
	}
	maxInt := int(^uint(0) >> 1)
	for _, index := range [][2]int{
		{-1, 0}, {0, -1}, {plan.Columns(), 0}, {0, plan.Rows()},
		{maxInt, 0}, {0, maxInt}, {-maxInt - 1, 0}, {0, -maxInt - 1},
	} {
		if _, ok := plan.Tile(index[0], index[1]); ok {
			t.Errorf("接受了越界索引 %v", index)
		}
	}
}

func TestUnreadyPlansNeverProvideTileTransforms(t *testing.T) {
	invalid, invalidStatus := New(nil, models.CTGraphicUnit{}, models.StBox{}, geom.Rect{}, nil)
	// 两个轴各自合法，但总图块数超限；行列数仍应保留以供诊断。
	limited, limitedStatus := New(&models.CtPattern{Width: 1, Height: 1}, models.CTGraphicUnit{},
		models.StBox{Width: 1000, Height: 1000}, geom.Rect{X1: 1000, Y1: 1000}, nil)
	if invalidStatus != Invalid || limitedStatus != LimitExceeded || limited.Columns() != 1003 || limited.Rows() != 1003 {
		t.Fatalf("无效或超限状态错误: invalid=%v limited=%v (%d,%d)", invalidStatus, limitedStatus, limited.Columns(), limited.Rows())
	}
	for _, plan := range []Plan{{}, invalid, limited} {
		if _, ok := plan.Tile(0, 0); ok {
			t.Error("未就绪计划提供了图块变换")
		}
	}
}
