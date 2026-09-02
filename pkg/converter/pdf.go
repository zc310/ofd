package converter

import (
	"errors"
	"fmt"
	"image/color"
	"io"
	"log/slog"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/pdf"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
)

func PDF(input interface{}, output io.Writer, opts ...Option) error {
	conv := newConverter(opts...)
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

	doc := render.NewDocument(color.Transparent, ofd.Documents[0])
	if len(doc.Pages) == 0 {
		return errors.New("文档没有页面")
	}
	pageStart, pageEnd := 0, len(doc.Pages)
	if conv.page > 0 {
		if conv.page > len(doc.Pages) {
			return nil
		}
		pageStart = conv.page - 1
		pageEnd = conv.page
	}
	var pdfDoc *pdf.PDF
	var c *canvas.Canvas
	for i := pageStart; i < pageEnd; i++ {
		page := doc.Pages[i]
		c, err = doc.Page(page)
		if err != nil {
			return fmt.Errorf("处理第%d页失败: %w", i+1, err)
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
