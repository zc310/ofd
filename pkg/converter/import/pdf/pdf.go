// Package pdf 为 converter 注册 PDF→OFD 导入器（输入格式 "pdf"）。
//
// 该包依赖 internal/pdf2ofd（进而依赖 pdfcpu）。单独成包是为了让只做 OFD→X
// 的使用方不必链接这些依赖；需要 PDF→OFD 时按空白导入启用：
//
//	import _ "github.com/zc310/ofd/pkg/converter/import/pdf"
//
// 之后即可使用 converter.Convert(ctx, "pdf", "ofd", ...)，或直接调用本包的
// Convert/ConvertFile。
package pdf

import (
	"context"
	"io"

	"github.com/zc310/ofd/internal/pdf2ofd"
	"github.com/zc310/ofd/pkg/converter"
)

// pdfImporter 把 PDF 输入导入为 OFD。
type pdfImporter struct{}

func (p *pdfImporter) Name() string         { return "pdf" }
func (p *pdfImporter) Extensions() []string { return []string{".pdf"} }
func (p *pdfImporter) MIME() string         { return "application/pdf" }

func (p *pdfImporter) Import(input any, output io.Writer, conv *converter.Converter) error {
	return pdf2ofd.ConvertWithOptions(conv.Context(), input, output, conv.InputPassword(), conv.PDFImportOptions())
}

// Options 控制直接 PDF 导入的口令及转换策略；零值使用默认预算并保存原始字体。
// 通用 converter.Convert 入口仍通过 WithPassword 和 WithPDFImportOptions 配置。
type Options struct {
	// PDFImportOptions 复用预算、字体、图片、诊断回调和严格模式配置。
	converter.PDFImportOptions

	// Password 用于打开加密 PDF；空值表示不提供口令。
	Password string
}
type Limits = converter.PDFImportLimits
type Diagnostic = converter.PDFImportDiagnostic
type LimitError = converter.PDFImportLimitError

var ErrLimitExceeded = converter.ErrPDFImportLimitExceeded
var ErrUnsupported = converter.ErrPDFImportUnsupported

// DefaultLimits 返回 PDF 导入的默认预算。
func DefaultLimits() Limits { return converter.DefaultPDFImportLimits() }

// Convert 将 PDF 输入转换为 OFD 文档，input 支持 PDF 文件路径、[]byte 或 io.Reader。
// 失败时 output 可能含部分输出，调用方必须丢弃；取消原样返回 ctx.Err()。
func Convert(ctx context.Context, input any, output io.Writer, options Options) error {
	return pdf2ofd.ConvertWithOptions(ctx, input, output, options.Password, options.PDFImportOptions)
}

// ConvertFile 在转换成功后原子替换输出文件；失败或取消保留原有文件。
func ConvertFile(ctx context.Context, pdfPath, ofdPath string, options Options) error {
	return pdf2ofd.ConvertFileWithOptions(ctx, pdfPath, ofdPath, options.Password, options.PDFImportOptions)
}

func init() {
	converter.RegisterImporter(&pdfImporter{})
}
