package markdown

import (
	"strings"

	"github.com/zc310/ofd/internal/textdoc"
)

// RenderTable 按 GFM 语法输出表格，表头为第一行。
func RenderTable(markdown *strings.Builder, table textdoc.Table) {
	columns := len(table.Header)
	if columns == 0 {
		return
	}
	markdown.WriteByte('\n')
	writeTableRow(markdown, table.Header, columns)
	markdown.WriteByte('|')
	for range columns {
		markdown.WriteString(" --- |")
	}
	markdown.WriteByte('\n')
	for _, row := range table.Rows {
		writeTableRow(markdown, row, columns)
	}
	markdown.WriteByte('\n')
}

func writeTableRow(markdown *strings.Builder, cells []string, columns int) {
	markdown.WriteByte('|')
	for i := range columns {
		markdown.WriteByte(' ')
		if i < len(cells) {
			markdown.WriteString(tableCell(cells[i]))
		}
		markdown.WriteString(" |")
	}
	markdown.WriteByte('\n')
}

// tableCell 转义 GFM 表格单元格中的竖线和反斜杠，并把换行压成空格。
func tableCell(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.TrimSpace(value)
}
