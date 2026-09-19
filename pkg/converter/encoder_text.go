package converter

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
)

func init() {
	Register(&textEncoder{})
	Register(&markdownEncoder{})
}

type textEncoder struct{}

func (e *textEncoder) Name() string         { return "text" }
func (e *textEncoder) Kind() Kind           { return KindDocument }
func (e *textEncoder) Extensions() []string { return []string{".txt"} }
func (e *textEncoder) MIME() string         { return "text/plain" }
func (e *textEncoder) Encode(input any, output io.Writer, conv *Converter) error {
	return encodeOFD(input, output, conv, e.textFromDocuments)
}

func (e *textEncoder) textFromDocuments(documents []*render.Document, output io.Writer, conv *Converter) error {
	return textDocuments(renderDocsToParserDocs(documents), output, conv)
}

type markdownEncoder struct{}

func (e *markdownEncoder) Name() string         { return "markdown" }
func (e *markdownEncoder) Kind() Kind           { return KindDocument }
func (e *markdownEncoder) Extensions() []string { return []string{".md", ".markdown"} }
func (e *markdownEncoder) MIME() string         { return "text/markdown" }
func (e *markdownEncoder) Encode(input any, output io.Writer, conv *Converter) error {
	return encodeOFD(input, output, conv, e.markdownFromDocuments)
}

func (e *markdownEncoder) markdownFromDocuments(documents []*render.Document, output io.Writer, conv *Converter) error {
	return markdownDocuments(renderDocsToParserDocs(documents), output, conv)
}

// renderDocsToParserDocs 从 render.Document 中提取 parser.Document。
func renderDocsToParserDocs(documents []*render.Document) []*parser.Document {
	docs := make([]*parser.Document, len(documents))
	for i, d := range documents {
		docs[i] = d.Document
	}
	return docs
}

const maxTextCompositeDepth = 32

// Text 提取 input 中 OFD 文档的文字并写入 output。
// 不会保留字体、颜色和布局信息；不同文字对象按行输出，不同页面使用分页符分隔。
func Text(input any, output io.Writer, opts ...Option) error {
	return Encode("text", input, output, opts...)
}

// TextDocument 提取已解析 OFD 文档中的文字并写入 output。
func TextDocument(doc *parser.Document, output io.Writer, opts ...Option) error {
	return TextDocuments([]*parser.Document{doc}, output, opts...)
}

// TextDocuments 按全局页码提取多个已解析 OFD 文档体中的文字并写入 output。
func TextDocuments(documents []*parser.Document, output io.Writer, opts ...Option) error {
	if output == nil {
		return errors.New("未设置文本输出参数")
	}
	return textDocuments(documents, output, newConverter(opts...))
}

func textDocuments(documents []*parser.Document, output io.Writer, conv *Converter) error {
	if output == nil {
		return errors.New("未设置文本输出参数")
	}
	pages, err := collectPageTexts(documents, conv.page)
	if err != nil {
		return err
	}
	text := strings.Join(pages, "\n\f\n")
	if text != "" {
		text += "\n"
	}
	_, err = io.WriteString(output, text)
	return err
}

// Markdown 提取 input 中 OFD 文档的文字并写入 Markdown 文档。
// 每个页面输出为二级标题；文字对象按行输出，并转义 Markdown 特殊字符。
func Markdown(input any, output io.Writer, opts ...Option) error {
	return Encode("markdown", input, output, opts...)
}

// MarkdownDocument 提取已解析 OFD 文档中的文字并写入 Markdown 文档。
func MarkdownDocument(doc *parser.Document, output io.Writer, opts ...Option) error {
	return MarkdownDocuments([]*parser.Document{doc}, output, opts...)
}

// MarkdownDocuments 按全局页码提取多个已解析 OFD 文档体中的文字并写入 Markdown 文档。
func MarkdownDocuments(documents []*parser.Document, output io.Writer, opts ...Option) error {
	if output == nil {
		return errors.New("未设置Markdown输出参数")
	}
	return markdownDocuments(documents, output, newConverter(opts...))
}

func markdownDocuments(documents []*parser.Document, output io.Writer, conv *Converter) error {
	if output == nil {
		return errors.New("未设置Markdown输出参数")
	}
	pages, err := collectPageTexts(documents, conv.page)
	if err != nil {
		return err
	}

	var markdown strings.Builder
	markdown.WriteString("# OFD 文档\n")
	for index, page := range pages {
		pageNumber := index + 1
		if conv.page > 0 {
			pageNumber = conv.page
		}
		fmt.Fprintf(&markdown, "\n## 第 %d 页\n\n", pageNumber)
		if page == "" {
			continue
		}
		for line := range strings.SplitSeq(page, "\n") {
			markdown.WriteString(escapeMarkdownLine(line))
			markdown.WriteByte('\n')
		}
	}
	_, err = io.WriteString(output, markdown.String())
	return err
}

