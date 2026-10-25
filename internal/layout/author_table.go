package layout

import (
	"context"
	"fmt"
	"math"
	"sort"
)

const authorCellPadding = 1.5
const authorTableBorder = 0.2

type authorTable struct {
	splitRows             bool
	cells                 []authorCell
	rowCells              []int
	rowOffsets            []float64
	colOffsets            []float64
	headerRows            int
	groups                []authorRowGroup
	caption, continuation []authorLine
	captionStyle          TextStyle
}

type authorCell struct {
	row, col         int
	rowSpan, colSpan int
	lines            []authorLine
	style            TextStyle
	height           float64
	runs             []Inline
	appearance       FlowCellStyle
	noWrap           bool
}

type authorRowGroup struct {
	start, end int
	height     float64
}

func (t *authorTable) height() float64 {
	return t.rowOffsets[len(t.rowOffsets)-1] + t.captionHeight(false)
}

func (t *authorTable) captionHeight(continued bool) float64 {
	lines := t.caption
	if continued {
		lines = t.continuation
	}
	if len(lines) == 0 {
		return 0
	}
	h := t.captionStyle.SpaceBefore + t.captionStyle.SpaceAfter
	for _, line := range lines {
		h += line.height
	}
	return h
}

func (t *authorTable) minimumHeight() float64 {
	height := t.rowOffsets[t.headerRows] + t.captionHeight(false)
	if len(t.groups) > 0 {
		if t.splitRows {
			height += t.minimumRowFragment(t.groups[0].start).height()
		} else {
			height += t.groups[0].height
		}
	}
	return height
}

// authorGrid 把纯文本简写转换为同一个网格模型，避免维护两套分页行为。
func authorGrid(table *FlowTable) ([][]FlowTableCell, int, error) {
	if table.Grid != nil {
		if len(table.Header) > 0 || len(table.Rows) > 0 {
			return nil, 0, fmt.Errorf("grid 与 header/rows 不能同时设置")
		}
		if len(table.Grid) == 0 {
			return nil, 0, fmt.Errorf("grid 没有行")
		}
		if table.HeaderRows < 0 || table.HeaderRows > len(table.Grid) {
			return nil, 0, fmt.Errorf("header_rows 超出表格行数")
		}
		return table.Grid, table.HeaderRows, nil
	}
	if table.HeaderRows != 0 {
		return nil, 0, fmt.Errorf("header_rows 仅用于 grid")
	}
	rows := table.Rows
	headerRows := 0
	if len(table.Header) > 0 {
		rows = append([][]string{table.Header}, rows...)
		headerRows = 1
	}
	if len(rows) == 0 {
		return nil, 0, fmt.Errorf("表格没有行")
	}
	grid := make([][]FlowTableCell, len(rows))
	for r, row := range rows {
		for _, text := range row {
			grid[r] = append(grid[r], FlowTableCell{Text: text})
		}
	}
	return grid, headerRows, nil
}

