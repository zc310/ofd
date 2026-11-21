package flow

import (
	"fmt"
	"math"

	"github.com/zc310/ofd/pkg/creator"
)

const leftBorderWidth = 2.4

// 只保留最后一条色条的图元位置；每次试排独立，不跨页面或栏合并。
type leftBorder struct {
	page, item int
	color      *creator.Color
}

// 色条是 GB/T 33190—2016 §9.1 的普通填充路径；随各内容片段实际高度分页，
// 不另造自定义 OFD 节点，也不改变正文、链接或文字提取的阅读顺序。
func (e *engine) drawLeftBorder(color *creator.Color, indent, top, height float64) {
	if color == nil {
		e.state.leftBorder = nil
		return
	}
	if height <= 0 {
		return
	}
	x, y := e.contentLeft+indent-5, e.geometry.PageHeight-top
	if previous := e.state.leftBorder; previous != nil && previous.page == e.pageIndex && previous.color == color {
		path, ok := e.cur().Items[previous.item].(creator.Path)
		if ok && math.Abs(path.X-x) < 0.000001 && math.Abs(path.Y+path.Height-y) < 0.000001 {
			// 段落、代码面板、表格行和段后间距连续时扩展同一路径，
			// 仅完整片段两端向右微弯，避免内部接缝重复出现弧形。
			path.Height += height
			path.Data = leftBorderPath(path.Width, path.Height)
			e.cur().Items[previous.item] = path
			return
		}
	}
	stroke := false
	e.add(creator.Path{X: x, Y: y, Width: leftBorderWidth, Height: height,
		Data: leftBorderPath(leftBorderWidth, height), Fill: true,
		StrokeSet: &stroke, FillColor: color})
	e.state.leftBorder = &leftBorder{page: e.pageIndex, item: len(e.cur().Items) - 1, color: color}
}

// 中段保持半个视口宽的直色条，上下端向右微弯成括号状；所有控制点均在视口内。
// 使用 GB/T 33190—2016 §9.1 表 36 的 Q 二次贝塞尔和 C 闭合，短片段只压缩弯曲高度。
func leftBorderPath(width, height float64) string {
	stem, bend := width/2, math.Min(width, height/2)
	return fmt.Sprintf("M %g 0 Q 0 0 0 %g L 0 %g Q 0 %g %g %g Q %g %g %g %g L %g %g Q %g 0 %g 0 C",
		width, bend, height-bend, height, width, height,
		stem, height, stem, height-bend, stem, bend, stem, width)
}

// 段后间距也属于色条区域，连续提示段之间不留下断开的竖线。
func (e *engine) spaceLeftBorder(color *creator.Color, indent, height float64) {
	top := e.y
	e.space(height)
	e.drawLeftBorder(color, indent, top, top-e.y)
}
