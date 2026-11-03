//go:build pdfcpu

package canvas

import (
	"context"
	"errors"
	"io"

	"github.com/zc310/ofd/internal/pdfcpuwriter"
	"github.com/zc310/ofd/internal/render/drawing"
)

// pdfcpuPDFDocument 将已有矢量表面回放到实验写入器；仅在 pdfcpu 标签下注册。
type pdfcpuPDFDocument struct {
	w        io.Writer
	options  drawing.PDFOptions
	doc      *pdfcpuwriter.Document
	closed   bool
	err      error
	closeErr error
}

var (
	errPDFCPUOutputNil = errors.New("PDF 输出为空")
	errPDFCPUSurface   = errors.New("不兼容的矢量表面")
	errPDFCPUNoPages   = errors.New("尚未写入任何页面，无法添加大纲")
)

func newPDFDocument(w io.Writer, options drawing.PDFOptions) (drawing.PDFDocument, error) {
	if w == nil {
		return nil, errPDFCPUOutputNil
	}
	return &pdfcpuPDFDocument{w: w, options: options}, nil
}

func (d *pdfcpuPDFDocument) AddPage(page drawing.VectorSurface, links []drawing.PageLink) error {
	if d.closed {
		return errPDFDocumentClosed
	}
	if d.err != nil {
		return d.err
	}
	surface, ok := page.(canvasVectorSurface)
	if !ok {
		return errPDFCPUSurface
	}
	// PDFDocument 工厂没有 context 参数；转换入口仍在每页前后检查取消。
	if d.doc == nil {
		d.doc, d.err = pdfcpuwriter.NewWithOptions(context.Background(), surface.Width(), surface.Height(), pdfcpuwriter.Options{
			LossyImages: d.options.LossyImages,
			Producer:    d.options.Producer,
		})
	} else {
		d.err = d.doc.NextPage(surface.Width(), surface.Height())
	}
	if d.err == nil {
		surface.c.RenderTo(d.doc)
		d.err = d.doc.Err()
	}
	if d.err == nil {
		d.err = d.doc.AddLinks(links)
	}
	return d.err
}

func (d *pdfcpuPDFDocument) AddOutlines(outlines []drawing.PageOutline) error {
	if d.closed {
		return errPDFDocumentClosed
	}
	if d.err != nil {
		return d.err
	}
	if d.doc == nil {
		return errPDFCPUNoPages
	}
	d.err = d.doc.AddOutlines(outlines)
	return d.err
}

func (d *pdfcpuPDFDocument) Close() error {
	if !d.closed {
		d.closed = true
		d.closeErr = d.err
		if d.closeErr == nil && d.doc != nil {
			d.closeErr = d.doc.Write(d.w)
		}
	}
	return d.closeErr
}
