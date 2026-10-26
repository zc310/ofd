package report

import (
	"path/filepath"
	"strings"
)

// FromOutput 根据报告输出路径扩展名推断报告格式。
func FromOutput(output string) (string, bool) {
	switch strings.ToLower(filepath.Ext(output)) {
	case ".txt":
		return "text", true
	case ".md", ".markdown":
		return "markdown", true
	case ".json":
		return "json", true
	case ".ofd":
		return "ofd", true
	case ".pdf":
		return "pdf", true
	case ".xlsx":
		return "xlsx", true
	default:
		return "", false
	}
}
