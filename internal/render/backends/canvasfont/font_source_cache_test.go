package canvasfont

import (
	"crypto/sha256"
	"sync"
	"testing"

	"github.com/zc310/ofd/internal/cache"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

func sourceCacheFonts(t testing.TB) (*Fonts, []string) {
	t.Helper()
	data, err := creator.Marshal(creator.Document{
		ID: "font-source-cache", Fonts: []creator.Font{{Name: "Regular", Data: goregular.TTF}, {Name: "Bold", Data: gobold.TTF}},
		Pages: []creator.Page{{Items: []creator.Item{
			creator.Text{Width: 20, Height: 5, Size: 4, Font: "Regular", Value: "A"},
			creator.Text{Width: 20, Height: 5, Size: 4, Font: "Bold", Value: "B"},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	fonts := NewFonts(ofd.Documents[0])
	t.Cleanup(func() { fonts.Close(); _ = ofd.Close() })
	var paths []string
	fonts.Document.ForEachFont(func(_ models.StID, font *models.Font) bool {
		paths = append(paths, string(font.FontFile))
		return true
	})
	if len(paths) != 2 {
		t.Fatalf("字体资源数=%d，期望2", len(paths))
	}
	return fonts, paths
}

func TestEmbeddedFontSourcesReuseBytesAndRespectBudget(t *testing.T) {
	fonts, paths := sourceCacheFonts(t)
	fonts.embeddedSources = cache.NewWeightedLRU[string, *embeddedFontSource](1, fontSourceBytes, nil)
	first, err := fonts.readEmbeddedFont(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	again, err := fonts.readEmbeddedFont(paths[0])
	if err != nil || first != again || first.digest != sha256.Sum256(first.data) {
		t.Fatal("重复加载未复用原始字体及摘要")
	}
	if _, err := fonts.readEmbeddedFont(paths[1]); err != nil {
		t.Fatal(err)
	}
	if fonts.embeddedSources.Len() != 1 || fonts.embeddedSources.Weight() > fontSourceBytes {
		t.Fatal("原始字体缓存没有遵守容量预算")
	}
	reloaded, err := fonts.readEmbeddedFont(paths[0])
	if err != nil || reloaded == first || reloaded.digest != first.digest {
		t.Fatal("淘汰后未重新读取，或内容摘要发生变化")
	}
	fonts.embeddedSources = cache.NewWeightedLRU[string, *embeddedFontSource](fontSourceCapacity, int64(len(first.data)-1), nil)
	if _, err := fonts.readEmbeddedFont(paths[0]); err != nil {
		t.Fatal(err)
	}
	if fonts.embeddedSources.Len() != 0 {
		t.Fatal("超过字节预算的字体仍被缓存")
	}
	fonts.Close()
	if fonts.embeddedSources != nil {
		t.Fatal("关闭后保留了字体原始数据")
	}
	if _, err := fonts.readEmbeddedFont(paths[0]); err == nil || fonts.embeddedSources != nil {
		t.Fatal("关闭后重新填充了字体原始数据缓存")
	}
}

func TestFontSourceConcurrentLoadingAndClose(t *testing.T) {
	fonts, paths := sourceCacheFonts(t)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = fonts.readEmbeddedFont(paths[0])
		}()
	}
	fonts.Close()
	wg.Wait()
	if fonts.embeddedSources != nil {
		t.Fatal("在途加载在关闭后重新发布字体数据")
	}
}

func TestGlyphSnapshotChangesOnlyWithMappingVersion(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	snapshot := func() *fontGlyphSnapshot {
		fonts.mu.Lock()
		defer fonts.mu.Unlock()
		return fonts.glyphSnapshot(7)
	}
	fonts.RegisterGlyphs(7, map[rune]uint16{'A': 1, 'B': 2})
	first := snapshot()
	digest := first.hash()
	if digest != hashGlyphMappings(first.mappings) {
		t.Fatal("快照没有使用完整的顺序无关摘要")
	}
	fonts.RegisterGlyphs(7, map[rune]uint16{'B': 2})
	if snapshot() != first {
		t.Fatal("未变化的映射作废了快照")
	}
	fonts.RegisterGlyphs(7, map[rune]uint16{'B': 3})
	second := snapshot()
	if second == first || second.hash() == digest || first.hash() != digest {
		t.Fatal("新映射未更新摘要，或原地改写了在途快照")
	}
	for id := range glyphSnapshotCapacity + 2 {
		fonts.RegisterGlyphs(models.StRefID(id+100), map[rune]uint16{'A': 1})
		fonts.mu.Lock()
		fonts.glyphSnapshot(models.StRefID(id + 100))
		fonts.mu.Unlock()
	}
	if fonts.glyphSnapshots.Len() > glyphSnapshotCapacity || fonts.glyphSnapshots.Weight() > glyphSnapshotBytes {
		t.Fatal("字形快照超过缓存上限")
	}
	fonts.Close()
	if fonts.glyphSnapshots != nil {
		t.Fatal("关闭后保留了字形映射快照")
	}
}

func BenchmarkEmbeddedFontSourceReuse(b *testing.B) {
	fonts, paths := sourceCacheFonts(b)
	if _, err := fonts.readEmbeddedFont(paths[0]); err != nil {
		b.Fatal(err)
	}
	b.Run("read-and-hash", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			data, err := fonts.FileCache.Read(paths[0])
			if err != nil {
				b.Fatal(err)
			}
			_ = sha256.Sum256(data)
		}
	})
	b.Run("cached-source", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := fonts.readEmbeddedFont(paths[0]); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkGlyphSnapshotReuse(b *testing.B) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	pairs := make(map[rune]uint16, 4096)
	for i := range 4096 {
		pairs[rune(0x4e00+i)] = uint16(i)
	}
	fonts.RegisterGlyphs(7, pairs)
	fonts.mu.Lock()
	snapshot := fonts.glyphSnapshot(7)
	fonts.mu.Unlock()
	_ = snapshot.hash()
	b.Run("copy-sort-hash", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			fonts.mu.Lock()
			list := fonts.glyphMappingList(7)
			fonts.mu.Unlock()
			_ = hashGlyphMappings(list)
		}
	})
	b.Run("cached-version", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			fonts.mu.Lock()
			snapshot := fonts.glyphSnapshot(7)
			fonts.mu.Unlock()
			_ = snapshot.hash()
		}
	})
}
