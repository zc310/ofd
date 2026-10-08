package render

import (
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/drawing"
)

// document_links.go 从 OFD 页面提取外部链接热区，供 PDF 等不支持交互的输出格式
// 生成可点击区域。链接可以出现在三处，都要扫：
//
//  1. 页面正文图层图元自身的 CT_GraphicUnit/Actions；
//  2. 页面所用模板页的图层（模板按 ZOrder 叠加到页面坐标）；
//  3. 页面注解文件中 Link 注解的 Appearance 图元。
//
// 与 pkg/webreader 的差别在于这里只取 URI：内部跳转的目标页在输出里未必存在
// （页码裁剪、多文档体合并），而提取热区时无从得知最终会输出哪些页，与其产出一个
// 指向不存在的目标，不如丢弃；PDF 后端对这些链接没有任何代价可省。

// PageExternalLinks 返回页面上所有外部链接热区，按出现顺序排列。
//
// page 为 nil，或页面内容读取失败时返回 nil：链接是附加信息，读不到内容就没有
// 热区可言，不应让整个转换失败。
func (d *Document) PageExternalLinks(page *parser.Page) []drawing.PageLink {
	if d == nil || d.Document == nil || page == nil {
		return nil
	}
	links := make([]drawing.PageLink, 0, 4)
	_ = page.WithPageContent(func(content *models.PageContent) error {
		if content == nil {
			return nil
		}
		if content.Content != nil {
			collectExternalLinks(content.Content.Layer, &links)
		}
		return nil
	})
	// 模板页上的链接同样可点：模板按 ZOrder 叠加到页面，其图元坐标已是页面坐标。
	for _, ref := range page.Template() {
		template := d.GetTemplate(models.StID(ref.TemplateID))
		if template == nil || template.Content == nil {
			continue
		}
		collectExternalLinks(template.Content.Layer, &links)
	}
	collectAnnotationExternalLinks(d.GetAnnotation(page.ID), &links)
	if len(links) == 0 {
		return nil
	}
	return links
}

// collectExternalLinks 遍历图层，把图元上带边界的 CLICK 外部链接追加到 links。
func collectExternalLinks(layers []*models.Layer, links *[]drawing.PageLink) {
	for _, layer := range layers {
		if layer == nil {
			continue
		}
		collectItemExternalLinks(layer.Items, links)
	}
}

func collectItemExternalLinks(items []models.PageItem, links *[]drawing.PageLink) {
	for _, item := range items {
		if item.Kind == models.PageItemBlock {
			if item.Block != nil {
				collectItemExternalLinks(item.Block.Items, links)
			}
			continue
		}
		unit := linkGraphicUnit(item)
		if unit == nil || unit.Actions == nil {
			continue
		}
		boundary := unit.Boundary
		if !linkBoundaryUsable(boundary) {
			continue
		}
		uri, ok := clickURI(unit.Actions.Action)
		if !ok {
			continue
		}
		*links = append(*links, drawing.PageLink{
			URI:    uri,
			X:      boundary.X,
			Y:      boundary.Y,
			Width:  boundary.Width,
			Height: boundary.Height,
		})
	}
}

// collectAnnotationExternalLinks 收集 Link 注解外观图元上的外部链接。
//
// 热区取注解 Appearance 自身的 Boundary：它是页面上的绝对位置，而外观图元的
// Boundary 相对注解定位。注解可能只有部分图元带链接，所以即便某图元无边界也
// 不能因此放弃整个注解。
func collectAnnotationExternalLinks(annot *models.PageAnnot, links *[]drawing.PageLink) {
	if annot == nil {
		return
	}
	for _, item := range annot.Annots {
		if item == nil || item.Appearance == nil {
			continue
		}
		boundary := item.Appearance.Boundary
		if boundary == nil || !linkBoundaryUsable(*boundary) {
			continue
		}
		uri, ok := appearanceClickURI(item.Appearance.Items)
		if !ok {
			continue
		}
		*links = append(*links, drawing.PageLink{
			URI:    uri,
			X:      boundary.X,
			Y:      boundary.Y,
			Width:  boundary.Width,
			Height: boundary.Height,
		})
	}
}

// appearanceClickURI 递归查找注解外观中首个带 URI 的 CLICK 动作。
func appearanceClickURI(items []models.PageItem) (string, bool) {
	for _, item := range items {
		if item.Kind == models.PageItemBlock {
			if item.Block != nil {
				if uri, ok := appearanceClickURI(item.Block.Items); ok {
					return uri, true
				}
			}
			continue
		}
		unit := linkGraphicUnit(item)
		if unit == nil || unit.Actions == nil {
			continue
		}
		if uri, ok := clickURI(unit.Actions.Action); ok {
			return uri, true
		}
	}
	return "", false
}

// clickURI 返回动作列表中首个 CLICK 外部链接。
func clickURI(actions []models.CtAction) (string, bool) {
	for _, action := range actions {
		if action.Event != models.ActionEventClick {
			continue
		}
		if action.URI != nil && action.URI.URI != "" {
			return action.URI.URI, true
		}
	}
	return "", false
}

// linkBoundaryUsable 判断热区能否成为点击区域。
//
// 真实文档里存在负宽高（internal/models 的 Boundary 注释对此有说明），负宽高
// 同样表示一个矩形，需在归一化时取绝对值；但宽高为零、非有限值则没有可点区域。
func linkBoundaryUsable(boundary models.StBox) bool {
	if !boundary.IsFinite() {
		return false
	}
	return boundary.Width != 0 && boundary.Height != 0
}

// linkGraphicUnit 返回图元携带的图形单元（含 Actions 与 Boundary）。
func linkGraphicUnit(item models.PageItem) *models.CTGraphicUnit {
	switch item.Kind {
	case models.PageItemText:
		if item.Text != nil {
			return &item.Text.CTGraphicUnit
		}
	case models.PageItemPath:
		if item.Path != nil {
			return &item.Path.CTGraphicUnit
		}
	case models.PageItemImage:
		if item.Image != nil {
			return &item.Image.CTGraphicUnit
		}
	case models.PageItemComposite:
		if item.Composite != nil {
			return &item.Composite.CTGraphicUnit
		}
	}
	return nil
}
