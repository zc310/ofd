package render

import (
	"image/color"
	"sync"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
)

func TestDocumentViewsShareFontsUntilLastViewCloses(t *testing.T) {
	previous := newFontEngine
	t.Cleanup(func() { newFontEngine = previous })
	font := &closeTrackingFont{}
	loads := 0
	newFontEngine = func(*parser.Document) FontEngine {
		loads++
		return font
	}
	base := NewDocument(color.Transparent, &parser.Document{})
	t.Cleanup(base.Close)
	if err := base.UseFallbackFont("fallback"); err != nil {
		t.Fatal(err)
	}
	view, err := base.NewView(color.White, geom.DPI(30))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(view.Close)
	if loads != 1 || view.fonts != base.fonts || view.Document != base.Document {
		t.Fatal("创建不同 DPI 视图重新创建了文档字体引擎")
	}
	if view.Background() != color.White || view.dpi != geom.DPI(30) || len(view.fallbackFontFamilies()) != 1 {
		t.Fatal("视图的背景、DPI 或回退字体配置不正确")
	}
	base.Close()
	if font.closes.Load() != 0 {
		t.Fatal("还有视图存活时关闭基础文档释放了共享字体")
	}
	if _, err := base.NewView(color.Transparent, geom.DPI(96)); err == nil {
		t.Fatal("关闭后的文档不应再创建视图")
	}
	view.Close()
	view.Close()
	if font.closes.Load() != 1 {
		t.Fatalf("字体关闭次数 = %d，期望 1", font.closes.Load())
	}
}

func TestDocumentViewCreationDuringCloseBalancesFontReferences(t *testing.T) {
	previous := newFontEngine
	t.Cleanup(func() { newFontEngine = previous })
	font := &closeTrackingFont{}
	newFontEngine = func(*parser.Document) FontEngine { return font }
	base := NewDocument(color.Transparent, nil)
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			if view, err := base.NewView(color.White, geom.DPI(30)); err == nil {
				view.Close()
			}
		})
	}
	base.Close()
	workers.Wait()
	if font.closes.Load() != 1 {
		t.Fatalf("并发创建/关闭视图后的字体关闭次数 = %d，期望 1", font.closes.Load())
	}
}

func TestSharedFontReleaseDoesNotUnderflowOrCloseTwice(t *testing.T) {
	for _, refs := range []int{0, 1, 4} {
		font := &closeTrackingFont{}
		shared := &sharedFontEngine{engine: font, refs: refs}
		var workers sync.WaitGroup
		for range 16 {
			workers.Go(shared.release)
		}
		workers.Wait()
		if shared.refs != 0 {
			t.Errorf("初始引用 %d，重复释放后引用 = %d，期望 0", refs, shared.refs)
		}
		wantCloses := int32(0)
		if refs > 0 {
			wantCloses = 1
		}
		if got := font.closes.Load(); got != wantCloses {
			t.Errorf("初始引用 %d，关闭次数 = %d，期望 %d", refs, got, wantCloses)
		}
	}
}
