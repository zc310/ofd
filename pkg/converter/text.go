package converter

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

const maxTextCompositeDepth = 32

// Text 提取 OFD 文档中的文字并写入 output。
// 不会保留字体、颜色和布局信息；不同文字对象按行输出，不同页面使用分页符分隔。
func Text(input interface{}, output io.Writer, opts ...Option) error {
	if output == nil {
		return errors.New("未设置文本输出参数")
	}
	ofd, err := parser.NewOFD(input)
	if err != nil {
		return fmt.Errorf("解析OFD失败: %w", err)
	}
	defer ofd.Close()
	if len(ofd.Documents) == 0 {
		return errors.New("没有文档")
	}
	return TextDocument(ofd.Documents[0], output, opts...)
}

// TextDocument 提取已解析 OFD 文档中的文字并写入 output。
func TextDocument(doc *parser.Document, output io.Writer, opts ...Option) error {
	if doc == nil || len(doc.Pages) == 0 {
		return errors.New("文档没有页面")
	}
	if output == nil {
		return errors.New("未设置文本输出参数")
	}
	conv := newConverter(opts...)
	pageStart, pageEnd := 0, len(doc.Pages)
	if conv.page > 0 {
		if conv.page > len(doc.Pages) {
			return nil
		}
		pageStart = conv.page - 1
		pageEnd = conv.page
	}

	pages := make([]string, 0, pageEnd-pageStart)
	for _, page := range doc.Pages[pageStart:pageEnd] {
		pages = append(pages, extractPageText(doc, page))
	}
	text := strings.Join(pages, "\n\f\n")
	if text != "" {
		text += "\n"
	}
	_, err := io.WriteString(output, text)
	return err
}

func extractPageText(doc *parser.Document, page *parser.Page) string {
	if page == nil {
		return ""
	}
	lines := make([]string, 0)
	for _, template := range page.Template {
		if content := doc.Templates[models.StID(template.TemplateID)]; content != nil {
			appendPageContentText(doc, content.Content, &lines, 0)
		}
	}
	appendPageContentText(doc, page.Content, &lines, 0)
	if annot := doc.Annotations[page.ID]; annot != nil {
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
			unit := doc.CompositeUnits[models.StID(item.Composite.ResourceID)]
			if unit != nil {
				appendTextItems(doc, unit.Content.Items, lines, depth+1)
			}
		}
	}
}

func textFillDisabled(object models.TextObject) bool {
	return strings.EqualFold(strings.TrimSpace(object.Fill), "false")
}
