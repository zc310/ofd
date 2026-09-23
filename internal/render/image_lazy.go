package render

import (
	"bytes"
	"image"
	"image/color"
	"sync"
)

// EncodedImage 是保留原始编码字节的懒解码图片：以标准 image.Image 暴露，
// 首次访问时才解码；同时保留编码数据，供 canvas PDF 写入器等按原字节内嵌。
// 它与绘制库无关；canvas 适配层可把编码数据再包装成 canvas/image.Image。
type EncodedImage struct {
	Format string // "jpeg" 或 "png"
	Data   []byte

	once sync.Once
	img  image.Image
	err  error

	configOnce sync.Once
	config     image.Config
	configErr  error

	canvas any // canvas 适配层缓存，避免中性代码依赖 canvas
}

// newEncodedImage 返回保留编码数据的懒解码图片。
func newEncodedImage(format string, data []byte) *EncodedImage {
	return &EncodedImage{Format: format, Data: data}
}

// Image 解码并返回底层原生图像，结果会缓存。它同时满足各后端用于“延迟解码
// 包装器”的最小接口（Image() (image.Image, error)）。
func (e *EncodedImage) Image() (image.Image, error) {
	if e == nil {
		return nil, nil
	}
	e.once.Do(func() {
		decoded, _, err := image.Decode(bytes.NewReader(e.Data))
		e.img, e.err = decoded, err
	})
	return e.img, e.err
}

// Bounds 返回图片边界。只读取图片头（DecodeConfig）获取尺寸，不触发完整解码。
func (e *EncodedImage) Bounds() image.Rectangle {
	if e == nil {
		return image.Rectangle{}
	}
	e.configOnce.Do(func() {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(e.Data))
		e.config, e.configErr = cfg, err
	})
	if e.configErr != nil || e.config.Width <= 0 || e.config.Height <= 0 {
		return image.Rectangle{}
	}
	return image.Rect(0, 0, e.config.Width, e.config.Height)
}

// At 返回像素颜色（会触发解码）。
func (e *EncodedImage) At(x, y int) color.Color {
	img, err := e.Image()
	if err != nil || img == nil {
		return color.RGBA{}
	}
	return img.At(x, y)
}

// ColorModel 返回颜色模型。只读取图片头，不触发完整解码。
func (e *EncodedImage) ColorModel() color.Model {
	if e == nil {
		return color.RGBAModel
	}
	e.configOnce.Do(func() {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(e.Data))
		e.config, e.configErr = cfg, err
	})
	if e.configErr != nil || e.config.ColorModel == nil {
		return color.RGBAModel
	}
	return e.config.ColorModel
}

// weight 估算该图片在缓存中的内存占用：编码字节 + 解码后的像素。只读取
// 图片头（DecodeConfig）获取尺寸与颜色模型，不触发完整解码。
func (e *EncodedImage) Weight() int64 {
	if e == nil {
		return 1
	}
	weight := int64(len(e.Data))
	e.configOnce.Do(func() {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(e.Data))
		e.config, e.configErr = cfg, err
	})
	if e.configErr == nil && e.config.Width > 0 && e.config.Height > 0 {
		weight += int64(e.config.Width) * int64(e.config.Height) * imageBytesPerPixel(e.config.ColorModel)
	}
	if weight <= 0 {
		return 1
	}
	return weight
}

// canvasCached 返回 canvas 适配层缓存的对象（供 canvas_backend.go 使用）。
func (e *EncodedImage) CanvasCached() any {
	if e == nil {
		return nil
	}
	return e.canvas
}

// setCanvasCached 缓存 canvas 适配层构造的对象。
func (e *EncodedImage) SetCanvasCached(v any) {
	if e != nil {
		e.canvas = v
	}
}

// NewEncodedImage 返回保留编码数据的懒解码图片（导出供 canvas 等后端或测试使用）。
func NewEncodedImage(format string, data []byte) *EncodedImage { return newEncodedImage(format, data) }
