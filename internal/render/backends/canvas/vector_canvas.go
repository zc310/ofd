package canvas

import (
	"errors"
	"fmt"
	"image"
	"io"

	"github.com/tdewolff/canvas"
	cimage "github.com/tdewolff/canvas/image"
	"github.com/tdewolff/canvas/renderers"
	"github.com/tdewolff/canvas/renderers/pdf"

	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
)

// vector_canvas.go 是 drawing.VectorSurface/PDFDocument 的 canvas 实现（默认注册）。
// 它是核心包内唯一做矢量序列化的文件：替换矢量引擎时删除本文件并注册自定义
// 工厂即可。
type canvasVectorSurface struct {
	c *canvas.Canvas
}

func (s canvasVectorSurface) Width() float64  { return s.c.W }
func (s canvasVectorSurface) Height() float64 { return s.c.H }

func (s canvasVectorSurface) Write(w io.Writer, format string) error {
	var writer canvas.Writer
	switch format {
	case "svg":
		writer = renderers.SVG()
	case "eps":
		writer = renderers.EPS()
	case "tex":
		writer = renderers.TeX()
	default:
		return fmt.Errorf("不支持的矢量格式 %q", format)
	}
	return s.c.Write(w, writer)
}

func (s canvasVectorSurface) Rasterize(resolution geom.Resolution) *image.RGBA {
	return rasterize(s.c, canvas.Resolution(resolution), canvas.DefaultColorSpace)
}

type canvasPDFDocument struct {
	w       io.Writer
	options pdf.Options
	doc     *pdf.PDF
}

func newCanvasPDFDocument(w io.Writer, options drawing.PDFOptions) (drawing.PDFDocument, error) {
	if w == nil {
		return nil, errors.New("PDF 输出为空")
	}
	imageEncoding := cimage.Lossless
	if options.LossyImages {
		imageEncoding = cimage.Lossy
	}
	return &canvasPDFDocument{
		w: w,
		options: pdf.Options{
			Compress:      options.Compress,
			SubsetFonts:   options.SubsetFonts,
			ImageEncoding: imageEncoding,
		},
	}, nil
}

func (d *canvasPDFDocument) AddPage(page drawing.VectorSurface, links []drawing.PageLink) error {
	surface, ok := page.(canvasVectorSurface)
	if !ok {
		return errors.New("不兼容的矢量表面")
	}
	if d.doc == nil {
		d.doc = pdf.New(d.w, surface.c.W, surface.c.H, &d.options)
	} else {
		d.doc.NewPage(surface.c.W, surface.c.H)
	}
	surface.c.RenderTo(d.doc)
	// 链接注解必须挂在当前页上：canvas 在 NewPage/Close 时才把页对象写出，所以只能在
	// RenderTo 之后、开下一页之前添加，本文件的 AddPage 恰好圈定了这个窗口。
	for _, link := range links {
		x0, y0, x1, y1 := link.Rect(surface.c.H)
		rect := canvas.Rect{X0: x0, Y0: y0, X1: x1, Y1: y1}
		if link.Target == nil {
			d.doc.AddLink(link.URI, rect)
			continue
		}
		// 名称树在 Close 时才构建，锚点可以先于目标页登记，因此向前跳和向后跳
		// 都能表达；AddAnchor 只能绑到当前页，向后跳必须走 AddAnchorToPage。
		d.doc.AddAnchorToPage(link.Target.Page, link.Target.Name, anchorRect(*link.Target))
		d.doc.AddLink("#"+link.Target.Name, rect)
	}
	return nil
}

// anchorRect 把 OFD 跳转目标换算成 canvas 用来推导目的地类型的矩形。
//
// canvas 不接受显式的目的地类型，而是按矩形的分量组合反推：全零→Fit、
// 只有 y→FitH、只有 x→FitV、某个轴相等→XYZ、否则 FitR。所以这里必须喂出恰好
// 命中对应分支的形状；OFD 坐标原点在左上角且 y 向下，PDF 在左下角且 y 向上，需要
// 按目标页高翻转。
//
// 两处已知的精度损失，源于 canvas 用矩形反推类型而非显式声明：
//
//   - DestXYZ 的 Left 为 0 时，矩形退化成「只有 y」，被反推为 FitH，落点仍是目标
//     页上同一个位置，但会顺带把页高适配到窗口，而不是保持原缩放。Left 非 0 时
//     能正确产出 XYZ。
//   - DestFitV 的 Left 为 0 时退化为整页 Fit（FitV 0 本就是左对齐，与 Fit 几乎
//     等价）。
//
// 另有 Zoom 始终丢失：canvas 把 XYZ 的缩放硬编码为 0，即保持当前缩放。OFD 未给
// Dest@Zoom 时这正是应有行为，只在文档显式指定缩放时无法满足。
func anchorRect(target drawing.LinkTarget) canvas.Rect {
	switch target.Type {
	case drawing.DestFit:
		// 全零命中整页适配分支。
		return canvas.Rect{}
	case drawing.DestFitH:
		y := target.PageHeight - target.Top
		return canvas.Rect{X0: 0, Y0: y, X1: 0, Y1: y}
	case drawing.DestFitV:
		return canvas.Rect{X0: target.Left, Y0: 0, X1: target.Left, Y1: 0}
	case drawing.DestXYZ:
		x, y := target.Left, target.PageHeight-target.Top
		return canvas.Rect{X0: x, Y0: y, X1: x, Y1: y}
	case drawing.DestFitR:
		return canvas.Rect{
			X0: target.Left, Y0: target.PageHeight - target.Bottom,
			X1: target.Right, Y1: target.PageHeight - target.Top,
		}
	}
	return canvas.Rect{}
}

func (d *canvasPDFDocument) Close() error {
	if d.doc == nil {
		return nil
	}
	return d.doc.Close()
}

// newCanvasVectorSurface 创建 canvas 矢量表面并回调绘制：调用方不直接接触
// canvas 类型，矢量画布的创建与封装集中在 canvas 适配层。
func newCanvasVectorSurface(width, height float64, draw func(drawing.DrawContext)) drawing.VectorSurface {
	c := canvas.New(width, height)
	if draw != nil {
		draw(newCanvasBackend(canvas.NewContext(c)))
	}
	return canvasVectorSurface{c: c}
}