func collectPageTexts(documents []*parser.Document, page int) ([]string, error) {
	pageCount := 0
	for _, doc := range documents {
		if doc != nil {
			for _, page := range doc.Pages {
				if page != nil {
					pageCount++
				}
			}
		}
	}
	if pageCount == 0 {
		return nil, errors.New("文档没有页面")
	}
	pageStart, pageEnd, err := pageRange(pageCount, page)
	if err != nil {
		return nil, err
	}

	pages := make([]string, 0, pageEnd-pageStart)
	globalPage := 0
	for _, doc := range documents {
		if doc == nil {
			continue
		}
		for _, page := range doc.Pages {
			if page == nil {
				continue
			}
			if globalPage >= pageStart && globalPage < pageEnd {
				pages = append(pages, extractPageText(doc, page))
			}
			globalPage++
			if globalPage >= pageEnd {
				break
			}
		}
		if globalPage >= pageEnd {
			break
		}
	}
	return pages, nil
}

func escapeMarkdownLine(line string) string {
	line = strings.NewReplacer(
		`\`, `\\`,
		"`", "\\`",
		"*", "\\*",
		"_", "\\_",
		"[", "\\[",
		"]", "\\]",
		"<", "\\<",
		">", "\\>",
		"|", "\\|",
		"~", "\\~",
	).Replace(line)
	if len(line) > 0 {
		switch line[0] {
		case '#', '-', '+', '=', '>':
			line = "\\" + line
		}
	}
	if index := strings.Index(line, ". "); index > 0 && allDigits(line[:index]) {
		line = line[:index] + `\.` + line[index+1:]
	}
	return line
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

func extractPageText(doc *parser.Document, page *parser.Page) string {
	if page == nil {
		return ""
	}
	lease, err := page.AcquireLease()
	if err != nil {
		return ""
	}
	defer lease.Release()
	lines := make([]string, 0)
	content := lease.Content()
	if content == nil {
		return ""
	}
	for _, template := range content.Template {
		if content := doc.GetTemplate(models.StID(template.TemplateID)); content != nil {
			appendPageContentText(doc, content.Content, &lines, 0)
		}
	}
	appendPageContentText(doc, content.Content, &lines, 0)
	if annot := doc.GetAnnotation(page.ID); annot != nil {
		for _, item := range annot.Annots {
			if item == nil || !item.Visible.Value(true) || item.Appearance == nil {
				continue
			}
			appendTextItems(doc, item.Appearance.Items, &lines, 0)
		}
	}
	return strings.Join(lines, "\n")
}

func appendPageContentText(doc *parser.Document, content *models.Content, lines *[]string, depth int) {
	if content == nil {
		return
	}
	// 与渲染顺序保持一致：背景层先于其他图层处理。
	for _, layer := range content.Layer {
		if layer != nil && layer.Type == "Background" {
			appendTextItems(doc, layer.Items, lines, depth)
		}
	}
	for _, layer := range content.Layer {
		if layer != nil && layer.Type != "Background" {
			appendTextItems(doc, layer.Items, lines, depth)
		}
	}
}

func appendTextItems(doc *parser.Document, items []models.PageItem, lines *[]string, depth int) {
	if depth > maxTextCompositeDepth {
		return
	}
	for _, item := range items {
		switch item.Kind {
		case models.PageItemText:
			if !item.Text.VisibleValue() || textFillDisabled(item.Text) {
				continue
			}
			var text strings.Builder
			for _, code := range item.Text.TextCode {
				text.WriteString(code.Value)
			}
			if text.Len() > 0 {
				*lines = append(*lines, text.String())
			}
		case models.PageItemBlock:
			appendTextItems(doc, item.Block.Items, lines, depth)
		case models.PageItemComposite:
			if !item.Composite.VisibleValue() {
				continue
			}
			unit := doc.GetCompositeUnit(models.StID(item.Composite.ResourceID))
			if unit != nil {
				appendTextItems(doc, unit.Content.Items, lines, depth+1)
			}
		}
	}
}

func textFillDisabled(object models.TextObject) bool {
	return !object.Fill.Value(true)
}
