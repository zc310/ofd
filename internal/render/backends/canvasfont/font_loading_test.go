package canvasfont

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zc310/canvas"
	"github.com/zc310/ofd/internal/render/drawing"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

func awaitFontSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("字体操作被无关加载阻塞")
	}
}

func TestSlowSystemFontLoadDoesNotBlockCachedFontsOrClose(t *testing.T) {
	hot, cold, other := NewFonts(nil), NewFonts(nil), NewFonts(nil)
	defer hot.Close()
	defer cold.Close()
	defer other.Close()
	key := systemFontKey{path: t.Name() + "/hot"}
	load := func(family *canvas.FontFamily) error { return family.LoadFont(goregular.TTF, 0, canvas.FontRegular) }
	family, ok := loadCachedFont(hot, key, "hot", load)
	if !ok {
		t.Fatal("热字体加载失败")
	}
	shared, ok := loadCachedFont(other, key, "alias", load)
	if !ok || shared != family {
		t.Fatal("热字体未共享")
	}
	lock := hot.RenderLock(family)
	entered, resume, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	go func() {
		defer close(done)
		_, ok := loadCachedFont(cold, systemFontKey{path: t.Name() + "/cold"}, "cold", func(family *canvas.FontFamily) error {
			close(entered)
			<-resume
			return family.LoadFont(gobold.TTF, 0, canvas.FontRegular)
		})
		if !ok {
			t.Error("冷字体加载失败")
		}
	}()
	defer func() { release(); <-done }()
	awaitFontSignal(t, entered)

	available := make(chan struct{})
	go func() {
		defer close(available)
		if reused, ok := loadCachedFont(hot, key, "cached", load); !ok || reused != family {
			t.Error("慢加载期间缓存命中失败")
		}
		if hot.RenderLock(family) != lock {
			t.Error("共享渲染锁发生变化")
		}
		defaultFallbackFont()
		other.Close()
	}()
	awaitFontSignal(t, available)
}

func TestSystemFontConcurrentLoadParsesOnce(t *testing.T) {
	const count = 8
	fonts := make([]*Fonts, count)
	for i := range fonts {
		fonts[i] = NewFonts(nil)
		defer fonts[i].Close()
	}
	key := systemFontKey{path: t.Name()}
	var calls atomic.Int32
	entered, resume := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	var wg sync.WaitGroup
	defer func() { release(); wg.Wait() }()
	families := make([]*canvas.FontFamily, count)
	load := func(family *canvas.FontFamily) error {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-resume
		return family.LoadFont(goregular.TTF, 0, canvas.FontRegular)
	}
	for i := range fonts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var ok bool
			families[i], ok = loadCachedFont(fonts[i], key, "concurrent", load)
			if !ok {
				t.Error("并发加载失败")
			}
		}()
	}
	awaitFontSignal(t, entered)
	if !waitFontMutex(t, "loadCachedFont") {
		t.Fatal("其他加载没有等待冷加载锁")
	}
	release()
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("同一文件解析了%d次", calls.Load())
	}
	lock := fonts[0].RenderLock(families[0])
	for i, family := range families {
		if family != families[0] || fonts[i].RenderLock(family) != lock {
			t.Fatal("并发加载没有共享字体族与渲染锁")
		}
	}
}

func TestSystemFontCloseDuringLoadDoesNotPublish(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	key := systemFontKey{path: t.Name()}
	entered, resume, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	go func() {
		defer close(done)
		if _, ok := loadCachedFont(fonts, key, "closing", func(family *canvas.FontFamily) error {
			close(entered)
			<-resume
			return family.LoadFont(goregular.TTF, 0, canvas.FontRegular)
		}); ok {
			t.Error("关闭后的加载仍成功发布")
		}
	}()
	defer func() { release(); <-done }()
	awaitFontSignal(t, entered)
	closed := make(chan struct{})
	go func() { fonts.Close(); close(closed) }()
	awaitFontSignal(t, closed)
	release()
	awaitFontSignal(t, done)
	fontCacheMu.Lock()
	entry := systemFontCache[key]
	fontCacheMu.Unlock()
	if entry != nil || len(fonts.renderLocks) != 0 {
		t.Fatal("关闭后重新发布了字体或渲染锁")
	}
}

func TestSystemFontFailedLoadCanRetry(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	key := systemFontKey{path: t.Name()}
	if _, ok := loadCachedFont(fonts, key, "retry", func(*canvas.FontFamily) error { return errors.New("读取失败") }); ok {
		t.Fatal("失败的加载被发布")
	}
	if _, ok := loadCachedFont(fonts, key, "retry", func(family *canvas.FontFamily) error {
		return family.LoadFont(goregular.TTF, 0, canvas.FontRegular)
	}); !ok {
		t.Fatal("加载失败后无法重试")
	}
}

