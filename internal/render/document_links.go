package render

import (
	"errors"
	"fmt"

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

// CompositeActionVisitor 同步读取复合资源内部的图元动作。
// unit 及其嵌套数据只读；matrix 将局部 OFD 坐标映射到页面，clip 为页面坐标裁剪。
// 回调只能在调用期间访问数据，不能保存模型指针或修改共享路径。
type CompositeActionVisitor func(id models.StID, templateID uint64, unit models.CTGraphicUnit, matrix geom.Matrix, clip *geom.Path)

var (
	errEmptyCompositeActionInput = errors.New("复合动作的文档、页面或回调为空")
	errEmptyCompositeActionPage  = errors.New("复合动作的页面内容或区域为空")
)

// WalkCompositeActions 遍历正文、模板及 Link 注解外观中的复合资源动作。
// §13 表50的单元映射、嵌套裁剪与绘制共用，动作解释交给阅读器，避免只剩 PDF 链接子集。
func (p *Document) WalkCompositeActions(page *parser.Page, visibility PageVisibility, visit CompositeActionVisitor) error {
	if p == nil || p.Document == nil || page == nil || visit == nil {
		return errEmptyCompositeActionInput
	}
	lease, err := page.AcquireLease()
	if err != nil {
		return err
	}
	defer lease.Release()
	pageContent := lease.Content()
	if pageContent == nil || pageContent.Area == nil {
		return errEmptyCompositeActionPage
	}
	var budget renderBudget
	budget.reset()
	frame := contentFrame{Scope: &contentSource{Document: p, budget: &budget},
		Box: pageContent.Area.PhysicalBox, Matrix: geom.Identity, Clip: content.ApplicationClip(pageContent.Area)}
	visitor := func(frame contentFrame, item models.PageItem) {
		if !frame.InComposite {
			return
		}
		unit := navigation.GraphicUnit(item)
		if unit != nil && unit.Actions != nil && navigation.BoundaryUsable(unit.Boundary) {
			visit(pageItemID(item), frame.TemplateID, *unit, frame.Matrix, frame.Clip)
		}
	}
	if pageContent.Content != nil {
		content.WalkLayers(frame, visiblePageLayers(pageContent.Content.Layer, visibility.HiddenLayers), visitor)
	}
	for _, ref := range pageContent.Template {
		if visibility.HiddenTemplates[models.StID(ref.TemplateID)] {
			continue
		}
		template, scope, release, err := p.templateScope(models.StID(ref.TemplateID))
		if err != nil {
			return fmt.Errorf("加载模板 %d 失败: %w", ref.TemplateID, err)
		}
		if template != nil && template.Content != nil {
			child := frame
			child.Scope = &contentSource{Document: scope, budget: &budget}
			if len(template.PageRes) != 0 {
				child.TemplateID = uint64(ref.TemplateID)
			}
			content.WalkLayers(child, template.Content.Layer, visitor)
		}
		release()
	}
	annotations, err := p.LoadAnnotation(page.ID)
	if err != nil {
		return fmt.Errorf("加载页面 %d 注解失败: %w", page.ID, err)
	}
	if annotations != nil {
		for _, annotation := range annotations.Annots {
			if annotation != nil && annotation.Type == models.AnnotTypeLink {
				content.WalkAnnotations(frame, &models.PageAnnot{Annots: []*models.Annot{annotation}}, false, visitor)
			}
		}
	}
	return nil
}

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
	frame.Clip = content.ApplicationClip(pageContent.Area)
	if pageContent.Content != nil {
		content.WalkLayers(frame, visiblePageLayers(pageContent.Content.Layer, visibility.HiddenLayers), visit)
	}
	for _, ref := range pageContent.Template {
		if visibility.HiddenTemplates[models.StID(ref.TemplateID)] {
			continue
		}
		template, scope, release, loadErr := p.templateScope(models.StID(ref.TemplateID))
		if loadErr != nil {
			return nil, fmt.Errorf("加载模板 %d 失败: %w", ref.TemplateID, loadErr)
		}
		if template == nil || template.Content == nil {
			release()
			continue
		}
		child := frame
		child.Scope = &contentSource{Document: scope, budget: &budget}
		content.WalkLayers(child, template.Content.Layer, visit)
		release()
	}
	annotation, err := p.LoadAnnotation(page.ID)
	if err != nil {
		return nil, fmt.Errorf("加载页面 %d 注解失败: %w", page.ID, err)
	}
	for _, link := range navigation.AnnotationLinks(annotation, resolve) {
		path := geom.Rectangle(link.Width, link.Height).Translate(link.X, link.Y)
		if frame.Clip != nil {
			path = path.And(frame.Clip)
		}
		if !path.Empty() {
			box := path.Bounds()
			link.X, link.Y, link.Width, link.Height = box.X0, box.Y0, box.W(), box.H()
			link.Region = navigation.NormalizedRegion(path)
			links = append(links, link)
		}
	}
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
	var link drawing.PageLink
	var path *geom.Path
	for _, action := range unit.Actions.Action {
		candidate, ok := navigation.ActionLink(models.Actions{Action: []models.CtAction{action}}, resolve)
		if ok {
			link = candidate
			path = navigation.ActionRegion(action, *unit).Transform(frame.Matrix)
			break
		}
	}
	if path == nil {
		return
	}
	link.ObjectID, link.InComposite = uint64(pageItemID(item)), frame.InComposite
	if frame.Clip != nil {
		path = path.And(frame.Clip)
	}
	if path.Empty() {
		return
	}
	bounds := path.Bounds()
	link.X, link.Y, link.Width, link.Height = bounds.X0, bounds.Y0, bounds.W(), bounds.H()
	link.Region = navigation.NormalizedRegion(path)
	*links = append(*links, link)
}

// PageExternalLinks 只返回外部链接热区。
func (p *Document) PageExternalLinks(page *parser.Page) []drawing.PageLink {
	return p.PageLinks(page, nil)
}
