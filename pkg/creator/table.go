package creator

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

const (
	defaultTableLineWidth   = 0.3
	defaultTableCellPadding = 3.0
	defaultCaptionSize      = 5.0
	// defaultCaptionGap 是说明文字与它所标注对象之间的默认间距（毫米）。
	// 正数，处在对象哪一侧由上下文决定：表格表题在上，图片说明在下。
	defaultCaptionGap = 6.0
	// textBaselineRatio 是汉字视觉中心相对基线的比例（基线上方），
	// 用于让单元格文字在行内视觉居中。
	textBaselineRatio = 0.37
	// strikeBaselineRatio 是删除线相对基线的位置（基线上方），落在字身中部。
	strikeBaselineRatio = 0.32
	// strikeLineWidthRatio 是删除线宽相对字号的比例。
	strikeLineWidthRatio = 0.06
	// ruleBaselineOffset 是分隔线相对基线的位置（基线上方，单位为字号倍数），
	// 落在行内上下留白的中间。
	ruleBaselineOffset = 0.75
	// ruleLineWidthRatio 是分隔线宽相对字号的比例。分隔线只是块内的视觉分界，
	// 不该和表格边框争分量，所以比删除线还细一档，4.2mm 字号下约 0.21mm。
	ruleLineWidthRatio = 0.05
	// ruleColorGray 是分隔线的默认灰度（三通道同值）。
	ruleColorGray = 160
)

type tableLayout struct {
	table      Table
	pageWidth  float64
	pageHeight float64
	colX       []float64
	colWidths  []float64
	rowY       []float64
	rowHeights []float64
	occupied   [][]bool
	owner      [][]cellRef
	numCols    int
	segments   []tableSegment
}

// cellRef 记录网格位置所属单元格的起始行列，用于判断合并单元格内部边界。
type cellRef struct {
	row int
	col int
}

type tableSegment struct {
	startRow       int
	endRow         int
	isFirst        bool
	isContinuation bool
}

func layoutTable(table Table, pageWidth, pageHeight float64) (*tableLayout, error) {
	if len(table.Rows) == 0 {
		return nil, errors.New("table has no rows")
	}

	l := &tableLayout{
		table:      table,
		pageWidth:  pageWidth,
		pageHeight: pageHeight,
	}

	if err := l.calculateColumns(); err != nil {
		return nil, err
	}
	l.calculateOccupied()
	l.calculateRows()
	l.calculateSegments()

	return l, nil
}

func (l *tableLayout) calculateColumns() error {
	l.numCols = 0
	for _, row := range l.table.Rows {
		col := 0
		for _, cell := range row.Cells {
			span := cell.ColSpan
			if span < 1 {
				span = 1
			}
			col += span
		}
		if col > l.numCols {
			l.numCols = col
		}
	}
	if l.numCols == 0 {
		return fmt.Errorf("表格没有有效的列")
	}

	if len(l.table.ColumnWidths) > 0 {
		if len(l.table.ColumnWidths) < l.numCols {
			return fmt.Errorf("ColumnWidths 数量 %d 少于表格列数 %d", len(l.table.ColumnWidths), l.numCols)
		}
		l.colWidths = make([]float64, l.numCols)
		copy(l.colWidths, l.table.ColumnWidths[:l.numCols])
	} else {
		l.colWidths = make([]float64, l.numCols)
		equalWidth := l.table.Width / float64(l.numCols)
		for i := range l.colWidths {
			l.colWidths[i] = equalWidth
		}
	}

	l.colX = make([]float64, l.numCols+1)
	l.colX[0] = l.table.X
	for i := 0; i < l.numCols; i++ {
		l.colX[i+1] = l.colX[i] + l.colWidths[i]
	}

	return nil
}

func (l *tableLayout) calculateOccupied() {
	numRows := len(l.table.Rows)
	l.occupied = make([][]bool, numRows)
	l.owner = make([][]cellRef, numRows)
	for i := range l.occupied {
		l.occupied[i] = make([]bool, l.numCols)
		l.owner[i] = make([]cellRef, l.numCols)
	}

	for rowIdx, row := range l.table.Rows {
		colIdx := 0
		for _, cell := range row.Cells {
			for colIdx < l.numCols && l.occupied[rowIdx][colIdx] {
				colIdx++
			}
			if colIdx >= l.numCols {
				break
			}
			colSpan := cell.ColSpan
			if colSpan < 1 {
				colSpan = 1
			}
			rowSpan := cell.RowSpan
			if rowSpan < 1 {
				rowSpan = 1
			}
			ref := cellRef{row: rowIdx, col: colIdx}
			for r := 0; r < rowSpan && rowIdx+r < numRows; r++ {
				for c := 0; c < colSpan && colIdx+c < l.numCols; c++ {
					l.occupied[rowIdx+r][colIdx+c] = true
					l.owner[rowIdx+r][colIdx+c] = ref
				}
			}
			colIdx += colSpan
		}
	}
}

