package navigation

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/drawing"
)

// 链接可以出现在页面图元、模板和批注中：
//
//  1. 页面正文图层图元自身的 CT_GraphicUnit/Actions；
//  2. 页面所用模板页的图层（模板按 ZOrder 叠加到页面坐标）；
//  3. 页面注解文件中 Link 注解的 Appearance 图元。
//
// 附件跳转、声音与影片动作没有 PDF 对应物，不在此处理。

// PageRef 是内部跳转目标在输出文档中的页序与页面高度。
type PageRef struct {
	// Index 是目标页在输出中的页序号（从 0 起）。
	Index int
	// Height 是目标页高度（mm）。
	Height float64
}

// PageResolver 把 OFD 页面的 StID 解析为目标页在输出中的位置。ok 为 false
// 表示目标页不会出现在输出里（例如被页码范围裁掉），此时对应链接必须丢弃。
type PageResolver func(pageID models.StID) (PageRef, bool)

// Links 收集对象块内的链接，使用图元自身的局部 Boundary，不展开复合资源。
// 页面级提取方通过 content 遍历取得映射，再调用 ActionLink 解析动作。
func Links(items []models.PageItem, resolve PageResolver) []drawing.PageLink {
	var links []drawing.PageLink
	collectItemLinks(items, resolve, &links)
	return links
}

func collectItemLinks(items []models.PageItem, resolve PageResolver, links *[]drawing.PageLink) {
	for _, item := range items {
		if item.Kind == models.PageItemBlock {
			if item.Block != nil {
				collectItemLinks(item.Block.Items, resolve, links)
			}
			continue
		}
		unit := GraphicUnit(item)
		if unit == nil || unit.Actions == nil {
			continue
		}
		boundary := unit.Boundary
		if !BoundaryUsable(boundary) {
			continue
		}
		link, ok := ActionLink(*unit.Actions, resolve)
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
func collectAnnotationLinks(annot *models.PageAnnot, resolve PageResolver, links *[]drawing.PageLink) {
	if annot == nil {
		return
	}
	for _, item := range annot.Annots {
		if item == nil || item.Appearance == nil {
			continue
		}
		boundary := item.Appearance.Boundary
		if boundary == nil || !BoundaryUsable(*boundary) {
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
func appearanceLink(items []models.PageItem, resolve PageResolver) (drawing.PageLink, bool) {
	for _, item := range items {
		if item.Kind == models.PageItemBlock {
			if item.Block != nil {
				if link, ok := appearanceLink(item.Block.Items, resolve); ok {
					return link, true
				}
			}
			continue
		}
		unit := GraphicUnit(item)
		if unit == nil || unit.Actions == nil {
			continue
		}
		if link, ok := ActionLink(*unit.Actions, resolve); ok {
			return link, true
		}
	}
	return drawing.PageLink{}, false
}

// ActionLink 按动作顺序返回首个可解析的 CLICK 链接，同一动作中 URI 优先于
// 内部跳转。目标页无法解析时跳过该动作，继续检查后续动作。
func ActionLink(actions models.Actions, resolve PageResolver) (drawing.PageLink, bool) {
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
func linkTarget(dest models.CtDest, resolve PageResolver) (drawing.LinkTarget, bool) {
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
		Zoom:       optionalValue(dest.Zoom),
		PageHeight: ref.Height,
	}
	target.Name = linkAnchorName(target)
	return target, true
}

// optionalValue 读取可选坐标，缺省按 0 处理。
//
// 缺省 0 与显式 0 在各 DestType 下语义一致（规范未定义坐标即按 0），因此无需
// 区分。Dest@Zoom 同样可选，缺省 0 表示保持阅读器当前缩放，与 PDF 的 XYZ 目的地
// 对缩放为 0 的解释一致。
func optionalValue(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

// linkDestType 把 OFD 的 Dest@Type 映射为 drawing.DestType。
// 未知类型返回 false，调用方丢弃无法解释的目标，避免生成语义不明的跳转。
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
		target.Left, target.Top, target.Right, target.Bottom,
		target.Zoom, target.PageHeight,
	} {
		binary.LittleEndian.PutUint64(scratch[:], math.Float64bits(value))
		_, _ = hash.Write(scratch[:])
	}
	return fmt.Sprintf("ofd_%d_%016x", target.Page, hash.Sum64())
}

// BoundaryUsable 判断热区能否成为点击区域。
//
// 真实文档里存在负宽高（internal/models 的 Boundary 注释对此有说明），负宽高
// 同样表示一个矩形，需在归一化时取绝对值；但宽高为零、非有限值则没有可点区域。
func BoundaryUsable(boundary models.StBox) bool {
	if !boundary.IsFinite() {
		return false
	}
	return boundary.Width != 0 && boundary.Height != 0
}

// GraphicUnit 返回图元携带的只读图形单元（含 Actions 与 Boundary）。
func GraphicUnit(item models.PageItem) *models.CTGraphicUnit {
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
	case models.PageItemBlock:
		return nil
	}
	return nil
}

// AnnotationLinks 按 Appearance 自身的页面边界收集批注链接。
func AnnotationLinks(annot *models.PageAnnot, resolve PageResolver) []drawing.PageLink {
	var links []drawing.PageLink
	collectAnnotationLinks(annot, resolve, &links)
	return links
}
