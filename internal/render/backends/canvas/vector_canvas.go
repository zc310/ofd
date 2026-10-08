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
		// 都能表达；AddAnchor 只能绑到当前页，向后跳必须走带页序的接口。
		d.doc.AddDestToPage(link.Target.Page, link.Target.PageHeight, link.Target.Name,
			destSpec(*link.Target))
		d.doc.AddLink("#"+link.Target.Name, rect)
	}
	return nil
}

// destSpec 把 OFD 跳转目标原样映射为 canvas 的目的地描述。
//
// canvas 的 Dest 坐标同样以左上角为原点、单位 mm，由 AddDestToPage 按目标页高翻转到
// PDF 的左下角原点，因此这里不做任何翻转。目的地类型逐类对应，不经过「用矩形形状
// 反推类型」的中间层——那会把 Left 为 0 的 XYZ 误判成 FitH，并且无法表达 Zoom。
func destSpec(target drawing.LinkTarget) pdf.Dest {
	switch target.Type {
	case drawing.DestFitH:
		return pdf.Dest{Kind: pdf.DestFitH, Y: target.Top}
	case drawing.DestFitV:
		return pdf.Dest{Kind: pdf.DestFitV, X: target.Left}
	case drawing.DestXYZ:
		return pdf.Dest{Kind: pdf.DestXYZ, X: target.Left, Y: target.Top, Zoom: target.Zoom}
	case drawing.DestFitR:
		return pdf.Dest{
			Kind: pdf.DestFitR,
			X0:   target.Left, Y0: target.Bottom,
			X1: target.Right, Y1: target.Top,
		}
	}
	return pdf.Dest{Kind: pdf.DestFit}
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
