package render

import (
	"math"
	"reflect"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
)

// TestTextLayoutSizeFollowsCTMScale 锁定 TextLayout.Size 是实际绘制字号而非对象
// 上声明的原始 Size。
//
// OFD 允许用很小的 Size 配一个放大倍数的 CTM 来排大标题：testdata/ofdrw/intro.ofd
// 第 14 页就是 Size="1" 配 CTM="9.8778 0 0 9.8778 0 0"，画出 9.88mm 的字。
// buildTextLayout 早先把修正只用在 Height 上，Size 原样返回，消费方（HTML 文字层
// 与 pkg/webreader）按它换算字号就会小近 10 倍——文字层缩成一条 1mm 的细线。
func TestTextLayoutSizeFollowsCTMScale(t *testing.T) {
	const declaredSize = 1.0
	const scale = 9.8778

	object := models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{
			Boundary: models.StBox{X: 20, Y: 20.6161, Width: 40.0445, Height: 9.1666},
			CTM:      &models.CTM{9.8778, 0, 0, 9.8778, 0, 0},
		},
		Size: declaredSize,
		TextCode: []models.TextCode{{
			DeltaX: models.StArrayF{0.995, 0.995, 0.995},
			Value:  "人脸检测",
		}},
	}}

	layout := buildTextLayout(nil, object, object.TextCode[0], 0)

	want := declaredSize * scale
	if math.Abs(layout.Size-want) > 1e-6 {
		t.Errorf("Size = %v，期望 %v（Size %v 乘 CTM.YScale %v）",
			layout.Size, want, declaredSize, scale)
	}
	// Height 早已应用同一系数，两者应当一致，否则字号与行高会脱节。
	if math.Abs(layout.Height-layout.Size) > 1e-6 {
		t.Errorf("Height = %v 与 Size = %v 不一致，CSS 行高会与字号脱节",
			layout.Height, layout.Size)
	}
}

// TestTextLayoutSizeWithoutCTM 确认没有 CTM 时 Size 就是声明值。
func TestTextLayoutSizeWithoutCTM(t *testing.T) {
	object := models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{
			Boundary: models.StBox{X: 10, Y: 10, Width: 30, Height: 6},
		},
		Size:     4.5,
		TextCode: []models.TextCode{{Value: "无 CTM"}},
	}}
	layout := buildTextLayout(nil, object, object.TextCode[0], 0)
	if math.Abs(layout.Size-4.5) > 1e-9 {
		t.Errorf("Size = %v，期望 4.5", layout.Size)
	}
}

type metricsFontFamily struct{}

func (metricsFontFamily) Name() string { return "metrics" }

// metricsFontFace 是只提供固定宽度与上伸/下延的字体面桩。
type metricsFontFace struct {
	ascent  float64
	descent float64
}

func (metricsFontFace) ToPath(string) *geom.Path     { return nil }
func (metricsFontFace) DirectPath(string) *geom.Path { return nil }
func (metricsFontFace) TextWidth(string) float64     { return 3 }
func (metricsFontFace) GlyphIndex(rune) uint16       { return 0 }
func (metricsFontFace) IsCFF() bool                  { return false }
func (metricsFontFace) Fill() geom.Paint             { return geom.Paint{} }
func (metricsFontFace) Size() float64                { return 0 }
func (f metricsFontFace) VerticalMetrics() (float64, float64) {
	return f.ascent, f.descent
}
func (metricsFontFace) ShapedRun(string) TextRun { return nil }

// metricsFontEngine 在空引擎基础上返回固定字体与字体面。
type metricsFontEngine struct {
	fakeFontEngine

	face FontFace
}

func (metricsFontEngine) LoadFont(models.StRefID) (FontFamily, error) {
	return metricsFontFamily{}, nil
}
func (e metricsFontEngine) FaceObject(FontFamily, models.TextObject, *CTColor) FontFace {
	return e.face
}

// TestTextGlyphBoxFollowsFontVerticalMetrics 验证字符框和整段覆盖层使用一致的
// 字体排版高度；上伸与下延之和不必等于字号。
func TestTextGlyphBoxFollowsFontVerticalMetrics(t *testing.T) {
	object := models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{
			Boundary: models.StBox{X: 10, Y: 20, Width: 20, Height: 5},
		},
		Size:     5,
		TextCode: []models.TextCode{{Value: "ab"}},
	}}

	// 无字体：EM 盒，框顶 = 基线 20 - 字号 5 = 15。
	plain := buildTextLayout(nil, object, object.TextCode[0], 0)
	if plain.Glyphs[0].Y != 15 || plain.Glyphs[0].Height != 5 {
		t.Fatalf("EM 盒 = %+v，期望 y=15 height=5", plain.Glyphs[0])
	}

	for _, tc := range []struct {
		name    string
		ascent  float64
		descent float64
		y       float64
		height  float64
	}{
		{"em height", 4, 1, 16, 5},
		{"larger than em", 5, 1, 15, 6},
		{"smaller than em", 3, 1, 17, 4},
		{"unsupported", 0, 0, 15, 5},
		{"nonfinite", math.Inf(1), 1, 15, 5},
		{"negative descent", 4, -1, 15, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := &Document{fonts: metricsFontEngine{face: metricsFontFace{ascent: tc.ascent, descent: tc.descent}}}
			layout := buildTextLayout(doc, object, object.TextCode[0], 0)
			if layout.Y != tc.y || layout.Height != tc.height || layout.Size != 5 {
				t.Fatalf("文字层 y=%g height=%g size=%g，期望 y=%g height=%g size=5", layout.Y, layout.Height, layout.Size, tc.y, tc.height)
			}
			for _, glyph := range layout.Glyphs {
				if glyph.Y != tc.y || glyph.Height != tc.height {
					t.Fatalf("字形框 = %+v，期望 y=%g height=%g", glyph, tc.y, tc.height)
				}
			}
		})
	}
}

