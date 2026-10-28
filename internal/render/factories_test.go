package render

import (
	"io"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
)

// 测试通过正式注册入口替换并恢复工厂，避免直接修改主包全局状态。
func setFontEngineFactory(t *testing.T, fn func(*parser.Document) FontEngine) {
	t.Helper()
	previous := drawing.RegisterFontEngineFactory(fn)
	t.Cleanup(func() { drawing.RegisterFontEngineFactory(previous) })
}

func setVectorSurfaceFactory(t *testing.T, fn func(float64, float64, func(DrawContext)) VectorSurface) {
	t.Helper()
	previous := drawing.RegisterVectorSurfaceFactory(fn)
	t.Cleanup(func() { drawing.RegisterVectorSurfaceFactory(previous) })
}

func setPDFDocumentFactory(t *testing.T, fn func(io.Writer, PDFOptions) (PDFDocument, error)) {
	t.Helper()
	previous := drawing.RegisterPDFDocumentFactory(fn)
	t.Cleanup(func() { drawing.RegisterPDFDocumentFactory(previous) })
}

func setSVGSceneFactory(t *testing.T, fn func([]byte) (SVGScene, error)) {
	t.Helper()
	previous := drawing.RegisterSVGSceneFactory(fn)
	t.Cleanup(func() { drawing.RegisterSVGSceneFactory(previous) })
}

func setOffscreenSurfaceFactory(t *testing.T, fn func(float64, float64, geom.Resolution) OffscreenSurface) {
	t.Helper()
	previous := drawing.RegisterOffscreenSurfaceFactory(fn)
	t.Cleanup(func() { drawing.RegisterOffscreenSurfaceFactory(previous) })
}

func setAlphaMaskSurfaceFactory(t *testing.T, fn func(float64, float64, geom.Resolution) AlphaMaskSurface) {
	t.Helper()
	previous := drawing.RegisterAlphaMaskSurfaceFactory(fn)
	t.Cleanup(func() { drawing.RegisterAlphaMaskSurfaceFactory(previous) })
}
