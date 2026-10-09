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
	defaultCaptionOffsetY   = -6
	// defaultCaptionGap 是表题底部与表格内容顶部之间的间距（毫米）。
	defaultCaptionGap = 6.0
	// textBaselineRatio 是汉字视觉中心相对基线的比例（基线上方），
	// 用于让单元格文字在行内视觉居中。
	textBaselineRatio = 0.37
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
	startRow   int
	endRow     int
	isFirst    bool
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
	contentTop := l.table.Y
	if l.table.Caption != "" {
		captionSize := defaultCaptionSize
		offsetY := float64(defaultCaptionOffsetY)
		if l.table.CaptionStyle != nil {
			if l.table.CaptionStyle.Size > 0 {
				captionSize = l.table.CaptionStyle.Size
			}
			if l.table.CaptionStyle.OffsetY != 0 {
				offsetY = l.table.CaptionStyle.OffsetY
			}
		}
		// 表题顶部在 table.Y+offsetY（offsetY 为负则位于表格上方），
		// 表格内容紧接表题底部下方 captionGap，避免表题与表格之间出现大空隙。
		contentTop = l.table.Y + offsetY + captionSize + defaultCaptionGap
	}
	l.rowY[0] = contentTop
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

func (l *tableLayout) calculateSegments() {
	numRows := len(l.table.Rows)
	if numRows == 0 {
		return
	}

	availableHeight := l.pageHeight - l.table.Y
	if availableHeight <= 0 {
		availableHeight = l.pageHeight / 2
	}

	startRow := 0
	for rowIdx := 1; rowIdx < numRows; rowIdx++ {
		segmentHeight := l.rowY[rowIdx] - l.rowY[startRow]
		if segmentHeight > availableHeight {
			l.segments = append(l.segments, tableSegment{
				startRow:       startRow,
				endRow:         rowIdx,
				isFirst:        startRow == 0,
				isContinuation: startRow > 0,
			})
			startRow = rowIdx
		}
	}

	if startRow < numRows {
		l.segments = append(l.segments, tableSegment{
			startRow:       startRow,
			endRow:         numRows,
			isFirst:        startRow == 0,
			isContinuation: startRow > 0,
		})
	}
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
		Data:       data,
		Stroke:     true,
		StrokeColor: color,
		LineWidth:  width,
	}
}

func (l *tableLayout) createVerticalLine(x, y0, y1, width float64, color *Color) Path {
	data := fmt.Sprintf("M %.3f %.3f L %.3f %.3f C", 0.0, 0.0, 0.0, y1-y0)
	return Path{
		X: x, Y: y0, Width: width, Height: y1 - y0,
		Data:       data,
		Stroke:     true,
		StrokeColor: color,
		LineWidth:  width,
	}
}

func (l *tableLayout) generateCellTexts(seg tableSegment) []Text {
	var texts []Text
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

			texts = append(texts, l.createCellTexts(cell, cellX, cellY, cellWidth, cellHeight, padding, rowIdx)...)

			colIdx += colSpan
		}
	}

	return texts
}

func (l *tableLayout) createCellTexts(cell TableCell, cellX, cellY, cellWidth, cellHeight, padding float64, rowIdx int) []Text {
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
	for i, lineRuns := range lines {
		indent := cellLineIndent(cell, i)
		lineWidth := 0.0
		for _, run := range lineRuns {
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
				runX += bodyWidth
			}
			runX += estimateTextWidth(trailing, size)
		}
	}
	return texts
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
	offsetY := float64(defaultCaptionOffsetY)

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
		if style.OffsetY != 0 {
			offsetY = style.OffsetY
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

	textY := l.table.Y + offsetY

	return &Text{
		X:     textX,
		Y:     textY,
		Width: textWidth,
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

// styledToken 是带样式的断行单元。forceBreak 为真表示强制换行标记。
type styledToken struct {
	text       string
	bold       bool
	italic     bool
	code       bool
	strike     bool
	forceBreak bool
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

// tokenizeStyledRuns 把行内片段展开成带样式的断行单元；\n 变成强制换行标记。
func tokenizeStyledRuns(runs []styledRun) []styledToken {
	var tokens []styledToken
	appendText := func(text string, run styledRun) {
		for _, token := range tokenizeCellText(text) {
			tokens = append(tokens, styledToken{text: token, bold: run.bold, italic: run.italic, code: run.code, strike: run.strike})
		}
	}
	for _, run := range runs {
		text := run.text
		for {
			idx := strings.IndexByte(text, '\n')
			if idx < 0 {
				break
			}
			appendText(text[:idx], run)
			tokens = append(tokens, styledToken{forceBreak: true})
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