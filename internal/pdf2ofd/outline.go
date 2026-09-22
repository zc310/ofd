package pdf2ofd

import (
	"math"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/pkg/creator"
)

// maxOutlineDepth 限制 PDF 大纲树的递归深度，避免损坏文件导致无限递归。
const maxOutlineDepth = 64

// convertOutlines 把 PDF 目录的 /Outlines 大纲树转换为 OFD 大纲。没有大纲或
// 大纲不可解析时返回 nil。
func convertOutlines(ctx *model.Context) []creator.Outline {
	if ctx == nil || ctx.XRefTable == nil {
		return nil
	}
	root := ctx.XRefTable.Outlines
	if root == nil {
		return nil
	}
	first := root.IndirectRefEntry("First")
	if first == nil {
		return nil
	}
	return convertOutlineSiblings(ctx, *first, pdfPageGeometries(ctx), 0)
}

// convertOutlineSiblings 转换同一层的大纲项链表（First/Next）。seen 用于截断
// 损坏文件中的循环引用。
func convertOutlineSiblings(ctx *model.Context, first types.IndirectRef, geometries []pdfPageInfo, depth int) []creator.Outline {
	if depth > maxOutlineDepth {
		return nil
	}
	var outlines []creator.Outline
	seen := map[int]bool{}
	for current := &first; current != nil; {
		objectNumber := current.ObjectNumber.Value()
		if seen[objectNumber] {
			break
		}
		seen[objectNumber] = true
		dict, err := ctx.XRefTable.DereferenceDict(*current)
		if err != nil || dict == nil {
			break
		}
		if outline, ok := convertOutlineItem(ctx, dict, geometries, depth); ok {
			outlines = append(outlines, outline)
		}
		current = dict.IndirectRefEntry("Next")
	}
	return outlines
}

func convertOutlineItem(ctx *model.Context, dict types.Dict, geometries []pdfPageInfo, depth int) (creator.Outline, bool) {
	// OFD 要求大纲标题非空，缺少标题的 PDF 大纲项直接跳过。
	title := pdfOutlineTitle(ctx, dict)
	if title == "" {
		return creator.Outline{}, false
	}
	outline := creator.Outline{Title: title}
	// PDF 的 Count 正数表示展开、负数表示折叠，绝对值是可见后代数量；OFD 的
	// Count 只能是非负后代数量，展开状态单独用 Expanded 表示。
	if count := dict.IntEntry("Count"); count != nil && *count != 0 {
		value := *count
		expanded := value >= 0
		if value < 0 {
			value = -value
		}
		outline.Count = &value
		outline.Expanded = &expanded
	}
	if action, ok := pdfOutlineAction(ctx, dict, geometries); ok {
		outline.Actions = []creator.Action{action}
	}
	if first := dict.IndirectRefEntry("First"); first != nil {
		outline.Children = convertOutlineSiblings(ctx, *first, geometries, depth+1)
	}
	return outline, true
}

func pdfOutlineAction(ctx *model.Context, dict types.Dict, geometries []pdfPageInfo) (creator.Action, bool) {
	// /Dest 优先于 /A：两者同时存在时以 /Dest 为准。
	if object, found := dict.Find("Dest"); found {
		if gotoAction, ok := pdfGotoAction(ctx, object, geometries); ok {
			return creator.Action{Event: creator.ActionEventClick, Goto: gotoAction}, true
		}
	}
	actionObject, found := dict.Find("A")
	if !found {
		return creator.Action{}, false
	}
	actionDict, err := ctx.XRefTable.DereferenceDict(actionObject)
	if err != nil || actionDict == nil {
		return creator.Action{}, false
	}
	name, ok := actionDict["S"].(types.Name)
	if !ok {
		return creator.Action{}, false
	}
	switch name.Value() {
	case "GoTo":
		dest, found := actionDict.Find("D")
		if !found {
			return creator.Action{}, false
		}
		if gotoAction, ok := pdfGotoAction(ctx, dest, geometries); ok {
			return creator.Action{Event: creator.ActionEventClick, Goto: gotoAction}, true
		}
	case "URI":
		if uri := pdfOutlineURI(ctx, actionDict); uri != "" {
			return creator.Action{Event: creator.ActionEventClick, URI: &creator.URIAction{URI: uri}}, true
		}
	}
	return creator.Action{}, false
}

