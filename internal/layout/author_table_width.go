package layout

import (
	"fmt"
	"math"
	"sort"
)

type authorColumnConstraint struct {
	col, span, row     int
	minimum, preferred float64
}

func (e *engine) sizeAuthorColumns(t *authorTable, table *FlowTable) error {
	n := len(t.colOffsets) - 1
	if len(table.Columns) == 0 {
		for i := 0; i < n; i++ {
			w := e.contentWidth / float64(n)
			if len(table.ColumnWidths) > 0 {
				w = table.ColumnWidths[i]
			}
			if w <= 0 || math.IsNaN(w) || math.IsInf(w, 0) {
				return fmt.Errorf("列宽必须为有限正数")
			}
			t.colOffsets[i+1] = t.colOffsets[i] + w
		}
		if t.colOffsets[n] > e.contentWidth+0.001 {
			return fmt.Errorf("表格总宽超过版心")
		}
		return nil
	}
	widths, limits, weights := make([]float64, n), make([]float64, n), make([]float64, n)
	for i, c := range table.Columns {
		for _, v := range []float64{c.Width, c.MinWidth, c.MaxWidth, c.Weight} {
			if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
				return fmt.Errorf("columns[%d]: 尺寸与比例必须为有限非负数", i)
			}
		}
		if c.Align != "" && c.Align != "left" && c.Align != "center" && c.Align != "right" && c.Align != "justify" {
			return fmt.Errorf("columns[%d]: align 必须为 left、center、right 或 justify", i)
		}
		widths[i], limits[i], weights[i] = math.Max(c.MinWidth, 0.001), math.MaxFloat64, c.Weight
		if weights[i] == 0 {
			weights[i] = 1
		}
		if c.MaxWidth > 0 {
			limits[i] = c.MaxWidth
		}
		if c.Width > 0 {
			if c.Width < c.MinWidth || c.Width > limits[i] {
				return fmt.Errorf("columns[%d]: 固定宽超出 min_width/max_width", i)
			}
			widths[i], limits[i] = c.Width, c.Width
		}
		if widths[i] > limits[i] {
			return fmt.Errorf("columns[%d]: min_width 超过 max_width", i)
		}
	}
	maxWeight := 0.0
	for _, weight := range weights {
		maxWeight = math.Max(maxWeight, weight)
	}
	for i := range weights {
		weights[i] /= maxWeight
	}
	lower := append([]float64(nil), widths...)
	constraints := make([]authorColumnConstraint, 0, len(t.cells))
	for _, cell := range t.cells {
		padding := cell.appearance.Padding.Left + cell.appearance.Padding.Right
		minimum, preferred := 0.0, 0.0
		for _, line := range e.authorLines(cell.runs, cell.style, math.MaxFloat64, cell.style.FirstLineIndent) {
			preferred = math.Max(preferred, e.lineWidth(line.atoms)+line.indent)
		}
		if cell.noWrap {
			minimum = preferred
		} else {
			for _, line := range e.authorLines(cell.runs, cell.style, 0, cell.style.FirstLineIndent) {
				minimum = math.Max(minimum, e.lineWidth(line.atoms)+line.indent)
			}
		}
		constraints = append(constraints, authorColumnConstraint{col: cell.col, span: cell.colSpan, row: cell.row,
			minimum: math.Max(minimum, cell.style.FirstLineIndent+0.001) + padding, preferred: math.Max(preferred, cell.style.FirstLineIndent+0.001) + padding})
	}
	// 先处理普通列，再处理合并区域；测量和正式断行共用实际字体与 UAX #14。
	sort.SliceStable(constraints, func(i, j int) bool { return constraints[i].span < constraints[j].span })
	var unsatisfied *authorColumnConstraint
	for _, c := range constraints {
		current := sumAuthorWidths(widths[c.col : c.col+c.span])
		if current < c.minimum {
			growAuthorWidths(widths[c.col:c.col+c.span], limits[c.col:c.col+c.span], weights[c.col:c.col+c.span], c.minimum-current)
		}
		if sumAuthorWidths(widths[c.col:c.col+c.span])+0.001 < c.minimum {
			unsatisfied = &c
			break
		}
	}
	// 重叠的跨列约束可以共享中间列；局部按比例增长可能占用不必要的总宽。
	// 必要时以列边界的差分约束重新求最小可行宽度，避免误报版心不足。
	if unsatisfied != nil || sumAuthorWidths(widths) > e.contentWidth+0.001 {
		if feasible, ok := minimumAuthorColumns(lower, limits, constraints, e.contentWidth); ok {
			widths = feasible
		} else if unsatisfied != nil {
			return fmt.Errorf("grid[%d] 第 %d 列: 不可断内容或内边距超过列宽上限", unsatisfied.row, unsatisfied.col)
		} else {
			return fmt.Errorf("自动列宽所需最小总宽超过版心，请调整字号、内边距或不换行设置")
		}
	}
	budget := e.contentWidth - sumAuthorWidths(widths)
	if budget < -0.001 {
		return fmt.Errorf("自动列宽所需最小总宽超过版心，请调整字号、内边距或不换行设置")
	}
	preferred := append([]float64(nil), widths...)
	for _, c := range constraints {
		growAuthorWidths(preferred[c.col:c.col+c.span], limits[c.col:c.col+c.span], weights[c.col:c.col+c.span], c.preferred-sumAuthorWidths(preferred[c.col:c.col+c.span]))
	}
	// 向理想宽度增长时以缺口乘比例为权重，避免首行表头独占余量。
	desire := make([]float64, n)
	for i := range desire {
		desire[i] = math.Max(0, preferred[i]-widths[i]) * weights[i]
	}
	budget -= growAuthorWidths(widths, preferred, desire, math.Max(0, budget))
	growAuthorWidths(widths, limits, weights, math.Max(0, budget))
	for i, w := range widths {
		t.colOffsets[i+1] = t.colOffsets[i] + w
	}
	return nil
}