// cellOwner 返回网格位置 (row,col) 所属单元格的起始行列；越界返回 {-1,-1}。
func (l *tableLayout) cellOwner(row, col int) cellRef {
	if row < 0 || row >= len(l.owner) || col < 0 || col >= l.numCols {
		return cellRef{row: -1, col: -1}
	}
	return l.owner[row][col]
}

func (l *tableLayout) calculateRows() {
	numRows := len(l.table.Rows)
	l.rowHeights = make([]float64, numRows)

	for rowIdx, row := range l.table.Rows {
		if row.Height > 0 {
			l.rowHeights[rowIdx] = row.Height
			continue
		}

		maxHeight := 0.0
		colIdx := 0
		for _, cell := range row.Cells {
			for colIdx < l.numCols && (l.occupied[rowIdx][colIdx] && !l.isCellStart(rowIdx, colIdx)) {
				colIdx++
			}
			if colIdx >= l.numCols {
				break
			}
			colSpan := cell.ColSpan
			if colSpan < 1 {
				colSpan = 1
			}
			rowSpan := cell.RowSpan
			if rowSpan < 1 {
				rowSpan = 1
			}

			cellWidth := 0.0
			for c := 0; c < colSpan && colIdx+c < l.numCols; c++ {
				cellWidth += l.colWidths[colIdx+c]
			}

			cellHeight := l.estimateCellHeight(cell, cellWidth, rowIdx < l.table.HeaderRows)
			if rowSpan == 1 && cellHeight > maxHeight {
				maxHeight = cellHeight
			}
			colIdx += colSpan
		}

		if maxHeight == 0 {
			maxHeight = l.estimateCellHeight(TableCell{Size: 4.2}, 20, rowIdx < l.table.HeaderRows)
		}
		l.rowHeights[rowIdx] = maxHeight
	}

	l.adjustRowSpanHeights()

	l.rowY = make([]float64, numRows+1)
	// table.Y 就是表格内容的顶边。表题在内容上方，占用内容之外的空间，
	// 不再把内容整体往下推——否则 table.Y 落在表题与内容之间的空档里，
	// 跨页切分与外部按 Y 定位都得绕开它。
	l.rowY[0] = l.table.Y
	for i := 0; i < numRows; i++ {
		l.rowY[i+1] = l.rowY[i] + l.rowHeights[i]
	}
}

func (l *tableLayout) isCellStart(row, col int) bool {
	for _, rowData := range l.table.Rows {
		colIdx := 0
		for _, cell := range rowData.Cells {
			for colIdx < l.numCols && l.occupied[row][colIdx] && colIdx < col {
				colIdx++
			}
			if colIdx == col {
				return true
			}
			colSpan := cell.ColSpan
			if colSpan < 1 {
				colSpan = 1
			}
			colIdx += colSpan
		}
	}
	return false
}

func (l *tableLayout) estimateCellHeight(cell TableCell, cellWidth float64, isHeader bool) float64 {
	size := cell.Size
	if size == 0 {
		size = 4.2
	}
	if isHeader && l.table.Style.HeaderSize > 0 {
		size = l.table.Style.HeaderSize
	}
	padding := defaultTableCellPadding
	firstWidth := cellWidth - 2*padding - cell.FirstLineIndent
	contWidth := cellWidth - 2*padding - cell.HangingIndent
	if firstWidth <= 0 {
		firstWidth = cellWidth
	}
	if contWidth <= 0 {
		contWidth = cellWidth
	}

	lines := len(wrapStyledRuns(cellRuns(cell), firstWidth, contWidth, size))

	// 行距取字号的 1.5 倍，容纳 ascent+descent（约 1.44 倍字号），
	// 避免字形上下溢出单元格；再额外留出 3mm 垂直余量。
	return float64(lines)*size*1.5 + 3.0
}

func (l *tableLayout) adjustRowSpanHeights() {
	for rowIdx := range l.table.Rows {
		colIdx := 0
		for _, cell := range l.table.Rows[rowIdx].Cells {
			for colIdx < l.numCols && (l.occupied[rowIdx][colIdx] && !l.isCellStart(rowIdx, colIdx)) {
				colIdx++
			}
			if colIdx >= l.numCols {
				break
			}
			rowSpan := cell.RowSpan
			if rowSpan < 1 {
				rowSpan = 1
			}
			if rowSpan > 1 {
				spanHeight := 0.0
				for r := 0; r < rowSpan && rowIdx+r < len(l.rowHeights); r++ {
					spanHeight += l.rowHeights[rowIdx+r]
				}
				cellHeight := l.estimateCellHeight(cell, l.cellWidth(colIdx, cell.ColSpan), rowIdx < l.table.HeaderRows)
				if cellHeight > spanHeight {
					if rowIdx+rowSpan-1 < len(l.rowHeights) {
						l.rowHeights[rowIdx+rowSpan-1] += cellHeight - spanHeight
					}
				}
			}
			colSpan := cell.ColSpan
			if colSpan < 1 {
				colSpan = 1
			}
			colIdx += colSpan
		}
	}
}

