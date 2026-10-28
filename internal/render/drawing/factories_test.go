package drawing

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
)

func TestMissingFactoriesReturnSafeFallbacks(t *testing.T) {
	font := RegisterFontEngineFactory(nil)
	pdf := RegisterPDFDocumentFactory(nil)
	vector := RegisterVectorSurfaceFactory(nil)
	offscreen := RegisterOffscreenSurfaceFactory(nil)
	alpha := RegisterAlphaMaskSurfaceFactory(nil)
	svg := RegisterSVGSceneFactory(nil)
	t.Cleanup(func() {
		RegisterFontEngineFactory(font)
		RegisterPDFDocumentFactory(pdf)
		RegisterVectorSurfaceFactory(vector)
		RegisterOffscreenSurfaceFactory(offscreen)
		RegisterAlphaMaskSurfaceFactory(alpha)
		RegisterSVGSceneFactory(svg)
	})
	if NewFontEngine(nil) != nil || NewOffscreenSurface(10, 20, geom.DPI(96)) != nil || NewAlphaMaskSurface(10, 20, geom.DPI(96)) != nil {
		t.Fatal("未注册工厂时应跳过可选绘制能力")
	}
	if _, err := NewPDFDocument(io.Discard, PDFOptions{}); err == nil {
		t.Fatal("未注册 PDF 写入器却没有返回错误")
	}
	if _, err := NewVectorSurface(10, 20, nil); err == nil {
		t.Fatal("未注册矢量表面却没有返回错误")
	}
	if _, err := ParseSVGScene(nil); err == nil {
		t.Fatal("未注册 SVG 解析器却没有返回错误")
	}
}

func TestSlowFactoryDoesNotBlockReplacementOrOtherFactories(t *testing.T) {
	entered, resume := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	var workers sync.WaitGroup
	var oldCalls, newCalls atomic.Int32
	doc := &parser.Document{}
	previous := RegisterFontEngineFactory(func(got *parser.Document) FontEngine {
		if got != doc {
			t.Error("字体工厂丢失文档作用域")
		}
		oldCalls.Add(1)
		close(entered)
		<-resume
		return nil
	})
	t.Cleanup(func() { RegisterFontEngineFactory(previous) })
	defer func() { release(); workers.Wait() }()
	workers.Go(func() { NewFontEngine(doc) })
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("字体工厂未开始执行")
	}
	available := make(chan struct{})
	workers.Go(func() {
		defer close(available)
		RegisterFontEngineFactory(func(*parser.Document) FontEngine { newCalls.Add(1); return nil })
		NewFontEngine(doc)
		want := errors.New("测试 PDF 工厂错误")
		pdf := RegisterPDFDocumentFactory(func(io.Writer, PDFOptions) (PDFDocument, error) { return nil, want })
		defer func() { RegisterPDFDocumentFactory(pdf) }()
		if _, err := NewPDFDocument(io.Discard, PDFOptions{}); !errors.Is(err, want) {
			t.Errorf("PDF 工厂错误未传递：%v", err)
		}
	})
	select {
	case <-available:
	case <-time.After(3 * time.Second):
		t.Fatal("慢工厂持有注册锁，阻塞了替换或其他工厂调用")
	}
	release()
	workers.Wait()
	if oldCalls.Load() != 1 || newCalls.Load() != 1 {
		t.Fatalf("在途调用或新调用未使用各自快照：old=%d new=%d", oldCalls.Load(), newCalls.Load())
	}
}

func TestVectorFactorySnapshotKeepsSynchronousDrawAndRestoresRegistration(t *testing.T) {
	var oldCalls, newCalls int
	previous := RegisterVectorSurfaceFactory(func(w, h float64, draw func(DrawContext)) VectorSurface {
		if w != 10 || h != 20 {
			t.Error("矢量表面的物理尺寸未传递给工厂")
		}
		oldCalls++
		draw(nil)
		return nil
	})
	t.Cleanup(func() { RegisterVectorSurfaceFactory(previous) })
	snapshot := VectorSurfaceFactory()
	old := RegisterVectorSurfaceFactory(func(_, _ float64, draw func(DrawContext)) VectorSurface {
		newCalls++
		draw(nil)
		return nil
	})
	draws := 0
	snapshot(10, 20, func(DrawContext) { draws++ })
	if _, err := NewVectorSurface(10, 20, func(DrawContext) { draws++ }); err != nil {
		t.Fatal(err)
	}
	RegisterVectorSurfaceFactory(old)
	if _, err := NewVectorSurface(10, 20, func(DrawContext) { draws++ }); err != nil {
		t.Fatal(err)
	}
	if draws != 3 || oldCalls != 2 || newCalls != 1 {
		t.Fatalf("工厂快照或恢复错误：draws=%d old=%d new=%d", draws, oldCalls, newCalls)
	}
	if _, err := NewVectorSurface(0, 20, nil); err == nil || oldCalls != 2 {
		t.Fatal("非法尺寸仍调用了绘制工厂")
	}
}
