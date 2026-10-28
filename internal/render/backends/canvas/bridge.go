// Package canvas 提供 tdewolff/canvas 的 render 后端实现：DrawContext
// （canvasBackend）、矢量表面与 PDF 写入器（VectorSurface/PDFDocument）、
// SVG 场景、离屏栅格表面、内置栅格后端（BackendCanvas），以及默认字体引擎
// （由 canvasfont 包注册）。
//
// 空白导入本包即可注册上述能力：
//
//	import _ "github.com/zc310/ofd/internal/render/backends/canvas"
package canvas

import (
	// 保持导入 canvas 后端时同时具备默认字体引擎。
	_ "github.com/zc310/ofd/internal/render/backends/canvasfont"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
)

func init() {
	// 栅格后端 BackendCanvas。
	_ = drawing.RegisterOrReplaceBackend(drawing.BackendCanvas, func(width, height float64, resolution geom.Resolution) (drawing.Backend, error) {
		return newCanvasRasterBackend(width, height, resolution), nil
	})
	// 离屏栅格表面。
	drawing.RegisterOffscreenSurfaceFactory(func(width, height float64, resolution geom.Resolution) drawing.OffscreenSurface {
		return newCanvasRasterBackend(width, height, resolution)
	})
	drawing.RegisterAlphaMaskSurfaceFactory(func(width, height float64, resolution geom.Resolution) drawing.AlphaMaskSurface {
		return newAlphaMaskSurface(width, height, resolution)
	})
	// 矢量表面与 PDF 写入器。
	drawing.RegisterVectorSurfaceFactory(newCanvasVectorSurface)
	drawing.RegisterPDFDocumentFactory(newCanvasPDFDocument)
	// SVG 场景解析。
	drawing.RegisterSVGSceneFactory(newCanvasSVGScene)
}