func (l *tableLayout) cellWidth(colIdx, colSpan int) float64 {
	width := 0.0
	for c := 0; c < colSpan && colIdx+c < l.numCols; c++ {
		width += l.colWidths[colIdx+c]
	}
	return width
}

// calculateSegments 按页底把表格切成若干段，每段各自不超页。
//
// 高度预算从表格内容顶边 table.Y 算起：表题在内容上方，不占页内高度，
// 所以这里与 rowY[0] 同值。续页还要为重复的表头行让出高度，那部分只有
// 重复表头时才出现。
//
// 切分点避开跨行合并单元格：把合并区域从中间切开，两侧都会缺半格边框。
func (l *tableLayout) calculateSegments() {
	numRows := len(l.table.Rows)
	if numRows == 0 {
		return
	}

	budget := l.pageHeight - l.rowY[0]
	if budget <= 0 {
		budget = l.pageHeight / 2
	}
	headerHeight := 0.0
	for rowIdx := 0; rowIdx < l.table.HeaderRows && rowIdx < numRows; rowIdx++ {
		headerHeight += l.rowHeights[rowIdx]
	}
	groupEnds := l.rowGroupEnds()

	startRow := 0
	for startRow < numRows {
		available := budget
		if startRow > 0 {
			// 续页顶部要重复表头，数据行只剩这么高。
			available = budget - headerHeight
			if available <= 0 {
				available = budget
			}
		}

		endRow := startRow
		used := 0.0
		for endRow < numRows && used+l.rowHeights[endRow] <= available {
			used += l.rowHeights[endRow]
			endRow++
		}
		if endRow == startRow {
			// 单行就比一整页还高：只能让它溢出，否则永远切不动。
			endRow = startRow + 1
		}
		if groupEnd := groupEnds[endRow-1]; groupEnd > endRow-1 {
			endRow = groupEnd + 1
		}

		l.segments = append(l.segments, tableSegment{
			startRow:       startRow,
			endRow:         endRow,
			isFirst:        startRow == 0,
			isContinuation: startRow > 0,
		})
		startRow = endRow
	}
}

// rowGroupEnds 返回每个行下标作为跨行合并单元格起点时，该单元格覆盖到的末行
// 下标；该行不是合并起点时返回它自己。
func (l *tableLayout) rowGroupEnds() []int {
	ends := make([]int, len(l.table.Rows))
	for rowIdx := range ends {
		ends[rowIdx] = rowIdx
	}
	for rowIdx, row := range l.table.Rows {
		for _, cell := range row.Cells {
			if cell.RowSpan < 2 {
				continue
			}
			end := rowIdx + cell.RowSpan - 1
			if end > len(ends)-1 {
				end = len(ends) - 1
			}
			if end > ends[rowIdx] {
				ends[rowIdx] = end
			}
		}
	}
	return ends
}

// tableFragments 把表格拆成各自不超页的片段表格：续页重复表头行，并把续表题
// 当作该片段的表题，于是每个片段都是一张完整表格，可以直接交给 layoutTable。
//
// 表格本来就放得下时只返回原表格，调用方据此判断页面无需拆分。
func (l *tableLayout) tableFragments() []Table {
	if len(l.segments) <= 1 {
		return []Table{l.table}
	}

	headerRows := l.table.HeaderRows
	fragments := make([]Table, 0, len(l.segments))
	for index, seg := range l.segments {
		fragment := l.table
		if index == 0 {
			fragment.Rows = l.table.Rows[:seg.endRow]
		} else {
			rows := make([]TableRow, 0, headerRows+seg.endRow-seg.startRow)
			rows = append(rows, l.table.Rows[:headerRows]...)
			rows = append(rows, l.table.Rows[seg.startRow:seg.endRow]...)
			fragment.Rows = rows
			caption, style := l.continuationCaption()
			fragment.Caption = caption
			fragment.CaptionStyle = style
			fragment.ContinuationCaption = ""
			fragment.ContinuationCaptionStyle = nil
		}
		fragments = append(fragments, fragment)
	}
	return fragments
}

// continuationCaption 返回续表题文字与样式，{caption} 占位符替换成原表题。
// 没有配置续表题时返回空串——续页就不带表题。
func (l *tableLayout) continuationCaption() (string, *CaptionStyle) {
	if l.table.ContinuationCaption == "" {
		return "", nil
	}
	caption := l.table.ContinuationCaption
	if strings.Contains(caption, "{caption}") {
		caption = strings.ReplaceAll(caption, "{caption}", l.table.Caption)
	}
	style := l.table.ContinuationCaptionStyle
	if style == nil {
		style = l.table.CaptionStyle
	}
	return caption, style
}

