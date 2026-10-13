package canvas

import (
	"fmt"
	"sync"
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

func glyphScanContent(count int) *models.PageContent {
	items := make([]models.PageItem, count)
	for i := range items {
		text := &models.TextObject{CtText: models.CtText{
			Font: 10, TextCode: []models.TextCode{{Value: "自动识别"}},
			CGTransform: []models.CTCGTransform{{CodeCount: 4, GlyphCount: 4, Glyphs: models.StArrayI{100, 200, 300, 400}}},
		}}
		text.Visible.Set(true)
		items[i] = models.PageItem{Kind: models.PageItemText, Text: text}
	}
	return &models.PageContent{Content: &models.Content{Layer: []*models.Layer{{
		CTPageBlock: models.CTPageBlock{Items: items},
	}}}}
}

func BenchmarkRegisterPageGlyphsRepeated(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprintf("objects-%d", count), func(b *testing.B) {
			doc := &parser.Document{}
			fonts := NewFonts(doc)
			defer fonts.Close()
			content := glyphScanContent(count)
			fonts.RegisterPageGlyphs(doc, nil, content)
			b.ReportAllocs()
			for b.Loop() {
				fonts.RegisterPageGlyphs(doc, nil, content)
			}
		})
	}
}

func TestCachedPageGlyphsReplayChangedMappings(t *testing.T) {
	doc := &parser.Document{}
	fonts := NewFonts(doc)
	defer fonts.Close()
	content := glyphScanContent(2)
	fonts.RegisterPageGlyphs(doc, nil, content)
	generation := fonts.generation
	fonts.RegisterPageGlyphs(doc, nil, content)
	if fonts.generation != generation {
		t.Fatal("unchanged cached mappings invalidated font")
	}
	fonts.RegisterGlyphs(10, map[rune]uint16{'自': 999})
	fonts.RegisterPageGlyphs(doc, nil, content)
	if got := fonts.glyphMappings[10]['自']; got != 100 {
		t.Fatalf("cached page mapping=%d, want 100", got)
	}
	if fonts.glyphCollections.Len() != 1 {
		t.Fatal("repeated page scan not cached")
	}
}

func TestGlyphCollectionCacheRecognizesReloadedContentAndIsBounded(t *testing.T) {
	doc := &parser.Document{}
	fonts := NewFonts(doc)
	defer fonts.Close()
	content := glyphScanContent(1)
	fonts.RegisterPageGlyphs(doc, nil, content)
	// PageContent 的地址由解析器复用，内部 Content 在页面重新加载时替换。
	reloaded := glyphScanContent(1)
	reloaded.Content.Layer[0].Items[0].Text.CGTransform[0].Glyphs[0] = 777
	content.Content = reloaded.Content
	fonts.RegisterPageGlyphs(doc, nil, content)
	if got := fonts.glyphMappings[10]['自']; got != 777 {
		t.Fatalf("reloaded mapping=%d, want 777", got)
	}
	for range glyphCollectionCapacity * 2 {
		fonts.RegisterPageGlyphs(doc, nil, glyphScanContent(1))
	}
	if got := fonts.glyphCollections.Len(); got > glyphCollectionCapacity {
		t.Fatalf("cache entries=%d", got)
	}
	fonts.Close()
	if fonts.glyphCollections != nil {
		t.Fatal("Close retained page glyph collections")
	}
}

func TestGlyphCollectionMergeDoesNotModifyCachedMappings(t *testing.T) {
	fonts := NewFonts(&parser.Document{})
	defer fonts.Close()
	collected := fonts.contentGlyphs(glyphScanContent(1).Content)
	merged := make(glyphCollection)
	mergeGlyphCollection(merged, collected)
	merged[10]['自'] = 999
	if collected[10]['自'] != 100 {
		t.Fatal("merge modified cached mapping")
	}
	// 模板映射先合并，页面后合并：同一个字符仍由后者覆盖。
	mergeGlyphCollection(merged, glyphCollection{10: {'自': 777}})
	if merged[10]['自'] != 777 || collected[10]['自'] != 100 {
		t.Fatal("mapping precedence or cached collection changed")
	}
}

func TestConcurrentPageGlyphRegistrationReusesCollection(t *testing.T) {
	doc := &parser.Document{}
	fonts := NewFonts(doc)
	defer fonts.Close()
	content := glyphScanContent(100)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				fonts.RegisterPageGlyphs(doc, nil, content)
			}
		}()
	}
	wg.Wait()
	if got := fonts.glyphCollections.Len(); got != 1 {
		t.Fatalf("cache entries=%d, want 1", got)
	}
	if got := fonts.glyphMappings[10]['自']; got != 100 {
		t.Fatalf("mapping=%d, want 100", got)
	}
}

func TestCollectObjectGlyphsPairsMultiCodeOneToOne(t *testing.T) {
	// 码位数与字形数相等的多码位一对一变换（OFD 子集字体最常见形态），应逐
	// 码位按序登记 Unicode→字形映射，使缺少 Unicode cmap 的子集字体也能按
	// 原始文本渲染，PDF 等输出保留可复制、可搜索的文字。
	object := models.TextObject{CtText: models.CtText{
		Font:     models.StRefID(10),
		TextCode: []models.TextCode{{Value: "自动识别"}},
		CGTransform: []models.CTCGTransform{
			{CodePosition: 0, CodeCount: 4, GlyphCount: 4, Glyphs: models.StArrayI{100, 200, 300, 400}},
		},
	}}
	object.Visible.Set(true)

	out := make(map[models.StRefID]map[rune]uint16)
	collectObjectGlyphs(object, out)
	want := map[rune]uint16{'自': 100, '动': 200, '识': 300, '别': 400}
	for r, glyph := range want {
		if got := out[10][r]; got != glyph {
			t.Fatalf("%c -> %d, want %d (out=%v)", r, got, glyph, out[10])
		}
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
