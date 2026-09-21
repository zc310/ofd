package render

import (
	"image"
	"image/color"

	"github.com/tdewolff/canvas"
)

// canvasBackend 是 DrawContext 的 tdewolff/canvas 实现：逐方法转发到
// canvas.Context，输出与传 *canvas.Context 直接绘制完全一致（本包现有
// 全部绘制代码就是这种直接方式）。
//
// 语义选择：
//   - ClearFill/ClearStroke 用 Transparent 色而非 canvas 的空 Paint：
//     对栅格化结果逐像素等价（空 Paint 不画、透明色画空白亦然）。
//   - TextPath 直接提交已翻转好的字形轮廓路径（同 document_text.go 的
//     face.ToPath + ReflectY 流程）。
//   - CopyStrokeToFill 复用 canvas 自身保存的描边样式。
type canvasBackend struct {
	ctx *canvas.Context
}

// NewCanvasBackend 包装一个 canvas.Context 为 DrawContext。
func NewCanvasBackend(ctx *canvas.Context) DrawContext {
	return &canvasBackend{ctx: ctx}
}

func (b *canvasBackend) Push() { b.ctx.Push() }
func (b *canvasBackend) Pop()  { b.ctx.Pop() }

func (b *canvasBackend) Translate(x, y float64) { b.ctx.Translate(x, y) }
func (b *canvasBackend) Scale(sx, sy float64)   { b.ctx.Scale(sx, sy) }
func (b *canvasBackend) Rotate(deg float64)     { b.ctx.Rotate(deg) }

func (b *canvasBackend) CurrentMatrix() canvas.Matrix {
	return b.ctx.CoordSystemView().Mul(b.ctx.View())
}

func (b *canvasBackend) SetFillColor(c color.Color) { b.ctx.SetFillColor(c) }
func (b *canvasBackend) SetFillGradient(g canvas.Gradient) {
	b.ctx.SetFillGradient(g)
}
func (b *canvasBackend) SetFillPaint(paint canvas.Paint) { b.ctx.SetFill(paint) }
func (b *canvasBackend) ClearFill()                      { b.ctx.SetFillColor(canvas.Transparent) }

func (b *canvasBackend) SetStrokeColor(c color.Color) { b.ctx.SetStrokeColor(c) }
func (b *canvasBackend) SetStrokeGradient(g canvas.Gradient) {
	b.ctx.SetStrokeGradient(g)
}
func (b *canvasBackend) SetStrokePaint(paint canvas.Paint) {
	b.ctx.SetStroke(paint)
}
func (b *canvasBackend) ClearStroke() { b.ctx.SetStrokeColor(canvas.Transparent) }
func (b *canvasBackend) CopyStrokeToFill() {
	b.ctx.SetFill(b.ctx.Style.Stroke)
}

func (b *canvasBackend) SetStrokeWidth(w float64) { b.ctx.SetStrokeWidth(w) }
func (b *canvasBackend) SetDashes(offset float64, dashes ...float64) {
	b.ctx.SetDashes(offset, dashes...)
}
func (b *canvasBackend) SetStrokeCapper(cap canvas.Capper) { b.ctx.SetStrokeCapper(cap) }
func (b *canvasBackend) SetStrokeJoiner(join canvas.Joiner) {
	b.ctx.SetStrokeJoiner(join)
}
func (b *canvasBackend) StrokeWidth() float64             { return b.ctx.Style.StrokeWidth }
func (b *canvasBackend) StrokeCapper() canvas.Capper      { return b.ctx.Style.StrokeCapper }
func (b *canvasBackend) StrokeJoiner() canvas.Joiner      { return b.ctx.Style.StrokeJoiner }
func (b *canvasBackend) SetFillRule(rule canvas.FillRule) { b.ctx.SetFillRule(rule) }

func (b *canvasBackend) DrawPath(x, y float64, p *canvas.Path) {
	b.ctx.DrawPath(x, y, p)
}
func (b *canvasBackend) TextPath(p *canvas.Path, x, y float64) {
	b.ctx.DrawPath(x, y, p)
}
func (b *canvasBackend) DrawImage(img image.Image, x, y float64, dpmm float64) {
	b.ctx.DrawImage(x, y, img, canvas.DPMM(dpmm))
}
func (b *canvasBackend) RenderImage(img image.Image, m canvas.Matrix) {
	b.ctx.RenderImage(img, m)
}

// DrawTextLine 实现 textDrawer：原样转发 canvas 的文字绘制，
// 与直接传 *canvas.Context 调用 ctx.DrawText 逐像素一致。
func (b *canvasBackend) DrawTextLine(line *canvas.Text) {
	if line == nil {
		return
	}
	b.ctx.DrawText(0, 0, line)
}

// RenderScene 实现 svgSceneRenderer：把 SVG 场景直接复用到底层渲染器，
// 保持 PDF/SVG 输出的矢量质量。
func (b *canvasBackend) RenderScene(svg *canvas.Canvas, m canvas.Matrix) {
	svg.RenderViewTo(b.ctx.Renderer, m)
}
