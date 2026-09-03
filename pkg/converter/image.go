package converter

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"log/slog"

	"github.com/nao1215/imaging"
	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
)

// Converter 配置转换器
type Converter struct {
	dpi         canvas.Resolution
	format      string // png, jpeg, svg, eps, tex
	bgColor     color.Color
	page        int
	thumbnail   int
	imageWriter func(page int, img image.Image) error
	fileWriter  func(page int) (io.WriteCloser, error)
}

// Option 配置选项类型
type Option func(*Converter)

// 默认配置
var defaultConverter = &Converter{
	dpi:       canvas.DPI(300),
	format:    "png",
	bgColor:   color.Transparent,
	page:      0,
	thumbnail: 0,
}

// newConverter 创建转换器
func newConverter(options ...Option) *Converter {
	conv := &Converter{
		dpi:       defaultConverter.dpi,
		format:    defaultConverter.format,
		bgColor:   defaultConverter.bgColor,
		page:      defaultConverter.page,
		thumbnail: defaultConverter.thumbnail,
	}

	for _, opt := range options {
		opt(conv)
	}
	return conv
}

// Thumbnail 设置缩略图大小
func Thumbnail(s int) Option {
	return func(c *Converter) {
		c.thumbnail = s
	}
}

// Writer 设置文件写入器
func Writer(f func(page int) (io.WriteCloser, error)) Option {
	return func(c *Converter) {
		c.fileWriter = f
	}
}

// ImageWriter 设置图像写入器
func ImageWriter(f func(page int, img image.Image) error) Option {
	return func(c *Converter) {
		c.imageWriter = f
	}
}

// DPI 设置DPI
func DPI(dpi float64) Option {
	return func(c *Converter) {
		c.dpi = canvas.DPI(dpi)
	}
}

// PNG 设置为PNG格式
func PNG() Option {
	return func(c *Converter) {
		c.format = "png"
	}
}

// JPG 设置为JPEG格式
func JPG() Option {
	return func(c *Converter) {
		c.format = "jpeg"
	}
}

// SVG 设置为 SVG 格式
func SVG() Option {
	return func(c *Converter) {
		c.format = "svg"
	}
}

// EPS 设置为 Encapsulated PostScript 格式
func EPS() Option {
	return func(c *Converter) {
		c.format = "eps"
	}
}

// TeX 设置为 TeX/PGF 格式
func TeX() Option {
	return func(c *Converter) {
		c.format = "tex"
	}
}

// BgColor 设置背景颜色
func BgColor(bg color.Color) Option {
	return func(c *Converter) {
		c.bgColor = bg
	}
}

// Page 设置全局页码，从 1 开始；0 表示处理全部页面。
func Page(page int) Option {
	return func(c *Converter) {
		c.page = page
	}
}

// renderPage 渲染单个页面
func (c *Converter) renderPage(pageNumber int, page *canvas.Canvas) error {
	// 文件写入器处理
	if c.fileWriter != nil {
		w, err := c.fileWriter(pageNumber)
		if err != nil {
			return fmt.Errorf("创建文件写入器失败: %w", err)
		}
		defer func() {
			if err := w.Close(); err != nil {
				slog.Error("关闭文件写入器失败", "error", err)
			}
		}()

		var renderer canvas.Writer
		switch c.format {
		case "jpeg":
			renderer = renderers.JPEG(c.dpi)
		case "svg":
			renderer = renderers.SVG()
		case "eps":
			renderer = renderers.EPS()
		case "tex":
			renderer = renderers.TeX()
		default:
			renderer = renderers.PNG(c.dpi)
		}

		if err := page.Write(w, renderer); err != nil {
			return fmt.Errorf("写入第%d页失败: %w", pageNumber, err)
		}
	}

	// 图像写入器处理
	if c.imageWriter != nil {
		var img image.Image
		img = rasterizer.Draw(page, c.dpi, canvas.DefaultColorSpace)

		// 缩略图处理
		if c.thumbnail > 0 {
			img = c.resizeThumbnail(img)
		}

		if err := c.imageWriter(pageNumber, img); err != nil {
			return fmt.Errorf("写入第%d页图像失败: %w", pageNumber, err)
		}
	}

	return nil
}

// resizeThumbnail 生成缩略图
func (c *Converter) resizeThumbnail(img image.Image) image.Image {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width > height {
		return imaging.Resize(img, c.thumbnail, 0, imaging.Lanczos)
	}
	return imaging.Resize(img, 0, c.thumbnail, imaging.Lanczos)
}

// validateConfig 验证配置
func (c *Converter) validateConfig() error {
	if c.fileWriter == nil && c.imageWriter == nil {
		return errors.New("未设置图像输出参数")
	}
	return nil
}

// Image 渲染OFD文档
func Image(input interface{}, opts ...Option) error {
	conv := newConverter(opts...)

	// 验证配置
	if err := conv.validateConfig(); err != nil {
		return err
	}

	// 解析 OFD
	ofd, err := parser.NewOFD(input)
	if err != nil {
		return fmt.Errorf("解析OFD失败: %w", err)
	}
	defer func() {
		if err := ofd.Close(); err != nil {
			slog.Error("关闭OFD文档失败", "error", err)
		}
	}()

	// 验证文档
	if len(ofd.Documents) == 0 {
		return errors.New("没有文档")
	}

	// 创建渲染文档。每个文档体保留独立的资源上下文。
	documents := make([]*render.Document, 0, len(ofd.Documents))
	for _, document := range ofd.Documents {
		documents = append(documents, render.NewDocument(conv.bgColor, document))
	}
	if len(collectDocumentPages(documents)) == 0 {
		return errors.New("文档没有页面")
	}

	return conv.renderDocuments(documents)
}

// ImageDocument 将已解析的 OFD 文档渲染为图像或矢量格式。
func ImageDocument(doc *render.Document, opts ...Option) error {
	conv := newConverter(opts...)
	if len(collectDocumentPages([]*render.Document{doc})) == 0 {
		return errors.New("文档没有页面")
	}
	return conv.renderDocuments([]*render.Document{doc})
}

// ImageDocuments 将多个已解析的 OFD 文档体按全局页码渲染为图像或矢量格式。
func ImageDocuments(documents []*render.Document, opts ...Option) error {
	conv := newConverter(opts...)
	if len(collectDocumentPages(documents)) == 0 {
		return errors.New("文档没有页面")
	}
	if err := conv.validateConfig(); err != nil {
		return err
	}
	return conv.renderDocuments(documents)
}

func (c *Converter) renderDocument(doc *render.Document) error {
	return c.renderDocuments([]*render.Document{doc})
}

func (c *Converter) renderDocuments(documents []*render.Document) error {
	pages := collectDocumentPages(documents)
	if len(pages) == 0 {
		return errors.New("文档没有页面")
	}
	pageStart, pageEnd, err := pageRange(len(pages), c.page)
	if err != nil {
		return err
	}
	pages = pages[pageStart:pageEnd]

	for _, page := range pages {
		canvasPage, err := page.document.Page(page.document.Pages[page.pageIndex])
		if err != nil {
			return fmt.Errorf("处理第%d页失败: %w", page.pageNumber, err)
		}

		if err := c.renderPage(page.pageNumber, canvasPage); err != nil {
			return err
		}
	}
	return nil
}
