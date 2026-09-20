package converter

import (
	"strings"

	"github.com/zc310/ofd/internal/textdoc"
)

// renderMarkdownTable 按 GFM 语法输出表格，表头为第一行。
func renderMarkdownTable(markdown *strings.Builder, table textdoc.Table) {
	columns := len(table.Header)
	if columns == 0 {
		return
	}
	markdown.WriteByte('\n')
	writeMarkdownTableRow(markdown, table.Header, columns)
	markdown.WriteByte('|')
	for range columns {
		markdown.WriteString(" --- |")
	}
	markdown.WriteByte('\n')
	for _, row := range table.Rows {
		writeMarkdownTableRow(markdown, row, columns)
	}
	markdown.WriteByte('\n')
}

func writeMarkdownTableRow(markdown *strings.Builder, cells []string, columns int) {
	markdown.WriteByte('|')
	for i := range columns {
		markdown.WriteByte(' ')
		if i < len(cells) {
			markdown.WriteString(markdownTableCell(cells[i]))
		}
		markdown.WriteString(" |")
	}
	markdown.WriteByte('\n')
}

// markdownTableCell 转义 GFM 表格单元格中的竖线和反斜杠，并把换行压成空格。
func markdownTableCell(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.TrimSpace(value)
}
