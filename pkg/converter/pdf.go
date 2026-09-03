package converter

import (
	"errors"
	"fmt"
	"image/color"
	"io"
	"log/slog"

	"github.com/tdewolff/canvas/renderers/pdf"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
)

func PDF(input interface{}, output io.Writer, opts ...Option) error {
	ofd, err := parser.NewOFD(input)
	if err != nil {
		return err
	}
	defer func() {
		if err = ofd.Close(); err != nil {
			slog.Error(err.Error())
		}
	}()
	if len(ofd.Documents) == 0 {
		return errors.New("没有文档")
	}

	documents := make([]*render.Document, 0, len(ofd.Documents))
	for _, document := range ofd.Documents {
		documents = append(documents, render.NewDocument(color.Transparent, document))
	}
	return PDFDocuments(documents, output, opts...)
}

// PDFDocuments 将多个已解析的 OFD 文档体按全局页码写入同一个 PDF。
func PDFDocuments(documents []*render.Document, output io.Writer, opts ...Option) error {
	if output == nil {
		return errors.New("未设置 PDF 输出参数")
	}
	conv := newConverter(opts...)
	pages := collectDocumentPages(documents)
	if len(pages) == 0 {
		return errors.New("文档没有页面")
	}
	pageStart, pageEnd, err := pageRange(len(pages), conv.page)
	if err != nil {
		return err
	}
	pages = pages[pageStart:pageEnd]

	var pdfDoc *pdf.PDF
	for _, page := range pages {
		c, err := page.document.Page(page.document.Pages[page.pageIndex])
		if err != nil {
			return fmt.Errorf("处理第%d页失败: %w", page.pageNumber, err)
		}
		if pdfDoc == nil {
			pdfDoc = pdf.New(output, c.W, c.H, nil)
		} else {
			pdfDoc.NewPage(c.W, c.H)
		}
		c.RenderTo(pdfDoc)
	}
	if pdfDoc == nil {
		return errors.New("PDF 文档创建失败")
	}
	return pdfDoc.Close()
}
