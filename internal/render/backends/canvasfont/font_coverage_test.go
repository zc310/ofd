package canvasfont

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"
	"weak"

	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/pkg/spec"
	"golang.org/x/image/font/gofont/goregular"
)

// 在真实 cmap 子表外计数，确认缓存命中后不再逐字查询；不改变字形映射。
type coverageCmapProbe struct {
	source interface {
		Get(r rune) (uint16, bool)
		ToUnicode(glyph uint16) []rune
	}
	calls atomic.Int32
	onGet func()
}

func (p *coverageCmapProbe) Get(r rune) (uint16, bool) {
	p.calls.Add(1)
	if p.onGet != nil {
		p.onGet()
	}
	return p.source.Get(r)
}

func (p *coverageCmapProbe) ToUnicode(glyph uint16) []rune {
	return p.source.ToUnicode(glyph)
}

func coverageTestFamily(t *testing.T) (*canvas.FontFamily, *coverageCmapProbe) {
	t.Helper()
	family := canvas.NewFontFamily("same-coverage-alias")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	loaded := family.Face(cjkProbeSize).Font
	probe := &coverageCmapProbe{source: loaded.Cmap.Subtables[0]}
	loaded.Cmap.Subtables[0] = probe
	return family, probe
}

func TestTextCoverageCacheReusesPositiveAndNegativeResults(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	family, probe := coverageTestFamily(t)
	for _, tc := range []struct {
		value string
		want  bool
	}{{"Hello 123", true}, {"Hello 中", false}} {
		object := textObjectWith(tc.value)
		if got := fonts.familyCoversText(family, object, FontRegular); got != tc.want {
			t.Fatalf("%q 覆盖=%v，期望 %v", tc.value, got, tc.want)
		}
		calls := probe.calls.Load()
		if calls == 0 {
			t.Fatal("首次检查没有查询实际字体")
		}
		for range 3 {
			object.Size++
			if got := fonts.familyCoversText(family, object, FontRegular); got != tc.want {
				t.Fatalf("重复检查 %q 覆盖=%v，期望 %v", tc.value, got, tc.want)
			}
		}
		if probe.calls.Load() != calls {
			t.Fatal("相同实际字体和文字再次查询了 cmap")
		}
	}
	// 首段已缓存为覆盖，仍须检查后面的 TextCode，不能缓存成整族可用。
	object := textObjectWith("")
	object.TextCode = []spec.TextCode{{Value: "Hello 123"}, {Value: "Hello 中"}}
	if fonts.familyCoversText(family, object, FontRegular) {
		t.Fatal("前一段命中缓存后跳过了后续 TextCode 的缺字")
	}
	if !fonts.familyCoversText(family, textObjectWith(""), FontRegular) || fonts.coverageCache.Len() != 2 {
		t.Fatal("空文字不应判为缺字或占用缓存")
	}
}

