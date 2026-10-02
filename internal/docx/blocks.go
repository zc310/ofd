package docx

import (
	"strconv"
	"strings"

	"github.com/zc310/ofd/internal/textdoc"
)

// 本文件把一页 OFD 文字条目编排成 DOCX 的块序列。结构推断复用 encoder_markdown.go
// 已经调好的零件：textdoc.Rows 聚行、DetectTables 认表格、detectBodyFontSize 与
// calculateHeaderLevel 判标题、markdownParagraphStarts 切段落。这里只重写编排，
// 不改 Markdown 的任何行为，两者行为分叉时各自独立演进。
//
// 编排与 Markdown 的差别只有一处：Markdown 输出线性字符串，无法表达块边界；
// DOCX 必须知道「哪几行属于同一段」，所以这里显式产出 Block 序列。

// BlockKind 是块类型。
type BlockKind int

const (
	BlockParagraph BlockKind = iota
	BlockHeading
	BlockTable
	BlockImage
)

// BlockAnchor 是块的纵向锚点（毫米，原点在页面左上角）。文字块取首行的
// y，图片块取图片左上角的 y。块之间按该锚点排序，从而把图片穿插到页面上大致
// 相同的高度，而不是统统堆在文末。
type BlockAnchor float64

// Block 是一个待写入 DOCX 的块。
type Block struct {
	// Kind 决定用哪种写入方法渲染。
	Kind BlockKind
	// Image 是图片块携带的图片条目，其他块为 nil。
	Image *textdoc.Image

	// anchor 是块的纵向锚点（毫米，原点在页面左上角），图片按它与文字交错。
	anchor BlockAnchor
	// level 是标题级别，1..6，对应 Word 内置 Heading N。
	level int
	// rows 是块内的文字行。段落块可能累积多行；标题块固定一行。
	rows [][]textdoc.Entry
	// cells 是表格块的单元格文本，外层是行。行列数必须一致。
	cells [][]string
	// columnWidths 是表格块的列宽（毫米），由文字条目的横向占位推算。
	columnWidths []float64
	// listItems 是本块内每行对应的列表条目，与 rows 一一对应。
	listItems []ListItem
}

// PageOptions 是一页 DOCX 输出的开关。
type PageOptions struct {
	// Tables 决定是否按位置识别表格。
	Tables bool
	// Annotations 决定是否保留批注层文字。批注承载的是叠加在正文上的标记
	// ——整页水印、电子印章、签章位置——默认应当剔除。
	Annotations bool
}

// MaxImagesPerPage 限制单页内嵌的图片数量。整版图片类的 OFD（例如把每页当成
// 一张扫描图）会在这里被截断，避免长篇扫描件产出体量失控的 docx。
const MaxImagesPerPage = 64

// BlockSequence 返回一页的块序列，图片按纵向锚点穿插在文字块之间。
func BlockSequence(entries []textdoc.Entry, pageHeight float64, options PageOptions, images []textdoc.Image) []Block {
	if !options.Annotations {
		entries = dropAnnotatedEntries(entries)
	}
	blocks := textBlocks(entries, pageHeight, options.Tables)
	return mergeImages(blocks, images)
}

// dropAnnotatedEntries 剔除批注来源的文字条目。
//
// 批注在 OFD 里承载的是叠加在正文上的标记：整页水印、电子印章、签章位置、
// 阅读批注。实测保密宣传册首页的「保密资料」水印由 81 个批注文字对象组成，
// 而同页真实正文只有 5 个对象——不剔除的话 DOCX 的每个段落都会被水印文字
// 淹没。因此默认排除，需要保留叠加层时用 WithDOCXAnnotations 打开。
//
// 模板层不剔除：它常放页眉页脚与标题栏，是真内容。pkg/invoice 取票面标题
// 就依赖模板文字层（选字号最大的一条），在提取层过滤会连带影响它。
func dropAnnotatedEntries(entries []textdoc.Entry) []textdoc.Entry {
	kept := make([]textdoc.Entry, 0, len(entries))
	for _, entry := range entries {
		if entry.Source != textdoc.EntrySourceAnnotation {
			kept = append(kept, entry)
		}
	}
	return kept
}

// mergeImages 按纵向锚点把图片插入文字块序列，保持各自内部的顺序不变。
func mergeImages(blocks []Block, images []textdoc.Image) []Block {
	if len(images) == 0 {
		return blocks
	}
	pending := append([]textdoc.Image(nil), images...)
	merged := make([]Block, 0, len(blocks)+len(pending))
	inserted := 0
	for _, block := range blocks {
		// 先排出锚点在本块之前的图片，保证同一高度上图片在文字之前。
		for inserted < len(pending) &&
			BlockAnchor(pending[inserted].Y) <= block.anchor {
			merged = append(merged, imageBlock(pending[inserted]))
			inserted++
		}
		merged = append(merged, block)
	}
	for ; inserted < len(pending); inserted++ {
		merged = append(merged, imageBlock(pending[inserted]))
	}
	return merged
}

