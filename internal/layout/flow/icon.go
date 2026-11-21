package flow

import (
	"fmt"
	"math"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/spec"
)

// 图标与文字共同参与首行占位，使用标准 PathObject 和 CTM 等比缩放，
// 遵循 GB/T 33190—2016 §8.1、§8.5 表 34 及 §9.1；不依赖字体中的图标字形。
func (e *engine) prepareIcon(p preparedBlock) (preparedBlock, error) {
	icon := p.block.LeadingIcon
	switch p.block.Kind {
	case "paragraph", "heading", "quote":
	case "list_item", "code":
		if p.block.callout == nil {
			return p, fmt.Errorf("%w：leading_icon 不支持普通列表或代码块", errContent)
		}
	default:
		return p, fmt.Errorf("%w：leading_icon 不支持块类型 %q", errContent, p.block.Kind)
	}
	for _, size := range []float64{icon.Width, icon.Height} {
		if size <= 0 || math.IsNaN(size) || math.IsInf(size, 0) {
			return p, fmt.Errorf("%w：图标视口必须为有限正数，实际 %g", errContent, size)
		}
	}
	if icon.Data == "" {
		return p, fmt.Errorf("%w：图标路径不能为空", errContent)
	}
	if _, err := spec.ParsePathData(icon.Data); err != nil {
		return p, fmt.Errorf("leading_icon.data: %w", err)
	}
	if icon.Rule != "" && icon.Rule != "NonZero" && icon.Rule != "Even-Odd" {
		return p, fmt.Errorf("%w：图标填充规则无效 %q", errContent, icon.Rule)
	}
	height := ptToMM(p.style.Size)
	scale := height / icon.Height
	width := icon.Width * scale
	if math.IsNaN(width) || math.IsInf(width, 0) || width <= 0 || width+height*0.35 >= e.contentWidth {
		return p, fmt.Errorf("%w：图标宽度 %g 超过可用行宽 %g", errContent, width, e.contentWidth)
	}
	stroke := false
	p.icon = &creator.Path{Width: width, Height: height, Data: icon.Data, Rule: icon.Rule,
		Fill: true, FillColor: p.style.Color, StrokeSet: &stroke, CTM: &creator.CTM{scale, 0, 0, scale, 0, 0}}
	if p.block.callout != nil {
		p.icon.FillColor = p.block.callout.AccentColor
	}
	p.markerWidth = width + height*0.35
	return p, nil
}

func (e *engine) drawIcon(p *preparedBlock, line textLine, left, width, top float64) {
	icon := *p.icon
	x := left + line.indent
	switch p.style.Align {
	case "center":
		x += (width - line.indent - e.lineWidth(line.atoms)) / 2
	case "right":
		x = left + width - e.lineWidth(line.atoms)
	}
	icon.X = x - p.markerWidth
	icon.Y = e.geometry.PageHeight - top + (line.height-icon.Height)/2
	e.add(icon)
}
