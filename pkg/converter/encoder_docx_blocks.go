package converter

import (
	"strconv"
	"strings"

	"github.com/zc310/ofd/internal/docx"
	"github.com/zc310/ofd/internal/textdoc"
)

// 本文件把一页 OFD 文字条目编排成 DOCX 的块序列。结构推断复用 encoder_markdown.go
// 已经调好的零件：textdoc.Rows 聚行、DetectTables 认表格、detectBodyFontSize 与
// calculateHeaderLevel 判标题、markdownParagraphStarts 切段落。这里只重写编排，
// 不改 Markdown 的任何行为，两者行为分叉时各自独立演进。
//
// 编排与 Markdown 的差别只有一处：Markdown 输出线性字符串，无法表达块边界；
// DOCX 必须知道「哪几行属于同一段」，所以这里显式产出 docxBlock 序列。

// docxBlockKind 是块类型。
type docxBlockKind int

const (
	docxBlockParagraph docxBlockKind = iota
	docxBlockHeading
	docxBlockTable
	docxBlockImage
)

// docxBlockAnchor 是块的纵向锚点（毫米，原点在页面左上角）。文字块取首行的
// y，图片块取图片左上角的 y。块之间按该锚点排序，从而把图片穿插到页面上大致
// 相同的高度，而不是统统堆在文末。
type docxBlockAnchor float64

// docxBlock 是一个待写入 DOCX 的块。
type docxBlock struct {
	kind   docxBlockKind
	anchor docxBlockAnchor
	// level 是标题级别，1..6，对应 Word 内置 Heading N。
	level int
	// rows 是块内的文字行。段落块通常只有一行；标题块固定一行。
	rows [][]textdoc.Entry
	// cells 是表格块的单元格文本，外层是行。行列数必须一致。
	cells [][]string
	// columnWidths 是表格块的列宽（毫米），由文字条目的横向占位推算。
	columnWidths []float64
	// image 是图片块携带的图片条目。
	image *textdoc.Image
}

// docxBlockSequence 返回一页的块序列。detectTables 决定是否识别表格，
// images 是同页的可内嵌图片，按纵向锚点与文字块交错。
func docxBlockSequence(entries []textdoc.Entry, pageHeight float64, detectTables bool, images []textdoc.Image) []docxBlock {
	blocks := docxTextBlocks(entries, pageHeight, detectTables)
	return mergeDocxImages(blocks, images)
}

// mergeDocxImages 按纵向锚点把图片插入文字块序列，保持各自内部的顺序不变。
func mergeDocxImages(blocks []docxBlock, images []textdoc.Image) []docxBlock {
	if len(images) == 0 {
		return blocks
	}
	pending := append([]textdoc.Image(nil), images...)
	merged := make([]docxBlock, 0, len(blocks)+len(pending))
	inserted := 0
	for _, block := range blocks {
		// 先排出锚点在本块之前的图片，保证同一高度上图片在文字之前。
		for inserted < len(pending) &&
			docxBlockAnchor(pending[inserted].Y) <= block.anchor {
			merged = append(merged, docxImageBlock(pending[inserted]))
			inserted++
		}
		merged = append(merged, block)
	}
	for ; inserted < len(pending); inserted++ {
		merged = append(merged, docxImageBlock(pending[inserted]))
	}
	return merged
}

// docxImageBlock 把一张图片包成块。
func docxImageBlock(image textdoc.Image) docxBlock {
	return docxBlock{kind: docxBlockImage, anchor: docxBlockAnchor(image.Y), image: &image}
}

