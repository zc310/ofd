package webreader

import (
	"testing"

	"github.com/zc310/ofd/internal/utils"
)

// TestTextRunsWeightCountsGlyphText 权重必须把字形里的 Text 算进去。
//
// 这是最容易漏的一项：Glyph 每个字符都自带一个 Text string，一页密集文档有两
// 千多个字形，漏掉它整个估算会低估一个数量级——缓存就会在"以为还很空"的时候
// 撞上内存上限。
func TestTextRunsWeightCountsGlyphText(t *testing.T) {
	noGlyphs := []TextRun{{Text: "abc"}}
	withGlyphs := []TextRun{{Text: "abc", Glyphs: []Glyph{{Text: "a"}, {Text: "b"}, {Text: "c"}}}}

	light := textRunsWeight(noGlyphs)
	heavy := textRunsWeight(withGlyphs)
	if heavy <= light {
		t.Fatalf("加字形后权重没有变大: %d -> %d", light, heavy)
	}
	// 三个字形各带一个字符，加上字形自身的固定开销，增长应明显大于 3。
	if delta := heavy - light; delta < 3*glyphWeightOverhead {
		t.Errorf("字形带来的增量 = %d，期望至少 %d", delta, 3*glyphWeightOverhead)
	}
}

// TestTextRunsWeightGrowsWithContent 内容量越大权重越大，且对空输入有下限。
func TestTextRunsWeightGrowsWithContent(t *testing.T) {
	small := textRunsWeight([]TextRun{{Text: "a"}})
	large := textRunsWeight([]TextRun{{Text: repeat('a', 4096)}})
	if large <= small {
		t.Errorf("内容量变大但权重没变: %d -> %d", small, large)
	}
	empty := textRunsWeight(nil)
	if empty != 0 {
		t.Errorf("空输入权重 = %d，期望 0", empty)
	}
}

// TestSearchPageWeightCountsBothParts 索引权重要同时算 runs 与 byRune。
func TestSearchPageWeightCountsBothParts(t *testing.T) {
	runsOnly := buildSearchIndex([]TextRun{{Text: "abc"}})
	withDup := buildSearchIndex([]TextRun{{Text: "a"}, {Text: "a"}, {Text: "a"}})
	// 三个 run 各自含 'a'，byRune['a'] 会有 3 个候选。
	if got := len(withDup.byRune['a']); got != 3 {
		t.Fatalf("byRune['a'] 候选数 = %d，期望 3", got)
	}
	if searchPageWeight(withDup) <= searchPageWeight(runsOnly) {
		t.Errorf("候选更多但权重没变: %d -> %d",
			searchPageWeight(runsOnly), searchPageWeight(withDup))
	}
}

// TestCachesAreByteWeighted 缓存确实按字节计量，而不是只按条目数。
//
// 这条守的是构造那几行：若有人改回 NewLRU，预算会静默失效，调用方无法察觉。
func TestCachesAreByteWeighted(t *testing.T) {
	r := &Reader{}
	r.text = newTextCache()
	r.search = newSearchCache(1)
	if got := r.text.Weight(); got != 0 {
		t.Errorf("空缓存权重 = %d，期望 0", got)
	}
	r.text.AddWeighted(1, []TextRun{{Text: "abc"}}, 1000)
	if got := r.text.Weight(); got != 1000 {
		t.Errorf("权重 = %d，期望 1000", got)
	}
	// 条目数上限仍在生效：字节预算只是第二道闸。
	if got := r.text.Len(); got != 1 {
		t.Errorf("缓存条目数 = %d，期望 1", got)
	}
}

// TestDenseDocumentRespectsByteBudget 密集文档触及字节预算时逐出，而不是撑到
// 页数上限。
//
// 这是加字节计量的全部意义：ano.ofd 每页 154 KB，64 页约 9.8 MB；预算调小后
// 应当只缓存到预算允许的页数。
func TestDenseDocumentRespectsByteBudget(t *testing.T) {
	cache := utils.NewWeightedLRU[int, []TextRun](1000, 1000, nil)
	// 每页权重 100，预算 1000 → 只能留 10 页
	for page := 1; page <= 20; page++ {
		cache.AddWeighted(page, []TextRun{{Text: "x"}}, 100)
	}
	if got := cache.Len(); got > 10 {
		t.Errorf("缓存了 %d 页，字节预算 1000 每页 100 最多容 10 页", got)
	}
	if got := cache.Weight(); got > 1000 {
		t.Errorf("权重 = %d，超出预算 1000", got)
	}
}

func repeat(r rune, n int) string {
	out := make([]rune, n)
	for i := range out {
		out[i] = r
	}
	return string(out)
}
