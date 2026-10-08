package render

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/drawing"
)

// document_links.go 从 OFD 页面提取链接热区，供 PDF 等不支持交互的输出格式生成
// 可点击区域。链接可以出现在三处，都要扫：
//
//  1. 页面正文图层图元自身的 CT_GraphicUnit/Actions；
//  2. 页面所用模板页的图层（模板按 ZOrder 叠加到页面坐标）；
//  3. 页面注解文件中 Link 注解的 Appearance 图元。
//
// 附件跳转、声音与影片动作没有 PDF 对应物，不在此处理。

// LinkTargetRef 是内部跳转目标的定位信息。
type LinkTargetRef struct {
	// Index 是目标页在输出中的页序号（从 0 起）。
	Index int
	// Height 是目标页高度（mm）。
	Height float64
}

// PageLinkResolver 把 OFD 页面的 StID 解析为目标页在输出中的位置。ok 为 false
// 表示目标页不会出现在输出里（例如被页码范围裁掉），此时对应链接必须丢弃。
type PageLinkResolver func(pageID models.StID) (LinkTargetRef, bool)

// PageLinks 返回页面上所有链接热区，按出现顺序排列。
//
// resolve 为 nil 时只导出外部链接，内部跳转一律丢弃——调用方不知道最终会输出
// 哪些页时，宁可不导出也不能写出指向不存在页面的链接。
//
// page 为 nil，或页面内容读取失败时返回 nil：链接是附加信息，读不到内容就没有
// 热区可言，不应让整个转换失败。
func (d *Document) PageLinks(page *parser.Page, resolve PageLinkResolver) []drawing.PageLink {
	if d == nil || d.Document == nil || page == nil {
		return nil
	}
	links := make([]drawing.PageLink, 0, 4)
	_ = page.WithPageContent(func(content *models.PageContent) error {
		if content == nil {
			return nil
		}
		if content.Content != nil {
			collectLinks(content.Content.Layer, resolve, &links)
		}
		return nil
	})
	// 模板页上的链接同样可点：模板按 ZOrder 叠加到页面，其图元坐标已是页面坐标。
	for _, ref := range page.Template() {
		template := d.GetTemplate(models.StID(ref.TemplateID))
		if template == nil || template.Content == nil {
			continue
		}
		collectLinks(template.Content.Layer, resolve, &links)
	}
	collectAnnotationLinks(d.GetAnnotation(page.ID), resolve, &links)
	if len(links) == 0 {
		return nil
	}
	return links
}

// PageExternalLinks 只返回外部链接热区，供不处理内部跳转的调用方使用。
func (d *Document) PageExternalLinks(page *parser.Page) []drawing.PageLink {
	return d.PageLinks(page, nil)
}

// collectLinks 遍历图层，把图元上带边界的 CLICK 链接追加到 links。
func collectLinks(layers []*models.Layer, resolve PageLinkResolver, links *[]drawing.PageLink) {
	for _, layer := range layers {
		if layer == nil {
			continue
		}
		collectItemLinks(layer.Items, resolve, links)
	}
}

