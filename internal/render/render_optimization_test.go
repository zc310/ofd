package render

import (
	"bytes"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"golang.org/x/image/font/gofont/goregular"
)

func TestRenderBudgetDiagnosticsAreOncePerReasonAndReset(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	var budget renderBudget
	budget.reset()
	budget.patternTiles = maxPatternTilesPerRender
	for range 3 {
		budget.allowPatternTiles(1)
	}
	for range 3 {
		budget.allowOffscreenPixels(1e9, 1e9, 300)
	}
	logs := output.String()
	if strings.Count(logs, `"budget":"pattern_tiles"`) != 1 || strings.Count(logs, `"budget":"offscreen_pixels"`) != 1 {
		t.Fatalf("缺失诊断或重复刷屏：%s", logs)
	}
	budget.reset()
	budget.patternTiles = maxPatternTilesPerRender
	budget.allowPatternTiles(1)
	if strings.Count(output.String(), `"budget":"pattern_tiles"`) != 2 {
		t.Fatal("重置预算没有恢复诊断")
	}
}

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

func BenchmarkTextLayoutFaceReuse(b *testing.B) {
	name := b.Name()
	if err := RegisterFallbackFont(goregular.TTF, name, FontRegular); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = UnregisterFallbackFont(name) })
	doc := NewDocument(nil, nil)
	b.Cleanup(doc.Close)
	if err := doc.UseFallbackFont(name); err != nil {
		b.Fatal(err)
	}
	object := models.TextObject{CtText: models.CtText{Size: 4}}
	for i := range 16 {
		object.TextCode = append(object.TextCode, models.TextCode{Value: "Benchmark text", Y: float64(i) * 5})
	}
	for _, reuse := range []bool{false, true} {
		b.Run(fmt.Sprintf("reuse=%v", reuse), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var layouts []TextLayout
				position := 0
				if reuse {
					collectObjectTextLayouts(object, &layouts, doc, nil, &position)
				} else {
					for _, code := range object.TextCode {
						layouts = append(layouts, buildTextLayout(doc, object, code, position))
						position += len([]rune(code.Value))
					}
				}
				if len(layouts) != len(object.TextCode) {
					b.Fatal("文字布局缺失")
				}
			}
		})
	}
}

func BenchmarkSealCacheKey(b *testing.B) {
	for _, size := range []int{64 << 10, 1 << 20} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			data := make([]byte, size)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for b.Loop() {
				_ = sealCacheKey(data, []string{"fallback"})
			}
		})
	}
}
