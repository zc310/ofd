package layout

import (
	"github.com/zc310/ofd/pkg/creator"
)

// creatorTableStyle 是 Markdown 表格转成 creator.Table 后的样式，取值与此前
// drawTableRow 手画时一致：表头浅灰底、0.2mm 边框、竖线。
func creatorTableStyle(headerFont string, headerSize float64, headerWeight int, codeFont string) creator.TableStyle {
	return creator.TableStyle{
		HorizontalLines: true,
		VerticalLines:   true,
		LineWidth:       0.2,
		OuterLineWidth:  0.2,
		StrokeColor:     colorBorder,
		HeaderFillColor: colorHeaderBG,
		HeaderFont:      headerFont,
		HeaderSize:      headerSize,
		HeaderWeight:    headerWeight,
		CodeFont:        codeFont,
	}
}

// creatorTableAlign 把列对齐方式转成 creator.TableCell 的取值。
func creatorTableAlign(align Align) string {
	switch align {
	case AlignCenter:
		return "center"
	case AlignRight:
		return "right"
	default:
		return "left"
	}
}

// creatorCellRuns 把一个单元格的行内片段转成 creator.TextRun。
//
// 走 TextRun 而不是拼 markdown 源码：layout 的 Inline 已经是解析结果，原文里的
// `*` `_` `[` 是普通字符，拼回源码再让创建器解析一遍会把 `a*b` 变成斜体、把链接
// 打坏。
func (e *engine) creatorCellRuns(cell Cell, base metricKey) []creator.TextRun {
	runs := make([]creator.TextRun, 0, len(cell))
	for _, inline := range cell {
		if inline.Text == "" {
			continue
		}
		runs = append(runs, creator.TextRun{
			Text:   inline.Text,
			Bold:   inline.Bold || base.bold,
			Italic: inline.Italic || base.italic,
			Code:   inline.Code || base.mono,
			Strike: inline.Strike,
			Href:   inline.Href,
		})
	}
	return runs
}

// creatorTableRow 把一行单元格转成 creator.TableRow。
func (e *engine) creatorTableRow(cells []Cell, cols int, aligns []Align, header bool, sizeMM float64) creator.TableRow {
	row := creator.TableRow{Cells: make([]creator.TableCell, 0, cols)}
	base := metricKey{bold: header}
	for index := 0; index < cols; index++ {
		var cell Cell
		if index < len(cells) {
			cell = cells[index]
		}
		align := AlignLeft
		if index < len(aligns) {
			align = aligns[index]
		}
		row.Cells = append(row.Cells, creator.TableCell{
			Runs:  e.creatorCellRuns(cell, base),
			Font:  e.fontName(base),
			Size:  sizeMM,
			Align: creatorTableAlign(align),
		})
	}
	return row
}

// creatorTableFor 把 layout.Table 转成整张 creator.Table。
func (e *engine) creatorTableFor(table *Table, widths []float64, sizeMM float64) creator.Table {
	cols := len(widths)
	rows := make([]creator.TableRow, 0, len(table.Rows)+1)
	if len(table.Header) > 0 {
		rows = append(rows, e.creatorTableRow(table.Header, cols, table.Align, true, sizeMM))
	}
	for _, row := range table.Rows {
		rows = append(rows, e.creatorTableRow(row, cols, table.Align, false, sizeMM))
	}
	return creator.Table{
		X:            e.contentLeft,
		Y:            0,
		Width:        e.contentWidth,
		ColumnWidths: append([]float64(nil), widths...),
		HeaderRows:   map[bool]int{true: 1}[len(table.Header) > 0],
		Rows:         rows,
		Style: creatorTableStyle(e.fontName(metricKey{bold: true}), sizeMM, 700,
			e.fontName(metricKey{mono: true})),
	}
}

// fitTableRows 返回在 budget 毫米内放得下的最大数据行数（不含表头），二分查找。
//
// 行高必须由 creator.TableHeight 量，不能自己估：估算口径与创建器落笔时的折行
// 不一致时，分片点会算多或算少，最后一页溢出页底或提前留白。用二分是因为行高对
// 行数单调。
func fitTableRows(base creator.Table, header []creator.TableRow, body []creator.TableRow,
	budget, pageWidth float64) int {
	if len(body) == 0 {
		return 0
	}
	height := func(count int) (float64, bool) {
		rows := make([]creator.TableRow, 0, len(header)+count)
		rows = append(rows, header...)
		rows = append(rows, body[:count]...)
		probe := base
		probe.Rows = rows
		value, err := creator.TableHeight(probe, pageWidth)
		if err != nil {
			return 0, false
		}
		return value, true
	}
	low, high := 1, len(body)
	best := 0
	for low <= high {
		mid := (low + high) / 2
		if value, ok := height(mid); ok && value <= budget {
			best, low = mid, mid+1
		} else {
			high = mid - 1
		}
	}
	return best
}

// emitCreatorTable 把表格按页高分片，逐片沿排版游标落下。
//
// 分片由 layout 做而不是交给 creator：创建器的跨页切分发生在 prepare 阶段的
// PageProvider 上，那时页数已经定死，而 layout 是流式游标，必须知道这张表实际
// 占了多高（含分页）才能摆后面的块。这样做换来的是续页重复表头——续片同样带表头
// 行，与创建器自己切分时的行为一致。
func (e *engine) emitCreatorTable(table creator.Table) {
	if len(table.Rows) == 0 {
		return
	}
	if table.HeaderRows <= 0 || table.HeaderRows >= len(table.Rows) {
		e.placeCreatorTable(table, table.Rows)
		return
	}
	header := table.Rows[:table.HeaderRows]
	body := table.Rows[table.HeaderRows:]
	for len(body) > 0 {
		fit := fitTableRows(table, header, body, e.y-e.contentBottom, e.opts.PageWidth)
		if fit == 0 && (e.pageIndex < 0 || len(e.cur().Items) > e.pageBase) {
			// 当前页放不下：换页后重新量。换页后必须重算而不是直接放一行，否则会
			// 在同一页上先放一个只含表头加一行的分片、再放一个正常分片，表头就出现
			// 两次。尚未开页时（pageIndex 为 -1、游标还是 0）也要走这里，否则第一片
			// 会带着负的可用高度被强行塞进新开的第一页。
			e.newPage()
			continue
		}
		if fit == 0 {
			// 空页仍放不下一行：只能放一行，越界交给创建器报错。
			fit = 1
		}
		fragment := make([]creator.TableRow, 0, len(header)+fit)
		fragment = append(fragment, header...)
		fragment = append(fragment, body[:fit]...)
		e.placeCreatorTable(table, fragment)
		body = body[fit:]
	}
}

// placeCreatorTable 把一张放得下单页的表格落到当前游标并前进。
func (e *engine) placeCreatorTable(base creator.Table, rows []creator.TableRow) {
	table := base
	table.Rows = rows
	height, err := creator.TableHeight(table, e.opts.PageWidth)
	if err != nil {
		// 高度算不出来（列数与行数不一致等）时按 0 落，仍交给创建器报错，
		// 不在这里再叠一层校验错误。
		height = 0
	}
	e.ensureHeight(height)
	// 游标是自底向上量到页底的，creator 的图元 Y 自页面顶端起算。
	table.Y = e.opts.PageHeight - e.y
	e.add(table)
	e.y -= height
}
