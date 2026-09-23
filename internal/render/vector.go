package render

import (
	"errors"
	"io"

	"github.com/zc310/ofd/internal/render/drawing"
)

// 矢量输出契约定义在 drawing 包，render 通过别名保持既有调用点可用。
type (
	VectorSurface = drawing.VectorSurface
	PDFOptions    = drawing.PDFOptions
	PDFDocument   = drawing.PDFDocument
)

// newPDFDocument 是当前 PDF 写入器工厂；默认未注册，需空白导入 canvas 后端包
// （internal/render/backends/canvas）或调用 RegisterPDFDocumentFactory 注入。
var newPDFDocument func(w io.Writer, options PDFOptions) (PDFDocument, error)

// RegisterPDFDocumentFactory 注册 PDF 文档写入器，供 canvas 后端注册或测试注入。
func RegisterPDFDocumentFactory(fn func(w io.Writer, options PDFOptions) (PDFDocument, error)) {
	if fn != nil {
		newPDFDocument = fn
	}
}

// NewPDFDocument 创建 PDF 文档；未注册写入器时返回错误。
func NewPDFDocument(w io.Writer, options PDFOptions) (PDFDocument, error) {
	if newPDFDocument == nil {
		return nil, errors.New("未注册 PDF 文档写入器（请空白导入 internal/render/backends/canvas 或注册自定义工厂）")
	}
	return newPDFDocument(w, options)
}

// newVectorSurface 是当前矢量表面工厂；默认未注册，需空白导入 canvas 后端包
// 或调用 RegisterVectorSurfaceFactory 注入。
var newVectorSurface func(width, height float64, draw func(DrawContext)) VectorSurface

// RegisterVectorSurfaceFactory 注册矢量表面工厂，供 canvas 后端注册或测试注入。
func RegisterVectorSurfaceFactory(fn func(width, height float64, draw func(DrawContext)) VectorSurface) {
	if fn != nil {
		newVectorSurface = fn
	}
}
