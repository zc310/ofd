package font_test

import (
	"testing"

	"github.com/golang/freetype/truetype"
	"github.com/zc310/ofd/internal/thirdparty/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
)

// 不依赖外部字体，验证迁入后的解析、子集化、写回与 WOFF2 转换链。
func TestBundledFontSubsetWOFF2RoundTrip(t *testing.T) {
	face, err := font.ParseSFNT(append([]byte(nil), goregular.TTF...), 0)
	if err != nil {
		t.Fatal(err)
	}
	glyphs := make([]uint16, 1, 6)
	for _, r := range "ABCé" {
		glyph := face.GlyphIndex(r)
		if glyph == 0 {
			t.Fatalf("测试字体缺少字符 %q", r)
		}
		glyphs = append(glyphs, glyph)
	}
	subset, err := face.Subset(glyphs, font.SubsetOptions{Tables: font.KeepMinTables})
	if err != nil {
		t.Fatal(err)
	}
	program, err := subset.WriteWOFF2()
	if err != nil {
		t.Fatal(err)
	}
	if kind, err := font.MediaType(program); err != nil || kind != "font/woff2" {
		t.Fatalf("WOFF2 类型 = %q, %v", kind, err)
	}
	reparsed, err := font.ParseSFNT(program, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range "ABCé" {
		glyph := reparsed.GlyphIndex(r)
		if glyph == 0 || reparsed.GlyphAdvance(glyph) != face.GlyphAdvance(face.GlyphIndex(r)) {
			t.Errorf("字符 %q 的字形或字宽在往返后丢失", r)
		}
	}
	if reparsed.GlyphIndex('Z') != 0 {
		t.Error("未使用的字符 Z 不应保留映射")
	}
}

// 第三方字体对象转换仍使用同一套内置解析器。
func TestBundledFontConvertsGoFontObjects(t *testing.T) {
	data := append([]byte(nil), goregular.TTF...)
	goFont, err := sfnt.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	freetypeFont, err := truetype.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	for name, program := range map[string][]byte{
		"sfnt":     font.FromGoSFNT(goFont),
		"freetype": font.FromGoFreetype(freetypeFont),
	} {
		t.Run(name, func(t *testing.T) {
			face, err := font.ParseSFNT(program, 0)
			if err != nil {
				t.Fatal(err)
			}
			if face.GlyphIndex('A') == 0 {
				t.Fatal("转换后丢失字符 A")
			}
		})
	}
}
