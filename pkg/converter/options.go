package converter

import (
	"image"
	"image/color"
	"io"
	"strings"

	"github.com/tdewolff/canvas"
)

// WithFormat 按注册的格式名（或别名）设置输出格式。
func WithFormat(format string) Option {
	return func(c *Converter) {
		c.format = strings.ToLower(strings.TrimSpace(format))
	}
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
func PNG() Option { return WithFormat("png") }

// JPG 设置为JPEG格式
func JPG() Option { return WithFormat("jpeg") }

// SVG 设置为 SVG 格式
func SVG() Option { return WithFormat("svg") }

// HTMLPNG 设置 HTML 页面使用内嵌 PNG 图片。
func HTMLPNG() Option {
	return func(c *Converter) {
		c.htmlImageFormat = "png"
	}
}

// HTMLJPG 设置 HTML 页面使用内嵌 JPG 图片。
func HTMLJPG() Option {
	return func(c *Converter) {
		c.htmlImageFormat = "jpg"
	}
}

// HTMLSVG 设置 HTML 页面使用内嵌 SVG 内容。
func HTMLSVG() Option {
	return func(c *Converter) {
		c.htmlImageFormat = "svg"
	}
}

// EPS 设置为 Encapsulated PostScript 格式
func EPS() Option { return WithFormat("eps") }

// TeX 设置为 TeX/PGF 格式
func TeX() Option { return WithFormat("tex") }

// BgColor 设置背景颜色
func BgColor(bg color.Color) Option {
	return func(c *Converter) {
		c.bgColor = bg
	}
}

// PageCache 设置页面缓存的最大页数和估算最大字节数；0 表示使用解析器默认值。
func PageCache(capacity int, maxBytes int64) Option {
	return func(c *Converter) {
		c.pageCacheCapacity = capacity
		c.pageCacheBytes = maxBytes
	}
}

// Page 设置全局页码，从 1 开始；0 表示处理全部页面。
func Page(page int) Option {
	return func(c *Converter) {
		c.page = page
	}
}

// PDFParallel 控制 PDF 页面是否并行渲染；默认关闭。
func PDFParallel(enabled bool) Option {
	return func(c *Converter) {
		c.pdfParallel = enabled
	}
}
