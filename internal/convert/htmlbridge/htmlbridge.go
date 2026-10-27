// Package htmlbridge 将预处理为 HTML 的输入接入现有浏览器转换链，统一复用
// 纸张、临时目录、资源访问、取消处理以及 PDF 导入设置。
package htmlbridge

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/zc310/ofd/internal/contextio"
	"github.com/zc310/ofd/internal/pdf2ofd"
	"github.com/zc310/ofd/pkg/converter"
	_ "github.com/zc310/ofd/pkg/converter/import/html"
	"github.com/zc310/ofd/pkg/creator"
)

// Document 是预处理后的页面内容和需原样保留的上游附件。
type Document struct {
	HTML        []byte
	Attachments []creator.Attachment
}

// Importer 对输入执行 Prepare，复用 HTML→PDF，然后在 OFD 写出时携带附件。
type Importer struct {
	Format, MIMEType string
	Prepare          func(input any, conv *converter.Converter) (Document, error)
}

func (i *Importer) Name() string         { return i.Format }
func (i *Importer) Extensions() []string { return []string{"." + i.Format} }
func (i *Importer) MIME() string         { return i.MIMEType }

func (i *Importer) Import(input any, output io.Writer, conv *converter.Converter) error {
	data, err := i.Prepare(input, conv)
	if err != nil {
		return err
	}
	transformer, ok := converter.TransformerFor("html", "pdf")
	if !ok {
		return fmt.Errorf("未注册 HTML→PDF 转换器")
	}
	var pdf bytes.Buffer
	if err := transformer.Transform(data.HTML, &pdf, conv); err != nil {
		return err
	}
	return pdf2ofd.ConvertWithAttachments(conv.Context(), pdf.Bytes(), output, "", conv.PDFImportOptions(), data.Attachments)
}

// PDFTransformer 直接输出浏览器 PDF，避免为 PDF 目标执行 OFD 往返转换。
type PDFTransformer struct{ Importer *Importer }

func (t PDFTransformer) From() []string { return []string{t.Importer.Format} }
func (t PDFTransformer) To() string     { return "pdf" }
func (t PDFTransformer) Transform(input any, output io.Writer, conv *converter.Converter) error {
	if output == nil {
		return fmt.Errorf("未设置 PDF 输出参数")
	}
	data, err := t.Importer.Prepare(input, conv)
	if err != nil {
		return err
	}
	transformer, ok := converter.TransformerFor("html", "pdf")
	if !ok {
		return fmt.Errorf("未注册 HTML→PDF 转换器")
	}
	return transformer.Transform(data.HTML, output, conv)
}

// ReadInput 有界读取路径、字节或 Reader 输入，调用方传入格式专用的原始字节上限。
func ReadInput(ctx context.Context, input any, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var reader io.Reader
	switch value := input.(type) {
	case string:
		file, err := os.Open(value)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		reader = file
	case []byte:
		if int64(len(value)) > limit {
			return nil, fmt.Errorf("输入超过大小上限 %d 字节", limit)
		}
		return value, nil
	case io.Reader:
		reader = value
	default:
		return nil, fmt.Errorf("不支持的输入类型 %T", input)
	}
	data, err := io.ReadAll(io.LimitReader(contextio.Reader(ctx, reader), limit+1))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("输入超过大小上限 %d 字节", limit)
	}
	return data, nil
}
