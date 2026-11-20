package main

const exportDPI = 150

//nolint:gochecknoglobals // 菜单和导出校验共用固定格式列表。
var exportFormats = [...]string{"pdf", "txt", "jpg", "png", "svg"}

type exportResult struct {
	path string
	err  error
}

type exportOptions struct {
	format string
	dpi    int
	white  bool
}

func exportExtension(format string, pages int) string {
	if format != "pdf" && format != "txt" && pages > 1 {
		return "zip"
	}
	return format
}
