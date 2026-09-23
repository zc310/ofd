package render

import (
	"image"
	"image/color"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"

	"github.com/zc310/ofd/internal/render/canvasconv"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
)

// 测试专用的 canvas DrawContext 适配：仅在 package render 的测试中用于驱动
// canvas 绘制。它直接依赖 tdewolff/canvas（测试文件不构成包的运行时依赖），
// 从而避免测试导入 backends/canvas 造成的 import cycle。

type testCanvasBackend struct {
	ctx *canvas.Context
}

// newCanvasBackend 仅供本包测试使用。
func newCanvasBackend(ctx *canvas.Context) drawing.DrawContext {
	return &testCanvasBackend{ctx: ctx}
}

func (b *testCanvasBackend) Push() { b.ctx.Push() }
func (b *testCanvasBackend) Pop()  { b.ctx.Pop() }

func (b *testCanvasBackend) Translate(x, y float64) { b.ctx.Translate(x, y) }
func (b *testCanvasBackend) Scale(sx, sy float64)   { b.ctx.Scale(sx, sy) }
func (b *testCanvasBackend) Rotate(deg float64)     { b.ctx.Rotate(deg) }

func (b *testCanvasBackend) CurrentMatrix() geom.Matrix {
	return geom.Matrix(b.ctx.CoordSystemView().Mul(b.ctx.View()))
}

func (b *testCanvasBackend) SetFillColor(c color.Color) { b.ctx.SetFillColor(c) }
func (b *testCanvasBackend) SetFillGradient(g geom.Gradient) {
	b.ctx.SetFillGradient(canvasconv.ToCanvasGradient(g))
}
func (b *testCanvasBackend) SetFillPaint(paint geom.Paint) {
	b.ctx.SetFill(canvasconv.ToCanvasPaint(paint))
}
func (b *testCanvasBackend) ClearFill() { b.ctx.SetFillColor(canvas.Transparent) }

func (b *testCanvasBackend) SetStrokeColor(c color.Color) { b.ctx.SetStrokeColor(c) }
func (b *testCanvasBackend) SetStrokeGradient(g geom.Gradient) {
	b.ctx.SetStrokeGradient(canvasconv.ToCanvasGradient(g))
}
func (b *testCanvasBackend) SetStrokePaint(paint geom.Paint) {
	b.ctx.SetStroke(canvasconv.ToCanvasPaint(paint))
}
func (b *testCanvasBackend) ClearStroke() { b.ctx.SetStrokeColor(canvas.Transparent) }
func (b *testCanvasBackend) CopyStrokeToFill() {
	b.ctx.SetFill(b.ctx.Style.Stroke)
}

func (b *testCanvasBackend) SetStrokeWidth(w float64) { b.ctx.SetStrokeWidth(w) }
func (b *testCanvasBackend) SetDashes(offset float64, dashes ...float64) {
	b.ctx.SetDashes(offset, dashes...)
}
func (b *testCanvasBackend) SetStrokeCapper(cap geom.Capper) {
	b.ctx.SetStrokeCapper(canvasconv.ToCanvasCapper(cap))
}
func (b *testCanvasBackend) SetStrokeJoiner(join geom.Joiner) {
	b.ctx.SetStrokeJoiner(canvasconv.ToCanvasJoiner(join))
}
func (b *testCanvasBackend) StrokeWidth() float64 { return b.ctx.Style.StrokeWidth }
func (b *testCanvasBackend) StrokeCapper() geom.Capper {
	return canvasconv.FromCanvasCapper(b.ctx.Style.StrokeCapper)
}
func (b *testCanvasBackend) StrokeJoiner() geom.Joiner {
	return canvasconv.FromCanvasJoiner(b.ctx.Style.StrokeJoiner)
}
func (b *testCanvasBackend) SetFillRule(rule geom.FillRule) {
	b.ctx.SetFillRule(canvasconv.ToCanvasFillRule(rule))
}

func (b *testCanvasBackend) DrawPath(x, y float64, p *geom.Path) {
	b.ctx.DrawPath(x, y, canvasconv.ToCanvasPath(p))
}
func (b *testCanvasBackend) TextPath(p *geom.Path, x, y float64) {
	b.ctx.DrawPath(x, y, canvasconv.ToCanvasPath(p))
}
func (b *testCanvasBackend) DrawImage(img image.Image, x, y float64, dpmm float64) {
	b.ctx.DrawImage(x, y, img, canvas.DPMM(dpmm))
}
func (b *testCanvasBackend) RenderImage(img image.Image, m geom.Matrix) {
	b.ctx.RenderImage(img, canvas.Matrix(m))
}

// rasterize 仅供本包测试使用：把 canvas 画布栅格化为 RGBA。
func rasterize(c *canvas.Canvas, resolution canvas.Resolution, colorSpace canvas.ColorSpace) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, int(c.W*resolution.DPMM()+0.5), int(c.H*resolution.DPMM()+0.5)))
	base := rasterizer.FromImage(img, resolution, colorSpace)
	c.RenderTo(base)
	base.Close()
	return img
}

// RenderScene 实现 canvas SVG 场景接口，使测试中的矢量嵌入路径可用。
func (b *testCanvasBackend) RenderScene(svg *canvas.Canvas, m geom.Matrix) {
	if svg == nil {
		return
	}
	svg.RenderViewTo(b.ctx.Renderer, canvas.Matrix(m))
}
