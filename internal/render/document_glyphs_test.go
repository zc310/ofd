package render

import (
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

func TestCollectObjectGlyphsPairsUnicodeWithGlyph(t *testing.T) {
	// 单码位对单字形时，应从 CGTransform.Glyphs 得到 Unicode→字形映射。
	object := models.TextObject{CtText: models.CtText{
		Font:     models.StRefID(10),
		TextCode: []models.TextCode{{Value: "中华"}},
		CGTransform: []models.CTCGTransform{
			{CodePosition: 0, CodeCount: 1, GlyphCount: 1, Glyphs: models.StArrayI{735}},
			{CodePosition: 1, CodeCount: 1, GlyphCount: 1, Glyphs: models.StArrayI{1264}},
		},
	}}
	object.Visible.Set(true)

	out := make(map[models.StRefID]map[rune]uint16)
	collectObjectGlyphs(object, out)
	if got := out[10]['中']; got != 735 {
		t.Fatalf("中 -> %d, want 735", got)
	}
	if got := out[10]['华']; got != 1264 {
		t.Fatalf("华 -> %d, want 1264", got)
	}
}

func TestCollectObjectGlyphsSkipsMultiGlyphAndInvisible(t *testing.T) {
	// 多字形（连字）无法可靠对应单个 Unicode，不应登记；不可见对象也不登记。
	multi := models.TextObject{CtText: models.CtText{
		Font:     models.StRefID(10),
		TextCode: []models.TextCode{{Value: "fi"}},
		CGTransform: []models.CTCGTransform{
			{CodePosition: 0, CodeCount: 2, GlyphCount: 1, Glyphs: models.StArrayI{5}},
		},
	}}
	multi.Visible.Set(true)
	out := make(map[models.StRefID]map[rune]uint16)
	collectObjectGlyphs(multi, out)
	if len(out) != 0 {
		t.Fatalf("multi-glyph transform should not be registered: %+v", out)
	}

	invisible := models.TextObject{CtText: models.CtText{
		Font:        models.StRefID(10),
		TextCode:    []models.TextCode{{Value: "中"}},
		CGTransform: []models.CTCGTransform{{CodePosition: 0, CodeCount: 1, GlyphCount: 1, Glyphs: models.StArrayI{735}}},
	}}
	invisible.Visible.Set(false)
	collectObjectGlyphs(invisible, out)
	if len(out) != 0 {
		t.Fatalf("invisible object should not be registered: %+v", out)
	}
}

func TestRegisterGlyphsInvalidatesChangedFont(t *testing.T) {
	fonts := NewFonts(&parser.Document{})
	fonts.Fonts[models.StRefID(1)] = nil // 占位，模拟已加载缓存

	fonts.RegisterGlyphs(models.StRefID(1), map[rune]uint16{'中': 735})
	if _, ok := fonts.Fonts[models.StRefID(1)]; ok {
		t.Fatal("changed mapping should invalidate cached font")
	}
	generation := fonts.generation

	// 相同映射再次登记不应触发作废。
	fonts.Fonts[models.StRefID(1)] = nil
	fonts.RegisterGlyphs(models.StRefID(1), map[rune]uint16{'中': 735})
	if fonts.generation != generation {
		t.Fatal("unchanged mapping should not bump generation")
	}
	if _, ok := fonts.Fonts[models.StRefID(1)]; !ok {
		t.Fatal("unchanged mapping should keep cached font")
	}
}
