package invoice

import (
	"errors"
	"fmt"
	"strings"

	"github.com/beevik/etree"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/textdoc"
)

// extractCustomTags 沿 Document/CustomTags -> CustomTag/FileLoc 读取标引，入口与
// 路径语义见 GB/T 33190—2016 第 16 章、表 63（印刷页 80–81）。发票字段名称、
// ObjectRef/PageRef 和根级明细排列是生产者的业务约定，由 ofdrw fixture 验证。
func extractCustomTags(data []byte) (*Invoice, error) {
	ofd, err := parser.NewOFD(data)
	if err != nil {
		return nil, fmt.Errorf("读取发票自定义标引失败: %w", err)
	}
	defer ofd.Close()
	var firstErr error
	sawTags := false
	for _, doc := range ofd.Documents {
		tags, err := doc.LoadCustomTags()
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("读取自定义标引清单失败: %w", err)
			}
			continue
		}
		if tags == nil {
			continue
		}
		resolver := newTagTextResolver(doc)
		listPath := doc.Document.CustomTags.Resolve(doc.BaseLoc)
		for _, tag := range tags.CustomTags {
			sawTags = true
			location := tag.FileLoc.Resolve(listPath.Dir())
			// 部分生产者把 FileLoc 写成相对于文档根目录的路径。
			if !doc.FileCache.Has(location.String()) {
				location = tag.FileLoc.Resolve(doc.BaseLoc)
			}
			raw, err := doc.FileCache.ReadLimit(location.String(), maxAttachmentBytes)
			var invoice *Invoice
			if err == nil {
				invoice, err = parseCustomTag(raw, resolver)
			}
			if err == nil {
				invoice.Attachment = location.String()
				return invoice, nil
			}
			if !errors.Is(err, ErrNotInvoice) && firstErr == nil {
				firstErr = fmt.Errorf("解析发票自定义标引 %s 失败: %w", location, err)
			}
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	if sawTags {
		return nil, ErrNotInvoice
	}
	return nil, ErrNoAttachment
}

func parseCustomTag(raw []byte, resolver *tagTextResolver) (*Invoice, error) {
	tree := etree.NewDocument()
	if err := tree.ReadFromBytes(raw); err != nil {
		return nil, err
	}
	root := tree.Root()
	if root == nil {
		return nil, ErrNotInvoice
	}
	// 先判断业务字段，避免把任意自定义标引当成发票。
	if root.SelectElement("InvoiceCode") == nil && root.SelectElement("InvoiceNo") == nil &&
		root.SelectElement("TaxInclusiveTotalAmount") == nil {
		return nil, ErrNotInvoice
	}
	if err := resolver.resolve(root); err != nil {
		return nil, err
	}
	wrapTagGoods(root)
	normalized, err := tree.WriteToBytes()
	if err != nil {
		return nil, err
	}
	return parseAttachment(normalized)
}

// wrapTagGoods 将根级按 Item 分组的明细放入附件解析器使用的 GoodsInfos 容器。
// 一个 Item 下的多个 ObjectRef 是同一项目的换行文字，不能拆成多行明细。
func wrapTagGoods(root *etree.Element) {
	if root.SelectElement("GoodsInfos") != nil {
		return
	}
	var goods, item *etree.Element
	for _, field := range root.ChildElements() {
		switch field.Tag {
		case "Item":
			if goods == nil {
				goods = root.CreateElement("GoodsInfos")
			}
			item = goods.CreateElement("GoodsItem")
		case "Specification", "MeasurementDimension", "Quantity", "Price", "Amount", "TaxScheme", "TaxRate", "TaxAmount":
			if item == nil {
				continue
			}
		default:
			continue
		}
		root.RemoveChild(field)
		item.AddChild(field)
	}
}

type tagTextResolver struct {
	doc   *parser.Document
	pages map[models.StID]*parser.Page
	texts map[models.StID]map[models.StID]string
}

func newTagTextResolver(doc *parser.Document) *tagTextResolver {
	r := &tagTextResolver{
		doc: doc, pages: make(map[models.StID]*parser.Page),
		texts: make(map[models.StID]map[models.StID]string),
	}
	for _, page := range doc.Pages {
		if page != nil {
			r.pages[page.ID] = page
		}
	}
	return r
}

func (r *tagTextResolver) resolve(element *etree.Element) error {
	var value strings.Builder
	hasRefs := false
	for _, child := range element.ChildElements() {
		if child.Tag != "ObjectRef" {
			if err := r.resolve(child); err != nil {
				return err
			}
			continue
		}
		text, err := r.text(child.SelectAttrValue("PageRef", ""), child.Text())
		if err != nil {
			return fmt.Errorf("字段 %s: %w", element.Tag, err)
		}
		value.WriteString(text)
		hasRefs = true
	}
	if hasRefs {
		for _, child := range element.ChildElements() {
			element.RemoveChild(child)
		}
		element.SetText(strings.TrimSpace(value.String()))
	}
	return nil
}

func (r *tagTextResolver) text(pageRef, objectRef string) (string, error) {
	var pageID, objectID models.StID
	if err := pageID.UnmarshalText([]byte(strings.TrimSpace(pageRef))); err != nil {
		return "", fmt.Errorf("无效 PageRef %q: %w", pageRef, err)
	}
	if err := objectID.UnmarshalText([]byte(strings.TrimSpace(objectRef))); err != nil {
		return "", fmt.Errorf("无效 ObjectRef %q: %w", objectRef, err)
	}
	texts, ok := r.texts[pageID]
	if !ok {
		page := r.pages[pageID]
		if page == nil {
			return "", fmt.Errorf("PageRef %s 指向的页面不存在", pageRef)
		}
		texts = make(map[models.StID]string)
		if err := page.WithPageContent(func(content *models.PageContent) error {
			for _, template := range content.Template {
				if tpl := r.doc.GetTemplate(models.StID(template.TemplateID)); tpl != nil {
					indexTagText(tpl.Content, texts)
				}
			}
			indexTagText(content.Content, texts)
			return nil
		}); err != nil {
			return "", fmt.Errorf("读取 PageRef %s 失败: %w", pageRef, err)
		}
		r.texts[pageID] = texts
	}
	text, ok := texts[objectID]
	if !ok {
		return "", fmt.Errorf("PageRef %s 中找不到 ObjectRef %s 的文字对象", pageRef, objectRef)
	}
	return text, nil
}

func indexTagText(content *models.Content, texts map[models.StID]string) {
	if content == nil {
		return
	}
	for _, layer := range content.Layer {
		if layer != nil {
			indexTagTextItems(layer.Items, texts)
		}
	}
}

func indexTagTextItems(items []models.PageItem, texts map[models.StID]string) {
	for _, item := range items {
		switch item.Kind {
		case models.PageItemText:
			var text strings.Builder
			for _, code := range item.Text.TextCode {
				text.WriteString(code.Value)
			}
			texts[item.Text.ID] = textdoc.EnsureUTF8(text.String())
		case models.PageItemBlock:
			indexTagTextItems(item.Block.Items, texts)
		}
	}
}
