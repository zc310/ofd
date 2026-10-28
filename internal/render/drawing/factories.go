package drawing

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"sync"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/imageasset"
)

// OffscreenSurface 是离屏 RGBA 绘制表面。
type OffscreenSurface = Backend

// AlphaMaskSurface 将实色路径直接栅格化为单通道覆盖率。
type AlphaMaskSurface interface {
	SetFillColor(c color.Color)
	DrawPath(x, y float64, path *geom.Path)
	RasterAlpha() *image.Alpha
}

// 工厂注册和快照共用锁，调用工厂时不持锁。慢字体加载或表面构建不会阻塞
// 其他创建与注册；在途调用使用取得的快照，后续调用使用新注册的工厂。
var factoryMu sync.RWMutex

var (
	errNoPDFDocument   = errors.New("未注册 PDF 文档写入器（请空白导入 internal/render/backends/canvas 或注册自定义工厂）")
	errNoVectorSurface = errors.New("未注册矢量表面工厂（请空白导入 internal/render/backends/canvas 或注册自定义工厂）")
	errInvalidSize     = errors.New("矢量表面尺寸无效")
)

var (
	newFontEngine       func(doc *parser.Document) FontEngine
	newPDFDocument      func(w io.Writer, options PDFOptions) (PDFDocument, error)
	newVectorSurface    func(width, height float64, draw func(DrawContext)) VectorSurface
	newOffscreenSurface func(width, height float64, resolution geom.Resolution) OffscreenSurface
	newAlphaMaskSurface func(width, height float64, resolution geom.Resolution) AlphaMaskSurface
	svgSceneFactory     imageasset.SVGParser
)

// RegisterFontEngineFactory 替换字体引擎工厂并返回旧值；nil 清除注册。
func RegisterFontEngineFactory(fn func(doc *parser.Document) FontEngine) func(doc *parser.Document) FontEngine {
	factoryMu.Lock()
	defer factoryMu.Unlock()
	previous := newFontEngine
	newFontEngine = fn
	return previous
}

// NewFontEngine 创建文档字体引擎，未注册时返回 nil。
func NewFontEngine(doc *parser.Document) FontEngine {
	factoryMu.RLock()
	fn := newFontEngine
	factoryMu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(doc)
}

// RegisterPDFDocumentFactory 替换 PDF 文档工厂并返回旧值；nil 清除注册。
func RegisterPDFDocumentFactory(fn func(w io.Writer, options PDFOptions) (PDFDocument, error)) func(w io.Writer, options PDFOptions) (PDFDocument, error) {
	factoryMu.Lock()
	defer factoryMu.Unlock()
	previous := newPDFDocument
	newPDFDocument = fn
	return previous
}

// NewPDFDocument 创建 PDF 写入器，未注册时返回错误。
func NewPDFDocument(w io.Writer, options PDFOptions) (PDFDocument, error) {
	factoryMu.RLock()
	fn := newPDFDocument
	factoryMu.RUnlock()
	if fn == nil {
		return nil, errNoPDFDocument
	}
	return fn(w, options)
}

// RegisterVectorSurfaceFactory 替换矢量表面工厂并返回旧值；nil 清除注册。
func RegisterVectorSurfaceFactory(fn func(width, height float64, draw func(DrawContext)) VectorSurface) func(width, height float64, draw func(DrawContext)) VectorSurface {
	factoryMu.Lock()
	defer factoryMu.Unlock()
	previous := newVectorSurface
	newVectorSurface = fn
	return previous
}

// VectorSurfaceFactory 返回当前矢量工厂快照。调用方须在 draw 同步执行期间
// 持有页面租约；模板缓存也使用该快照保证检查和构建来自同一工厂。
func VectorSurfaceFactory() func(width, height float64, draw func(DrawContext)) VectorSurface {
	factoryMu.RLock()
	defer factoryMu.RUnlock()
	return newVectorSurface
}

// NewVectorSurface 创建指定毫米尺寸的矢量表面，工厂在返回前同步执行 draw。
func NewVectorSurface(width, height float64, draw func(DrawContext)) (VectorSurface, error) {
	fn := VectorSurfaceFactory()
	if fn == nil {
		return nil, errNoVectorSurface
	}
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("%w: %v x %v", errInvalidSize, width, height)
	}
	return fn(width, height, draw), nil
}

// RegisterOffscreenSurfaceFactory 替换离屏表面工厂并返回旧值；nil 清除注册。
func RegisterOffscreenSurfaceFactory(fn func(width, height float64, resolution geom.Resolution) OffscreenSurface) func(width, height float64, resolution geom.Resolution) OffscreenSurface {
	factoryMu.Lock()
	defer factoryMu.Unlock()
	previous := newOffscreenSurface
	newOffscreenSurface = fn
	return previous
}

// NewOffscreenSurface 创建离屏表面，未注册时返回 nil。
func NewOffscreenSurface(width, height float64, resolution geom.Resolution) OffscreenSurface {
	factoryMu.RLock()
	fn := newOffscreenSurface
	factoryMu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(width, height, resolution)
}

// RegisterAlphaMaskSurfaceFactory 替换单通道表面工厂并返回旧值；nil 清除注册。
func RegisterAlphaMaskSurfaceFactory(fn func(width, height float64, resolution geom.Resolution) AlphaMaskSurface) func(width, height float64, resolution geom.Resolution) AlphaMaskSurface {
	factoryMu.Lock()
	defer factoryMu.Unlock()
	previous := newAlphaMaskSurface
	newAlphaMaskSurface = fn
	return previous
}

// NewAlphaMaskSurface 创建单通道覆盖率表面，未注册时返回 nil。
func NewAlphaMaskSurface(width, height float64, resolution geom.Resolution) AlphaMaskSurface {
	factoryMu.RLock()
	fn := newAlphaMaskSurface
	factoryMu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(width, height, resolution)
}

// RegisterSVGSceneFactory 替换 SVG 解析工厂并返回旧值；nil 清除注册。
func RegisterSVGSceneFactory(fn imageasset.SVGParser) imageasset.SVGParser {
	factoryMu.Lock()
	defer factoryMu.Unlock()
	previous := svgSceneFactory
	svgSceneFactory = fn
	return previous
}

// SVGSceneFactory 返回当前 SVG 解析工厂快照，供资源缓存使用。
func SVGSceneFactory() imageasset.SVGParser {
	factoryMu.RLock()
	defer factoryMu.RUnlock()
	return svgSceneFactory
}

// ParseSVGScene 解析 SVG，并保护场景的共享绘制状态。
func ParseSVGScene(data []byte) (SVGScene, error) {
	return imageasset.ParseSVG(data, SVGSceneFactory())
}