func TestSharedRenderLockIsCachedPerInstance(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(map[bool]string{false: "system", true: "fallback"}[fallback], func(t *testing.T) {
			fonts, observer := NewFonts(nil), NewFonts(nil)
			defer fonts.Close()
			defer observer.Close()
			var family *canvas.FontFamily
			if fallback {
				useFallback(t, fonts, goregular.TTF, t.Name(), FontRegular)
				defer UnregisterFallbackFont(t.Name())
				family = fonts.fallbacks[t.Name()]
			} else {
				var ok bool
				family, ok = loadCachedFont(fonts, systemFontKey{path: t.Name()}, "cached-lock", func(family *canvas.FontFamily) error {
					return family.LoadFont(goregular.TTF, 0, canvas.FontRegular)
				})
				if !ok {
					t.Fatal("字体加载失败")
				}
			}
			lock := fonts.RenderLock(family)
			if observer.RenderLock(family) != lock {
				t.Fatal("全局查询没有取得共享渲染锁")
			}
			// 全局锁被占用时，持有者和首次查询过的实例都必须走本地缓存。
			fontCacheMu.Lock()
			done := make(chan struct{})
			go func() {
				defer close(done)
				if fonts.RenderLock(family) != lock || observer.RenderLock(family) != lock {
					t.Error("实例缓存没有保留共享锁")
				}
			}()
			defer func() { fontCacheMu.Unlock(); <-done }()
			awaitFontSignal(t, done)
		})
	}
}

func TestFallbackParsingDoesNotBlockGlobalCache(t *testing.T) {
	for _, update := range []bool{false, true} {
		t.Run(map[bool]string{false: "initial", true: "style"}[update], func(t *testing.T) {
			fonts, other := NewFonts(nil), NewFonts(nil)
			defer fonts.Close()
			defer other.Close()
			name := t.Name()
			defer UnregisterFallbackFont(name)
			var old *canvas.FontFamily
			if update {
				useFallback(t, fonts, goregular.TTF, name, FontRegular)
				old = fonts.fallbacks[name]
			}
			entered, resume, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			release := sync.OnceFunc(func() { close(resume) })
			pause := sync.OnceFunc(func() { close(entered); <-resume })
			go func() {
				defer close(done)
				_, err := registerFallbackFamily(name, FontBold, gobold.TTF, func(family *canvas.FontFamily, data []byte, style drawing.FontStyle) error {
					pause()
					return loadFallbackFace(family, data, style)
				})
				if err != nil {
					t.Error(err)
				}
			}()
			defer func() { release(); <-done }()
			awaitFontSignal(t, entered)
			available := make(chan struct{})
			go func() {
				defer close(available)
				defaultFallbackFont()
				other.Close()
				err := fonts.UseFallbackFont(name)
				if update {
					if err != nil || fonts.fallbacks[name] != old || len(fonts.fallbackFaces) != 1 {
						t.Error("更新解析期间没有保留完整旧快照")
					}
				} else if err == nil {
					t.Error("首次解析尚未完成就发布了注册条目")
				}
			}()
			awaitFontSignal(t, available)
			release()
			awaitFontSignal(t, done)
			if err := fonts.UseFallbackFont(name); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFallbackReRegistrationDuringParsingKeepsNewEntry(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	name := t.Name()
	defer UnregisterFallbackFont(name)
	if err := registerFallbackFont(goregular.TTF, name, FontRegular); err != nil {
		t.Fatal(err)
	}
	fontCacheMu.Lock()
	old := fallbackRegistry[name]
	fontCacheMu.Unlock()
	entered, resume, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	pause := sync.OnceFunc(func() { close(entered); <-resume })
	go func() {
		defer close(done)
		_, err := registerFallbackFamily(name, FontBold, gobold.TTF, func(family *canvas.FontFamily, data []byte, style drawing.FontStyle) error {
			pause()
			return loadFallbackFace(family, data, style)
		})
		if err != nil {
			t.Error(err)
		}
	}()
	defer func() { release(); <-done }()
	awaitFontSignal(t, entered)
	if err := UnregisterFallbackFont(name); err != nil {
		t.Fatal(err)
	}
	if err := registerFallbackFont(goregular.TTF, name, FontRegular); err != nil {
		t.Fatal(err)
	}
	if err := fonts.UseFallbackFont(name); err != nil {
		t.Fatal(err)
	}
	fontCacheMu.Lock()
	current := fallbackRegistry[name]
	fontCacheMu.Unlock()
	release()
	awaitFontSignal(t, done)
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	if current == old || fallbackRegistry[name] != current || current.refs != 1 || len(current.sources) != 2 {
		t.Fatal("旧解析结果覆盖了同名新条目或丢失引用/样式")
	}
	if fonts.RenderLock(fonts.fallbacks[name]) != current.renderMu {
		t.Fatal("同名重注册后没有保留新条目的共享锁")
	}
}

func TestFallbackConcurrentInitialRegistrationParsesOnce(t *testing.T) {
	name := t.Name()
	defer UnregisterFallbackFont(name)
	entered, resume := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	var calls atomic.Int32
	load := func(family *canvas.FontFamily, data []byte, style drawing.FontStyle) error {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-resume
		return loadFallbackFace(family, data, style)
	}
	var wg sync.WaitGroup
	defer func() { release(); wg.Wait() }()
	families := make([]*canvas.FontFamily, 8)
	for i := range families {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var err error
			families[i], err = registerFallbackFamily(name, FontRegular, goregular.TTF, load)
			if err != nil {
				t.Error(err)
			}
		}()
	}
	awaitFontSignal(t, entered)
	if !waitFontMutex(t, "registerFallbackFamily") {
		t.Fatal("其他首次注册没有等待冷加载锁")
	}
	release()
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("相同的首次注册解析了%d次", calls.Load())
	}
	for _, family := range families {
		if family == nil || family != families[0] {
			t.Fatal("并发首次注册没有共享字体族")
		}
	}
}
