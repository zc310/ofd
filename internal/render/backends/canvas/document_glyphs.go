package canvas

import (
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

// 本文件负责从 OFD 文本对象中收集“Unicode 码位 → 字形 ID”映射，并登记到字体
// 上下文。部分内嵌子集字体缺少 Unicode cmap，只能用 CGTransform.Glyphs 定位
// 字形；把文档自身的权威映射注入字体后，渲染就能按原始文本走正常文字接口，
// 在 PDF 等输出中保留可复制、可搜索的文字。

// registerPageGlyphs 扫描页面（含模板与注释）的文字对象并登记字形映射。
func (p *Fonts) RegisterPageGlyphs(doc *parser.Document, page *parser.Page, content *models.PageContent) {
	if p == nil || doc == nil || content == nil {
		return
	}
	collected := make(map[models.StRefID]map[rune]uint16)
	for _, template := range content.Template {
		templateContent, err := doc.LoadTemplate(models.StID(template.TemplateID))
		if err != nil || templateContent == nil {
			continue
		}
		collectContentGlyphs(templateContent.Content, collected)
	}
	collectContentGlyphs(content.Content, collected)
	if page != nil {
		if annot := doc.GetAnnotation(page.ID); annot != nil {
			for _, item := range annot.Annots {
				if item == nil || !item.Visible.Value(true) || item.Appearance == nil {
					continue
				}
				collectItemGlyphs(item.Appearance.Items, collected)
			}
		}
	}
	for id, pairs := range collected {
		p.RegisterGlyphs(id, pairs)
	}
}

func collectContentGlyphs(content *models.Content, out map[models.StRefID]map[rune]uint16) {
	if content == nil {
		return
	}
	for _, layer := range content.Layer {
		if layer == nil {
			continue
		}
		collectItemGlyphs(layer.Items, out)
	}
}

func collectItemGlyphs(items []models.PageItem, out map[models.StRefID]map[rune]uint16) {
	for _, item := range items {
		switch item.Kind {
		case models.PageItemText:
			collectObjectGlyphs(*item.Text, out)
		case models.PageItemBlock:
			collectItemGlyphs(item.Block.Items, out)
		case models.PageItemComposite:
			// 复合图元内容在渲染时展开，这里不深入以避免重复加载。
		}
	}
}

// collectObjectGlyphs 把一个文字对象的 CGTransform 与 TextCode 配对，得到
// 单码位对单字形时的 Unicode→字形映射。
func collectObjectGlyphs(object models.TextObject, out map[models.StRefID]map[rune]uint16) {
	if len(object.CGTransform) == 0 || !object.VisibleValue() {
		return
	}
	byPosition := make(map[int]models.CTCGTransform, len(object.CGTransform))
	for _, transform := range object.CGTransform {
		byPosition[transform.CodePosition] = transform
	}
	pairs := out[object.Font]
	codePosition := 0
	for _, code := range object.TextCode {
		runes := []rune(code.Value)
		for i := 0; i < len(runes); {
			transform, ok := byPosition[codePosition+i]
			if !ok {
				i++
				continue
			}
			ids := transform.Glyphs
			if transform.GlyphCount > 0 && transform.GlyphCount < len(ids) {
				ids = ids[:transform.GlyphCount]
			}
			if transform.CodeCount == 1 && len(ids) == 1 && ids[0] >= 0 && ids[0] <= 0xffff {
				if r := runes[i]; renderableRune(r) {
					if pairs == nil {
						pairs = make(map[rune]uint16)
						out[object.Font] = pairs
					}
					pairs[r] = uint16(ids[0])
				}
			}
			codeCount := transform.CodeCount
			if codeCount <= 0 {
				codeCount = 1
			}
			remaining := len(runes) - i
			if codeCount >= remaining {
				break
			}
			i += codeCount
		}
		codePosition += len(runes)
	}
}

func renderableRune(r rune) bool {
	return r >= 0x20 && r != 0x7f && !(r >= 0x80 && r <= 0x9f) && r != '\uFFFD'
}
