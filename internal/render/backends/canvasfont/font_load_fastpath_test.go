package canvasfont

import (
	"fmt"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/spec"
	"golang.org/x/image/font/gofont/goregular"
)

func logicalFontLoadFixture(tb testing.TB, count int) (*Fonts, spec.StRefID, *spec.Font) {
	tb.Helper()
	document := creator.Document{ID: "logical-font-load", Pages: []creator.Page{{}}}
	for i := range count {
		document.Fonts = append(document.Fonts, creator.Font{Name: fmt.Sprintf("logical%d", i)})
	}
	data, err := creator.Marshal(document)
	if err != nil {
		tb.Fatal(err)
	}
	ofd, err := parser.NewOFD(data)
	if err != nil {
		tb.Fatal(err)
	}
	fonts := NewFonts(ofd.Documents[0])
	tb.Cleanup(func() { fonts.Close(); _ = ofd.Close() })
	if got := len(ofd.Documents[0].Fonts()); got != count {
		tb.Fatalf("字体资源数 = %d，期望 %d", got, count)
	}
	var id spec.StRefID
	var resource *spec.Font
	fonts.ForEachFont(func(candidateID spec.StID, candidate *spec.Font) bool {
		id, resource = spec.StRefID(candidateID), candidate
		return false
	})
	return fonts, id, resource
}

func TestLogicalFontLoadDoesNotAllocateResourceSnapshot(t *testing.T) {
	fonts, id, resource := logicalFontLoadFixture(t, 128)
	family := canvas.NewFontFamily("fallback")
	fallbacks := []fallbackFace{{family: family}}
	allocations := testing.AllocsPerRun(100, func() {
		got, err := fonts.loadFontUncached(id, resource, fallbacks, nil)
		if err != nil || got.family != family || got.embedded {
			t.Fatalf("逻辑字体回退失败：字体=%v，错误=%v", got, err)
		}
	})
	if allocations != 0 {
		t.Fatalf("逻辑字体加载不应复制字体资源表，实际分配 %.0f 次", allocations)
	}
}

func TestCachedLoadFontBypassesLoadLocks(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	family, err := loadCachedEmbeddedFont(fonts, "cached", goregular.TTF, FontRegular, nil)
	if err != nil {
		t.Fatal(err)
	}
	fonts.Fonts[1] = family
	lock := fonts.loadLock(1)
	lock.Lock()
	fonts.loadLocksMu.Lock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		got, err := fonts.LoadFont(1)
		if err != nil || got != family {
			t.Errorf("缓存字体加载失败：字体=%v，错误=%v", got, err)
		}
	}()
	defer func() { fonts.loadLocksMu.Unlock(); lock.Unlock(); <-done }()
	// 缓存命中只需实例锁，不能等待锁表或同一字体的加载锁。
	awaitFontSignal(t, done)
}

func TestCachedLoadFontRespectsGlyphInvalidationAndClose(t *testing.T) {
	fonts, _ := sourceCacheFonts(t)
	var id spec.StRefID
	fonts.ForEachFont(func(candidateID spec.StID, _ *spec.Font) bool {
		id = spec.StRefID(candidateID)
		return false
	})
	original, err := fonts.LoadFont(id)
	if err != nil {
		t.Fatal(err)
	}
	if cached, err := fonts.LoadFont(id); err != nil || cached != original {
		t.Fatalf("未复用缓存字体：%v", err)
	}
	fonts.RegisterGlyphs(id, map[rune]uint16{'\uE000': 1})
	if rebuilt, err := fonts.LoadFont(id); err != nil || rebuilt == original {
		t.Fatalf("字形映射变更后未重建字体：%v", err)
	}
	fonts.Close()
	if family, err := fonts.LoadFont(id); err == nil || family != nil {
		t.Fatal("关闭后仍返回缓存字体")
	}
}

func TestNormalizeFallbackNamePreservesMatchingRules(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{"", ""},
		{" \t\n", ""},
		{"Noto Sans_CJK-SC", "notosanscjksc"},
		{"\u3000仿宋_GB2312\u3000", "仿宋gb2312"},
		{"DEJAVU SANS MONO", "dejavusansmono"},
	} {
		if got := normalizeFallbackName(test.input); got != test.want {
			t.Errorf("normalizeFallbackName(%q) = %q，期望 %q", test.input, got, test.want)
		}
	}
}

func BenchmarkLogicalFontLoad(b *testing.B) {
	for _, count := range []int{16, 1024} {
		b.Run(fmt.Sprintf("resources-%d", count), func(b *testing.B) {
			fonts, id, resource := logicalFontLoadFixture(b, count)
			fallbacks := []fallbackFace{{family: canvas.NewFontFamily("fallback")}}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := fonts.loadFontUncached(id, resource, fallbacks, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCachedLoadFont(b *testing.B) {
	fonts := NewFonts(nil)
	b.Cleanup(fonts.Close)
	fonts.Fonts[1] = canvas.NewFontFamily("cached")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := fonts.LoadFont(1); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNormalizeFallbackName(b *testing.B) {
	for _, name := range []string{"logical0", "Noto Sans_CJK-SC"} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				normalizeFallbackName(name)
			}
		})
	}
}
