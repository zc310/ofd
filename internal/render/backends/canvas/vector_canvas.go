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

func (d *canvasPDFDocument) AddPage(page drawing.VectorSurface) error {
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
	return nil
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