// imageBlock 把一张图片包成块。
func imageBlock(image textdoc.Image) Block {
	return Block{Kind: BlockImage, anchor: BlockAnchor(image.Y), Image: &image}
}

// textBlocks 只编排文字与表格块。
func textBlocks(entries []textdoc.Entry, pageHeight float64, detectTables bool) []Block {
	bodyFontSize := textdoc.BodyFontSize(entries)
	allRows := textdoc.Rows(entries)

	// 与 Markdown 一致：丢掉空行和页码行，但保留可参与表格识别的行。
	rows := make([][]textdoc.Entry, 0, len(allRows))
	for _, row := range allRows {
		if len(row) == 0 || textdoc.IsPageNumberRow(row, pageHeight) {
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

	rowInfos := make([]textdoc.RowInfo, len(rows))
	rawItems := make([]ListItem, len(rows))
	for i, row := range rows {
		if inTable[i] {
			continue
		}
		maxSize := textdoc.RowSize(row)
		rowText := strings.TrimSpace(textdoc.JoinText(row))
		level := textdoc.DetectHeadingLevel(rowText, maxSize, bodyFontSize)
		// 带行首编号的行按列表条目处理，不参与标题层级归一化。真实的
		// 「1.2.3 标题」仍会走编号标题分支——isNumberedHeader 先于字号判定，
		// 那类行的行首序号不带后续空格，不被列表正则命中。
		if _, isItem := RowListMarker(row); isItem {
			if item, ok := DetectListItem(rowText); ok {
				rawItems[i] = item
				rowInfos[i] = textdoc.RowInfo{Text: rowText, MaxSize: maxSize, IsList: true}
				continue
			}
		}
		rowInfos[i] = textdoc.RowInfo{Text: rowText, MaxSize: maxSize, Level: level, IsTitle: level > 0}
	}
	normalizeHeadingLevels(rowInfos, inTable)

	// 只保留可信连续段里的条目；被清空的行按普通段落处理。
	rawItems = FilterCredibleListItems(rawItems)
	for i := range rowInfos {
		rowInfos[i].IsList = rawItems[i].marker != ""
		if rowInfos[i].IsList {
			rowInfos[i].IsTitle = false
		}
	}

	starts := textdoc.ParagraphStarts(rows, inTable, rowInfos)
	blocks := make([]Block, 0, len(rows))
	for i := range rows {
		if table, ok := tableAt[i]; ok {
			blocks = append(blocks, tableBlock(table, rows))
			continue
		}
		if inTable[i] {
			continue
		}
		// 段落边界：页首、paragraphStarts 判定的新段起点、标题自带一段、
		// 列表条目各自成段，以及紧跟在标题之后的那一行都不能并进上一段。
		isHeading := rowInfos[i].IsTitle
		isList := rowInfos[i].IsList
		newBlock := i == 0 || starts[i] || isHeading || isList ||
			(i > 0 && (rowInfos[i-1].IsTitle || rowInfos[i-1].IsList))
		if newBlock {
			kind := BlockParagraph
			if isHeading {
				kind = BlockHeading
			}
			blocks = append(blocks, Block{
				Kind:      kind,
				anchor:    BlockAnchor(textdoc.RowTop(rows[i])),
				level:     rowInfos[i].Level,
				rows:      [][]textdoc.Entry{rows[i]},
				listItems: []ListItem{rawItems[i]},
			})
			continue
		}
		blocks[len(blocks)-1].rows = append(blocks[len(blocks)-1].rows, rows[i])
	}
	return blocks
}

// normalizeHeadingLevels 把本页最浅的标题提升为第 1 级，使页内层级从 1 起算，
// 与 Markdown 的归一化规则一致。
func normalizeHeadingLevels(rowInfos []textdoc.RowInfo, inTable []bool) {
	minimum := 7
	for i := range rowInfos {
		if inTable[i] || !rowInfos[i].IsTitle {
			continue
		}
		if rowInfos[i].Level < minimum {
			minimum = rowInfos[i].Level
		}
	}
	if minimum <= 1 {
		return
	}
	for i := range rowInfos {
		if inTable[i] || !rowInfos[i].IsTitle {
			continue
		}
		rowInfos[i].Level = min(max(rowInfos[i].Level-minimum+1, 1), 6)
	}
}

// tableBlock 把识别出的表格转成等宽矩形网格。
//
// textdoc.Table 携带的是单元格文本，而列宽只能从文字条目的横向占位反推：
// 对每个条目取 [X, X+Width] 区间，落到第几列就贡献到哪一列，再取各列贡献的
// 最大值。这样短文本不会被压缩，长表头列能拿到真实宽度。
//
// 行列数按 DetectTables 报告的 Header 宽度对齐，缺格补空串——现有实现把跨列
// 单元格当作噪声排除（columnIndex 返回 -1 时该行降级为 barrier），所以这里
// 不会出现合并单元格，也不需要 colspan。
func tableBlock(table textdoc.Table, rows [][]textdoc.Entry) Block {
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
	anchor := BlockAnchor(0)
	if table.Start >= 0 && table.Start < len(rows) {
		anchor = BlockAnchor(textdoc.RowTop(rows[table.Start]))
	}
	return Block{
		Kind:         BlockTable,
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

// Paragraph 把块渲染成 w:p。段落内的多行按 Word 的正常行为接续，不再插入换行符：
// OFD 的硬换行位置由排版器决定，流式输出交给 Word 重排。
func (b Block) Paragraph() *Paragraph {
	paragraph := &Paragraph{}
	properties := &ParagraphProperties{}
	if b.Kind == BlockHeading {
		properties.Style = StringVal("Heading" + strconv.Itoa(b.level))
	}
	for index, row := range b.rows {
		// 列表条目在 Word 里由 numbering.xml 自动渲染序号，正文里必须去掉
		// 行首序号，否则会显示两遍。
		item, isItem := listItemAt(b.listItems, index)
		if isItem {
			properties.Numbering = listProperties(item)
		}
		first := true
		for _, entry := range row {
			text := strings.TrimSpace(entry.Text)
			if text == "" {
				continue
			}
			if isItem && first {
				text = StripListMarker(text)
				first = false
				if text == "" {
					continue
				}
			}
			paragraph.Runs = append(paragraph.Runs, runOf(entry, text))
		}
	}
	if properties.Style != nil || properties.Numbering != nil {
		paragraph.Properties = properties
	}
	return paragraph
}

// listItemAt 返回下标对应的列表条目。
func listItemAt(items []ListItem, index int) (ListItem, bool) {
	if index < 0 || index >= len(items) {
		return ListItem{}, false
	}
	item := items[index]
	return item, item.marker != ""
}

// runOf 把一个文字条目转成带格式的 w:r。
func runOf(entry textdoc.Entry, text string) Run {
	properties := &RunProperties{}
	if entry.FontName != "" {
		properties.Fonts = &RunFonts{ASCII: entry.FontName, EastAsia: entry.FontName}
	}
	if size := fontSizeHalfPoints(entry.Size); size > 0 {
		properties.Size = IntVal(size)
	}
	if entry.Bold {
		properties.Bold = BoolVal(true)
	}
	if entry.Italic {
		properties.Italic = BoolVal(true)
	}
	return Run{Properties: properties, Text: NewText(text)}
}

// fontSizeHalfPoints 把 OFD 的毫米字号转成 w:sz 的半磅单位。转换要求输入有限且为正。
func fontSizeHalfPoints(mm float64) int {
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

// Table 把表格块渲染成 w:tbl。列宽从毫米换算成 twip。
func (b Block) Table() *Table {
	widths := make([]int, len(b.columnWidths))
	total := 0
	for i, mm := range b.columnWidths {
		widths[i] = max(Twips(mm), 1)
		total += widths[i]
	}
	table := &Table{
		Grid: NewTableGrid(widths),
		Properties: &TableProperties{
			Width:   NewMeasure(total),
			Layout:  &TypeVal{Value: "fixed"},
			Look:    StringVal("04A0"),
			Borders: tableBorders(),
		},
	}
	for rowIndex, values := range b.cells {
		cells := make([]TableCell, 0, len(widths))
		for column, value := range values {
			if column >= len(widths) {
				break
			}
			cell := TableCell{
				Properties: &TableCellProperties{Width: NewMeasure(widths[column])},
				Paragraphs: []Paragraph{*paragraphOfText(value)},
			}
			cells = append(cells, cell)
		}
		row := TableRow{Cells: cells}
		if rowIndex == 0 {
			// 首行标为表头，跨页时 Word 会自动重复。
			row.Properties = &TableRowProperties{TableHeader: BoolVal(true)}
		}
		table.Rows = append(table.Rows, row)
	}
	return table
}

// tableBorders 返回单线全框表格，线宽 0.5pt（w:sz 以八分之一磅为单位）。
func tableBorders() *TableBorders {
	edge := func() *Border { return &Border{Val: "single", Size: 4, Color: "000000"} }
	return &TableBorders{
		Top: edge(), Left: edge(), Bottom: edge(), Right: edge(),
		InsideH: edge(), InsideV: edge(),
	}
}

// paragraphOfText 把纯文本包成一个不带格式的段落。
func paragraphOfText(text string) *Paragraph {
	paragraph := &Paragraph{}
	if text != "" {
		paragraph.Runs = []Run{{Text: NewText(text)}}
	}
	return paragraph
}