func (l *tableLayout) outerLineWidth() float64 {
	if l.table.Style.OuterLineWidth > 0 {
		return l.table.Style.OuterLineWidth
	}
	return l.table.Style.LineWidth
}

func (l *tableLayout) headerLineWidth() float64 {
	if l.table.Style.HeaderLineWidth > 0 {
		return l.table.Style.HeaderLineWidth
	}
	return l.table.Style.LineWidth
}

func (l *tableLayout) lineWidth() float64 {
	if l.table.Style.LineWidth > 0 {
		return l.table.Style.LineWidth
	}
	return defaultTableLineWidth
}

func (l *tableLayout) strokeColor() *Color {
	if l.table.Style.StrokeColor != nil {
		return l.table.Style.StrokeColor
	}
	return &Color{R: 0, G: 0, B: 0}
}

// strikeColor 返回删除线颜色，未指定时与表格边框同色——删除线是边框的延伸，
// 单独给一种颜色会让单元格里多出一处不一致的色调。
func (l *tableLayout) strikeColor() *Color {
	if l.table.Style.StrikeColor != nil {
		return l.table.Style.StrikeColor
	}
	return l.strokeColor()
}

func (l *tableLayout) generateBorderPaths(seg tableSegment) []Path {
	var paths []Path
	strokeColor := l.strokeColor()
	numRows := len(l.table.Rows)

	if l.table.Style.HorizontalLines {
		for row := seg.startRow; row <= seg.endRow; row++ {
			width := l.lineWidth()
			if row == 0 || row == numRows {
				width = l.outerWidth()
			} else if row == l.table.HeaderRows {
				width = l.headerWidth()
			}
			y := l.rowY[row]
			col := 0
			for col < l.numCols {
				if !l.needHorizontalSegment(row, col) {
					col++
					continue
				}
				startCol := col
				for col < l.numCols && l.needHorizontalSegment(row, col) {
					col++
				}
				paths = append(paths, l.createHorizontalLine(l.colX[startCol], y, l.colX[col], width, strokeColor))
			}
		}
	}

	if l.table.Style.VerticalLines {
		for col := 0; col <= l.numCols; col++ {
			width := l.lineWidth()
			if col == 0 || col == l.numCols {
				width = l.outerWidth()
			}
			x := l.colX[col]
			row := seg.startRow
			for row < seg.endRow {
				if !l.needVerticalSegment(row, col) {
					row++
					continue
				}
				startRow := row
				for row < seg.endRow && l.needVerticalSegment(row, col) {
					row++
				}
				paths = append(paths, l.createVerticalLine(x, l.rowY[startRow], l.rowY[row], width, strokeColor))
			}
		}
	}

	return paths
}

// needHorizontalSegment 判断行边界 row 处、列 col 的横线段是否绘制。
// 若上下相邻网格属于同一合并单元格，则该段是单元格内部线，不绘制。
func (l *tableLayout) needHorizontalSegment(row, col int) bool {
	if row == 0 || row == len(l.table.Rows) {
		return true
	}
	return l.cellOwner(row-1, col) != l.cellOwner(row, col)
}

// needVerticalSegment 判断列边界 col 处、行 row 的竖线段是否绘制。
// 若左右相邻网格属于同一合并单元格，则该段是单元格内部线，不绘制。
func (l *tableLayout) needVerticalSegment(row, col int) bool {
	if col == 0 || col == l.numCols {
		return true
	}
	return l.cellOwner(row, col-1) != l.cellOwner(row, col)
}

func (l *tableLayout) outerWidth() float64 {
	return l.outerLineWidth()
}

func (l *tableLayout) headerWidth() float64 {
	return l.headerLineWidth()
}

func (l *tableLayout) createHorizontalLine(x0, y, x1, width float64, color *Color) Path {
	data := fmt.Sprintf("M %.3f %.3f L %.3f %.3f C", 0.0, 0.0, x1-x0, 0.0)
	return Path{
		X: x0, Y: y, Width: x1 - x0, Height: width,
		Data:        data,
		Stroke:      true,
		StrokeColor: color,
		LineWidth:   width,
	}
}

func (l *tableLayout) createVerticalLine(x, y0, y1, width float64, color *Color) Path {
	data := fmt.Sprintf("M %.3f %.3f L %.3f %.3f C", 0.0, 0.0, 0.0, y1-y0)
	return Path{
		X: x, Y: y0, Width: width, Height: y1 - y0,
		Data:        data,
		Stroke:      true,
		StrokeColor: color,
		LineWidth:   width,
	}
}

