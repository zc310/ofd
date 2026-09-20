package converter

import (
	"errors"
	"io"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/textdoc"
)

var (
	// chapterHeaderRegex 匹配“第 X 章”形式的章节标题。
	chapterHeaderRegex = regexp.MustCompile(`^第\s*[0-9一二三四五六七八九十百千万]+\s*章`)
	// numberedHeaderRegex 匹配“1.”“1.2.”“1.2.3.”等形式的多级编号标题。
	numberedHeaderRegex = regexp.MustCompile(`^(\d+\.)(?:\d+\.)*`)
	// pageNumberRegex 匹配纯页码，例如“12”。
	pageNumberRegex = regexp.MustCompile(`^\d{1,3}$`)
	// dashPageNumberRegex 匹配带破折号的页码，例如“- 12 -”“— 12 —”。
	dashPageNumberRegex = regexp.MustCompile(`^[—–\-]\s*\d+\s*[—–\-]$`)
	// markdownParagraphMarkerRegex 匹配常见的中文段落起始标记。
	markdownParagraphMarkerRegex = regexp.MustCompile(
		`^(第\s*[0-9一二三四五六七八九十百千零两]+\s*[条编]|[（(][一二三四五六七八九十百0-9]+[）)]|[一二三四五六七八九十百]+、)`)
	// markdownEscaper 复用 Markdown 特殊字符转义表，避免每行重新构建。
	markdownEscaper = strings.NewReplacer(
		`\`, `\\`,
		"`", "\\`",
		"*", "\\*",
		"_", "\\_",
		"[", "\\[",
		"]", "\\]",
		"<", "\\<",
		">", "\\>",
		"|", "\\|",
		"~", "\\~",
	)
)

type markdownEncoder struct{}

func (e *markdownEncoder) Name() string         { return "markdown" }
func (e *markdownEncoder) Kind() Kind           { return KindDocument }
func (e *markdownEncoder) Extensions() []string { return []string{".md", ".markdown"} }
func (e *markdownEncoder) MIME() string         { return "text/markdown" }
func (e *markdownEncoder) Encode(input any, output io.Writer, conv *Converter) error {
	return encodeOFD(input, output, conv, e.markdownFromDocuments)
}

func (e *markdownEncoder) markdownFromDocuments(documents []*render.Document, output io.Writer, conv *Converter) error {
	return markdownDocuments(documents, output, conv)
}

// Markdown 提取 input 中 OFD 文档的文字并写入 Markdown 文档。
// “第 X 章”识别为二级标题，其他标题按字号和编号层级输出。
func Markdown(input any, output io.Writer, opts ...Option) error {
	return Encode("markdown", input, output, opts...)
}

// MarkdownDocument 提取已解析 OFD 文档中的文字并写入 Markdown 文档。
func MarkdownDocument(doc *parser.Document, output io.Writer, opts ...Option) error {
	return MarkdownDocuments([]*parser.Document{doc}, output, opts...)
}

// MarkdownDocuments 按全局页码提取多个已解析 OFD 文档体中的文字并写入 Markdown 文档。
func MarkdownDocuments(documents []*parser.Document, output io.Writer, opts ...Option) error {
	if output == nil {
		return errors.New("未设置Markdown输出参数")
	}
	docs := make([]*render.Document, len(documents))
	for i, d := range documents {
		docs[i] = &render.Document{Document: d}
	}
	return markdownDocuments(docs, output, newConverter(opts...))
}

func markdownDocuments(documents []*render.Document, output io.Writer, conv *Converter) error {
	if output == nil {
		return errors.New("未设置Markdown输出参数")
	}
	pages, err := collectTextPages(documents, conv.page)
	if err != nil {
		return err
	}

	var markdown strings.Builder
	title := strings.TrimSpace(conv.docTitle)
	if title == "" {
		title = "OFD 文档"
	}
	markdown.WriteString("# ")
	markdown.WriteString(escapeMarkdownLine(title))
	markdown.WriteString("\n\n")

	firstPage := true
	for _, page := range pages {
		if len(page.Entries) == 0 {
			continue
		}
		if !firstPage {
			markdown.WriteString("\n---\n\n")
		}
		firstPage = false
		writeMarkdownPage(&markdown, page.Entries, page.Height, conv.MarkdownTables())
	}

	_, err = io.WriteString(output, markdown.String())
	return err
}

func writeMarkdownPage(markdown *strings.Builder, entries []textdoc.Entry, pageHeight float64, detectTables bool) {
	bodyFontSize := detectBodyFontSize(entries)
	allRows := textdoc.Rows(entries)

	// 过滤空行和页码行，保留可用于表格识别的行。纯数字只有位于页面顶部或
	// 底部时才按页码处理，避免误删表格或数据中的数字。
	rows := make([][]textdoc.Entry, 0, len(allRows))
	for _, row := range allRows {
		if len(row) == 0 {
			continue
		}
		if isPageNumberRow(row, pageHeight) {
			continue
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return
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

	// 归一化标题层级：把本页最浅的标题提升为第 1 级。
	minLevel := 7
	for i := range rowInfos {
		if inTable[i] {
			continue
		}
		if rowInfos[i].isTitle && rowInfos[i].level < minLevel {
			minLevel = rowInfos[i].level
		}
	}
	if minLevel > 1 {
		for i := range rowInfos {
			if inTable[i] || !rowInfos[i].isTitle {
				continue
			}
			rowInfos[i].level -= minLevel - 1
			if rowInfos[i].level < 1 {
				rowInfos[i].level = 1
			}
			if rowInfos[i].level > 6 {
				rowInfos[i].level = 6
			}
		}
	}

	paragraphStarts := markdownParagraphStarts(rows, inTable, rowInfos)

	prevWasTitle := false
	emitted := false
	for i := range rows {
		if table, ok := tableAt[i]; ok {
			renderMarkdownTable(markdown, table)
			prevWasTitle = false
			emitted = true
			continue
		}
		if inTable[i] {
			continue
		}
		ri := rowInfos[i]
		if ri.text == "" {
			continue
		}
		escaped := escapeMarkdownLine(ri.text)

		if chapterHeaderRegex.MatchString(ri.text) {
			if !prevWasTitle {
				markdown.WriteByte('\n')
			}
			markdown.WriteString("## ")
			markdown.WriteString(escaped)
			markdown.WriteByte('\n')
			prevWasTitle = true
			emitted = true
			continue
		}

		if ri.level > 0 {
			if !prevWasTitle {
				markdown.WriteByte('\n')
			}
			level := ri.level + 2
			if level > 6 {
				level = 6
			}
			markdown.WriteString(strings.Repeat("#", level))
			markdown.WriteByte(' ')
			markdown.WriteString(escaped)
			markdown.WriteByte('\n')
			prevWasTitle = true
			emitted = true
			continue
		}

		if prevWasTitle {
			markdown.WriteByte('\n')
		} else if emitted && paragraphStarts[i] {
			// 段落之间补空行，避免 GFM 把多行软换行合并成同一段。
			markdown.WriteByte('\n')
		}
		markdown.WriteString(escaped)
		markdown.WriteByte('\n')
		prevWasTitle = false
		emitted = true
	}
}

// markdownRowInfo 保存页内一行的文本与标题判定结果。
type markdownRowInfo struct {
	text    string
	maxSize float64
	level   int
	isTitle bool
}

// markdownParagraphStarts 判断每个正文行是否为段落起点。段落边界按以下信号：
//  1. 段首缩进：行左边缘比正文左边距多出约一个字符宽；
//  2. 段落标记：第 X 条、第 X 编、(一)、一、 等；
//  3. 纵向间距明显大于页内常见行距（部分文档使用段间距而非缩进）。
func markdownParagraphStarts(rows [][]textdoc.Entry, inTable []bool, rowInfos []markdownRowInfo) []bool {
	starts := make([]bool, len(rows))

	margins := make(map[float64]int)
	for i, row := range rows {
		if inTable[i] || rowInfos[i].isTitle {
			continue
		}
		margins[math.Round(textdoc.RowLeft(row)*2)/2]++
	}
	// 取出现次数最多的左边距作为正文左边距；次数相同时取更小值，保证输出
	// 与 map 迭代顺序无关（否则段落判定会随运行变化）。
	margin, marginCount := 0.0, -1
	for value, count := range margins {
		if count > marginCount || (count == marginCount && value < margin) {
			margin, marginCount = value, count
		}
	}

	pageRight := 0.0
	for i, row := range rows {
		if inTable[i] || rowInfos[i].isTitle {
			continue
		}
		if right := textdoc.RowRight(row); right > pageRight {
			pageRight = right
		}
	}

	var gaps []float64
	previousY := math.NaN()
	for i, row := range rows {
		if inTable[i] || rowInfos[i].isTitle {
			previousY = math.NaN()
			continue
		}
		y := textdoc.RowTop(row)
		if !math.IsNaN(previousY) && y > previousY {
			gaps = append(gaps, y-previousY)
		}
		previousY = y
	}
	typicalGap := markdownMedian(gaps)

	previousY = math.NaN()
	for i, row := range rows {
		if inTable[i] || rowInfos[i].isTitle {
			previousY = math.NaN()
			continue
		}
		size := rowInfos[i].maxSize
		indent := textdoc.RowLeft(row) - margin
		// 段首缩进要求该行是通栏行，避免把居中的标题/落款逐行拆成段落。
		fullWidth := pageRight > 0 && textdoc.RowRight(row) >= pageRight-math.Max(3, size)
		switch {
		case indent > math.Max(2.5, size) && fullWidth:
			starts[i] = true
		case markdownParagraphMarkerRegex.MatchString(rowInfos[i].text):
			starts[i] = true
		case typicalGap > 0 && !math.IsNaN(previousY):
			if gap := textdoc.RowTop(row) - previousY; gap > typicalGap*1.5 {
				starts[i] = true
			}
		}
		previousY = textdoc.RowTop(row)
	}
	return starts
}

func markdownMedian(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

func escapeMarkdownLine(line string) string {
	line = markdownEscaper.Replace(line)
	if len(line) > 0 {
		switch line[0] {
		case '#', '-', '+', '=', '>':
			line = "\\" + line
		}
	}
	if index := strings.Index(line, ". "); index > 0 && allDigits(line[:index]) {
		line = line[:index] + `\.` + line[index+1:]
	}
	return line
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

const (
	h1Ratio = 2.0
	h2Ratio = 1.75
	h3Ratio = 1.5
	h4Ratio = 1.3
	h5Ratio = 1.15
)

func detectBodyFontSize(entries []textdoc.Entry) float64 {
	fontSizes := make(map[float64]int)
	for _, entry := range entries {
		if entry.Size > 0 {
			fontSizes[entry.Size]++
		}
	}
	maxCount := 0
	bodyFontSize := 6.0
	for size, count := range fontSizes {
		if count > maxCount || (count == maxCount && size < bodyFontSize) {
			maxCount = count
			bodyFontSize = size
		}
	}
	return bodyFontSize
}

func isNumberedHeader(text string) (bool, int) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false, 0
	}
	matches := numberedHeaderRegex.FindStringSubmatch(trimmed)
	if len(matches) > 0 {
		marker := matches[1]
		dotCount := min(strings.Count(marker, ".")+2, 6)
		if dotCount > 0 {
			return true, dotCount
		}
	}
	return false, 0
}

func calculateHeaderLevel(fontSize, bodyFontSize float64) int {
	if bodyFontSize <= 0 || fontSize <= 0 {
		return 0
	}
	ratio := fontSize / bodyFontSize
	switch {
	case ratio >= h1Ratio:
		return 1
	case ratio >= h2Ratio:
		return 2
	case ratio >= h3Ratio:
		return 3
	case ratio >= h4Ratio:
		return 4
	case ratio >= h5Ratio:
		return 5
	case ratio >= 1.0:
		return 6
	}
	return 0
}

func isBoldHeading(text string) bool {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) < 3 || len(trimmed) > 100 {
		return false
	}
	if !strings.HasPrefix(trimmed, "**") || !strings.HasSuffix(trimmed, "**") {
		return false
	}
	inner := strings.TrimSpace(trimmed[2 : len(trimmed)-2])
	if len(inner) < 3 || len(inner) > 100 {
		return false
	}
	if strings.Contains(inner, "\n") {
		return false
	}
	if len(inner) > 0 {
		first := inner[0]
		if first >= '0' && first <= '9' {
			return false
		}
	}
	last := inner[len(inner)-1]
	if last == '.' || last == ',' || last == ';' {
		return false
	}
	return true
}

// isPageNumber 判断文本是否为页码形式（纯数字或两侧带破折号的数字）。
func isPageNumber(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	return pageNumberRegex.MatchString(trimmed) || dashPageNumberRegex.MatchString(trimmed)
}

// isPageNumberRow 判断一行是否为页码行。只有页码形式的文本且位于页面顶部或
// 底部（页眉/页脚区域）时才认定为页码，避免把表格或数据中的纯数字行删掉。
func isPageNumberRow(row []textdoc.Entry, pageHeight float64) bool {
	if !isPageNumber(strings.TrimSpace(textdoc.JoinText(row))) {
		return false
	}
	if !textdoc.Finite(pageHeight) || pageHeight <= 0 {
		return true
	}
	y := textdoc.RowTop(row)
	return y < pageHeight*0.12 || y > pageHeight*0.88
}

func isBodyText(text string) bool {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) > 80 {
		return true
	}
	return strings.HasSuffix(trimmed, ".") || strings.HasSuffix(trimmed, "。") ||
		strings.HasSuffix(trimmed, "?") || strings.HasSuffix(trimmed, "？") ||
		strings.HasSuffix(trimmed, "!") || strings.HasSuffix(trimmed, "！")
}

func detectHeadingLevel(text string, fontSize, bodyFontSize float64) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}
	if isPageNumber(trimmed) {
		return 0
	}
	if isNumbered, level := isNumberedHeader(trimmed); isNumbered {
		return level
	}

	level := calculateHeaderLevel(fontSize, bodyFontSize)
	if level > 0 {
		firstRune := rune(trimmed[0])
		if !(firstRune >= 'A' && firstRune <= 'Z') &&
			!(firstRune >= '0' && firstRune <= '9') &&
			!(firstRune >= 0x4e00 && firstRune <= 0x9fff) {
			return 0
		}
		if isBodyText(trimmed) {
			return 0
		}
		return level
	}
	if isBoldHeading(trimmed) {
		return 4
	}
	return 0
}
