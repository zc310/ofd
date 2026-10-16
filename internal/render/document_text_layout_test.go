package render

import (
	"math"
	"testing"

	"github.com/zc310/ofd/internal/models"
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
