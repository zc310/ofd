package canvas

import (
	"github.com/zc310/ofd/internal/cache"
	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

// 本文件负责从 OFD 文本对象中收集“Unicode 码位 → 字形 ID”映射，并登记到字体
// 上下文。部分内嵌子集字体缺少 Unicode cmap，只能用 CGTransform.Glyphs 定位
// 字形；把文档自身的权威映射注入字体后，渲染就能按原始文本走正常文字接口，
// 在 PDF 等输出中保留可复制、可搜索的文字。

// 内容由解析器加载后保持只读；重新加载时 Content 指针改变。缓存只保留少量
// 最近内容，避免字形扫描缓存随文档页数增长并长期持有被淘汰的页面。
const glyphCollectionCapacity = 8

type glyphCollection map[models.StRefID]map[rune]uint16

// RegisterPageGlyphs 收集页面（含模板与注释）的字形映射。命中扫描缓存后仍登记
// 映射，保证其他页面更新同一字符的字形后，重绘本页能恢复对应映射。
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
		mergeGlyphCollection(collected, p.contentGlyphs(templateContent.Content))
	}
	mergeGlyphCollection(collected, p.contentGlyphs(content.Content))
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

func (p *Fonts) contentGlyphs(content *models.Content) glyphCollection {
	if content == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.isClosed() {
		return nil
	}
	if p.glyphCollections == nil {
		p.glyphCollections = cache.NewLRU[*models.Content, glyphCollection](glyphCollectionCapacity, nil)
	}
	if collected, ok := p.glyphCollections.Get(content); ok {
		return collected
	}
	collected := make(glyphCollection)
	collectContentGlyphs(content, collected)
	p.glyphCollections.Add(content, collected)
	return collected
}

// 缓存里的映射只读，合并时复制到本次登记集合，保持模板、页面、注解的覆盖顺序。
func mergeGlyphCollection(target, source glyphCollection) {
	for id, pairs := range source {
		mapping := target[id]
		if mapping == nil {
			mapping = make(map[rune]uint16, len(pairs))
			target[id] = mapping
		}
		for r, glyph := range pairs {
			mapping[r] = glyph
		}
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
			if item.Text != nil {
				collectObjectGlyphs(*item.Text, out)
			}
		case models.PageItemBlock:
			if item.Block != nil {
				collectItemGlyphs(item.Block.Items, out)
			}
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
	transforms := models.ExpandCGTransformPositions(object.TextCode, object.CGTransform)
	byPosition := make(map[int]models.CTCGTransform, len(object.CGTransform))
	for _, transform := range transforms {
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
			// 为每个 GID 单独登记私有码位，CFF 绘制时绕过 Unicode 整形与字形替换。
			for _, id := range ids {
				if id < 0 || id > 0xffff {
					continue
				}
				if pairs == nil {
					pairs = make(map[rune]uint16)
					out[object.Font] = pairs
				}
				gid := uint16(id)
				pairs[fontrepair.GlyphRune(gid)] = gid
			}
			codeCount := transform.CodeCount
			if codeCount <= 0 {
				codeCount = 1
			}
			// 一对一变换（码位数与字形数相等，绝大多数 OFD 子集字体如此）：
			// 逐码位与字形按序配对登记。缺少 Unicode cmap 的子集字体借此获得
			// 原始文本到字形的映射，渲染即可走正常文字接口而非轮廓路径。
			if codeCount == len(ids) && i+codeCount <= len(runes) {
				for k := 0; k < codeCount; k++ {
					glyph := ids[k]
					if glyph < 0 || glyph > 0xffff {
						continue
					}
					if r := runes[i+k]; renderableRune(r) {
						if pairs == nil {
							pairs = make(map[rune]uint16)
							out[object.Font] = pairs
						}
						pairs[r] = uint16(glyph)
					}
				}
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
