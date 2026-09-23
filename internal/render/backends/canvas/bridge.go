// Package canvas 提供 tdewolff/canvas 的 render 后端实现：DrawContext
// （canvasBackend）、矢量表面与 PDF 写入器（VectorSurface/PDFDocument）、
// SVG 场景、离屏栅格表面、内置栅格后端（BackendCanvas），以及默认字体引擎
// （Fonts）。
//
// 空白导入本包即可注册上述能力：
//
//	import _ "github.com/zc310/ofd/internal/render/backends/canvas"
package canvas

import (
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
)

func init() {
	// 栅格后端 BackendCanvas。
	_ = drawing.RegisterOrReplaceBackend(drawing.BackendCanvas, func(width, height float64, resolution geom.Resolution) (drawing.Backend, error) {
		return newCanvasRasterBackend(width, height, resolution), nil
	})
	// 离屏栅格表面。
	render.RegisterOffscreenSurfaceFactory(func(width, height float64, resolution geom.Resolution) render.OffscreenSurface {
		return newCanvasRasterBackend(width, height, resolution)
	})
	// 字体引擎。
	render.RegisterFontEngineFactory(func(doc *parser.Document) render.FontEngine {
		return NewFonts(doc)
	})
	// 矢量表面与 PDF 写入器。
	render.RegisterVectorSurfaceFactory(newCanvasVectorSurface)
	render.RegisterPDFDocumentFactory(newCanvasPDFDocument)
	// SVG 场景解析。
	render.RegisterSVGSceneFactory(newCanvasSVGScene)
}