func (e *engine) prepareAuthorTable(p authorBlock) (authorBlock, error) {
	grid, headers, err := authorGrid(p.block.Table)
	if err != nil {
		return p, err
	}
	cols := len(p.block.Table.ColumnWidths)
	if len(p.block.Table.Columns) > 0 {
		if cols > 0 {
			return p, fmt.Errorf("columns 与 column_widths 互斥")
		}
		cols = len(p.block.Table.Columns)
	}
	if cols == 0 {
		for _, cell := range grid[0] {
			span := cell.ColSpan
			if span < 0 {
				return p, fmt.Errorf("grid[0]: col_span 不能为负数")
			}
			if span == 0 {
				span = 1
			}
			// 根据版心限制推导的列数，避免无效跨度导致分配巨大的网格。
			if span > max(1, int(e.contentWidth/authorTableBorder))-cols {
				return p, fmt.Errorf("列数超过版心可容纳的范围")
			}
			cols += span
		}
	}
	if cols == 0 {
		return p, fmt.Errorf("表格没有列")
	}
	t := &authorTable{splitRows: p.block.Table.SplitRows, headerRows: headers, colOffsets: make([]float64, cols+1), rowCells: make([]int, len(grid)+1)}
	if err := e.prepareTableCaptions(t, p.block.Table); err != nil {
		return p, err
	}
	occupied := make([][]bool, len(grid))
	heights := make([]float64, len(grid))
	rowEnds := make([]int, len(grid))
	for r := range grid {
		occupied[r] = make([]bool, cols)
		heights[r] = 2 * authorCellPadding
		rowEnds[r] = r + 1
	}
	for r, row := range grid {
		if err := e.author.ctx.Err(); err != nil {
			return p, err
		}
		col := 0
		for c, cell := range row {
			for col < cols && occupied[r][col] {
				col++
			}
			rowSpan, colSpan := cell.RowSpan, cell.ColSpan
			if rowSpan < 0 || colSpan < 0 {
				return p, fmt.Errorf("grid[%d][%d]: row_span/col_span 不能为负数", r, c)
			}
			if rowSpan == 0 {
				rowSpan = 1
			}
			if colSpan == 0 {
				colSpan = 1
			}
			if colSpan > cols-col || rowSpan > len(grid)-r {
				return p, fmt.Errorf("grid[%d][%d]: 合并跨度超出网格", r, c)
			}
			if r < headers && rowSpan > headers-r {
				return p, fmt.Errorf("grid[%d][%d]: 合并单元格不能跨越表头与正文边界", r, c)
			}
			if t.splitRows && r >= headers && rowSpan > 1 {
				return p, fmt.Errorf("grid[%d][%d]: split_rows 暂不支持正文 row_span 大于 1", r, c)
			}
			for y := r; y < r+rowSpan; y++ {
				for x := col; x < col+colSpan; x++ {
					if occupied[y][x] {
						return p, fmt.Errorf("grid[%d][%d]: 合并单元格重叠", r, c)
					}
					occupied[y][x] = true
				}
			}
			if cell.Text != "" && len(cell.Runs) > 0 {
				return p, fmt.Errorf("grid[%d][%d]: text 与 runs 不能同时设置", r, c)
			}
			style := p.style
			style.SpaceBefore, style.SpaceAfter, style.FirstLineIndent = 0, 0, 0
			if len(p.block.Table.Columns) > 0 && p.block.Table.Columns[col].Align != "" {
				style.Align = p.block.Table.Columns[col].Align
			}
			if cell.Style != nil {
				style = mergeTextStyle(style, *cell.Style)
			}
			if err := validateTextStyle(style); err != nil {
				return p, fmt.Errorf("grid[%d][%d].style: %w", r, c, err)
			}
			if e.author.faces[style.Font] == nil {
				return p, fmt.Errorf("grid[%d][%d].style: 字体 %q 未注册", r, c, style.Font)
			}
			runs := append([]Inline(nil), cell.Runs...)
			if cell.Text != "" {
				runs = []Inline{{Text: cell.Text}}
			}
			if r < headers {
				for i := range runs {
					runs[i].Bold = true
				}
			}
			appearance, err := e.tableCellAppearance(p.block.Table, cell.Appearance, r, headers)
			if err != nil {
				return p, fmt.Errorf("grid[%d][%d].appearance: %w", r, c, err)
			}
			noWrap := false
			for x := col; x < col+colSpan && len(p.block.Table.Columns) > 0; x++ {
				noWrap = noWrap || p.block.Table.Columns[x].NoWrap
			}
			t.cells = append(t.cells, authorCell{row: r, col: col, rowSpan: rowSpan, colSpan: colSpan, runs: runs, style: style, appearance: appearance, noWrap: noWrap})
			rowEnds[r] = max(rowEnds[r], r+rowSpan)
			col += colSpan
		}
		for c, full := range occupied[r] {
			if !full {
				return p, fmt.Errorf("grid[%d]: 第 %d 列未被单元格覆盖", r, c)
			}
		}
		t.rowCells[r+1] = len(t.cells)
	}
	if err := e.sizeAuthorColumns(t, p.block.Table); err != nil {
		return p, err
	}
	for i := range t.cells {
		cell := &t.cells[i]
		padding := cell.appearance.Padding
		width := t.colOffsets[cell.col+cell.colSpan] - t.colOffsets[cell.col] - padding.Left - padding.Right
		if width <= cell.style.FirstLineIndent {
			return p, fmt.Errorf("grid[%d] 第 %d 列: 缩进或内边距超过单元格宽度", cell.row, cell.col)
		}
		lineWidth := width
		if cell.noWrap {
			lineWidth = math.MaxFloat64
		}
		cell.lines = e.authorLines(cell.runs, cell.style, lineWidth, cell.style.FirstLineIndent)
		cell.height = padding.Top + padding.Bottom + cell.style.SpaceBefore + cell.style.SpaceAfter
		for _, line := range cell.lines {
			if e.lineWidth(line.atoms)+line.indent > width+0.001 {
				return p, fmt.Errorf("grid[%d] 第 %d 列: 文本超过列宽", cell.row, cell.col)
			}
			cell.height += line.height
		}
		if cell.rowSpan == 1 {
			heights[cell.row] = math.Max(heights[cell.row], cell.height)
		}
	}
	// 先满足普通行高，再按跨度从小到大补足合并单元格需要的高度。
	order := make([]int, len(t.cells))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool { return t.cells[order[i]].rowSpan < t.cells[order[j]].rowSpan })
	for _, index := range order {
		cell := t.cells[index]
		if cell.rowSpan == 1 {
			continue
		}
		height := 0.0
		for r := cell.row; r < cell.row+cell.rowSpan; r++ {
			height += heights[r]
		}
		if height < cell.height {
			extra := (cell.height - height) / float64(cell.rowSpan)
			for r := cell.row; r < cell.row+cell.rowSpan; r++ {
				heights[r] += extra
			}
		}
	}
	t.rowOffsets = make([]float64, len(grid)+1)
	for r, height := range heights {
		t.rowOffsets[r+1] = t.rowOffsets[r] + height
	}
	for start := headers; start < len(grid); {
		end := start + 1
		for r := start; r < end; r++ {
			end = max(end, rowEnds[r])
		}
		group := authorRowGroup{start: start, end: end, height: t.rowOffsets[end] - t.rowOffsets[start]}
		captionHeight := t.captionHeight(true)
		if start == headers {
			captionHeight = t.captionHeight(false)
		}
		if !t.splitRows && group.height+t.rowOffsets[headers]+captionHeight > e.contentHeight+0.001 {
			return p, fmt.Errorf("grid[%d:%d]: 行组与表头总高超过整页版心", start, end)
		}
		t.groups = append(t.groups, group)
		start = end
	}
	if t.rowOffsets[headers]+t.captionHeight(false) > e.contentHeight+0.001 {
		return p, fmt.Errorf("表头高度超过整页版心")
	}
	p.table = t
	return p, nil
}

