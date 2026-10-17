package canvas

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zc310/canvas"
	"github.com/zc310/ofd/internal/fontrepair"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

// 等待目标调用确实阻塞在互斥锁上，避免靠固定延时猜测并发时序。
func waitFontMutex(t *testing.T, function string) bool {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		buffer := make([]byte, 1<<20)
		n := runtime.Stack(buffer, true)
		for _, stack := range strings.Split(string(buffer[:n]), "\n\n") {
			if strings.Contains(stack, function) && strings.Contains(stack, "[sync.Mutex.Lock]") {
				return true
			}
		}
		runtime.Gosched()
	}
	return false
}

func TestUseFallbackFontPinsCurrentRegistration(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	name := t.Name()
	if err := registerFallbackFont(goregular.TTF, name, FontRegular); err != nil {
		t.Fatal(err)
	}
	defer UnregisterFallbackFont(name)

	fonts.mu.Lock()
	done := make(chan error, 1)
	go func() { done <- fonts.UseFallbackFont(name) }()
	blocked := waitFontMutex(t, "(*Fonts).UseFallbackFont")
	// 应用尚未取得实例锁时，注销旧条目并以同名注册不同字体。
	err := UnregisterFallbackFont(name)
	if err == nil {
		err = registerFallbackFont(gobold.TTF, name, FontBold)
	}
	fonts.mu.Unlock()
	useErr := <-done
	if !blocked || err != nil || useErr != nil {
		t.Fatalf("未能完成并发重注册：等待=%v，注册=%v，应用=%v", blocked, err, useErr)
	}
	fontCacheMu.Lock()
	reg := fallbackRegistry[name]
	current, refs := reg.family, reg.refs
	fontCacheMu.Unlock()
	if fonts.fallbacks[name] != current || refs != 1 {
		t.Fatalf("字体快照与引用条目不一致：当前字体=%v，引用数=%d", fonts.fallbacks[name] == current, refs)
	}
	fonts.Close()
	if err := UnregisterFallbackFont(name); err != nil {
		t.Fatal(err)
	}
	if _, ok := fallbackFontData(name); ok {
		t.Fatal("最后一个持有者关闭并注销后仍保留字体数据")
	}
}

func TestFallbackRegistrationWaitsWithoutHoldingCacheLock(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	name := t.Name()
	useFallback(t, fonts, goregular.TTF, name, FontRegular)
	defer UnregisterFallbackFont(name)
	fontCacheMu.Lock()
	reg := fallbackRegistry[name]
	fontCacheMu.Unlock()
	reg.renderMu.Lock()
	done := make(chan error, 1)
	go func() { done <- registerFallbackFont(gobold.TTF, name, FontBold) }()
	blocked := waitFontMutex(t, "registerFallbackFamily")
	available := fontCacheMu.TryLock()
	if available {
		fontCacheMu.Unlock()
	}
	reg.renderMu.Unlock()
	err := <-done
	if !blocked || !available || err != nil {
		t.Fatalf("注册等待渲染锁时阻塞全局缓存：等待=%v，缓存可用=%v，注册=%v", blocked, available, err)
	}
}

func TestFallbackFontRejectsIncompleteAndFailedRegistration(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	name := t.Name()
	if err := registerFallbackFont([]byte("invalid font"), name, FontRegular); err == nil {
		t.Fatal("损坏字体注册成功")
	}
	fontCacheMu.Lock()
	_, published := fallbackRegistry[name]
	// 构造尚未加载字体面的条目，验证应用入口不会将其当成可用字体。
	fallbackRegistry[name] = &fallbackRegistration{renderMu: &sync.Mutex{}}
	fontCacheMu.Unlock()
	defer UnregisterFallbackFont(name)
	if published {
		t.Error("失败的首次注册发布了全局条目")
	}
	if err := fonts.UseFallbackFont(name); err == nil {
		t.Fatal("未完成的注册被当成可用字体")
	}
}

