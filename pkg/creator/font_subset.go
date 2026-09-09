package creator

import (
	"fmt"
	"strings"

	"github.com/tdewolff/font"
)

// subsetEmbeddedFonts 只保留文字对象实际引用的字形。由于子集化会重新分配字形编号，
// 因此还需要同步更新显式 CGTransforms 中的字形编号。
func subsetEmbeddedFonts(document *Document) error {
	if document == nil || len(document.Fonts) == 0 {
		return nil
	}
	cloneDocumentFontSubsetInputs(document)
	used := make(map[string]*fontUsage, len(document.Fonts))
	for _, resource := range document.Fonts {
		if len(resource.Data) > 0 {
			used[strings.TrimSpace(resource.Name)] = &fontUsage{}
		}
	}
	collector := fontUsageCollector{used: used, patterns: make(map[*Pattern]bool)}
	for _, drawParam := range document.DrawParams {
		collector.collectColor(drawParam.FillColor)
		collector.collectColor(drawParam.StrokeColor)
	}
	for _, template := range document.Templates {
		collector.collectItems(template.Items)
		collector.collectLayers(template.Layers)
	}
	for _, page := range document.Pages {
		collector.collectItems(page.Items)
		collector.collectLayers(page.Layers)
	}
	for _, page := range document.Annotations {
		for _, annotation := range page.Items {
			collector.collectItems(annotation.Items)
		}
	}
	for _, composite := range document.Composites {
		collector.collectItems(composite.Items)
	}
	for index := range document.Fonts {
		resource := &document.Fonts[index]
		usage := used[strings.TrimSpace(resource.Name)]
		if usage == nil || (len(usage.glyphs) == 0 && len(usage.runes) == 0) {
			continue
		}
		if err := subsetFont(resource, usage); err != nil {
			return fmt.Errorf("字体资源 %q 子集化失败: %w", resource.Name, err)
		}
	}
	for index := range document.Templates {
		document.Templates[index].Items = rewriteItemsFontGlyphs(document.Templates[index].Items, used)
		rewriteLayersFontGlyphs(document.Templates[index].Layers, used)
	}
	for index := range document.Pages {
		document.Pages[index].Items = rewriteItemsFontGlyphs(document.Pages[index].Items, used)
		rewriteLayersFontGlyphs(document.Pages[index].Layers, used)
	}
	for pageIndex := range document.Annotations {
		for itemIndex := range document.Annotations[pageIndex].Items {
			document.Annotations[pageIndex].Items[itemIndex].Items = rewriteItemsFontGlyphs(document.Annotations[pageIndex].Items[itemIndex].Items, used)
		}
	}
	for index := range document.Composites {
		document.Composites[index].Items = rewriteItemsFontGlyphs(document.Composites[index].Items, used)
	}
	for pattern := range collector.patterns {
		pattern.Items = rewriteItemsFontGlyphs(pattern.Items, used)
		rewriteLayersFontGlyphs(pattern.Layers, used)
	}
	return nil
}

func cloneDocumentFontSubsetInputs(document *Document) {
	document.Fonts = append([]Font(nil), document.Fonts...)
	document.Templates = append([]TemplatePage(nil), document.Templates...)
	for index := range document.Templates {
		document.Templates[index].Items = cloneItemsForFontSubset(document.Templates[index].Items)
		document.Templates[index].Layers = cloneLayersForFontSubset(document.Templates[index].Layers)
	}
	document.Pages = append([]Page(nil), document.Pages...)
	for index := range document.Pages {
		document.Pages[index].Items = cloneItemsForFontSubset(document.Pages[index].Items)
		document.Pages[index].Layers = cloneLayersForFontSubset(document.Pages[index].Layers)
	}
	document.Composites = append([]CompositeGraphicUnit(nil), document.Composites...)
	for index := range document.Composites {
		document.Composites[index].Items = cloneItemsForFontSubset(document.Composites[index].Items)
	}
	document.Annotations = append([]AnnotationPage(nil), document.Annotations...)
	for pageIndex := range document.Annotations {
		document.Annotations[pageIndex].Items = append([]Annotation(nil), document.Annotations[pageIndex].Items...)
		for itemIndex := range document.Annotations[pageIndex].Items {
			document.Annotations[pageIndex].Items[itemIndex].Items = cloneItemsForFontSubset(document.Annotations[pageIndex].Items[itemIndex].Items)
		}
	}
}

