package canvas

import (
	"bytes"
	"runtime"
	"testing"
	"weak"

	"github.com/zc310/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/drawing"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

func TestOnlyExplicitFallbackRegistrationCancelsUnregister(t *testing.T) {
	for _, registerAgain := range []bool{false, true} {
		name := t.Name() + "-held"
		if registerAgain {
			name = t.Name() + "-registered-again"
		}
		func() {
			defer UnregisterFallbackFont(name)
			if err := registerFallbackFont(goregular.TTF, name, FontRegular); err != nil {
				t.Fatal(err)
			}
			first, second := NewFonts(nil), NewFonts(nil)
			defer first.Close()
			defer second.Close()
			if err := first.UseFallbackFont(name); err != nil {
				t.Fatal(err)
			}
			if err := UnregisterFallbackFont(name); err != nil {
				t.Fatal(err)
			}
			if err := second.UseFallbackFont(name); err != nil {
				t.Fatal(err)
			}
			if registerAgain {
				if err := registerFallbackFont(goregular.TTF, name, FontRegular); err != nil {
					t.Fatal(err)
				}
			}
			first.Close()
			second.Close()
			if _, exists := fallbackFontData(name); exists != registerAgain {
				t.Errorf("显式重新注册=%v，关闭全部文档后来源仍存在=%v", registerAgain, exists)
			}
		}()
	}
}

func TestFallbackStyleReplacementKeepsOnlyCurrentSources(t *testing.T) {
	name := t.Name()
	defer UnregisterFallbackFont(name)
	for _, tc := range []struct {
		style drawing.FontStyle
		data  []byte
	}{
		{FontRegular, goregular.TTF},
		{FontBold, gobold.TTF},
		{FontRegular, gobold.TTF},
		{FontRegular, goregular.TTF},
	} {
		if err := registerFallbackFont(tc.data, name, tc.style); err != nil {
			t.Fatal(err)
		}
		reg := fallbackRegistry[name]
		seen := make(map[drawing.FontStyle]bool)
		for _, source := range reg.sources {
			if seen[source.style] {
				t.Fatalf("样式 %v 保留了多个历史字体数据", source.style)
			}
			seen[source.style] = true
			if source.style == tc.style && !bytes.Equal(source.data, tc.data) {
				t.Fatal("样式替换后未保存当前数据")
			}
		}
		if got := reg.family.Face(12, canvas.Black, canvasStyle(tc.style)).Font.GlyphIndex('A'); got == 0 {
			t.Fatal("样式替换后的字体无法绘制")
		}
	}
}

func TestFallbackStyleReplacementFailurePreservesPreviousSources(t *testing.T) {
	name := t.Name()
	defer UnregisterFallbackFont(name)
	if err := registerFallbackFont(goregular.TTF, name, FontRegular); err != nil {
		t.Fatal(err)
	}
	previous := fallbackRegistry[name].family
	if err := registerFallbackFont([]byte("broken font"), name, FontRegular); err == nil {
		t.Fatal("损坏的替换字体不应注册成功")
	}
	reg := fallbackRegistry[name]
	if reg.family != previous || len(reg.sources) != 1 || !bytes.Equal(reg.sources[0].data, goregular.TTF) {
		t.Fatal("失败的替换破坏了当前可用字体或其源数据")
	}
}

func TestFallbackStyleUpdateDoesNotMutatePublishedFamily(t *testing.T) {
	name := t.Name()
	defer UnregisterFallbackFont(name)
	if err := registerFallbackFont(goregular.TTF, name, FontRegular); err != nil {
		t.Fatal(err)
	}
	previous := fallbackRegistry[name].family
	previousBold := previous.Face(12, canvas.Black, canvas.FontBold).Font
	// 相同数据用于另一种样式时，也不能就地修改已经交给页面的字体表。
	if err := registerFallbackFont(goregular.TTF, name, FontBold); err != nil {
		t.Fatal(err)
	}
	if fallbackRegistry[name].family == previous || previous.Face(12, canvas.Black, canvas.FontBold).Font != previousBold {
		t.Fatal("新增样式改写了页面正在使用的旧字体族")
	}
}

func TestFallbackStyleUpdateReleasesPreviousShapingCache(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	name := t.Name()
	defer UnregisterFallbackFont(name)
	previous := cacheRemovedFallback(t, fonts, name)
	if err := registerFallbackFont(gobold.TTF, name, FontRegular); err != nil {
		t.Fatal(err)
	}
	if err := fonts.UseFallbackFont(name); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	if previous.Value() != nil {
		t.Fatal("更新回退字体后整形缓存仍保留旧字体")
	}
	runtime.KeepAlive(fonts)
}

func TestRemovedFallbackCanBeCollectedWhileFontEngineStaysOpen(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	name := t.Name()
	defer UnregisterFallbackFont(name)
	previous := cacheRemovedFallback(t, fonts, name)
	if err := UnregisterFallbackFont(name); err != nil {
		t.Fatal(err)
	}
	fonts.RemoveFallbackFont(name)
	runtime.GC()
	if previous.Value() != nil {
		t.Fatal("回退字体注销并移除后仍被底层切片或整形缓存保留")
	}
	if _, ok := fallbackFontData(name); ok {
		t.Fatal("最后一个持有者移除后注册表仍保留字体数据")
	}
	runtime.KeepAlive(fonts)
}

func cacheRemovedFallback(t *testing.T, fonts *Fonts, name string) weak.Pointer[canvas.Font] {
	t.Helper()
	if err := registerFallbackFont(goregular.TTF, name, FontRegular); err != nil {
		t.Fatal(err)
	}
	if err := fonts.UseFallbackFont(name); err != nil {
		t.Fatal(err)
	}
	family := fonts.fallbacks[name]
	face := family.Face(12, canvas.Black)
	fonts.shapedTextPath(face, "ABC")
	fonts.shapedTextLine(face, "ABC")
	fonts.cachedTextWidth(face, "ABC")
	fonts.RenderLock(family)
	return weak.Make(face.Font)
}

func TestCloseClearsGlyphMappingAndFallbackBookkeeping(t *testing.T) {
	fonts := NewFonts(nil)
	fonts.RegisterGlyphs(1, map[rune]uint16{'A': 1})
	fonts.fallbackByFont[1] = "fallback"
	fonts.loadLock(1)
	fonts.Close()
	if len(fonts.glyphMappings) != 0 || len(fonts.fallbackByFont) != 0 || len(fonts.loadLocks) != 0 {
		t.Fatalf("关闭后保留字形表=%d、回退选择=%d、加载锁=%d",
			len(fonts.glyphMappings), len(fonts.fallbackByFont), len(fonts.loadLocks))
	}
	if _, err := fonts.LoadFont(models.StRefID(2)); err == nil {
		t.Fatal("关闭后不应再加载字体")
	}
	if len(fonts.loadLocks) != 0 {
		t.Fatal("关闭后的调用重新创建了字体加载锁")
	}
}
