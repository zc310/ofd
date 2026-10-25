package creator

import "strings"

// PruneUnusedFonts 按已物化页面、模板、注解、复合图元、裁剪和底纹中的文字引用，
// 移除未使用的字体声明。只替换 Fonts 切片，不读取字体来源或修改调用方原切片。
// 没有物化页面或含原始资源 XML 时保留声明，避免影响 PageProvider 或原始数字资源引用。
func (d *Document) PruneUnusedFonts() {
	if d == nil || len(d.Fonts) == 0 || len(d.Pages) == 0 || len(d.PublicRes) > 0 {
		return
	}
	for _, page := range d.Pages {
		for _, resource := range page.Resources {
			if len(resource.Data) > 0 {
				return
			}
		}
	}
	usage := collectDocumentFontUsage(d).used
	fonts := make([]Font, 0, len(d.Fonts))
	for _, resource := range d.Fonts {
		if u := usage[strings.TrimSpace(resource.Name)]; u != nil && u.referenced {
			fonts = append(fonts, resource)
		}
	}
	d.Fonts = fonts
}

func collectDocumentFontUsage(document *Document) fontUsageCollector {
	used := make(map[string]*fontUsage, len(document.Fonts))
	for _, resource := range document.Fonts {
		used[strings.TrimSpace(resource.Name)] = &fontUsage{}
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
	return collector
}