// pdfGotoAction 把 PDF 目标（数组或命名目标）转换为 OFD 页面跳转动作，坐标
// 统一换算为毫米。无法解析页面时返回 false，由调用方忽略该动作。
func pdfGotoAction(ctx *model.Context, object types.Object, geometries []pdfPageInfo) (*creator.GotoAction, bool) {
	array, ok := pdfDestArray(ctx, object)
	if !ok || len(array) == 0 {
		return nil, false
	}
	pageIndex, ok := pdfDestPageIndex(ctx, array[0])
	if !ok || pageIndex < 0 || pageIndex >= len(geometries) {
		return nil, false
	}
	info := geometries[pageIndex]
	action := creator.GotoAction{Page: pageIndex, Type: "Fit"}
	if len(array) < 2 {
		return &action, true
	}
	name, ok := array[1].(types.Name)
	if !ok {
		return &action, true
	}
	// 页面旋转 90/270 时横纵坐标互换，FitH/FitV 的目标轴需要相应调整。
	swapped := info.rotate == 90 || info.rotate == 270
	switch name.Value() {
	case "XYZ":
		action.Type = "XYZ"
		left, okLeft := pdfDestNumber(ctx, array, 2)
		top, okTop := pdfDestNumber(ctx, array, 3)
		switch {
		case okLeft && okTop:
			x, y := pdfPagePoint(info, left, top)
			action.Left, action.Top = &x, &y
		case okLeft:
			x, _ := pdfPagePoint(info, left, info.minY)
			action.Left = &x
		case okTop:
			_, y := pdfPagePoint(info, info.minX, top)
			action.Top = &y
		}
		if zoom, ok := pdfDestNumber(ctx, array, 4); ok && zoom > 0 {
			action.Zoom = &zoom
		}
	case "FitH", "FitBH":
		value, ok := pdfDestNumber(ctx, array, 2)
		if !ok {
			break
		}
		x, y := pdfPagePoint(info, info.minX, value)
		if swapped {
			action.Type, action.Left = "FitV", &x
		} else {
			action.Type, action.Top = "FitH", &y
		}
	case "FitV", "FitBV":
		value, ok := pdfDestNumber(ctx, array, 2)
		if !ok {
			break
		}
		x, y := pdfPagePoint(info, value, info.minY)
		if swapped {
			action.Type, action.Top = "FitH", &y
		} else {
			action.Type, action.Left = "FitV", &x
		}
	case "FitR":
		left, okLeft := pdfDestNumber(ctx, array, 2)
		bottom, okBottom := pdfDestNumber(ctx, array, 3)
		right, okRight := pdfDestNumber(ctx, array, 4)
		top, okTop := pdfDestNumber(ctx, array, 5)
		if !okLeft || !okBottom || !okRight || !okTop {
			break
		}
		x1, y1 := pdfPagePoint(info, left, bottom)
		x2, y2 := pdfPagePoint(info, right, top)
		minX, maxX := math.Min(x1, x2), math.Max(x1, x2)
		minY, maxY := math.Min(y1, y2), math.Max(y1, y2)
		action.Type = "FitR"
		action.Left, action.Right = &minX, &maxX
		action.Top, action.Bottom = &minY, &maxY
	}
	return &action, true
}

// pdfDestArray 解析目标对象：既可以是目标数组，也可以是指向 /Dests 或名称树
// 的命名目标（Name/String）。
func pdfDestArray(ctx *model.Context, object types.Object) (types.Array, bool) {
	resolved, err := ctx.XRefTable.Dereference(object)
	if err != nil || resolved == nil {
		return nil, false
	}
	switch value := resolved.(type) {
	case types.Array:
		return value, true
	case types.Name, types.StringLiteral, types.HexLiteral:
		name, err := ctx.XRefTable.DestName(resolved)
		if err != nil || name == "" {
			return nil, false
		}
		array, err := ctx.XRefTable.DereferenceDestArray(name)
		if err != nil {
			return nil, false
		}
		return array, true
	}
	return nil, false
}

// pdfDestPageIndex 把目标数组第一项（页面对象的间接引用）解析为从 0 开始的
// 页面索引。远程跳转中的整数页序号不属于本文档页面，返回 false。
func pdfDestPageIndex(ctx *model.Context, object types.Object) (int, bool) {
	reference, ok := object.(types.IndirectRef)
	if !ok {
		return 0, false
	}
	pageNumber, err := ctx.XRefTable.PageNumber(reference.ObjectNumber.Value())
	if err != nil || pageNumber <= 0 {
		return 0, false
	}
	return pageNumber - 1, true
}

func pdfDestNumber(ctx *model.Context, array types.Array, index int) (float64, bool) {
	if index < 0 || index >= len(array) {
		return 0, false
	}
	resolved, err := ctx.XRefTable.Dereference(array[index])
	if err != nil || resolved == nil {
		return 0, false
	}
	return numberValue(resolved)
}

// pdfPageGeometries 返回每页的几何信息，用于把目标坐标换算到对应页面的
// OFD 页面坐标系。页面索引与转换时的页码一致。
func pdfPageGeometries(ctx *model.Context) []pdfPageInfo {
	if ctx.PageCount <= 0 {
		return nil
	}
	geometries := make([]pdfPageInfo, 0, ctx.PageCount)
	for pageNumber := 1; pageNumber <= ctx.PageCount; pageNumber++ {
		pageDict, _, inherited, err := ctx.PageDict(pageNumber, false)
		if err != nil {
			geometries = append(geometries, pdfPageInfo{userUnit: 1})
			continue
		}
		info, err := pdfPageInfoFor(ctx, pageDict, inherited)
		if err != nil {
			info = pdfPageInfo{userUnit: 1}
		}
		geometries = append(geometries, info)
	}
	return geometries
}

func pdfOutlineTitle(ctx *model.Context, dict types.Dict) string {
	return strings.TrimSpace(pdfTextString(ctx, dict, "Title"))
}

func pdfOutlineURI(ctx *model.Context, dict types.Dict) string {
	return strings.TrimSpace(pdfTextString(ctx, dict, "URI"))
}

// pdfTextString 读取并解码 PDF 文本字符串（String/Hex），支持 UTF-16BE 与
// PDFDocEncoding。
func pdfTextString(ctx *model.Context, dict types.Dict, key string) string {
	object, found := dict.Find(key)
	if !found {
		return ""
	}
	resolved, err := ctx.XRefTable.Dereference(object)
	if err != nil || resolved == nil {
		return ""
	}
	switch value := resolved.(type) {
	case types.StringLiteral:
		text, err := types.StringLiteralToString(value)
		if err != nil {
			return ""
		}
		return text
	case types.HexLiteral:
		text, err := types.HexLiteralToString(value)
		if err != nil {
			return ""
		}
		return text
	}
	return ""
}

// pdfPageModeUseOutlines 判断 PDF 目录是否要求打开时显示大纲面板。
func pdfPageModeUseOutlines(ctx *model.Context) bool {
	if ctx == nil || ctx.XRefTable == nil || ctx.XRefTable.PageMode == nil {
		return false
	}
	return *ctx.XRefTable.PageMode == model.PageModeUseOutlines
}