// generateCellTexts 返回该段内的文字图元与删除线路径。两者按绘制顺序交替出现
// 才是对的：线要盖在文字上，但线在文字之前画会被 subsequently 的文字压住边缘，
// 所以实际写出时先画全部文字、再画全部删除线。
func (l *tableLayout) generateCellTexts(seg tableSegment) ([]Text, []Path, []Path) {
	var texts []Text
	var strikes []Path
	var rules []Path
	padding := defaultTableCellPadding

	for rowIdx := seg.startRow; rowIdx < seg.endRow; rowIdx++ {
		row := l.table.Rows[rowIdx]
		colIdx := 0
		for _, cell := range row.Cells {
			for colIdx < l.numCols && (l.occupied[rowIdx][colIdx] && !l.isCellStart(rowIdx, colIdx)) {
				colIdx++
			}
			if colIdx >= l.numCols {
				break
			}
			colSpan := cell.ColSpan
			if colSpan < 1 {
				colSpan = 1
			}
			rowSpan := cell.RowSpan
			if rowSpan < 1 {
				rowSpan = 1
			}

			if !l.isCellStart(rowIdx, colIdx) {
				continue
			}

			cellX := l.colX[colIdx]
			cellWidth := l.cellWidth(colIdx, colSpan)
			cellY := l.rowY[rowIdx]
			cellHeight := 0.0
			for r := 0; r < rowSpan && rowIdx+r < len(l.rowHeights); r++ {
				cellHeight += l.rowHeights[rowIdx+r]
			}

			cellTexts, cellStrikes, cellRules := l.createCellTexts(cell, cellX, cellY, cellWidth, cellHeight, padding, rowIdx)
			texts = append(texts, cellTexts...)
			strikes = append(strikes, cellStrikes...)
			rules = append(rules, cellRules...)

			colIdx += colSpan
		}
	}

	return texts, strikes, rules
}

// createCellTexts 返回单元格内的文字图元，以及删除线片段。OFD 的 TextObject
// 没有删除线属性，只能另画横线，所以两者必须一起产出：线要盖在被划掉的文字上，
// 与文字共用同一套 X 累进，分开算会错位。
func (l *tableLayout) createCellTexts(cell TableCell, cellX, cellY, cellWidth, cellHeight, padding float64, rowIdx int) ([]Text, []Path, []Path) {
	size := cell.Size
	if size == 0 {
		size = 4.2
	}
	font := cell.Font
	weight := cell.Weight
	italic := cell.Italic

	isHeader := rowIdx < l.table.HeaderRows
	if isHeader {
		if l.table.Style.HeaderSize > 0 {
			size = l.table.Style.HeaderSize
		}
		if l.table.Style.HeaderFont != "" {
			font = l.table.Style.HeaderFont
		}
		if l.table.Style.HeaderWeight > 0 {
			weight = l.table.Style.HeaderWeight
		}
	}

	if font == "" {
		font = "SimSun"
	}

	firstWidth := cellWidth - 2*padding - cell.FirstLineIndent
	contWidth := cellWidth - 2*padding - cell.HangingIndent
	if firstWidth <= 0 {
		firstWidth = cellWidth
	}
	if contWidth <= 0 {
		contWidth = cellWidth
	}
	runs := cellRuns(cell)
	lines := wrapStyledRuns(runs, firstWidth, contWidth, size)

	lineHeight := size * 1.5
	totalHeight := float64(len(lines)) * lineHeight
	// 汉字主体位于基线上方、em 盒底部留白，其视觉中心约在基线上方
	// textBaselineRatio 倍字号处。令多行文字整体的视觉中心对齐单元格中心，
	// 避免文字看着偏下。
	firstBaseline := cellY + cellHeight/2 - totalHeight/2 + lineHeight/2 + textBaselineRatio*size

	var texts []Text
	var strikes []Path
	var rules []Path
	for i, lineRuns := range lines {
		indent := cellLineIndent(cell, i)
		lineWidth := 0.0
		hasRule := false
		for _, run := range lineRuns {
			if run.rule {
				hasRule = true
				continue
			}
			lineWidth += estimateTextWidth(run.text, size)
		}
		var textX float64
		switch cell.Align {
		case "center":
			textX = cellX + padding + indent + (cellWidth-2*padding-indent-lineWidth)/2
		case "right":
			textX = cellX + cellWidth - padding - lineWidth
		default:
			textX = cellX + padding + indent
		}
		baseline := firstBaseline + float64(i)*lineHeight
		if hasRule {
			// 分隔线横贯整个单元格内容区（去掉内边距），与文字对齐方式无关。
			rules = append(rules, l.rulePath(cellX+padding, cellWidth-2*padding, baseline, size))
			continue
		}
		// 同一行内按样式变化拆成多个 TextObject，共享基线、X 依次累加。
		// 文本首尾空格不写入 TextObject，只累加到 X 偏移——部分阅读器会忽略
		// 对象首尾空格，写在内容里会导致相邻片段间距不一致。
		runX := textX
		for _, run := range lineRuns {
			text := run.text
			leading := text[:len(text)-len(strings.TrimLeft(text, " \t"))]
			runX += estimateTextWidth(leading, size)
			body := strings.Trim(text, " \t")
			trailing := strings.TrimLeft(text, " \t")[len(body):]
			if body != "" {
				runFont := font
				runWeight := weight
				runItalic := italic
				if run.code && l.table.Style.CodeFont != "" {
					runFont = l.table.Style.CodeFont
				}
				if run.bold {
					runWeight = 700
				}
				if run.italic {
					runItalic = true
				}
				bodyWidth := estimateTextWidth(body, size)
				offset := size
				texts = append(texts, Text{
					X:         runX,
					Y:         baseline - offset,
					Width:     bodyWidth,
					Height:    size,
					Value:     body,
					Font:      runFont,
					Size:      size,
					Weight:    runWeight,
					Italic:    runItalic,
					TextCodes: []TextCode{{Value: body, Y: &offset}},
				})
				if run.strike {
					strikes = append(strikes, l.strikePath(runX, bodyWidth, baseline, size))
				}
				runX += bodyWidth
			}
			runX += estimateTextWidth(trailing, size)
		}
	}
	return texts, strikes, rules
}

