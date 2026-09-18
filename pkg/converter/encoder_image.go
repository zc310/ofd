package converter

import (
	"errors"
	"io"

	"github.com/tdewolff/canvas/renderers"
	"github.com/zc310/ofd/internal/render"
)

func init() {
	Register(&pngEncoder{})
	Register(&jpegEncoder{})
	Register(&svgEncoder{})
	Register(&epsEncoder{})
	Register(&texEncoder{})
}

type pngEncoder struct{}

func (e *pngEncoder) Name() string         { return "png" }
func (e *pngEncoder) Kind() Kind           { return KindImage }
func (e *pngEncoder) Extensions() []string { return []string{".png"} }
func (e *pngEncoder) MIME() string         { return "image/png" }
func (e *pngEncoder) Encode(input any, output io.Writer, conv *Converter) error {
	conv.format = "png"
	conv.renderer = renderers.PNG(conv.dpi)
	return encodeOFD(input, output, conv, encodeDocuments)
}

type jpegEncoder struct{}

func (e *jpegEncoder) Name() string         { return "jpeg" }
func (e *jpegEncoder) Kind() Kind           { return KindImage }
func (e *jpegEncoder) Extensions() []string { return []string{".jpg", ".jpeg"} }
func (e *jpegEncoder) MIME() string         { return "image/jpeg" }
func (e *jpegEncoder) Encode(input any, output io.Writer, conv *Converter) error {
	conv.format = "jpeg"
	conv.renderer = renderers.JPEG(conv.dpi)
	return encodeOFD(input, output, conv, encodeDocuments)
}

type svgEncoder struct{}

func (e *svgEncoder) Name() string         { return "svg" }
func (e *svgEncoder) Kind() Kind           { return KindImage }
func (e *svgEncoder) Extensions() []string { return []string{".svg"} }
func (e *svgEncoder) MIME() string         { return "image/svg+xml" }
func (e *svgEncoder) Encode(input any, output io.Writer, conv *Converter) error {
	conv.format = "svg"
	conv.renderer = renderers.SVG()
	return encodeOFD(input, output, conv, encodeDocuments)
}

type epsEncoder struct{}

func (e *epsEncoder) Name() string         { return "eps" }
func (e *epsEncoder) Kind() Kind           { return KindImage }
func (e *epsEncoder) Extensions() []string { return []string{".eps"} }
func (e *epsEncoder) MIME() string         { return "application/postscript" }
func (e *epsEncoder) Encode(input any, output io.Writer, conv *Converter) error {
	conv.format = "eps"
	conv.renderer = renderers.EPS()
	return encodeOFD(input, output, conv, encodeDocuments)
}

type texEncoder struct{}

func (e *texEncoder) Name() string         { return "tex" }
func (e *texEncoder) Kind() Kind           { return KindImage }
func (e *texEncoder) Extensions() []string { return []string{".tex"} }
func (e *texEncoder) MIME() string         { return "application/x-tex" }
func (e *texEncoder) Encode(input any, output io.Writer, conv *Converter) error {
	conv.format = "tex"
	conv.renderer = renderers.TeX()
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

// Image 渲染 OFD 文档。输出格式由 PNG、JPG、SVG、EPS、TeX 或 WithFormat 选项决定，
// 默认 PNG；输出目标由 Writer 或 ImageWriter 选项提供。
func Image(input interface{}, opts ...Option) error {
	conv := newConverter(opts...)
	if err := conv.validateConfig(); err != nil {
		return err
	}
	return encodeWithConverterFormat(imageFormatName(conv), input, nil, conv)
}

// ImageDocument 将已解析的 OFD 文档渲染为图像或矢量格式。
func ImageDocument(doc *render.Document, opts ...Option) error {
	return ImageDocuments([]*render.Document{doc}, opts...)
}

// ImageDocuments 将多个已解析的 OFD 文档体按全局页码渲染为图像或矢量格式。
func ImageDocuments(documents []*render.Document, opts ...Option) error {
	conv := newConverter(opts...)
	if len(collectDocumentPages(documents)) == 0 {
		return errors.New("文档没有页面")
	}
	f, err := lookupFormat(imageFormatName(conv))
	if err != nil {
		return err
	}
	return f.encoder.Encode(documents, nil, conv)
}

// imageFormatName 返回 Converter 中配置的图像格式名，未配置时回退到 PNG。
func imageFormatName(conv *Converter) string {
	if conv.format == "" {
		return "png"
	}
	return conv.format
}
