// Package converter 提供 OFD 文档到 PDF、HTML、文本、Markdown 和图像等格式的转换能力。
package converter

import (
	"image"
	"image/color"
	"io"

	"github.com/tdewolff/canvas"
)

// Converter 配置转换器
type Converter struct {
	dpi               canvas.Resolution
	format            string // png, jpeg, svg, eps, tex
	htmlImageFormat   string // png, svg
	bgColor           color.Color
	page              int
	thumbnail         int
	pageCacheCapacity int
	pageCacheBytes    int64
	pdfParallel       bool
	imageWriter       func(page int, img image.Image) error
	fileWriter        func(page int) (io.WriteCloser, error)
}

// Option 配置选项类型
type Option func(*Converter)

// 默认配置
var defaultConverter = &Converter{
	dpi:             canvas.DPI(300),
	format:          "png",
	htmlImageFormat: "png",
	bgColor:         color.Transparent,
	page:            0,
	thumbnail:       0,
}

// newConverter 创建转换器
func newConverter(options ...Option) *Converter {
	conv := &Converter{
		dpi:               defaultConverter.dpi,
		format:            defaultConverter.format,
		htmlImageFormat:   defaultConverter.htmlImageFormat,
		bgColor:           defaultConverter.bgColor,
		page:              defaultConverter.page,
		thumbnail:         defaultConverter.thumbnail,
		pageCacheCapacity: defaultConverter.pageCacheCapacity,
		pageCacheBytes:    defaultConverter.pageCacheBytes,
		pdfParallel:       false,
	}

	for _, opt := range options {
		opt(conv)
	}
	return conv
}
