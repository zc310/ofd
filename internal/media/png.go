package media

import (
	"image"
	"io"
	"sync"

	wpng "github.com/woozymasta/png"
)

var pngEncoderBuffers sync.Pool

type encoderBufferPool struct {
	pool *sync.Pool
}

func (p encoderBufferPool) Get() *wpng.EncoderBuffer {
	if buffer := p.pool.Get(); buffer != nil {
		return buffer.(*wpng.EncoderBuffer)
	}
	return new(wpng.EncoderBuffer)
}

func (p encoderBufferPool) Put(buffer *wpng.EncoderBuffer) {
	p.pool.Put(buffer)
}

// EncodePNG 使用默认压缩级别和池化的 PNG 编码缓冲区写出图像。
func EncodePNG(w io.Writer, img image.Image) error {
	return encodePNG(w, img, wpng.DefaultCompression)
}

// EncodePNGLevel 使用指定压缩级别和池化缓冲区写出图像。
func EncodePNGLevel(w io.Writer, img image.Image, level int) error {
	return encodePNG(w, img, wpng.CompressionLevel(level))
}

func encodePNG(w io.Writer, img image.Image, level wpng.CompressionLevel) error {
	encoder := wpng.Encoder{
		CompressionLevel: level,
		BufferPool:       encoderBufferPool{pool: &pngEncoderBuffers},
	}
	return encoder.Encode(w, img)
}
