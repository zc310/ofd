package layout

import (
	"fmt"
	"math"
)

func (e *engine) tableCellAppearance(table *FlowTable, cell *FlowCellStyle, row, headers int) (FlowCellStyle, error) {
	padding := FlowMargins{Top: authorCellPadding, Right: authorCellPadding, Bottom: authorCellPadding, Left: authorCellPadding}
	border := FlowTableBorder{Width: authorTableBorder, Color: e.author.opts.Theme.BorderColor}
	borders := FlowCellBorders{Top: &border, Right: &border, Bottom: &border, Left: &border}
	style := FlowCellStyle{Padding: &padding, Borders: &borders, VerticalAlign: "top"}
	if row < headers {
		style.Fill = e.author.opts.Theme.HeaderFill
	} else if (row-headers)%2 == 1 {
		style.Fill = table.StripeFill
	}
	for _, s := range []*FlowCellStyle{table.CellStyle, cell} {
		if s == nil {
			continue
		}
		if s.Fill != nil {
			style.Fill = s.Fill
		}
		if s.Padding != nil {
			padding = *s.Padding
		}
		if s.VerticalAlign != "" {
			style.VerticalAlign = s.VerticalAlign
		}
		if s.Borders != nil {
			for i, source := range []*FlowTableBorder{s.Borders.Top, s.Borders.Right, s.Borders.Bottom, s.Borders.Left} {
				if source == nil {
					continue
				}
				if math.IsNaN(source.Width) || math.IsInf(source.Width, 0) || source.Width < 0 {
					return style, fmt.Errorf("边框宽度必须为有限非负数")
				}
				b := *source
				if b.Color == nil {
					b.Color = border.Color
				}
				*[]**FlowTableBorder{&borders.Top, &borders.Right, &borders.Bottom, &borders.Left}[i] = &b
			}
		}
		// 即使被单元格覆盖，无效的表格默认设置也应报错。
		if s.Padding != nil {
			for _, v := range []float64{s.Padding.Top, s.Padding.Right, s.Padding.Bottom, s.Padding.Left} {
				if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
					return style, fmt.Errorf("内边距必须为有限非负数")
				}
			}
		}
		switch s.VerticalAlign {
		case "", "top", "middle", "bottom":
		default:
			return style, fmt.Errorf("vertical_align 必须为 top、middle 或 bottom")
		}
	}
	return style, nil
}
