package converter

import (
	"fmt"
	"strings"
)

// CSVImportOptions 控制 CSV/TSV 的文字解码和表格排版。零值使用 UTF-8、首行表头、
// 自动列宽和系统中文字体；纯拉丁文本使用内置 Go 字体。纸张与方向使用 Paper 配置。
type CSVImportOptions struct {
	// NoHeader 将首条记录作为普通数据，不生成续页重复表头。
	NoHeader bool
	// Title 为空时使用输入文件名（不含扩展名），字节或 Reader 输入使用 CSV/TSV。
	Title string
	// Encoding 支持 utf-8（可带 BOM）、gb18030 和 gbk，不自动猜测编码。
	Encoding string
	// FontFile 指定需要嵌入的 TTF、OTF 或 TTC（第一个字体面）。
	FontFile string
	// FontData 与 FontFile 互斥；调用期间只读借用，适合没有本地文件的调用方。
	FontData []byte
}

// Normalize 补齐默认编码，并检查互斥配置。
func (o CSVImportOptions) Normalize() (CSVImportOptions, error) {
	o.Encoding = strings.ToLower(strings.TrimSpace(o.Encoding))
	switch o.Encoding {
	case "", "utf8", "utf-8":
		o.Encoding = "utf-8"
	case "gb18030", "gbk":
	default:
		return o, fmt.Errorf("CSV/TSV 编码必须为 utf-8、gb18030 或 gbk")
	}
	if o.FontFile != "" && len(o.FontData) != 0 {
		return o, fmt.Errorf("CSV/TSV 的 FontFile 与 FontData 不能同时设置")
	}
	return o, nil
}

// WithCSVImportOptions 设置 CSV 与 TSV 共用的导入选项。
func WithCSVImportOptions(options CSVImportOptions) Option {
	return func(c *Converter) { c.csvImportOptions = options }
}

// CSVImportOptions 返回本次转换的 CSV/TSV 导入配置。
func (c *Converter) CSVImportOptions() CSVImportOptions {
	if c == nil {
		return CSVImportOptions{}
	}
	return c.csvImportOptions
}