func collectItemLinks(items []models.PageItem, resolve PageLinkResolver, links *[]drawing.PageLink) {
	for _, item := range items {
		if item.Kind == models.PageItemBlock {
			if item.Block != nil {
				collectItemLinks(item.Block.Items, resolve, links)
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
		link, ok := actionLink(*unit.Actions, resolve)
		if !ok {
			continue
		}
		link.X, link.Y, link.Width, link.Height = boundary.X, boundary.Y, boundary.Width, boundary.Height
		*links = append(*links, link)
	}
}

// collectAnnotationLinks 收集 Link 注解外观图元上的链接。
//
// 热区取注解 Appearance 自身的 Boundary：它是页面上的绝对位置，而外观图元的
// Boundary 相对注解定位。注解可能只有部分图元带链接，所以即便某图元无边界也
// 不能因此放弃整个注解。
func collectAnnotationLinks(annot *models.PageAnnot, resolve PageLinkResolver, links *[]drawing.PageLink) {
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
		link, ok := appearanceLink(item.Appearance.Items, resolve)
		if !ok {
			continue
		}
		link.X, link.Y, link.Width, link.Height = boundary.X, boundary.Y, boundary.Width, boundary.Height
		*links = append(*links, link)
	}
}

// appearanceLink 递归查找注解外观中的首个 CLICK 链接。
func appearanceLink(items []models.PageItem, resolve PageLinkResolver) (drawing.PageLink, bool) {
	for _, item := range items {
		if item.Kind == models.PageItemBlock {
			if item.Block != nil {
				if link, ok := appearanceLink(item.Block.Items, resolve); ok {
					return link, true
				}
			}
			continue
		}
		unit := linkGraphicUnit(item)
		if unit == nil || unit.Actions == nil {
			continue
		}
		if link, ok := actionLink(*unit.Actions, resolve); ok {
			return link, true
		}
	}
	return drawing.PageLink{}, false
}

// actionLink 返回动作列表中首个 CLICK 链接：外部链接优先，其次是能解析出目标页
// 的内部跳转。目标页无法解析时跳过该动作，继续看同一图元上的其它动作。
func actionLink(actions models.Actions, resolve PageLinkResolver) (drawing.PageLink, bool) {
	for _, action := range actions.Action {
		if action.Event != models.ActionEventClick {
			continue
		}
		if action.URI != nil && action.URI.URI != "" {
			return drawing.PageLink{URI: action.URI.URI}, true
		}
		if action.Goto == nil || action.Goto.Dest == nil {
			continue
		}
		if target, ok := linkTarget(*action.Goto.Dest, resolve); ok {
			return drawing.PageLink{Target: &target}, true
		}
	}
	return drawing.PageLink{}, false
}

// linkTarget 把 OFD 跳转目标换算为输出坐标下的锚点。resolve 为 nil 表示调用方不
// 处理内部跳转。
func linkTarget(dest models.CtDest, resolve PageLinkResolver) (drawing.LinkTarget, bool) {
	if resolve == nil {
		return drawing.LinkTarget{}, false
	}
	ref, ok := resolve(models.StID(dest.PageID))
	if !ok {
		return drawing.LinkTarget{}, false
	}
	kind, ok := linkDestType(dest.Type)
	if !ok {
		return drawing.LinkTarget{}, false
	}
	target := drawing.LinkTarget{
		Page:       ref.Index,
		Type:       kind,
		Left:       optionalValue(dest.Left),
		Top:        optionalValue(dest.Top),
		Right:      optionalValue(dest.Right),
		Bottom:     optionalValue(dest.Bottom),
		PageHeight: ref.Height,
	}
	target.Name = linkAnchorName(target)
	return target, true
}

// optionalValue 读取可选坐标，缺省按 0 处理。
//
// 缺省 0 与显式 0 在各 DestType 下语义一致（规范未定义坐标即按 0），因此无需
// 区分。Zoom 同样可选，但 PDF 的 XYZ 目的地把缩放写死为 0（保持当前缩放），
// 表达不了 OFD 的 Zoom，这里整体不读取。
func optionalValue(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

// linkDestType 把 OFD 的 Dest@Type 映射为 drawing.DestType。未知类型按 Fit 处理：
// 目标页一定能打开，只是位置退化为整页适配。
func linkDestType(kind models.DestType) (drawing.DestType, bool) {
	switch kind {
	case models.DestTypeFit:
		return drawing.DestFit, true
	case models.DestTypeFitH:
		return drawing.DestFitH, true
	case models.DestTypeFitV:
		return drawing.DestFitV, true
	case models.DestTypeXYZ:
		return drawing.DestXYZ, true
	case models.DestTypeFitR:
		return drawing.DestFitR, true
	}
	return drawing.DestFit, false
}

// linkAnchorName 由目标位置推导锚点名。
//
// 名字必须由内容决定而不能来自自增序号：提取是逐页流式进行的，没有共享计数器，
// 而同名锚点在名称树里会自动去重——指向同一位置的多个链接共用一个锚点正是想要
// 的结果。名字只用字母数字与下划线，避免 PDF 名称对象的转义问题。
func linkAnchorName(target drawing.LinkTarget) string {
	hash := fnv.New64a()
	var scratch [8]byte
	for _, value := range []float64{
		float64(target.Page), float64(target.Type),
		target.Left, target.Top, target.Right, target.Bottom, target.PageHeight,
	} {
		binary.LittleEndian.PutUint64(scratch[:], math.Float64bits(value))
		_, _ = hash.Write(scratch[:])
	}
	return fmt.Sprintf("ofd_%d_%016x", target.Page, hash.Sum64())
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
