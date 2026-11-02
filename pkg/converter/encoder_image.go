package converter

import (
	"context"
	"errors"
	"io"

	"github.com/zc310/ofd/internal/render"
)

func init() {
	Register(&pngEncoder{})
	Register(&jpegEncoder{})
	Register(&svgEncoder{})
}

type pngEncoder struct{}

var errPNGCompressionLevel = errors.New("PNG 压缩级别必须为 1-9（0 使用默认级别 7）")

func (e *pngEncoder) Name() string         { return "png" }
func (e *pngEncoder) Kind() Kind           { return KindImage }
func (e *pngEncoder) Extensions() []string { return []string{".png"} }
func (e *pngEncoder) MIME() string         { return "image/png" }
func (e *pngEncoder) Encode(input any, output io.Writer, conv *Converter) error {
	if conv.pngCompressionLevel < 1 || conv.pngCompressionLevel > 9 {
		return errPNGCompressionLevel
	}
	conv.format = "png"
	return encodeOFD(input, output, conv, encodeDocuments)
}

type jpegEncoder struct{}

func (e *jpegEncoder) Name() string         { return "jpeg" }
func (e *jpegEncoder) Kind() Kind           { return KindImage }
func (e *jpegEncoder) Extensions() []string { return []string{".jpg", ".jpeg"} }
func (e *jpegEncoder) MIME() string         { return "image/jpeg" }
func (e *jpegEncoder) Encode(input any, output io.Writer, conv *Converter) error {
	conv.format = "jpeg"
	return encodeOFD(input, output, conv, encodeDocuments)
}

type svgEncoder struct{}

func (e *svgEncoder) Name() string         { return "svg" }
func (e *svgEncoder) Kind() Kind           { return KindImage }
func (e *svgEncoder) Extensions() []string { return []string{".svg"} }
func (e *svgEncoder) MIME() string         { return "image/svg+xml" }
func (e *svgEncoder) Encode(input any, output io.Writer, conv *Converter) error {
	conv.format = "svg"
	return encodeOFD(input, output, conv, encodeDocuments)
}

// encodeDocuments 使用已配置的 Converter 将文档编码到 output。
// output 非空时覆盖 Converter 的文件写入器；为空时沿用 Option 中的 Writer/ImageWriter。
func encodeDocuments(documents []*render.Document, output io.Writer, conv *Converter) error {
	if output != nil {
		conv.fileWriter = func(page int) (io.WriteCloser, error) {
			return &nopWriteCloser{output}, nil
		}
	}
	return conv.renderDocuments(documents)
}

// nopWriteCloser 包装 io.Writer 使其满足 io.WriteCloser 接口。
type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

// Image 渲染 OFD 文档。输出格式由 PNG、JPG、TIFF、SVG 或 WithFormat 选项决定，
// 默认 PNG；输出目标由 Writer 或 ImageWriter 选项提供。
func Image(ctx context.Context, input interface{}, opts ...Option) error {
	conv := newConverter(ctx, opts...)
	if err := conv.checkCancelled(); err != nil {
		return err
	}
	if err := conv.validateConfig(); err != nil {
		return err
	}
	return encodeWithConverterFormat(imageFormatName(conv), input, nil, conv)
}

// imageFormatName 返回 Converter 中配置的图像格式名，未配置时回退到 PNG。
func imageFormatName(conv *Converter) string {
	if conv.format == "" {
		return "png"
	}
	return conv.format
}
