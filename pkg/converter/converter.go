package converter

import (
	"image"
	"image/color"
	"io"

	"github.com/tdewolff/canvas"
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