func TestFallbackStyleUpdateKeepsOldFamilyRenderLock(t *testing.T) {
	first, second := NewFonts(nil), NewFonts(nil)
	defer first.Close()
	defer second.Close()
	name := t.Name()
	useFallback(t, first, goregular.TTF, name, FontRegular)
	defer UnregisterFallbackFont(name)
	if err := second.UseFallbackFont(name); err != nil {
		t.Fatal(err)
	}
	old := first.fallbacks[name]
	lock := first.RenderLock(old)
	if err := registerFallbackFont(gobold.TTF, name, FontBold); err != nil {
		t.Fatal(err)
	}
	if err := first.UseFallbackFont(name); err != nil {
		t.Fatal(err)
	}
	if first.RenderLock(old) != lock || second.RenderLock(old) != lock || first.RenderLock(first.fallbacks[name]) != lock {
		t.Fatal("新增样式后新旧字体族不再共用渲染锁")
	}
}

func TestClosedFontsDoNotRefillCaches(t *testing.T) {
	fonts := NewFonts(nil)
	family, err := loadCachedEmbeddedFont(fonts, "close-caches", goregular.TTF, FontRegular, nil)
	if err != nil {
		t.Fatal(err)
	}
	face := family.Face(12, canvas.Black)
	fonts.RenderLock(family)
	fonts.shapedTextPath(face, "A")
	fonts.shapedTextLine(face, "A")
	fonts.cachedTextWidth(face, "A")
	embeddedRepairer(fonts, goregular.TTF)
	fonts.Close()

	// 模拟关闭前已开始的调用继续完成；它们都不得重新缓存字体或整形成果。
	if _, err := loadCachedEmbeddedFont(fonts, "close-caches", goregular.TTF, FontRegular, nil); err == nil {
		t.Error("关闭后仍接受内嵌字体加载")
	}
	embeddedRepairer(fonts, gobold.TTF)
	fonts.RenderLock(family)
	fonts.shapedTextPath(face, "B")
	fonts.shapedTextLine(face, "B")
	fonts.cachedTextWidth(face, "B")
	if len(fonts.embeddedFonts) != 0 || len(fonts.embeddedRepairers) != 0 || len(fonts.renderLocks) != 0 ||
		len(fonts.pathCache) != 0 || len(fonts.textLineCache) != 0 || len(fonts.textWidthCache) != 0 {
		t.Fatal("关闭后的调用重新填充了字体、渲染锁或整形缓存")
	}
}

func TestCloseRacesEmbeddedLoadsWithoutRetainingResources(t *testing.T) {
	for i := 0; i < 16; i++ {
		fonts := NewFonts(nil)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for worker := 0; worker < 4; worker++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				family, err := loadCachedEmbeddedFont(fonts, "inflight", goregular.TTF, FontRegular,
					[]fontrepair.GlyphMapping{{Rune: 'A', Glyph: 1}})
				if err == nil {
					fonts.RenderLock(family)
				}
			}()
		}
		close(start)
		runtime.Gosched()
		fonts.Close()
		wg.Wait()
		if len(fonts.embeddedFonts) != 0 || len(fonts.embeddedRepairers) != 0 || len(fonts.renderLocks) != 0 {
			t.Fatalf("第%d轮关闭后仍保留内嵌字体或渲染锁", i)
		}
	}
}

func TestClosedFontsCannotRetainSystemFont(t *testing.T) {
	file := filepath.Join(t.TempDir(), "closed.ttf")
	if err := os.WriteFile(file, goregular.TTF, 0o600); err != nil {
		t.Fatal(err)
	}
	fonts := NewFonts(nil)
	fonts.Close()
	if _, ok := loadCachedFontFile(fonts, file, "closed-system", FontRegular); ok {
		t.Fatal("关闭后的实例重新取得了系统字体缓存引用")
	}
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	for _, entry := range systemFontCache {
		if _, held := entry.holders[fonts]; held {
			t.Fatal("关闭后的实例仍在系统字体持有者集合中")
		}
	}
}

func TestCloseRacesShapingWithoutRefillingCaches(t *testing.T) {
	fonts := NewFonts(nil)
	family, err := loadCachedEmbeddedFont(fonts, "shaping-close", goregular.TTF, FontRegular, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer fonts.Close()
	face := family.Face(12, canvas.Black)
	started, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 128; i++ {
			fonts.shapedTextPath(face, "ABC")
			fonts.shapedTextLine(face, "ABC")
			fonts.cachedTextWidth(face, "ABC")
			if i == 0 {
				close(started)
			}
		}
	}()
	<-started
	fonts.Close()
	<-done
	if len(fonts.pathCache) != 0 || len(fonts.textLineCache) != 0 || len(fonts.textWidthCache) != 0 {
		t.Fatal("关闭与整形并发后缓存仍有残留")
	}
}
