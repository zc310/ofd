package text

import (
	"math"
	"reflect"
	"sync"
	"testing"

	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

func TestApplyCGTransformWidthsUsesMappedGlyphs(t *testing.T) {
	widths := []float64{2, 3, 4}
	runes := []rune("abc")
	widthOf := func(value string) float64 {
		if value == string(fontrepair.GlyphRune(65)) {
			return 9
		}
		if value == string(fontrepair.GlyphRune(66)) {
			return 6
		}
		return 0
	}
	applyCGTransformWidths(widths, runes, []models.CTCGTransform{{
		CodePosition: 1, CodeCount: 2, GlyphCount: 2, Glyphs: models.StArrayI{65, 66},
	}}, 0, widthOf)
	if widths[0] != 2 || widths[1] != 9 || widths[2] != 6 {
		t.Fatalf("mapped widths = %v, want [2 9 6]", widths)
	}
}

type runPositionFace struct{}

func (runPositionFace) TextWidth(value string) float64 {
	return float64(len([]rune(value))) * 1.0000015277777778
}
func (runPositionFace) GlyphIndex(rune) uint16              { return 1 }
func (runPositionFace) IsCFF() bool                         { return false }
func (runPositionFace) VerticalMetrics() (float64, float64) { return .8, .2 }

func TestWalkRunsPreservesSmallExplicitAdvancesBeforeCTM(t *testing.T) {
	for _, delta := range []float64{.975, 1.025, 0, -.1} {
		code := models.TextCode{Value: "方案的人工智能公司。", Y: .835}
		for range len([]rune(code.Value)) - 1 {
			code.DeltaX = append(code.DeltaX, delta)
		}
		object := models.TextObject{CtText: models.CtText{
			Size: 1, TextCode: []models.TextCode{code},
			CTGraphicUnit: models.CTGraphicUnit{CTM: &models.CTM{9.8778, 0, 0, 9.8778, 0, 0}},
		}}
		var runs []Run
		WalkRuns(runPositionFace{}, object, code, 0, func(run Run) { runs = append(runs, run) })
		if len(runs) != len([]rune(code.Value)) {
			t.Fatalf("字距 %g 合并后忽略了源定位: %+v", delta, runs)
		}
		for i, run := range runs {
			if math.Abs(run.X-float64(i)*delta) > 1e-12 || run.Y != code.Y {
				t.Fatalf("字距 %g 字符 %d 坐标=(%g,%g)，期望=(%g,%g)", delta, i, run.X, run.Y, float64(i)*delta, code.Y)
			}
		}
	}
}

func TestWalkRunsFallsBackToNaturalAdvanceAfterShortDeltaArray(t *testing.T) {
	code := models.TextCode{Value: "重庆增值税电子普通发票", DeltaX: models.StArrayF{6.53}}
	object := models.TextObject{CtText: models.CtText{
		Size: 1, TextCode: []models.TextCode{code},
	}}
	var runs []Run
	WalkRuns(runPositionFace{}, object, code, 0, func(run Run) { runs = append(runs, run) })
	if len(runs) != 2 || runs[0].Value != "重" || runs[1].Value != "庆增值税电子普通发票" {
		t.Fatalf("短 DeltaX 应只显式定位第一段，其余字符沿用自然字宽: %+v", runs)
	}
	if math.Abs(runs[1].X-6.53) > 1e-12 {
		t.Fatalf("短 DeltaX 后续文字段起点=%g，期望=6.53", runs[1].X)
	}
	naturalWidth := (runPositionFace{}).TextWidth("庆")
	dx, dy := Advance(naturalWidth, object, code, 1)
	if dx != naturalWidth || dy != 0 {
		t.Fatalf("短 DeltaX 后续字符步进=(%g,%g)，期望自然字宽=(%g,0)", dx, dy, naturalWidth)
	}
}

func TestWalkRunsKeepsNaturalAdvanceWithFontUnitRounding(t *testing.T) {
	code := models.TextCode{Value: "ABC", DeltaX: models.StArrayF{1, 1}}
	object := models.TextObject{CtText: models.CtText{Size: 1, TextCode: []models.TextCode{code}}}
	var runs []Run
	WalkRuns(runPositionFace{}, object, code, 0, func(run Run) { runs = append(runs, run) })
	if len(runs) != 1 || runs[0].Value != code.Value {
		t.Fatalf("字号换算误差不应拆开自然步进: %+v", runs)
	}
}

func TestTextCodeGlyphsClampsCodeCountBeforeMapping(t *testing.T) {
	for _, tc := range []struct {
		name     string
		count    int
		semantic string
		glyphs   int
	}{
		{"exact remainder", 2, "ab", 2},
		{"exceeds remainder", 3, "ab", 2},
		{"overflow after prefix", int(^uint(0) >> 1), "ab", 2},
		{"zero defaults to one", 0, "a", 3},
		{"negative defaults to one", -1, "a", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transform := models.CTCGTransform{CodePosition: 1, CodeCount: tc.count, Glyphs: []int{65}}
			glyphs := CodeGlyphs(nil, []rune("zab"), []models.CTCGTransform{transform}, 0)
			if len(glyphs) != tc.glyphs || glyphs[0].Value != "z" || glyphs[1].Semantic != tc.semantic || glyphs[1].CodePosition != 1 {
				t.Fatalf("glyphs = %+v，期望前缀 z 与语义 %q（共 %d 个字形）", glyphs, tc.semantic, tc.glyphs)
			}
			widths := []float64{4, 4, 4}
			applyCGTransformWidths(widths, []rune("zab"), []models.CTCGTransform{transform}, 0, func(string) float64 { return 10 })
			mappedWidth := widths[1]
			if len(tc.semantic) == 2 {
				mappedWidth += widths[2]
			}
			if mappedWidth != 10 {
				t.Fatalf("文字层映射宽度 %v 与绘制字形步进 10 不一致", widths)
			}
		})
	}
}