// rulePath 生成一条 Markdown 分隔线：横贯单元格内容区的细横线。
//
// 线位取基线上方半个行距，与上下文字留出呼吸；线宽比删除线略粗一点，
// 分隔线的视觉权重本来就高于删除线。
func (l *tableLayout) rulePath(x, width, baseline, size float64) Path {
	lineWidth := size * ruleLineWidthRatio
	y := baseline - size*ruleBaselineOffset
	return Path{
		X:           x,
		Y:           y - lineWidth/2,
		Width:       width,
		Height:      lineWidth,
		Data:        fmt.Sprintf("M %.3f %.3f L %.3f %.3f C", 0.0, 0.0, width, 0.0),
		Stroke:      true,
		LineWidth:   lineWidth,
		StrokeColor: l.ruleColor(),
	}
}

// ruleColor 返回分隔线颜色。未指定时用浅灰而不是边框色：分隔线是块内分界，
// 与表格边框同色会让它看起来像多出来的一道框线，读者会去找它围的是什么。
func (l *tableLayout) ruleColor() *Color {
	if l.table.Style.RuleColor != nil {
		return l.table.Style.RuleColor
	}
	return &Color{R: ruleColorGray, G: ruleColorGray, B: ruleColorGray}
}

// strikePath 生成一段删除线：横贯 runX 起、width 宽的细横线。
//
// 线高在基线上方 strikeBaselineRatio 倍字号处——约在汉字字身中部，与常见排版的
// 删除线位置一致。线宽取字号的固定比例，太细在缩放后容易断掉。
func (l *tableLayout) strikePath(runX, width, baseline, size float64) Path {
	lineWidth := size * strikeLineWidthRatio
	y := baseline - size*strikeBaselineRatio
	return Path{
		X:           runX,
		Y:           y - lineWidth/2,
		Width:       width,
		Height:      lineWidth,
		Data:        fmt.Sprintf("M %.3f %.3f L %.3f %.3f C", 0.0, 0.0, width, 0.0),
		Stroke:      true,
		LineWidth:   lineWidth,
		StrokeColor: l.strikeColor(),
	}
}

func (l *tableLayout) generateCaption(seg tableSegment) *Text {
	if seg.isFirst && l.table.Caption != "" {
		return l.createCaptionText(l.table.Caption, l.table.CaptionStyle)
	}
	if seg.isContinuation && l.table.ContinuationCaption != "" {
		caption := l.table.ContinuationCaption
		if strings.Contains(caption, "{caption}") {
			caption = strings.ReplaceAll(caption, "{caption}", l.table.Caption)
		}
		style := l.table.ContinuationCaptionStyle
		if style == nil {
			style = l.table.CaptionStyle
		}
		return l.createCaptionText(caption, style)
	}
	return nil
}

func (l *tableLayout) createCaptionText(text string, style *CaptionStyle) *Text {
	size := defaultCaptionSize
	font := "SimHei"
	weight := 700
	align := "center"

	if style != nil {
		if style.Size > 0 {
			size = style.Size
		}
		if style.Font != "" {
			font = style.Font
		}
		if style.Weight > 0 {
			weight = style.Weight
		}
		if style.Align != "" {
			align = style.Align
		}
	}

	textWidth := estimateTextWidth(text, size)
	var textX float64
	switch align {
	case "center":
		textX = l.table.X + (l.table.Width-textWidth)/2
	case "right":
		textX = l.table.X + l.table.Width - textWidth
	default:
		textX = l.table.X
	}

	// 表题在表格内容上方：题底距 table.Y 一个 Gap，题顶再往上一个字号。
	textY := l.table.Y - captionGap(style) - size

	return &Text{
		X:      textX,
		Y:      textY,
		Width:  textWidth,
		Height: size,
		Value:  text,
		Font:   font,
		Size:   size,
		Weight: weight,
	}
}

