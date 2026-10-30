package reader

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/backends/canvasfont"
	"golang.org/x/image/font/gofont/goregular"
)

type observedFallbackEngine struct {
	render.FontEngine
	before, after func()
	applications  int
}

func (f *observedFallbackEngine) UseFallbackFont(family string) error {
	if f.before != nil {
		f.before()
	}
	if err := f.FontEngine.UseFallbackFont(family); err != nil {
		return err
	}
	f.applications++
	if f.after != nil {
		f.after()
	}
	return nil
}

func TestUseFallbackFontConcurrentUnregisterDoesNotPartiallyApply(t *testing.T) {
	for _, point := range []string{"before first application", "after first application"} {
		t.Run(point, func(t *testing.T) {
			family := t.Name()
			if err := RegisterFallbackFont(FontSource{Family: family, Data: goregular.TTF}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				// 仅为失败用例收尾，生命周期断言必须在此之前完成。
				if _, ok := FallbackFontData(family); ok {
					_ = UnregisterFallbackFont(family)
				}
			})
			t.Cleanup(func() {
				render.RegisterFontEngineFactory(func(doc *parser.Document) render.FontEngine {
					return canvasfont.NewFonts(doc)
				})
			})
			ready, proceed := make(chan struct{}), make(chan struct{})
			pause := func() { close(ready); <-proceed }
			var engines []*observedFallbackEngine
			render.RegisterFontEngineFactory(func(doc *parser.Document) render.FontEngine {
				engine := &observedFallbackEngine{FontEngine: canvasfont.NewFonts(doc)}
				if len(engines) == 0 {
					if point == "before first application" {
						engine.before = pause
					} else {
						engine.after = pause
					}
				}
				engines = append(engines, engine)
				return engine
			})
			r, err := Open(duplicatePageIDsPackage(t))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = r.Close() })
			if len(engines) != 2 {
				t.Fatalf("需要两个独立文档体，得到 %d 个", len(engines))
			}
			var once sync.Once
			resume := func() { once.Do(func() { close(proceed) }) }
			t.Cleanup(resume)
			previousText, previousSearch := r.text, r.search
			done := make(chan error, 1)
			go func() { done <- r.UseFallbackFont(family) }()
			select {
			case <-ready:
			case <-time.After(10 * time.Second):
				t.Fatal("应用未到达预定时序")
			}
			if err := UnregisterFallbackFont(family); err != nil {
				t.Fatal(err)
			}
			resume()
			select {
			case err = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("应用未结束")
			}
			if point == "before first application" {
				if err == nil || r.fallbackFamily != "" || len(r.fallbackFamilies) != 0 || r.text != previousText || r.search != previousSearch {
					t.Errorf("首次应用前注销后应失败且保持原状态：err=%v family=%q families=%v", err, r.fallbackFamily, r.fallbackFamilies)
				}
				for _, engine := range engines {
					if engine.applications != 0 {
						t.Error("失败前不应有文档应用该字体")
					}
				}
			} else {
				if err != nil || r.fallbackFamily != family || !slices.Equal(r.fallbackFamilies, []string{family}) || r.text == previousText || r.search == previousSearch {
					t.Errorf("首个文档持有字体后应完成全部应用并更新状态：err=%v family=%q families=%v", err, r.fallbackFamily, r.fallbackFamilies)
				}
				for _, engine := range engines {
					if engine.applications != 1 {
						t.Errorf("每个文档应成功应用一次，得到 %d", engine.applications)
					}
				}
			}
			if closeErr := r.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
			if _, ok := FallbackFontData(family); ok {
				t.Error("仅注销一次后，后续文档持有覆盖了注销意图，关闭 Reader 仍保留字体来源")
			}
		})
	}
}
