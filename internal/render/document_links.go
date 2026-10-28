package render

import (
	"fmt"
	"math"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/content"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/navigation"
)

// LinkTargetRef 是内部跳转目标在输出文档中的位置。
type LinkTargetRef = navigation.PageRef

// PageLinkResolver 将 OFD 页面 ID 映射为输出页序和页面高度。
type PageLinkResolver = navigation.PageResolver

// PageLinks 返回页面链接热区，resolve 为 nil 时仅导出外部链接。
// 页面为空或加载失败时返回 nil；需要保留错误时使用 PageLinksWithVisibilityE。
func (p *Document) PageLinks(page *parser.Page, resolve PageLinkResolver) []drawing.PageLink {
	return p.PageLinksWithVisibility(page, resolve, PageVisibility{})
}

// PageLinksWithVisibility 排除隐藏正文图层和模板中的链接，保留批注链接。
func (p *Document) PageLinksWithVisibility(page *parser.Page, resolve PageLinkResolver, visibility PageVisibility) []drawing.PageLink {
	links, _ := p.PageLinksWithVisibilityE(page, resolve, visibility)
	return links
}

// PageLinksWithVisibilityE 排除隐藏项并保留页面、模板和批注加载错误。
func (p *Document) PageLinksWithVisibilityE(page *parser.Page, resolve PageLinkResolver, visibility PageVisibility) ([]drawing.PageLink, error) {
	if p == nil || p.Document == nil || page == nil {
		return nil, fmt.Errorf("链接文档或页面为空")
	}
	links := make([]drawing.PageLink, 0, 4)
	var budget renderBudget
	budget.reset()
	frame := contentFrame{Scope: &contentSource{Document: p, budget: &budget}, Matrix: geom.Identity}
	visit := func(frame contentFrame, item models.PageItem) {
		collectTransformedItemLink(frame, item, resolve, &links)
	}
	lease, err := page.AcquireLease()
	if err != nil {
		return nil, err
	}
	defer lease.Release()
	pageContent := lease.Content()
	if pageContent == nil {
		return nil, fmt.Errorf("页面内容为空")
	}
	frame.Box = pageContent.Area.PhysicalBox
	if pageContent.Content != nil {
		content.WalkLayers(frame, visiblePageLayers(pageContent.Content.Layer, visibility.HiddenLayers), visit)
	}
	for _, ref := range pageContent.Template {
		if visibility.HiddenTemplates[models.StID(ref.TemplateID)] {
			continue
		}
		template, loadErr := p.LoadTemplate(models.StID(ref.TemplateID))
		if loadErr != nil {
			return nil, fmt.Errorf("加载模板 %d 失败: %w", ref.TemplateID, loadErr)
		}
		if template == nil || template.Content == nil {
			continue
		}
		content.WalkLayers(frame, template.Content.Layer, visit)
	}
	annotation, err := p.LoadAnnotation(page.ID)
	if err != nil {
		return nil, fmt.Errorf("加载页面 %d 注解失败: %w", page.ID, err)
	}
	links = append(links, navigation.AnnotationLinks(annotation, resolve)...)
	if len(links) == 0 {
		return nil, nil
	}
	return links, nil
}

func collectTransformedItemLink(frame contentFrame, item models.PageItem, resolve PageLinkResolver, links *[]drawing.PageLink) {
	unit := navigation.GraphicUnit(item)
	if unit == nil || unit.Actions == nil || !navigation.BoundaryUsable(unit.Boundary) {
		return
	}
	link, ok := navigation.ActionLink(*unit.Actions, resolve)
	if !ok {
		return
	}
	link.ObjectID, link.InComposite = uint64(pageItemID(item)), frame.InComposite
	box := unit.Boundary
	x0, x1 := math.Min(box.X, box.X+box.Width), math.Max(box.X, box.X+box.Width)
	y0, y1 := math.Min(box.Y, box.Y+box.Height), math.Max(box.Y, box.Y+box.Height)
	path := geom.Rectangle(x1-x0, y1-y0).Translate(x0, y0).Transform(frame.Matrix)
	if frame.Clip != nil {
		path = path.And(frame.Clip)
	}
	if path.Empty() {
		return
	}
	bounds := path.Bounds()
	link.X, link.Y, link.Width, link.Height = bounds.X0, bounds.Y0, bounds.W(), bounds.H()
	*links = append(*links, link)
}

// PageExternalLinks 只返回外部链接热区。
func (p *Document) PageExternalLinks(page *parser.Page) []drawing.PageLink {
	return p.PageLinks(page, nil)
}
