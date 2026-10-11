package converter

import "github.com/zc310/ofd/internal/pdf2ofd/config"

// PDFImportLimits 控制 PDF 导入的资源预算。零值字段使用默认值，负值无效。
type PDFImportLimits = config.Limits

// PDFImportDiagnostic 描述转换中无法完整保留的内容及其来源。
type PDFImportDiagnostic = config.Diagnostic

// PDFImportOptions 设置 PDF 导入的预算、诊断回调和严格模式。
type PDFImportOptions = config.Options

// PDFImportLimitError 包含超限的资源名称、上限和已观察到的用量。
type PDFImportLimitError = config.LimitError

// ErrPDFImportLimitExceeded 表示 PDF 导入超过配置的资源预算。
var ErrPDFImportLimitExceeded = config.ErrLimitExceeded

// ErrPDFImportUnsupported 表示严格模式遇到已报告的内容缺失或近似转换。
var ErrPDFImportUnsupported = config.ErrUnsupported

// DefaultPDFImportLimits 返回默认 PDF 导入预算。
func DefaultPDFImportLimits() PDFImportLimits { return config.DefaultLimits() }

// WithPDFImportOptions 设置 PDF→OFD 导入选项，也作用于 Office/HTML 经 PDF 的导入。
func WithPDFImportOptions(options PDFImportOptions) Option {
	return func(c *Converter) { c.pdfImportOptions = options }
}

// PDFImportOptions 返回本次转换的 PDF 导入配置。
func (c *Converter) PDFImportOptions() PDFImportOptions {
	if c == nil {
		return PDFImportOptions{}
	}
	return c.pdfImportOptions
}
