package render

import (
	"image"
	"io"
	"sync"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
)

// fakePDFDocument 记录 PDF 文档工厂的调用情况。
type fakePDFDocument struct {
	pages int
	added []VectorSurface
	close int
}

func (d *fakePDFDocument) AddPage(page VectorSurface) error {
	d.pages++
	d.added = append(d.added, page)
	return nil
}

func (d *fakePDFDocument) Close() error { d.close++; return nil }

// TestRegisterPDFDocumentFactory 验证 PDF 写入器可通过工厂替换，且
// NewPDFDocument 会转发 AddPage/Close。
func TestRegisterPDFDocumentFactory(t *testing.T) {
	prev := newPDFDocument
	defer func() { newPDFDocument = prev }()

	fake := &fakePDFDocument{}
	RegisterPDFDocumentFactory(func(w io.Writer, options PDFOptions) (PDFDocument, error) {
		if w == nil {
			t.Fatal("writer should be forwarded")
		}
		return fake, nil
	})
	doc, err := NewPDFDocument(io.Discard, PDFOptions{Compress: true})
	if err != nil {
		t.Fatal(err)
	}
	if doc != PDFDocument(fake) {
		t.Fatal("NewPDFDocument did not use the registered factory")
	}
	if err := doc.AddPage(nil); err != nil {
		t.Fatal(err)
	}
	if err := doc.Close(); err != nil {
		t.Fatal(err)
	}
	if fake.pages != 1 || fake.close != 1 {
		t.Fatalf("fake PDF calls = pages:%d close:%d", fake.pages, fake.close)
	}
}

// fakeSVGScene 是 SVG 解析器替换测试用的最小场景。
type fakeSVGScene struct{ w, h float64 }

func (s fakeSVGScene) Width() float64                        { return s.w }
func (s fakeSVGScene) Height() float64                       { return s.h }
func (s fakeSVGScene) Rasterize(geom.Resolution) image.Image { return nil }
func (s fakeSVGScene) RenderVector(any, geom.Matrix) bool    { return false }

// TestRegisterSVGSceneFactory 验证 SVG 解析器可替换。
func TestRegisterSVGSceneFactory(t *testing.T) {
	prev := svgSceneFactory
	defer func() { svgSceneFactory = prev }()

	RegisterSVGSceneFactory(func(data []byte) (SVGScene, error) {
		return fakeSVGScene{w: 1, h: 2}, nil
	})
	scene, err := parseSVGScene([]byte("<svg/>"))
	if err != nil || scene == nil || scene.Width() != 1 || scene.Height() != 2 {
		t.Fatalf("custom SVG parser not used: %v %v", scene, err)
	}

	svgSceneFactory = nil
	if _, err := parseSVGScene(nil); err == nil {
		t.Fatal("unregistered SVG parser should return an error")
	}
}

// fakeFontEngine 是字体引擎替换测试用的空实现。
type fakeFontEngine struct{}

func (fakeFontEngine) LoadFont(models.StRefID) (FontFamily, error) { return nil, nil }
func (fakeFontEngine) FaceObject(FontFamily, models.TextObject, *CTColor) FontFace {
	return nil
}
func (fakeFontEngine) RenderLock(FontFamily) *sync.Mutex { return &sync.Mutex{} }
func (fakeFontEngine) RegisterPageGlyphs(*parser.Document, *parser.Page, *models.PageContent) {
}
func (fakeFontEngine) UseFallbackFont(string) error              { return nil }
func (fakeFontEngine) FallbackFontFamily(models.StRefID) string  { return "" }
func (fakeFontEngine) HasLoadedEmbeddedFont(models.StRefID) bool { return false }

// TestRegisterFontEngineFactory 验证字体引擎可替换。
func TestRegisterFontEngineFactory(t *testing.T) {
	prev := newFontEngine
	defer func() { newFontEngine = prev }()

	RegisterFontEngineFactory(func(*parser.Document) FontEngine { return fakeFontEngine{} })
	doc := NewDocument(nil, &parser.Document{})
	if _, ok := doc.fonts.(fakeFontEngine); !ok {
		t.Fatalf("document font engine = %T, want fakeFontEngine", doc.fonts)
	}
}