type countingLayoutFontEngine struct {
	metricsFontEngine

	loads, faces int
}

func (e *countingLayoutFontEngine) LoadFont(id models.StRefID) (FontFamily, error) {
	e.loads++
	return e.metricsFontEngine.LoadFont(id)
}

func (e *countingLayoutFontEngine) FaceObject(family FontFamily, object models.TextObject, fill *CTColor) FontFace {
	e.faces++
	return e.metricsFontEngine.FaceObject(family, object, fill)
}

func TestTextLayoutReusesFaceWithinObjectAndKeepsSelectedRunGeometry(t *testing.T) {
	engine := &countingLayoutFontEngine{metricsFontEngine: metricsFontEngine{face: metricsFontFace{ascent: 3, descent: 1}}}
	doc := &Document{fonts: engine}
	object := models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{X: 3, Y: 4, Width: 30, Height: 20}, CTM: &models.CTM{2, 0, 0, 2, 0, 0}},
		Size:          2, TextCode: []models.TextCode{{Value: "AB", Y: 4}, {Value: "CD", X: 8, Y: 8}, {Value: "EF", X: 16, Y: 12}},
	}}
	want := make([]TextLayout, len(object.TextCode))
	for i, code := range object.TextCode {
		want[i] = buildTextLayout(doc, object, code, i*2)
	}
	engine.loads, engine.faces = 0, 0
	var got []TextLayout
	index := 0
	collectObjectTextLayouts(object, &got, doc, nil, &index)
	if !reflect.DeepEqual(got, want) || engine.loads != 1 || engine.faces != 1 {
		t.Fatalf("复用字体面改变布局，或重复选字体：loads=%d faces=%d got=%+v", engine.loads, engine.faces, got)
	}
	engine.loads, engine.faces = 0, 0
	got, index = nil, 0
	collectObjectTextLayouts(object, &got, doc, map[int]struct{}{1: {}}, &index)
	if !reflect.DeepEqual(got, want[1:2]) || index != 3 || engine.faces != 1 {
		t.Fatal("只选中间 run 时改变了字符位置或 run 索引")
	}
	engine.loads, engine.faces = 0, 0
	got, index = nil, 0
	collectObjectTextLayouts(object, &got, doc, map[int]struct{}{99: {}}, &index)
	if len(got) != 0 || engine.loads != 0 || engine.faces != 0 {
		t.Fatal("未选中任何 run 仍加载了字体")
	}
}

// run 索引跳过空文本和不可见对象，CGTransform 的位置则按对象内 Unicode 字符计数。
// 只请求后面的 run 时，也必须保留前面 TextCode 对字符位置的贡献。
func TestTextLayoutsForRunsPreservesCharacterPositions(t *testing.T) {
	invisible := textItem(models.StBox{Width: 12, Height: 3}, nil)
	invisible.Text.Visible.Set(false)
	invisible.Text.TextCode = []models.TextCode{{Value: "hidden"}}
	object := textItem(models.StBox{X: 10, Y: 20, Width: 12, Height: 3}, nil)
	object.Text.Size = 3
	object.Text.TextCode = []models.TextCode{{}, {Value: "中"}, {Value: "fi", X: 4}}
	object.Text.CGTransform = []models.CTCGTransform{{CodePosition: 1, CodeCount: 2, GlyphCount: 1, Glyphs: models.StArrayI{7}}}
	block := models.PageItem{
		Kind:  models.PageItemBlock,
		Block: &models.PageBlock{CTPageBlock: models.CTPageBlock{Items: []models.PageItem{object}}},
	}
	page := parser.NewPage(models.PageContent{Content: &models.Content{Layer: []*models.Layer{
		{CTPageBlock: models.CTPageBlock{Items: []models.PageItem{invisible, block}}},
	}}})
	doc := &Document{Document: &parser.Document{}, fonts: metricsFontEngine{face: metricsFontFace{ascent: 3}}}
	t.Cleanup(doc.Close)
	if got := doc.TextValues(page); !reflect.DeepEqual(got, []string{"中", "fi"}) {
		t.Fatalf("TextValues = %v", got)
	}
	all := doc.TextLayouts(page)
	if len(all) != 2 || len(all[1].Glyphs) != 2 {
		t.Fatalf("layouts = %+v", all)
	}
	for i, glyph := range all[1].Glyphs {
		if glyph.Width != 1.5 || glyph.X != 14+float64(i)*1.5 {
			t.Errorf("ligature character %d = %+v, want width 1.5 and x %g", i, glyph, 14+float64(i)*1.5)
		}
	}
	if got := doc.TextLayoutsForRuns(page, []int{1}); !reflect.DeepEqual(got, all[1:]) {
		t.Fatalf("selected layout lost character position: %+v, want %+v", got, all[1:])
	}
	for _, indices := range [][]int{nil, {}, {-1, -2}, {99}} {
		if got := doc.TextLayoutsForRuns(page, indices); len(got) != 0 {
			t.Errorf("indices %v returned layouts: %+v", indices, got)
		}
	}
}
