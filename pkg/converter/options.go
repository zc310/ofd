package converter

import (
	"image"
	"image/color"
	"io"

	"github.com/tdewolff/canvas"
)

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
