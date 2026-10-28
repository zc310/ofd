package canvasfont

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/ofd/internal/render/drawing"
	"golang.org/x/image/font/gofont/goregular"
)

// 无真实字体解析的容量测试，单独替换注册表并在结束时恢复。
func emptyFallbackRegistry(t *testing.T) {
	t.Helper()
	fontCacheMu.Lock()
	previous := fallbackRegistry
	fallbackRegistry = make(map[string]*fallbackRegistration)
	fontCacheMu.Unlock()
	t.Cleanup(func() {
		fontCacheMu.Lock()
		fallbackRegistry = previous
		fontCacheMu.Unlock()
	})
}

func TestFallbackRegistryCountLimitKeepsExistingRegistrations(t *testing.T) {
	emptyFallbackRegistry(t)
	load := func(*canvas.FontFamily, []byte, drawing.FontStyle) error { return nil }
	for i := 0; i < maxFallbackFamilies; i++ {
		if _, err := registerFallbackFamily(fmt.Sprint(i), FontRegular, []byte{1}, load); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := registerFallbackFamily("overflow", FontRegular, []byte{1}, load); err == nil || !strings.Contains(err.Error(), "数量") {
		t.Fatalf("超过字体族上限应报错，实际为 %v", err)
	}
	if _, err := registerFallbackFamily("0", FontBold, []byte{2}, load); err != nil {
		t.Fatalf("达到族数量上限后仍应允许更新已有族：%v", err)
	}
	if err := UnregisterFallbackFont("0"); err != nil {
		t.Fatal(err)
	}
	if _, err := registerFallbackFamily("overflow", FontRegular, []byte{1}, load); err != nil {
		t.Fatalf("注销后额度应可复用：%v", err)
	}
}

func TestFallbackRegistryByteLimitAndReplacement(t *testing.T) {
	emptyFallbackRegistry(t)
	// 八份共享同一底层数据，仅分配 32 MiB，按八份注册分别计费。
	data := make([]byte, 32<<20)
	load := func(*canvas.FontFamily, []byte, drawing.FontStyle) error { return nil }
	for i := 0; i < maxFallbackSourceBytes/len(data); i++ {
		fallbackRegistry[fmt.Sprint(i)] = &fallbackRegistration{
			family: canvas.NewFontFamily(fmt.Sprint(i)), renderMu: &sync.Mutex{},
			sources: []fallbackFontSource{{data: data, style: FontRegular, digest: sha256.Sum256(data)}},
		}
	}
	if _, err := registerFallbackFamily("overflow", FontRegular, []byte{1}, load); err == nil || !strings.Contains(err.Error(), "总量") {
		t.Fatalf("源数据超限应报错，实际为 %v", err)
	}
	// 替换已有来源时先扣除旧来源，不能重复计费。
	if _, err := registerFallbackFamily("0", FontRegular, []byte{2}, load); err != nil {
		t.Fatalf("替换为较小来源应释放额度：%v", err)
	}
	if _, err := registerFallbackFamily("overflow", FontRegular, []byte{1}, load); err != nil {
		t.Fatal(err)
	}
}

func TestFallbackRegistryConcurrentRegistrationHonorsLimit(t *testing.T) {
	emptyFallbackRegistry(t)
	for i := 0; i < maxFallbackFamilies-1; i++ {
		fallbackRegistry[fmt.Sprint(i)] = &fallbackRegistration{}
	}
	load := func(*canvas.FontFamily, []byte, drawing.FontStyle) error { return nil }
	var workers sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		workers.Go(func() {
			_, err := registerFallbackFamily(fmt.Sprintf("new-%d", i), FontRegular, []byte{1}, load)
			results <- err
		})
	}
	workers.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 || len(fallbackRegistry) != maxFallbackFamilies {
		t.Fatalf("并发超限：成功=%d，族数量=%d", successes, len(fallbackRegistry))
	}
}

func TestSystemFontReverseOwnershipSurvivesFontMapReset(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	key := systemFontKey{path: t.Name()}
	family, ok := loadCachedFont(fonts, key, "reverse-index", func(f *canvas.FontFamily) error {
		return f.LoadFont(goregular.TTF, 0, canvas.FontRegular)
	})
	if !ok {
		t.Fatal("加载失败")
	}
	fonts.RegisterGlyphs(1, map[rune]uint16{'A': 1})
	fontCacheMu.Lock()
	entry := fonts.systemFonts[key]
	fontCacheMu.Unlock()
	if entry == nil {
		t.Fatal("没有登记反向持有索引")
	}
	fonts.Close()
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	if len(fonts.systemFonts) != 0 || systemFontCache[key] != nil || fontRenderLocks[family] != nil {
		t.Fatal("关闭后系统字体与锁没有释放")
	}
}

func BenchmarkReleaseSystemFontOwnership(b *testing.B) {
	for _, count := range []int{64, 1024, 16384} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			fonts, holder := NewFonts(nil), NewFonts(nil)
			fontCacheMu.Lock()
			previous := systemFontCache
			systemFontCache = make(map[systemFontKey]*systemFontCacheEntry, count)
			for i := 0; i < count; i++ {
				systemFontCache[systemFontKey{path: fmt.Sprint(i)}] = &systemFontCacheEntry{
					fontHolders: fontHolders{holders: map[*Fonts]struct{}{holder: {}}},
				}
			}
			fontCacheMu.Unlock()
			defer func() {
				fontCacheMu.Lock()
				systemFontCache = previous
				fontCacheMu.Unlock()
				fonts.Close()
				holder.Close()
			}()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				releaseSystemFonts(fonts)
			}
		})
	}
}