// docxTextBlocks 只编排文字与表格块。
func docxTextBlocks(entries []textdoc.Entry, pageHeight float64, detectTables bool) []docxBlock {
	bodyFontSize := detectBodyFontSize(entries)
	allRows := textdoc.Rows(entries)

	// 与 Markdown 一致：丢掉空行和页码行，但保留可参与表格识别的行。
	rows := make([][]textdoc.Entry, 0, len(allRows))
	for _, row := range allRows {
		if len(row) == 0 || isPageNumberRow(row, pageHeight) {
			continue
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil
	}

	var tables []textdoc.Table
	if detectTables {
		tables = textdoc.DetectTables(rows)
	}
	tableAt := make(map[int]textdoc.Table, len(tables))
	inTable := make([]bool, len(rows))
	for _, table := range tables {
		tableAt[table.Start] = table
		for i := table.Start; i <= table.End && i < len(rows); i++ {
			inTable[i] = true
		}
	}

	rowInfos := make([]markdownRowInfo, len(rows))
	for i, row := range rows {
		if inTable[i] {
			continue
		}
		maxSize := textdoc.RowSize(row)
		rowText := strings.TrimSpace(textdoc.JoinText(row))
		level := detectHeadingLevel(rowText, maxSize, bodyFontSize)
		rowInfos[i] = markdownRowInfo{text: rowText, maxSize: maxSize, level: level, isTitle: level > 0}
	}
	normalizeHeadingLevels(rowInfos, inTable)

	starts := markdownParagraphStarts(rows, inTable, rowInfos)
	blocks := make([]docxBlock, 0, len(rows))
	for i := range rows {
		if table, ok := tableAt[i]; ok {
			blocks = append(blocks, docxTableBlock(table, rows))
			continue
		}
		if inTable[i] {
			continue
		}
		// 段落边界：页首、paragraphStarts 判定的新段起点、标题自带一段，
		// 以及紧跟在标题之后的那一行都不能并进上一段。
		isHeading := rowInfos[i].isTitle
		newBlock := i == 0 || starts[i] || isHeading || (i > 0 && rowInfos[i-1].isTitle)
		if newBlock {
			kind := docxBlockParagraph
			if isHeading {
				kind = docxBlockHeading
			}
			blocks = append(blocks, docxBlock{
				kind:   kind,
				anchor: docxBlockAnchor(textdoc.RowTop(rows[i])),
				level:  rowInfos[i].level,
				rows:   [][]textdoc.Entry{rows[i]},
			})
			continue
		}
		blocks[len(blocks)-1].rows = append(blocks[len(blocks)-1].rows, rows[i])
	}
	return blocks
}

// normalizeHeadingLevels 把本页最浅的标题提升为第 1 级，使页内层级从 1 起算，
// 与 Markdown 的归一化规则一致。
func normalizeHeadingLevels(rowInfos []markdownRowInfo, inTable []bool) {
	minimum := 7
	for i := range rowInfos {
		if inTable[i] || !rowInfos[i].isTitle {
			continue
		}
		if rowInfos[i].level < minimum {
			minimum = rowInfos[i].level
		}
	}
	if minimum <= 1 {
		return
	}
	for i := range rowInfos {
		if inTable[i] || !rowInfos[i].isTitle {
			continue
		}
		rowInfos[i].level = min(max(rowInfos[i].level-minimum+1, 1), 6)
	}
}

// docxTableBlock 把识别出的表格转成等宽矩形网格。
//
// textdoc.Table 携带的是单元格文本，而列宽只能从文字条目的横向占位反推：
// 对每个条目取 [X, X+Width] 区间，落到第几列就贡献到哪一列，再取各列贡献的
// 最大值。这样短文本不会被压缩，长表头列能拿到真实宽度。
//
// 行列数按 DetectTables 报告的 Header 宽度对齐，缺格补空串——现有实现把跨列
// 单元格当作噪声排除（columnIndex 返回 -1 时该行降级为 barrier），所以这里
// 不会出现合并单元格，也不需要 colspan。
func docxTableBlock(table textdoc.Table, rows [][]textdoc.Entry) docxBlock {
	columns := len(table.Header)
	if columns < 1 {
		columns = 1
	}
	widths := make([]float64, columns)
	grid := make([][]string, 0, table.End-table.Start+1)
	for i := table.Start; i <= table.End && i < len(rows); i++ {
		values := tableRowCells(rows[i], columns)
		grid = append(grid, values)
		accumulateColumnWidths(rows[i], widths)
	}
	for i := range widths {
		if widths[i] <= 0 {
			widths[i] = 1
		}
	}
	anchor := docxBlockAnchor(0)
	if table.Start >= 0 && table.Start < len(rows) {
		anchor = docxBlockAnchor(textdoc.RowTop(rows[table.Start]))
	}
	return docxBlock{
		kind:         docxBlockTable,
		anchor:       anchor,
		cells:        grid,
		columnWidths: widths,
	}
}

// tableRowCells 把一行文字条目切成固定列数的单元格文本。
func tableRowCells(row []textdoc.Entry, columns int) []string {
	values := make([]string, columns)
	if len(row) == 0 || columns == 0 {
		return values
	}
	// 条目已按 x 排序，用中点做分界：相邻条目的中线落在哪一列，条目就属于哪一列。
	boundaries := columnBoundaries(row, columns)
	for i, entry := range row {
		column := boundaries[i]
		if column >= columns {
			column = columns - 1
		}
		if text := strings.TrimSpace(entry.Text); text != "" {
			values[column] = joinCellText(values[column], text)
		}
	}
	return values
}

// columnBoundaries 为一行内的每个条目计算所属列下标。
func columnBoundaries(row []textdoc.Entry, columns int) []int {
	out := make([]int, len(row))
	if columns <= 1 {
		return out
	}
	// 以条目中点聚成 columns 组：把行按中点位置等分。
	left := textdoc.RowLeft(row)
	span := textdoc.RowRight(row) - left
	if span <= 0 {
		return out
	}
	for i, entry := range row {
		center := entry.X + entry.Width/2
		if !textdoc.Finite(center) {
			continue
		}
		column := int((center - left) / span * float64(columns))
		out[i] = min(max(column, 0), columns-1)
	}
	return out
}

// joinCellText 合并落到同一格的多个条目，中文之间不补空格。
func joinCellText(existing, addition string) string {
	if existing == "" {
		return addition
	}
	return existing + addition
}

// accumulateColumnWidths 用条目占位反推列宽，贡献取最大值。
func accumulateColumnWidths(row []textdoc.Entry, widths []float64) {
	if len(widths) == 0 {
		return
	}
	columns := len(widths)
	left := textdoc.RowLeft(row)
	span := textdoc.RowRight(row) - left
	if span <= 0 {
		return
	}
	for _, entry := range row {
		if !textdoc.Finite(entry.X) || entry.Width <= 0 {
			continue
		}
		center := entry.X + entry.Width/2
		column := int((center - left) / span * float64(columns))
		column = min(max(column, 0), columns-1)
		if entry.Width > widths[column] {
			widths[column] = entry.Width
		}
	}
}

// docxParagraph 把块渲染成 w:p。段落内的多行按 Word 的正常行为接续，不再插入换行符：
// OFD 的硬换行位置由排版器决定，流式输出交给 Word 重排。
func (b docxBlock) docxParagraph() *docx.Paragraph {
	paragraph := &docx.Paragraph{}
	if b.kind == docxBlockHeading {
		paragraph.Properties = &docx.ParagraphProperties{
			Style: docx.StringVal("Heading" + strconv.Itoa(b.level)),
		}
	}
	for _, row := range b.rows {
		for _, entry := range row {
			text := strings.TrimSpace(entry.Text)
			if text == "" {
				continue
			}
			paragraph.Runs = append(paragraph.Runs, docxRun(entry, text))
		}
	}
	return paragraph
}

// docxRun 把一个文字条目转成带格式的 w:r。
func docxRun(entry textdoc.Entry, text string) docx.Run {
	properties := &docx.RunProperties{}
	if entry.FontName != "" {
		properties.Fonts = &docx.RunFonts{ASCII: entry.FontName, EastAsia: entry.FontName}
	}
	if size := docxFontSize(entry.Size); size > 0 {
		properties.Size = docx.IntVal(size)
	}
	if entry.Bold {
		properties.Bold = docx.BoolVal(true)
	}
	if entry.Italic {
		properties.Italic = docx.BoolVal(true)
	}
	return docx.Run{Properties: properties, Text: docx.NewText(text)}
}

// docxFontSize 把 OFD 的毫米字号转成 w:sz 的半磅单位。转换要求输入有限且为正。
func docxFontSize(mm float64) int {
	if !textdoc.Finite(mm) || mm <= 0 {
		return 0
	}
	// 1mm = 72/25.4 pt，w:sz 再乘 2 变成半磅。
	halfPoints := mm * 72.0 / 25.4 * 2.0
	if halfPoints < 1 {
		return 1
	}
	return int(halfPoints + 0.5)
}

// docxTable 把表格块渲染成 w:tbl。列宽从毫米换算成 twip。
func (b docxBlock) docxTable() *docx.Table {
	widths := make([]int, len(b.columnWidths))
	total := 0
	for i, mm := range b.columnWidths {
		widths[i] = max(docx.Twips(mm), 1)
		total += widths[i]
	}
	table := &docx.Table{
		Grid: docx.NewTableGrid(widths),
		Properties: &docx.TableProperties{
			Width:   docx.NewMeasure(total),
			Layout:  &docx.TypeVal{Value: "fixed"},
			Look:    docx.StringVal("04A0"),
			Borders: docxTableBorders(),
		},
	}
	for rowIndex, values := range b.cells {
		cells := make([]docx.TableCell, 0, len(widths))
		for column, value := range values {
			if column >= len(widths) {
				break
			}
			cell := docx.TableCell{
				Properties: &docx.TableCellProperties{Width: docx.NewMeasure(widths[column])},
				Paragraphs: []docx.Paragraph{*docxParagraphOfText(value)},
			}
			cells = append(cells, cell)
		}
		row := docx.TableRow{Cells: cells}
		if rowIndex == 0 {
			// 首行标为表头，跨页时 Word 会自动重复。
			row.Properties = &docx.TableRowProperties{TableHeader: docx.BoolVal(true)}
		}
		table.Rows = append(table.Rows, row)
	}
	return table
}

// docxTableBorders 返回单线全框表格，线宽 0.5pt（w:sz 以八分之一磅为单位）。
func docxTableBorders() *docx.TableBorders {
	edge := func() *docx.Border { return &docx.Border{Val: "single", Size: 4, Color: "000000"} }
	return &docx.TableBorders{
		Top: edge(), Left: edge(), Bottom: edge(), Right: edge(),
		InsideH: edge(), InsideV: edge(),
	}
}

// docxParagraphOfText 把纯文本包成一个不带格式的段落。
func docxParagraphOfText(text string) *docx.Paragraph {
	paragraph := &docx.Paragraph{}
	if text != "" {
		paragraph.Runs = []docx.Run{{Text: docx.NewText(text)}}
	}
	return paragraph
}