func TestTextCoverageCacheSeparatesSameNameFontsAndActualStyles(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	first, _ := coverageTestFamily(t)
	second, probe := coverageTestFamily(t)
	object := textObjectWith("\uE000")
	if fonts.familyCoversText(first, object, FontRegular) || fonts.familyCoversText(second, object, FontRegular) {
		t.Fatal("原始 Go 字体不应包含测试私有字符")
	}
	if probe.calls.Load() == 0 || fonts.coverageCache.Len() != 2 {
		t.Fatal("同名但不同实例的字体错误共享了覆盖结果")
	}
	repaired, err := fontrepair.RepairWithGlyphs(goregular.TTF, []fontrepair.GlyphMapping{{Rune: '\uE000', Glyph: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.LoadFont(repaired, 0, canvas.FontBold); err != nil {
		t.Fatal(err)
	}
	if !fonts.familyCoversText(first, object, FontBold) || fonts.familyCoversText(first, object, FontRegular) {
		t.Fatal("不同样式实际选中的字体混用了覆盖结果")
	}
	// 同一族的同一样式被替换后，原先缓存的缺字结果不能遮蔽新字体。
	if err := first.LoadFont(repaired, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	if !fonts.familyCoversText(first, object, FontRegular) {
		t.Fatal("字体替换后仍命中旧实例的缺字结果")
	}
}

func TestTextCoverageCacheSeparatesEmbeddedGlyphMappingVersions(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	previous, err := loadCachedEmbeddedFont(fonts, t.Name(), goregular.TTF, FontRegular, nil)
	if err != nil {
		t.Fatal(err)
	}
	object := textObjectWith("\uE000")
	if fonts.familyCoversText(previous, object, FontRegular) {
		t.Fatal("映射前不应覆盖私有字符")
	}
	current, err := loadCachedEmbeddedFont(fonts, t.Name(), goregular.TTF, FontRegular,
		[]fontrepair.GlyphMapping{{Rune: '\uE000', Glyph: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if !fonts.familyCoversText(current, object, FontRegular) || fonts.familyCoversText(previous, object, FontRegular) {
		t.Fatal("修复后的新字体与仍在绘制的旧字体混用了覆盖结果")
	}
}

func TestTextCoverageCacheObservesRegisteredFallbackReplacement(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	name := t.Name()
	t.Cleanup(func() {
		if err := UnregisterFallbackFont(name); err != nil {
			t.Error(err)
		}
	})
	if err := registerFallbackFont(goregular.TTF, name, FontRegular); err != nil {
		t.Fatal(err)
	}
	if err := fonts.UseFallbackFont(name); err != nil {
		t.Fatal(err)
	}
	current, _ := coverageTestFamily(t)
	object := textObjectWith("\uE000")
	if fonts.registeredFallbackCovering(current, object, FontRegular).family != nil {
		t.Fatal("更新前注册的回退字体不应覆盖私有字符")
	}
	repaired, err := fontrepair.RepairWithGlyphs(goregular.TTF, []fontrepair.GlyphMapping{{Rune: '\uE000', Glyph: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := registerFallbackFont(repaired, name, FontRegular); err != nil {
		t.Fatal(err)
	}
	if err := fonts.UseFallbackFont(name); err != nil {
		t.Fatal(err)
	}
	if got := fonts.registeredFallbackCovering(current, object, FontRegular); got.family == nil || got.family != fonts.fallbacks[name] {
		t.Fatal("更新回退注册表后仍沿用了旧字体的缺字结果")
	}
}

func TestTextCoverageCacheBoundsEntriesAndBytesWithoutSkippingGlyphs(t *testing.T) {
	for _, size := range []string{"entries", "bytes"} {
		t.Run(size, func(t *testing.T) {
			fonts := NewFonts(nil)
			defer fonts.Close()
			family, _ := coverageTestFamily(t)
			loaded := family.Face(cjkProbeSize).Font
			for index := range textCoverageCacheEntries + 10 {
				fonts.fontCoversValue(loaded, "hot")
				value := fmt.Sprintf("cold-%d", index)
				if size == "bytes" {
					value += strings.Repeat("A", textCoverageMaxValueBytes-len(value))
				}
				if !fonts.fontCoversValue(loaded, value) {
					t.Fatal("已覆盖的文字被误判缺字")
				}
			}
			key := textCoverageKey{font: weak.Make(loaded), value: "hot"}
			if _, ok := fonts.coverageCache.Get(key); !ok {
				t.Fatal("缓存压力淘汰了持续使用的热条目")
			}
			if fonts.coverageCache.Len() > textCoverageCacheEntries || fonts.coverageCache.Weight() > textCoverageCacheBytes {
				t.Fatal("文字覆盖缓存未遵守条数或字节上限")
			}
			entries, weight := fonts.coverageCache.Len(), fonts.coverageCache.Weight()
			oversized := strings.Repeat("A", textCoverageMaxValueBytes+1)
			if !fonts.fontCoversValue(loaded, oversized) || fonts.fontCoversValue(loaded, oversized+"中") {
				t.Fatal("超长文字跳过了检查或漏检末尾缺字")
			}
			if fonts.coverageCache.Len() != entries || fonts.coverageCache.Weight() != weight {
				t.Fatal("超长文字冲掉了已有热条目")
			}
		})
	}
}

func TestTextCoverageCacheCopiesTextWithoutRetainingSourceBuffer(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	family, _ := coverageTestFamily(t)
	source := strings.Repeat("A", 1<<20)
	value := source[123:140]
	if !fonts.familyCoversText(family, textObjectWith(value), FontRegular) {
		t.Fatal("普通文字应被覆盖")
	}
	saved := fonts.coverageCache.first.key.value
	if saved != value || unsafe.StringData(saved) == unsafe.StringData(value) {
		t.Fatal("缓存文字没有独立复制，可能钉住整个来源缓冲区")
	}
}

func TestTextCoverageCacheDoesNotRetainFont(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	previous := cacheCoverageOnlyFont(t, fonts)
	runtime.GC()
	if previous.Value() != nil || fonts.coverageCache.Len() != 1 {
		t.Fatal("覆盖缓存反向持有字体，或测试没有留下待检查的缓存条目")
	}
	runtime.KeepAlive(fonts)
}

func cacheCoverageOnlyFont(t *testing.T, fonts *Fonts) weak.Pointer[canvas.Font] {
	t.Helper()
	family, _ := coverageTestFamily(t)
	if !fonts.familyCoversText(family, textObjectWith("ABC"), FontRegular) {
		t.Fatal("普通文字应被覆盖")
	}
	return weak.Make(family.Face(cjkProbeSize).Font)
}

func TestTextCoverageRetirementRemovesOnlyRetiredFontEntries(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	retired, _ := coverageTestFamily(t)
	active, activeProbe := coverageTestFamily(t)
	for _, family := range []*canvas.FontFamily{retired, active} {
		if !fonts.familyCoversText(family, textObjectWith("ABC"), FontRegular) ||
			fonts.familyCoversText(family, textObjectWith("ABC中"), FontRegular) {
			t.Fatal("退役前的覆盖检查结果不正确")
		}
	}
	if fonts.coverageCache.Len() != 4 {
		t.Fatal("未缓存两份字体的正负覆盖结果")
	}
	fonts.forgetShapingCache(retired, FontRegular)
	if fonts.coverageCache.Len() != 2 {
		t.Fatal("没有仅清除退役字体的覆盖缓存")
	}
	calls := activeProbe.calls.Load()
	if !fonts.familyCoversText(active, textObjectWith("ABC"), FontRegular) ||
		fonts.familyCoversText(active, textObjectWith("ABC中"), FontRegular) ||
		activeProbe.calls.Load() != calls {
		t.Fatal("字体退役误清除了另一字体的正负覆盖缓存")
	}
	if !fonts.familyCoversText(retired, textObjectWith("ABC"), FontRegular) ||
		fonts.familyCoversText(retired, textObjectWith("ABC中"), FontRegular) ||
		fonts.coverageCache.Len() != 2 {
		t.Fatal("旧页面应能继续检查退役字体，但不能重新缓存")
	}
}

func TestTextCoverageRetirementDuringCheckDoesNotRepublishCache(t *testing.T) {
	for _, value := range []string{"ABC", "中"} {
		t.Run(value, func(t *testing.T) {
			fonts := NewFonts(nil)
			defer fonts.Close()
			family, probe := coverageTestFamily(t)
			want := value == "ABC"
			entered, resume, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			release := sync.OnceFunc(func() { close(resume) })
			probe.onGet = sync.OnceFunc(func() { close(entered); <-resume })
			go func() {
				defer close(done)
				if got := fonts.familyCoversText(family, textObjectWith(value), FontRegular); got != want {
					t.Errorf("退役期间覆盖检查=%v，期望 %v", got, want)
				}
			}()
			defer func() { release(); <-done }()
			awaitFontSignal(t, entered)
			fonts.forgetShapingCache(family, FontRegular)
			release()
			awaitFontSignal(t, done)
			if fonts.coverageCache.Len() != 0 || fonts.coverageCache.Weight() != 0 {
				t.Fatal("退役期间的检查重新填充了覆盖缓存")
			}
		})
	}
}

func TestTextCoverageCloseDuringCheckDoesNotRepublishCache(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	family, probe := coverageTestFamily(t)
	if !fonts.familyCoversText(family, textObjectWith("warm"), FontRegular) {
		t.Fatal("普通文字应被覆盖")
	}
	entered, resume, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	probe.onGet = sync.OnceFunc(func() { close(entered); <-resume })
	go func() {
		defer close(done)
		if !fonts.familyCoversText(family, textObjectWith("late"), FontRegular) {
			t.Error("关闭期间仍持有字体的检查应能完成")
		}
	}()
	defer func() { release(); <-done }()
	awaitFontSignal(t, entered)
	fonts.Close()
	release()
	awaitFontSignal(t, done)
	if fonts.coverageCache.Len() != 0 || fonts.coverageCache.Weight() != 0 {
		t.Fatal("关闭期间的检查重新填充了缓存")
	}
	if !fonts.familyCoversText(family, textObjectWith("after"), FontRegular) || fonts.coverageCache.Len() != 0 {
		t.Fatal("关闭后的检查应继续正确计算，但不能重新缓存")
	}
}

func TestTextCoverageConcurrentChecksAndClose(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	family, _ := coverageTestFamily(t)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			<-start
			for range 100 {
				if !fonts.familyCoversText(family, textObjectWith("ABC"), FontRegular) ||
					fonts.familyCoversText(family, textObjectWith("ABC中"), FontRegular) {
					t.Error("并发覆盖检查结果发生变化")
					return
				}
			}
		})
	}
	workers.Go(func() { <-start; fonts.Close() })
	close(start)
	workers.Wait()
	if fonts.coverageCache.Len() != 0 {
		t.Fatal("关闭后仍残留覆盖缓存")
	}
}
