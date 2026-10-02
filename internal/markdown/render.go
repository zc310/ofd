// Package markdown 把 OFD 页面渲染成 Markdown（GFM）。
//
// 版面结构推断——标题层级、正文基准字号、段落边界、页码行、表格识别——在
// internal/textdoc，与 DOCX 共用同一套判定；本包只负责把文字行编排成 Markdown
// 结构并处理 GFM 转义。表格识别默认由调用方关闭，误判成表格的代价比漏判高。
package markdown

import (
	"regexp"
	"strings"

	"github.com/zc310/ofd/internal/textdoc"
)

var (
	// chapterHeaderRegex 匹配“第 X 章”形式的章节标题。
	chapterHeaderRegex = regexp.MustCompile(`^第\s*[0-9一二三四五六七八九十百千万]+\s*章`)

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

func RenderPage(markdown *strings.Builder, entries []textdoc.Entry, pageHeight float64, detectTables bool) {
	bodyFontSize := textdoc.BodyFontSize(entries)
	allRows := textdoc.Rows(entries)

	// 过滤空行和页码行，保留可用于表格识别的行。纯数字只有位于页面顶部或
	// 底部时才按页码处理，避免误删表格或数据中的数字。
	rows := make([][]textdoc.Entry, 0, len(allRows))
	for _, row := range allRows {
		if len(row) == 0 {
			continue
		}
		if textdoc.IsPageNumberRow(row, pageHeight) {
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

	rowInfos := make([]textdoc.RowInfo, len(rows))
	for i, row := range rows {
		if inTable[i] {
			continue
		}
		maxSize := textdoc.RowSize(row)
		rowText := strings.TrimSpace(textdoc.JoinText(row))
		level := textdoc.DetectHeadingLevel(rowText, maxSize, bodyFontSize)
		rowInfos[i] = textdoc.RowInfo{Text: rowText, MaxSize: maxSize, Level: level, IsTitle: level > 0}
	}

	// 归一化标题层级：把本页最浅的标题提升为第 1 级。
	minLevel := 7
	for i := range rowInfos {
		if inTable[i] {
			continue
		}
		if rowInfos[i].IsTitle && rowInfos[i].Level < minLevel {
			minLevel = rowInfos[i].Level
		}
	}
	if minLevel > 1 {
		for i := range rowInfos {
			if inTable[i] || !rowInfos[i].IsTitle {
				continue
			}
			rowInfos[i].Level -= minLevel - 1
			if rowInfos[i].Level < 1 {
				rowInfos[i].Level = 1
			}
			if rowInfos[i].Level > 6 {
				rowInfos[i].Level = 6
			}
		}
	}

	paragraphStarts := textdoc.ParagraphStarts(rows, inTable, rowInfos)

	prevWasTitle := false
	emitted := false
	for i := range rows {
		if table, ok := tableAt[i]; ok {
			RenderTable(markdown, table)
			prevWasTitle = false
			emitted = true
			continue
		}
		if inTable[i] {
			continue
		}
		ri := rowInfos[i]
		if ri.Text == "" {
			continue
		}
		escaped := EscapeLine(ri.Text)

		if chapterHeaderRegex.MatchString(ri.Text) {
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

		if ri.Level > 0 {
			if !prevWasTitle {
				markdown.WriteByte('\n')
			}
			level := ri.Level + 2
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

// EscapeLine 转义一行 Markdown 文本：转义表格特殊字符，并给会被解析成块级
// 标记的行首字符与有序列表序号补反斜杠。
func EscapeLine(line string) string {
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