func estimateTextWidth(text string, size float64) float64 {
	if text == "" {
		return 0
	}
	width := 0.0
	for _, r := range text {
		width += charWidth(r, size)
	}
	return width
}

// charWidth 返回单个字符的估算宽度：CJK 等全角字符按字号，ASCII 按半个字号。
func charWidth(r rune, size float64) float64 {
	if r > 127 {
		return size
	}
	return size * 0.5
}

// isCJK 判断字符是否属于中日韩表意文字或假名、谚文，这些字符可逐字断行。
func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}

// cellLineIndent 返回单元格第 line 行的缩进（毫米）：首行用 FirstLineIndent，
// 续行用 HangingIndent。
func cellLineIndent(cell TableCell, line int) float64 {
	if line == 0 {
		return cell.FirstLineIndent
	}
	return cell.HangingIndent
}

// wrapCellText 把单元格文本断成多行：先按 \n 强制分段，段内再按可用宽度自动换行。
// CJK 逐字断，ASCII 按词断（单个词超宽时强制逐字符断）；保留行首空格、去除行尾空格。
func wrapCellText(value string, firstWidth, contWidth, size float64) []string {
	var lines []string
	for _, paragraph := range strings.Split(value, "\n") {
		lines = append(lines, wrapParagraph(paragraph, firstWidth, contWidth, size)...)
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines
}

// wrapParagraph 对单个段落断行。firstWidth 用于第 0 行，contWidth 用于其余行，
// 二者差异来自首行缩进与续行缩进不同。
func wrapParagraph(text string, firstWidth, contWidth, size float64) []string {
	tokens := tokenizeCellText(text)
	var lines []string
	var current []string
	currentWidth := 0.0
	lineIndex := 0

	width := func() float64 {
		if lineIndex == 0 {
			return firstWidth
		}
		return contWidth
	}
	flush := func() {
		lines = append(lines, strings.TrimRight(strings.Join(current, ""), " "))
		current = current[:0]
		currentWidth = 0
		lineIndex++
	}

	for _, token := range tokens {
		tokenWidth := estimateTextWidth(token, size)
		// 当前行放不下时先换行，避免 token 被拆到两行。
		if currentWidth+tokenWidth > width() && len(current) > 0 {
			flush()
		}
		// 换行后仍放不下（单个 token 超过整行宽度），强制逐字符断。
		if tokenWidth > width() {
			for _, r := range token {
				rw := charWidth(r, size)
				if currentWidth+rw > width() && len(current) > 0 {
					flush()
				}
				current = append(current, string(r))
				currentWidth += rw
			}
			continue
		}
		current = append(current, token)
		currentWidth += tokenWidth
	}
	if len(current) > 0 || len(lines) == 0 {
		flush()
	}
	return lines
}

// tokenizeCellText 把文本切成断行单元：CJK 字符各自成单元，连续的 ASCII
// 字母数字（含其中的标点）合成一个词，避免英文单词被拆开。单个空格作为
// 独立的词分隔单元（换行时丢弃），连续 2 个及以上的空格作为下一词的前缀
// （用户显式缩进，换行后保留在行首）。
func tokenizeCellText(text string) []string {
	var tokens []string
	var word strings.Builder
	var pendingSpaces string
	flushWord := func() {
		if word.Len() > 0 {
			tokens = append(tokens, pendingSpaces+word.String())
			word.Reset()
			pendingSpaces = ""
		}
	}
	runes := []rune(text)
	for i := 0; i < len(runes); {
		r := runes[i]
		switch {
		case r == ' ' || r == '\t':
			flushWord()
			j := i
			for j < len(runes) && (runes[j] == ' ' || runes[j] == '\t') {
				j++
			}
			run := string(runes[i:j])
			if len(runes[i:j]) >= 2 {
				pendingSpaces = run
			} else {
				tokens = append(tokens, run)
			}
			i = j
		case isCJK(r):
			flushWord()
			tokens = append(tokens, pendingSpaces+string(r))
			pendingSpaces = ""
			i++
		default:
			word.WriteRune(r)
			i++
		}
	}
	flushWord()
	if pendingSpaces != "" {
		tokens = append(tokens, pendingSpaces)
	}
	return tokens
}

// styledToken 是带样式的断行单元。forceBreak 为真表示强制换行标记，rule 为真
// 表示该单元是一条分隔线而非文字。
type styledToken struct {
	text       string
	bold       bool
	italic     bool
	code       bool
	strike     bool
	forceBreak bool
	rule       bool
}

// cellRuns 返回单元格文本对应的行内片段：Format 为 "markdown" 时解析行内
// Markdown 语法，其余按单段纯文本处理。
func cellRuns(cell TableCell) []styledRun {
	if strings.EqualFold(strings.TrimSpace(cell.Format), "markdown") {
		return parseMarkdownRuns(cell.Value)
	}
	if cell.Value == "" {
		return nil
	}
	return []styledRun{{text: cell.Value}}
}

// wrapStyledRuns 把带样式的行内片段按宽度断成多行，每行是 styledRun 序列。
// firstWidth 用于第 0 行，contWidth 用于其余行；\n 触发强制换行。CJK 逐字断，
// ASCII 按词断，保留行首空格、去除行尾空格。
func wrapStyledRuns(runs []styledRun, firstWidth, contWidth, size float64) [][]styledRun {
	tokens := tokenizeStyledRuns(runs)
	var lines [][]styledRun
	var current []styledToken
	currentWidth := 0.0
	lineIndex := 0

	width := func() float64 {
		if lineIndex == 0 {
			return firstWidth
		}
		return contWidth
	}
	flush := func() {
		lines = append(lines, tokensToRuns(current))
		current = current[:0]
		currentWidth = 0
		lineIndex++
	}

	for _, token := range tokens {
		if token.forceBreak {
			flush()
			continue
		}
		if token.rule {
			// 分隔线独占一行：先把已攒的文字冲掉，线本身不占宽度。
			if len(current) > 0 {
				flush()
			}
			lines = append(lines, []styledRun{{rule: true}})
			lineIndex++
			continue
		}
		tokenWidth := estimateTextWidth(token.text, size)
		if currentWidth+tokenWidth > width() && len(current) > 0 {
			flush()
		}
		if tokenWidth > width() {
			// 单个 token 超过整行宽度，强制逐字符断。
			for _, r := range token.text {
				rw := charWidth(r, size)
				if currentWidth+rw > width() && len(current) > 0 {
					flush()
				}
				next := token
				next.text = string(r)
				next.forceBreak = false
				current = append(current, next)
				currentWidth += rw
			}
			continue
		}
		current = append(current, token)
		currentWidth += tokenWidth
	}
	if len(current) > 0 || len(lines) == 0 {
		flush()
	}
	return lines
}

// tokenizeStyledRuns 把行内片段展开成带样式的断行单元；\n 变成强制换行标记，
// 分隔线变成独占一行的 rule 单元。相邻的强制换行会被合并，否则一次块分隔加上
// 分隔线自带的换行会多出一整行空白。
func tokenizeStyledRuns(runs []styledRun) []styledToken {
	var tokens []styledToken
	appendBreak := func() {
		if len(tokens) == 0 {
			return
		}
		if last := tokens[len(tokens)-1]; last.forceBreak || last.rule {
			return
		}
		tokens = append(tokens, styledToken{forceBreak: true})
	}
	appendText := func(text string, run styledRun) {
		for _, token := range tokenizeCellText(text) {
			tokens = append(tokens, styledToken{text: token, bold: run.bold, italic: run.italic, code: run.code, strike: run.strike})
		}
	}
	for _, run := range runs {
		if run.rule {
			// 分隔线本身没有文字，长度为零的文本经 tokenizeCellText 会产出空结果。
			// 换行交给两侧的块分隔去补，这里只放线本身。
			appendBreak()
			tokens = append(tokens, styledToken{rule: true})
			continue
		}
		text := run.text
		for {
			idx := strings.IndexByte(text, '\n')
			if idx < 0 {
				break
			}
			appendText(text[:idx], run)
			appendBreak()
			text = text[idx+1:]
		}
		appendText(text, run)
	}
	return tokens
}

// tokensToRuns 把一行内的断行单元合并回行内片段：去掉行尾空格（换行产生的
// 边界空格），合并相邻同样式。空格保留为独立片段，由 createCellTexts 转成
// X 偏移，避免阅读器忽略文本对象首尾空格造成间距不一致。
func tokensToRuns(tokens []styledToken) []styledRun {
	end := len(tokens)
	for end > 0 && !tokens[end-1].forceBreak && strings.TrimSpace(tokens[end-1].text) == "" {
		end--
	}
	tokens = tokens[:end]
	var runs []styledRun
	for _, token := range tokens {
		if token.forceBreak {
			continue
		}
		if len(runs) > 0 && sameRunStyle(runs[len(runs)-1], token) {
			runs[len(runs)-1].text += token.text
			continue
		}
		runs = append(runs, styledRun{text: token.text, bold: token.bold, italic: token.italic, code: token.code, strike: token.strike})
	}
	return runs
}

func sameRunStyle(run styledRun, token styledToken) bool {
	return run.bold == token.bold && run.italic == token.italic && run.code == token.code && run.strike == token.strike
}

// captionGap 返回说明文字与所标注对象的间距，未设置时用默认值。
func captionGap(style *CaptionStyle) float64 {
	if style == nil || style.Gap <= 0 {
		return defaultCaptionGap
	}
	return style.Gap
}