func (e *engine) placeAuthorTable(ctx context.Context, p *authorBlock) error {
	t := p.table
	if t.splitRows {
		return e.placeAuthorSplitTable(ctx, p)
	}
	end := t.headerRows
	if len(t.groups) > 0 {
		end = t.groups[0].end
	}
	firstLines := t.noteLines(0, end)
	plan, err := e.planAuthorFootnotes(firstLines, e.currentAuthorFootnotes())
	if err != nil {
		return err
	}
	gap := 0.0
	if e.authorHasContent() {
		gap = p.style.SpaceBefore
	}
	if e.y-t.minimumHeight()-gap < e.authorFlowBottom()+plan.height-0.001 && e.authorHasContent() {
		e.nextAuthorColumn()
		gap = 0
		plan, err = e.planAuthorFootnotes(firstLines, e.currentAuthorFootnotes())
		if err != nil {
			return err
		}
	}
	if e.y-t.minimumHeight()-gap < e.authorFlowBottom()+plan.height-0.001 {
		return fmt.Errorf("表题、表头、首个行组与脚注无法同时放入整页版心")
	}
	e.commitAuthorFootnotes(plan)
	if e.authorHasContent() {
		e.space(p.style.SpaceBefore)
	}
	e.recordAuthorAnchor(p.block.ID)
	if e.author.opts.Official != nil && e.pageIndex == 0 {
		e.author.officialFirstBody = true
	}
	e.drawTableCaption(t, false)
	if t.headerRows > 0 {
		e.drawAuthorTableRows(t, 0, t.headerRows)
	}
	for _, group := range t.groups {
		if err := ctx.Err(); err != nil {
			return err
		}
		lines := t.noteLines(group.start, group.end)
		plan, err := e.planAuthorFootnotes(lines, e.currentAuthorFootnotes())
		if err != nil {
			return err
		}
		if e.y-group.height < e.authorFlowBottom()+plan.height-0.001 {
			e.nextAuthorColumn()
			lines = append(t.noteLines(0, t.headerRows), lines...)
			plan, err = e.planAuthorFootnotes(lines, e.currentAuthorFootnotes())
			if err != nil {
				return err
			}
			need := group.height + t.rowOffsets[t.headerRows] + t.captionHeight(true)
			if e.y-need < e.authorFlowBottom()+plan.height-0.001 {
				return fmt.Errorf("grid[%d:%d]: 行组、续表题、表头与脚注无法同时放入整页版心", group.start, group.end)
			}
			e.commitAuthorFootnotes(plan)
			e.drawTableCaption(t, true)
			if t.headerRows > 0 {
				e.drawAuthorTableRows(t, 0, t.headerRows)
			}
		} else {
			e.commitAuthorFootnotes(plan)
		}
		e.drawAuthorTableRows(t, group.start, group.end)
	}
	e.space(p.style.SpaceAfter)
	return e.author.err
}