func cloneItemsForFontSubset(items []Item) []Item {
	result := append([]Item(nil), items...)
	for index, item := range result {
		if block, ok := item.(PageBlock); ok {
			block.Items = cloneItemsForFontSubset(block.Items)
			result[index] = block
		}
	}
	return result
}

func cloneLayersForFontSubset(layers []Layer) []Layer {
	result := append([]Layer(nil), layers...)
	for index := range result {
		result[index].Items = cloneItemsForFontSubset(result[index].Items)
	}
	return result
}

type fontUsage struct {
	// 按首次使用顺序保存字形，确保子集字体输出结果稳定。
	glyphs []uint16
	runes  []rune
	seen   map[uint16]bool
	remap  map[uint16]int
}

type fontUsageCollector struct {
	used     map[string]*fontUsage
	patterns map[*Pattern]bool
}

func (u *fontUsage) add(glyphID uint16) {
	if u.seen == nil {
		u.seen = make(map[uint16]bool)
	}
	if !u.seen[glyphID] {
		u.seen[glyphID] = true
		u.glyphs = append(u.glyphs, glyphID)
	}
}

func (c fontUsageCollector) collectItems(items []Item) {
	for _, item := range items {
		c.collectItem(item)
	}
}

func (c fontUsageCollector) collectLayers(layers []Layer) {
	for _, layer := range layers {
		c.collectItems(layer.Items)
	}
}

func (c fontUsageCollector) collectItem(item Item) {
	switch value := item.(type) {
	case Text:
		collectTextFontUsage(value.Font, value.Value, value.TextCodes, value.CGTransforms, c.used)
		c.collectClips(value.Clips)
		c.collectColor(value.FillColor)
		c.collectColor(value.StrokeColor)
	case Path:
		c.collectClips(value.Clips)
		c.collectColor(value.FillColor)
		c.collectColor(value.StrokeColor)
	case Image:
		c.collectClips(value.Clips)
	case Composite:
		c.collectClips(value.Clips)
	case PageBlock:
		c.collectItems(value.Items)
	}
}

func collectTextFontUsage(name, value string, codes []TextCode, transforms []CGTransform, used map[string]*fontUsage) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "SimSun"
	}
	usage := used[name]
	if usage == nil {
		return
	}
	text := value
	if len(codes) > 0 {
		text = ""
		for _, code := range codes {
			text += code.Value
		}
	}
	runes := []rune(text)
	if len(transforms) == 0 {
		usage.runes = append(usage.runes, runes...)
		return
	}
	mapped := make(map[int]bool, len(transforms))
	for _, transform := range transforms {
		count := transform.GlyphCount
		if count < 0 {
			count = 0
		} else if count == 0 || count > len(transform.Glyphs) {
			count = len(transform.Glyphs)
		}
		for _, glyphID := range transform.Glyphs[:count] {
			if glyphID >= 0 && glyphID <= 0xffff {
				usage.add(uint16(glyphID))
			}
		}
		codeCount := transform.CodeCount
		if codeCount <= 0 {
			codeCount = 1
		}
		for position := transform.CodePosition; position < transform.CodePosition+codeCount; position++ {
			mapped[position] = true
		}
	}
	for position, r := range runes {
		if !mapped[position] {
			usage.runes = append(usage.runes, r)
		}
	}
}