// 绘制字形和搜索字符数量可以不同，两套坐标必须来自同一定位队列。
func TestGlyphAndCharacterPositionsShareExplicitDeltas(t *testing.T) {
	for _, tc := range []struct {
		name       string
		code       models.TextCode
		transform  models.CTCGTransform
		glyphs     []geom.Point
		characters []geom.Point
	}{
		{"ligature", models.TextCode{Value: "fiA", Y: 4, DeltaX: models.StArrayF{7}},
			models.CTCGTransform{CodeCount: 2, GlyphCount: 1, Glyphs: models.StArrayI{21}},
			[]geom.Point{{}, {X: 7}}, []geom.Point{{}, {X: 3.5}, {X: 7}}},
		{"expanded", models.TextCode{Value: "ab", Y: 4, DeltaX: models.StArrayF{2, 3}, DeltaY: models.StArrayF{1, -1}},
			models.CTCGTransform{CodeCount: 1, GlyphCount: 2, Glyphs: models.StArrayI{21, 22}},
			[]geom.Point{{}, {X: 2, Y: 1}, {X: 5}}, []geom.Point{{}, {X: 5}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			object := models.TextObject{CtText: models.CtText{
				CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{X: 10, Y: 20, Width: 12, Height: 4}},
				Size:          4, TextCode: []models.TextCode{tc.code}, CGTransform: []models.CTCGTransform{tc.transform},
			}}
			glyphs := CodeGlyphs(nil, []rune(tc.code.Value), object.CGTransform, 0)
			if len(glyphs) != len(tc.glyphs) || !UsesGlyphDeltas(tc.code, glyphs) {
				t.Fatalf("绘制字形队列=%+v", glyphs)
			}
			x, y := 0.0, 0.0
			for i, glyph := range glyphs {
				if i > 0 {
					dx, dy := PositionedAdvance(0, object, tc.code, glyphs[i-1], glyph, true)
					x, y = x+dx, y+dy
				}
				if got := (geom.Point{X: x, Y: y}); got != tc.glyphs[i] {
					t.Fatalf("绘制字形%d坐标=%v，期望%v", i, got, tc.glyphs[i])
				}
			}
			layout := Build(nil, object, tc.code, 0)
			if len(layout.Glyphs) != len(tc.characters) {
				t.Fatalf("搜索字符=%+v", layout.Glyphs)
			}
			for i, character := range layout.Glyphs {
				if got := (geom.Point{X: character.X - 10, Y: character.Y - 20}); got != tc.characters[i] {
					t.Fatalf("搜索字符%d坐标=%v，期望%v", i, got, tc.characters[i])
				}
			}
		})
	}
}

func TestCTMLinearMatrixPreservesOFDGlyphOrientation(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctm  *models.CTM
		want *geom.Point
	}{
		{"absent", nil, nil},
		{"translation only", &models.CTM{1, 0, 0, 1, 100, 200}, nil},
		{"nonfinite", &models.CTM{math.Inf(1), 0, 0, 1}, nil},
		{"shear", &models.CTM{1, 0, .5, 1, 100, 200}, &geom.Point{X: 0, Y: 4}},
		{"rotated scale", &models.CTM{0, 2, -3, 0}, &geom.Point{X: 12, Y: -4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := CTMLinearMatrix(tc.ctm)
			if tc.want == nil {
				if m != nil {
					t.Fatalf("无需字形线性变换，得到%v", m)
				}
				return
			}
			if m == nil || m.Dot(geom.Point{X: 2, Y: 4}) != *tc.want {
				t.Fatalf("字形坐标变换=%v，期望映射到%v", m, *tc.want)
			}
		})
	}
}

func TestConcurrentBuildDoesNotMutateSharedTransformsOrDeltas(t *testing.T) {
	code := models.TextCode{Value: "fiA", Y: 4, DeltaX: models.StArrayF{7}}
	object := models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{Width: 12, Height: 4}},
		Size:          4, TextCode: []models.TextCode{code},
		CGTransform: []models.CTCGTransform{{CodeCount: 2, GlyphCount: 1, Glyphs: models.StArrayI{21}}},
	}}
	want := Build(nil, object, code, 0)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 32 {
				if got := Build(nil, object, code, 0); !reflect.DeepEqual(got, want) {
					t.Error("并发布局改变了字符几何")
				}
			}
		})
	}
	wg.Wait()
	if !reflect.DeepEqual(code.DeltaX, models.StArrayF{7}) || object.CGTransform[0].Glyphs[0] != 21 || object.CGTransform[0].CodeCount != 2 {
		t.Fatal("布局修改了共享增量或字形映射")
	}
}