// 每个单元格独立生成标准路径边框，合并区域内部不生成分隔线。
// 背景与边框使用 GB/T 33190—2016 §9.1 的填充路径，坐标按 §8.1 转为页面空间。
func (e *engine) drawAuthorTableRows(t *authorTable, start, end int) {
	top := e.y
	for _, cell := range t.cells[t.rowCells[start]:t.rowCells[end]] {
		x := e.contentLeft + t.colOffsets[cell.col]
		y := top - (t.rowOffsets[cell.row] - t.rowOffsets[start])
		width := t.colOffsets[cell.col+cell.colSpan] - t.colOffsets[cell.col]
		height := t.rowOffsets[cell.row+cell.rowSpan] - t.rowOffsets[cell.row]
		e.drawAuthorTableCell(cell, x, y, width, height)
	}
	e.y = top - (t.rowOffsets[end] - t.rowOffsets[start])
}

func (e *engine) drawAuthorTableCell(cell authorCell, x, y, width, height float64) {
	if cell.appearance.Fill != nil {
		e.fillRect(x, y-height, width, height, cell.appearance.Fill)
	}
	padding := cell.appearance.Padding
	textTop := y - padding.Top - cell.style.SpaceBefore
	slack := math.Max(0, height-cell.height)
	switch cell.appearance.VerticalAlign {
	case "middle":
		textTop -= slack / 2
	case "bottom":
		textTop -= slack
	}
	for _, line := range cell.lines {
		e.drawAuthorLine(line, cell.style, x+padding.Left, width-padding.Left-padding.Right, textTop)
		textTop -= line.height
	}
	b := cell.appearance.Borders
	if b.Left.Width > 0 {
		e.fillRect(x, y-height, math.Min(b.Left.Width, width), height, b.Left.Color)
	}
	if b.Right.Width > 0 {
		w := math.Min(b.Right.Width, width)
		e.fillRect(x+width-w, y-height, w, height, b.Right.Color)
	}
	if b.Top.Width > 0 {
		h := math.Min(b.Top.Width, height)
		e.fillRect(x, y-h, width, h, b.Top.Color)
	}
	if b.Bottom.Width > 0 {
		e.fillRect(x, y-height, width, math.Min(b.Bottom.Width, height), b.Bottom.Color)
	}
}