func (c fontUsageCollector) collectClips(clips *Clips) {
	if clips == nil {
		return
	}
	for _, clip := range clips.Items {
		for _, area := range clip.Areas {
			if area.Text != nil {
				collectTextFontUsage(area.Text.Font, area.Text.Value, area.Text.TextCodes, nil, c.used)
				c.collectColor(area.Text.FillColor)
				c.collectColor(area.Text.StrokeColor)
			}
			if area.Path != nil {
				c.collectColor(area.Path.FillColor)
				c.collectColor(area.Path.StrokeColor)
			}
		}
	}
}

func (c fontUsageCollector) collectColor(color *Color) {
	if color == nil || color.Pattern == nil || c.patterns[color.Pattern] {
		return
	}
	c.patterns[color.Pattern] = true
	c.collectItems(color.Pattern.Items)
	c.collectLayers(color.Pattern.Layers)
}

func subsetFont(resource *Font, usage *fontUsage) error {
	sfnt, err := font.ParseSFNT(resource.Data, 0)
	if err != nil {
		// 允许调用方提供自定义字体格式，或在其他位置自行校验字体数据。
		return nil
	}
	// 字体源数据已解析，重新根据 cmap 获取字符对应的字形编号。
	glyphs := make([]uint16, 0, len(usage.glyphs))
	seen := make(map[uint16]bool)
	add := func(glyphID uint16) {
		if !seen[glyphID] {
			seen[glyphID] = true
			glyphs = append(glyphs, glyphID)
		}
	}
	add(0)
	for _, r := range usage.runes {
		add(sfnt.GlyphIndex(r))
	}
	for _, glyphID := range usage.glyphs {
		if glyphID >= sfnt.NumGlyphs() {
			return fmt.Errorf("字形编号超出字体范围: %d", glyphID)
		}
		add(glyphID)
	}
	if len(glyphs) == 1 {
		return nil
	}
	originalGlyphs := append([]uint16(nil), glyphs...)
	subset, err := sfnt.Subset(glyphs, font.SubsetOptions{Tables: font.KeepMinTables})
	if err != nil {
		if strings.Contains(err.Error(), "only single-font CFFs are supported") {
			return nil
		}
		return err
	}
	resource.Data = subset.Write()
	if sfnt.IsTrueType {
		resource.Format = "ttf"
	} else if sfnt.IsCFF {
		resource.Format = "otf"
	}
	usage.remap = make(map[uint16]int, len(originalGlyphs))
	for index, glyphID := range originalGlyphs {
		usage.remap[glyphID] = index
	}
	return nil
}

func rewriteLayersFontGlyphs(layers []Layer, used map[string]*fontUsage) {
	for index := range layers {
		layers[index].Items = rewriteItemsFontGlyphs(layers[index].Items, used)
	}
}

func rewriteItemsFontGlyphs(items []Item, used map[string]*fontUsage) []Item {
	for index, item := range items {
		items[index] = rewriteItemFontGlyphs(item, used)
	}
	return items
}

func rewriteItemFontGlyphs(item Item, used map[string]*fontUsage) Item {
	switch value := item.(type) {
	case Text:
		value.CGTransforms = remapTextTransforms(value.Font, value.CGTransforms, used)
		return value
	case Path:
		return value
	case Image:
		return value
	case Composite:
		return value
	case PageBlock:
		value.Items = rewriteItemsFontGlyphs(value.Items, used)
		return value
	default:
		return item
	}
}

func remapTextTransforms(name string, transforms []CGTransform, used map[string]*fontUsage) []CGTransform {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "SimSun"
	}
	usage := used[name]
	if usage == nil || len(usage.remap) == 0 {
		return transforms
	}
	result := append([]CGTransform(nil), transforms...)
	for index := range result {
		result[index].Glyphs = remapGlyphs(result[index].Glyphs, usage.remap)
	}
	return result
}

func remapGlyphs(glyphs []int, remap map[uint16]int) []int {
	result := append([]int(nil), glyphs...)
	for index, glyphID := range result {
		if glyphID >= 0 && glyphID <= 0xffff {
			if subsetID, ok := remap[uint16(glyphID)]; ok {
				result[index] = subsetID
			}
		}
	}
	return result
}
