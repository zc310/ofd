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
	// vector 表示该后端用于矢量序列化（PDF/SVG/EPS/TeX），可以把
	// Linear/RadialGradient 写成原生 Shading，从而保留文字可复制性。
	vector bool
}

// newCanvasBackend 包装一个 canvas.Context 为 DrawContext（矢量输出）。
func newCanvasBackend(ctx *canvas.Context) drawing.DrawContext {
	return &canvasBackend{ctx: ctx, vector: true}
}

// ShadingText 实现 drawing.ShadingTextBackend：矢量后端支持把原生
// Linear/RadialGradient 作为文字着色图案序列化。
func (b *canvasBackend) ShadingText() bool { return b.vector }

func (b *canvasBackend) Push() { b.ctx.Push() }
func (b *canvasBackend) Pop()  { b.ctx.Pop() }

func (b *canvasBackend) Translate(x, y float64) { b.ctx.Translate(x, y) }
func (b *canvasBackend) Scale(sx, sy float64)   { b.ctx.Scale(sx, sy) }
func (b *canvasBackend) Rotate(deg float64)     { b.ctx.Rotate(deg) }

// Transform 右乘当前视图矩阵，与 canvas 的 ComposeView 语义一致。
func (b *canvasBackend) Transform(m geom.Matrix) { b.ctx.ComposeView(canvasconv.ToCanvasMatrix(m)) }

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

// SetDashes 把 OFD 的绝对长度虚线换算到 canvas 的比例虚线语义。
//
// canvas 的虚线是相对线宽的比例：各渲染器输出前会按线宽缩放虚线数组与相位
// （canvas.ScaleDash，见上游提交 76412ab「Setting dashes in Canvas is
// proportional to the stroke width」，同一提交引入的 Dashed=[3,3] 等常量
// 也是比例值）。而 OFD 的 DashPattern/DashOffset 以毫米为单位、与
// LineWidth 无关，PDF 32000-1 §9.3.6 与 SVG 规范同样规定虚线是用户空间
// 长度、不随线宽缩放。
//
// 因此这里先除掉线宽，由 canvas 侧再乘回来，净效果为零。其余栅格后端由
// rastercore 直接按毫米换算，无需补偿，所以只在本适配层做一次。
func (b *canvasBackend) SetDashes(offset float64, dashes ...float64) {
	lineWidth := b.ctx.Style.StrokeWidth
	if !(lineWidth > 0) {
		b.ctx.SetDashes(offset, dashes...)
		return
	}
	scaled := make([]float64, len(dashes))
	for i, d := range dashes {
		scaled[i] = d / lineWidth
	}
	b.ctx.SetDashes(offset/lineWidth, scaled...)
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
//
// 构造与缓存由 CanvasImageOnce 一起完成，因此并发渲染同一文档时只会构造一次，
// 也不会出现读到写了一半的接口值。
func canvasImage(img image.Image) image.Image {
	lazy, ok := img.(*render.EncodedImage)
	if !ok {
		return img
	}
	ci := lazy.CanvasImageOnce(func() image.Image {
		var (
			out image.Image
			err error
		)
		switch lazy.Format {
		case "jpeg":
			out, err = cimage.NewJPEGImage(bytes.NewReader(lazy.Data))
		case "png":
			out, err = cimage.NewPNGImage(bytes.NewReader(lazy.Data))
		}
		if err != nil || out == nil {
			return nil
		}
		// canvas 的 PDF 写入器固定按 DeviceRGB 声明图像，而 Go 的 JPEG 编码器只对
		// 具体的 *image.Gray 输出单通道灰度 JPEG；canvas 快路径会把灰度 PNG 解码成
		// *image.Gray。灰度 JPEG 按 DeviceRGB 解码会错位，表现为整幅图重复/花屏，
		// 因此灰度图跳过原字节内嵌快路径，交回 canvas 走通用 RGB 编码。
		if lazy.ColorModel() == color.GrayModel {
			return nil
		}
		return out
	})
	if ci == nil {
		return img
	}
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
