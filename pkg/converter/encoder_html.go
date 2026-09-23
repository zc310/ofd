package converter

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"strconv"
	"strings"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
)

func init() { Register(&htmlEncoder{}) }

type htmlEncoder struct{}

func (e *htmlEncoder) Name() string         { return "html" }
func (e *htmlEncoder) Kind() Kind           { return KindDocument }
func (e *htmlEncoder) Extensions() []string { return []string{".html", ".htm"} }
func (e *htmlEncoder) MIME() string         { return "text/html" }
func (e *htmlEncoder) Encode(input any, output io.Writer, conv *Converter) error {
	return encodeOFD(input, output, conv, e.htmlFromDocuments)
}

func (e *htmlEncoder) htmlFromDocuments(documents []*render.Document, output io.Writer, conv *Converter) error {
	title := "OFD 文档"
	if conv.docTitle != "" {
		title = conv.docTitle
	}
	return htmlDocuments(documents, title, output, conv)
}

// HTML 将 input 中的 OFD 文档转换为单个 HTML 文件。
// 默认每页使用内嵌 PNG 图片，也可以通过 HTMLJPG 或 HTMLSVG 选择 JPG 或 SVG。
func HTML(input any, output io.Writer, opts ...Option) error {
	return Encode("html", input, output, opts...)
}

// HTMLDocuments 将多个已解析的 OFD 文档体按全局页码写入单个 HTML 文件。
func HTMLDocuments(documents []*render.Document, output io.Writer, opts ...Option) error {
	return EncodeDocuments("html", documents, output, opts...)
}

func htmlDocuments(documents []*render.Document, title string, output io.Writer, conv *Converter) error {
	if output == nil {
		return errors.New("未设置 HTML 输出参数")
	}
	pages := collectDocumentPages(documents)
	if len(pages) == 0 {
		return errors.New("文档没有页面")
	}
	if conv.htmlImageFormat != "png" && conv.htmlImageFormat != "jpg" && conv.htmlImageFormat != "svg" {
		return fmt.Errorf("不支持的 HTML 页面格式: %s", conv.htmlImageFormat)
	}
	start, end, err := pageRange(len(pages), conv.page)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(output, htmlHeader(title)); err != nil {
		return fmt.Errorf("写入 HTML 头部失败: %w", err)
	}
	err = walkDocumentPages(documents, start, end, func(pageInfo documentPage) error {
		page, err := pageInfo.document.Page(pageInfo.document.Pages[pageInfo.pageIndex])
		if err != nil {
			return fmt.Errorf("处理第%d页失败: %w", pageInfo.pageNumber, err)
		}
		var data bytes.Buffer
		if conv.htmlImageFormat == "svg" {
			if err := page.Write(&data, "svg"); err != nil {
				return fmt.Errorf("编码第%d页 SVG 失败: %w", pageInfo.pageNumber, err)
			}
			dataBytes := stripSVGXMLDeclaration(data.Bytes())
			data.Reset()
			if _, err := data.Write(dataBytes); err != nil {
				return fmt.Errorf("准备第%d页 SVG 失败: %w", pageInfo.pageNumber, err)
			}
		} else if conv.htmlImageFormat == "jpg" {
			if err := encodeHTMLJPG(&data, page, geom.Resolution(conv.dpi)); err != nil {
				return fmt.Errorf("编码第%d页 JPG 失败: %w", pageInfo.pageNumber, err)
			}
		} else {
			if err := encodeHTMLPNG(&data, page, geom.Resolution(conv.dpi)); err != nil {
				return fmt.Errorf("编码第%d页 PNG 失败: %w", pageInfo.pageNumber, err)
			}
		}
		return writeHTMLPage(output, pageInfo.pageNumber, page.Width(), page.Height(), data.Bytes(), conv.htmlImageFormat)
	})
	if err != nil {
		return err
	}
	if _, err := io.WriteString(output, htmlFooter()); err != nil {
		return fmt.Errorf("写入 HTML 尾部失败: %w", err)
	}
	return nil
}

func encodeHTMLPNG(output io.Writer, page render.VectorSurface, dpi geom.Resolution) error {
	return png.Encode(output, page.Rasterize(dpi))
}

func encodeHTMLJPG(output io.Writer, page render.VectorSurface, dpi geom.Resolution) error {
	return jpeg.Encode(output, opaqueImage(page.Rasterize(dpi), color.White), &jpeg.Options{Quality: 90})
}

func opaqueImage(source image.Image, background color.Color) image.Image {
	bounds := source.Bounds()
	result := image.NewRGBA(bounds)
	draw.Draw(result, bounds, &image.Uniform{C: background}, image.Point{}, draw.Src)
	draw.Draw(result, bounds, source, bounds.Min, draw.Over)
	return result
}

func writeHTMLPage(output io.Writer, number int, width, height float64, data []byte, format string) error {
	if format == "svg" {
		_, err := fmt.Fprintf(output, "<section class=\"page\" aria-label=\"第%d页\" style=\"width:%smm;height:%smm\">%s</section>\n", number, htmlNumber(width), htmlNumber(height), data)
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	mimeType := "image/png"
	if format == "jpg" {
		mimeType = "image/jpeg"
	}
	_, err := fmt.Fprintf(output, "<section class=\"page\" aria-label=\"第%d页\" style=\"width:%smm;height:%smm\"><img src=\"data:%s;base64,%s\" alt=\"第%d页\"></section>\n", number, htmlNumber(width), htmlNumber(height), mimeType, encoded, number)
	return err
}

func htmlHeader(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "OFD 文档"
	}
	return "<!doctype html>\n<html lang=\"zh-CN\">\n<head>\n<meta charset=\"utf-8\">\n<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n<title>" + html.EscapeString(title) + "</title>\n<style>html,body{margin:0;padding:0;background:#eef1f5}.document{padding:24px}.page{box-sizing:border-box;margin:0 auto 24px;background:#fff;overflow:hidden;box-shadow:0 2px 12px #17203326}.page img,.page svg{display:block;width:100%;height:100%}@media print{html,body{background:#fff}.document{padding:0}.page{margin:0;box-shadow:none;break-after:page;page-break-after:always}.page:last-child{break-after:auto;page-break-after:auto}}</style>\n</head>\n<body><main class=\"document\">\n"
}

func htmlFooter() string {
	return "</main></body>\n</html>\n"
}

func htmlNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', 4, 64)
}

func stripSVGXMLDeclaration(data []byte) []byte {
	data = bytes.TrimLeft(data, " \t\r\n")
	if !bytes.HasPrefix(data, []byte("<?xml")) {
		return data
	}
	if end := bytes.Index(data, []byte("?>")); end >= 0 {
		return bytes.TrimLeft(data[end+2:], " \t\r\n")
	}
	return data
}

// documentTitle 返回 OFD 第一个文档体的非空 DocInfo.Title；没有标题时返回默认标题。
func documentTitle(ofd *parser.OFD) string {
	if ofd != nil {
		for _, body := range ofd.DocBodies {
			if body.DocInfo.Title != nil {
				if title := strings.TrimSpace(*body.DocInfo.Title); title != "" {
					return title
				}
			}
		}
	}
	return "OFD 文档"
}
