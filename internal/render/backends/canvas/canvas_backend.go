package canvas

import (
	"bytes"
	"image"
	"image/color"

	"github.com/tdewolff/canvas"
	cimage "github.com/tdewolff/canvas/image"

	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/backends/canvas/canvasconv"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
)

// canvasBackend 是 DrawContext 的 tdewolff/canvas 实现：把与绘制库无关的
// geom 绘制指令转换成 canvas 类型并转发到 canvas.Context，输出与直接用
// canvas 绘制一致（本包矢量输出 Page()/PDF/SVG 都走这条路径）。
//
// 语义选择：
//   - ClearFill/ClearStroke 用 Transparent 色而非 canvas 的空 Paint：
//     对栅格化结果逐像素等价（空 Paint 不画、透明色画空白亦然）。
//   - TextPath 直接提交已翻转好的字形轮廓路径。
//   - CopyStrokeToFill 复用 canvas 自身保存的描边样式。
type canvasBackend struct {
	ctx *canvas.Context
}

// newCanvasBackend 包装一个 canvas.Context 为 DrawContext。
func newCanvasBackend(ctx *canvas.Context) drawing.DrawContext {
	return &canvasBackend{ctx: ctx}
}

func (b *canvasBackend) Push() { b.ctx.Push() }
func (b *canvasBackend) Pop()  { b.ctx.Pop() }

func (b *canvasBackend) Translate(x, y float64) { b.ctx.Translate(x, y) }
func (b *canvasBackend) Scale(sx, sy float64)   { b.ctx.Scale(sx, sy) }
func (b *canvasBackend) Rotate(deg float64)     { b.ctx.Rotate(deg) }

func (b *canvasBackend) CurrentMatrix() geom.Matrix {
	return geom.Matrix(b.ctx.CoordSystemView().Mul(b.ctx.View()))
}

func (b *canvasBackend) SetFillColor(c color.Color) { b.ctx.SetFillColor(c) }
func (b *canvasBackend) SetFillGradient(g geom.Gradient) {
	b.ctx.SetFillGradient(canvasconv.ToCanvasGradient(g))
}
func (b *canvasBackend) SetFillPaint(paint geom.Paint) {
	b.ctx.SetFill(canvasconv.ToCanvasPaint(paint))
}
func (b *canvasBackend) ClearFill() { b.ctx.SetFillColor(canvas.Transparent) }

func (b *canvasBackend) SetStrokeColor(c color.Color) { b.ctx.SetStrokeColor(c) }
func (b *canvasBackend) SetStrokeGradient(g geom.Gradient) {
	b.ctx.SetStrokeGradient(canvasconv.ToCanvasGradient(g))
}
func (b *canvasBackend) SetStrokePaint(paint geom.Paint) {
	b.ctx.SetStroke(canvasconv.ToCanvasPaint(paint))
}
func (b *canvasBackend) ClearStroke() { b.ctx.SetStrokeColor(canvas.Transparent) }
func (b *canvasBackend) CopyStrokeToFill() {
	b.ctx.SetFill(b.ctx.Style.Stroke)
}

func (b *canvasBackend) SetStrokeWidth(w float64) { b.ctx.SetStrokeWidth(w) }
func (b *canvasBackend) SetDashes(offset float64, dashes ...float64) {
	b.ctx.SetDashes(offset, dashes...)
}
func (b *canvasBackend) SetStrokeCapper(cap geom.Capper) {
	b.ctx.SetStrokeCapper(canvasconv.ToCanvasCapper(cap))
}
func (b *canvasBackend) SetStrokeJoiner(join geom.Joiner) {
	b.ctx.SetStrokeJoiner(canvasconv.ToCanvasJoiner(join))
}
func (b *canvasBackend) StrokeWidth() float64 { return b.ctx.Style.StrokeWidth }
func (b *canvasBackend) StrokeCapper() geom.Capper {
	return canvasconv.FromCanvasCapper(b.ctx.Style.StrokeCapper)
}
func (b *canvasBackend) StrokeJoiner() geom.Joiner {
	return canvasconv.FromCanvasJoiner(b.ctx.Style.StrokeJoiner)
}
func (b *canvasBackend) SetFillRule(rule geom.FillRule) {
	b.ctx.SetFillRule(canvasconv.ToCanvasFillRule(rule))
}

func (b *canvasBackend) DrawPath(x, y float64, p *geom.Path) {
	b.ctx.DrawPath(x, y, canvasconv.ToCanvasPath(p))
}
func (b *canvasBackend) TextPath(p *geom.Path, x, y float64) {
	b.ctx.DrawPath(x, y, canvasconv.ToCanvasPath(p))
}
func (b *canvasBackend) DrawImage(img image.Image, x, y float64, dpmm float64) {
	b.ctx.DrawImage(x, y, canvasImage(img), canvas.DPMM(dpmm))
}
func (b *canvasBackend) RenderImage(img image.Image, m geom.Matrix) {
	b.ctx.RenderImage(canvasImage(img), canvas.Matrix(m))
}

// canvasImage 把保留原始编码字节的懒解码图片转换为 canvas/image.Image，使
// canvas PDF 写入器能按原字节内嵌（DCTDecode 等）；其它图片原样返回。
func canvasImage(img image.Image) image.Image {
	lazy, ok := img.(*render.EncodedImage)
	if !ok {
		return img
	}
	if cached := lazy.CanvasCached(); cached != nil {
		if ci, ok := cached.(image.Image); ok {
			return ci
		}
	}
	var (
		ci  image.Image
		err error
	)
	switch lazy.Format {
	case "jpeg":
		ci, err = cimage.NewJPEGImage(bytes.NewReader(lazy.Data))
	case "png":
		ci, err = cimage.NewPNGImage(bytes.NewReader(lazy.Data))
	}
	if err != nil || ci == nil {
		return img
	}
	lazy.SetCanvasCached(ci)
	return ci
}

// DrawTextLine 实现 textDrawer：原样转发 canvas 的文字绘制，
// 与直接传 *canvas.Context 调用 ctx.DrawText 逐像素一致。
func (b *canvasBackend) DrawTextLine(run drawing.TextRun) {
	line, ok := run.(*canvas.Text)
	if !ok || line == nil {
		return
	}
	b.ctx.DrawText(0, 0, line)
}

// RenderScene 实现 svgSceneRenderer：把 SVG 场景直接复用到底层渲染器，
// 保持 PDF/SVG 输出的矢量质量。svg 由 canvas SVG 场景传入，此处断言为
// *canvas.Canvas。
func (b *canvasBackend) RenderScene(svg any, m geom.Matrix) {
	c, ok := svg.(*canvas.Canvas)
	if !ok || c == nil {
		return
	}
	c.RenderViewTo(b.ctx.Renderer, canvas.Matrix(m))
}