// prefix 是从表格左边界起的列边界。每个约束都写成 prefix[to] >= prefix[from]+distance，
// 通过最长路径松弛满足单列上下限和全部合并区域的最小宽度；正环表示约束冲突。
func minimumAuthorColumns(lower, upper []float64, constraints []authorColumnConstraint, available float64) ([]float64, bool) {
	n := len(lower)
	prefix := make([]float64, n+1)
	for i, w := range lower {
		prefix[i+1] = prefix[i] + w
	}
	for pass := 0; pass <= n; pass++ {
		changed := false
		relax := func(from, to int, distance float64) {
			if next := prefix[from] + distance; prefix[to]+0.000001 < next {
				prefix[to] = next
				changed = true
			}
		}
		for i, w := range lower {
			relax(i, i+1, w)
		}
		for _, c := range constraints {
			relax(c.col, c.col+c.span, c.minimum)
		}
		for i := n - 1; i >= 0; i-- {
			relax(i+1, i, -math.Min(upper[i], available))
		}
		if prefix[0] > 0.001 || prefix[n] > available+0.001 {
			return nil, false
		}
		if !changed {
			widths := make([]float64, n)
			for i := range widths {
				widths[i] = prefix[i+1] - prefix[i]
			}
			return widths, true
		}
	}
	return nil, false
}

func sumAuthorWidths(widths []float64) float64 {
	total := 0.0
	for _, w := range widths {
		total += w
	}
	return total
}

// 按权重分配宽度，触达上限的列退出下一轮；最多进行列数加一轮。
func growAuthorWidths(widths, limits, weights []float64, amount float64) float64 {
	remaining := amount
	for pass := 0; pass <= len(widths) && remaining > 0.000001; pass++ {
		total := 0.0
		for i, w := range widths {
			if w < limits[i]-0.000001 {
				total += weights[i]
			}
		}
		if total == 0 {
			break
		}
		used := 0.0
		for i, w := range widths {
			if w >= limits[i]-0.000001 {
				continue
			}
			extra := math.Min(limits[i]-w, remaining*weights[i]/total)
			widths[i] += extra
			used += extra
		}
		remaining -= used
	}
	return amount - remaining
}
