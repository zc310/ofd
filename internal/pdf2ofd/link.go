package pdf2ofd

import (
	"context"
	"fmt"
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/pkg/creator"
)

// linkAnnotationIDBase 是转换出的链接注解 ID 起点。
//
// 创建器从 1 开始为图元分配 ID，注解 ID 不登记进它的预留表，两者理论上会撞车。
// 用高位起点把注解推到序列之外：OFD 的 ID 上限是 2^32-1，这之上仍有两亿个 ID
// 可用，足够任何一份 PDF 的链接数量。
const linkAnnotationIDBase = uint64(1) << 31

// convertPageLinks 把页面 /Annots 中带有可转换点击动作的 Link/Widget 注解
// 转换为 OFD 链接注解。
//
// PDF 的链接注解只有一个矩形和一个动作，没有外观；OFD 侧把动作挂在注解外观里的
// 一个不可见路径上——阅读器和 PDF 导出都是从外观图元的动作取热区，位置取注解
// Appearance 的 Boundary，而不是图元自身的 Boundary（见 links.yaml）。
//
// 动作语义沿用大纲的转换：/Dest 与 /A 的 GoTo、URI 能落到 OFD 动作，其余类型
// （Launch、GoToR、Named、Movie、Sound）没有等价物，连同不可见的注解一起丢掉——
// 留一个点下去没反应的热区比没有热区更糟。id 在整个文档范围内递增。
func convertPageLinks(gctx context.Context, ctx *model.Context, pageDict types.Dict,
	info pdfPageInfo, geometries []pdfPageInfo, id *uint64) []creator.Annotation {
	var links []creator.Annotation
	for _, annotation := range pageAnnotationDicts(ctx, pageDict) {
		name, ok := annotation["Subtype"].(types.Name)
		if !ok || (name.Value() != "Link" && name.Value() != "Widget") {
			continue
		}
		if linkHidden(annotation) {
			continue
		}
		rect := pdfNumberArray(ctx, annotation["Rect"])
		box, ok := pdfLinkBox(info, rect)
		if !ok {
			continue
		}
		action, ok := pdfClickAction(gctx, ctx, annotation, geometries)
		if !ok {
			continue
		}
		visible := false
		links = append(links, creator.Annotation{
			ID:   *id,
			Type: "Link",
			// PDF 链接注解不带可靠的作者，署名转换器本身；LastModDate 留空，
			// 由创建器用文档的 ModDate/CreationDate 兜底。
			Creator:  converterName,
			Boundary: box,
			Items: []creator.Item{creator.Path{
				X: 0, Y: 0, Width: box.Width, Height: box.Height,
				Data:    fmt.Sprintf("M 0 0 L %.4f 0 L %.4f %.4f L 0 %.4f C", box.Width, box.Width, box.Height, box.Height),
				Stroke:  false,
				Fill:    false,
				Visible: &visible,
				Actions: []creator.Action{action},
			}},
		})
		*id++
	}
	return links
}

// linkHidden 判断注解是否被标记为不可交互。位 1 Invisible、位 2 Hidden、
// 位 6 NoView 的注解都不参与交互。
func linkHidden(annotation types.Dict) bool {
	flags, ok := integerValue(annotation["F"])
	return ok && flags&(1|2|32) != 0
}

// pdfLinkBox 把 PDF 链接注解的 Rect 换算为 OFD 页面坐标下的矩形（毫米），原点
// 在页面左上角。页面旋转由 pdfPagePoint 处理，矩形用 min/max 归一化，因此
// Rect 端点顺序颠倒或页面带旋转时都得到正宽高。
func pdfLinkBox(info pdfPageInfo, rect []float64) (*creator.Box, bool) {
	if len(rect) < 4 {
		return nil, false
	}
	// Rect 可能完全在页面之外，也可能有角点超出页面。前者丢弃，后者裁到版心：
	// 越界的矩形会让阅读器在页面外生成热区。
	x0, y0 := pdfPagePoint(info, rect[0], rect[1])
	x1, y1 := pdfPagePoint(info, rect[2], rect[3])
	left, right := math.Min(x0, x1), math.Max(x0, x1)
	top, bottom := math.Min(y0, y1), math.Max(y0, y1)
	pageWidth := (info.maxX - info.minX) * info.userUnit * pdfPointToMillimeter
	pageHeight := (info.maxY - info.minY) * info.userUnit * pdfPointToMillimeter
	if info.rotate == 90 || info.rotate == 270 {
		pageWidth, pageHeight = pageHeight, pageWidth
	}
	left, right = math.Max(0, math.Min(left, pageWidth)), math.Max(0, math.Min(right, pageWidth))
	top, bottom = math.Max(0, math.Min(top, pageHeight)), math.Max(0, math.Min(bottom, pageHeight))
	box := &creator.Box{X: left, Y: top, Width: right - left, Height: bottom - top}
	if box.Width <= 0 || box.Height <= 0 || !finiteLinkNumber(box.X) || !finiteLinkNumber(box.Y) {
		return nil, false
	}
	return box, true
}

// finiteLinkNumber 拒绝 NaN 与无穷，避免它们写进 XML。
func finiteLinkNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
