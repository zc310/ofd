package preview

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/gif" // 额外格式只在解码为静态 PNG 后交给网页。
	_ "image/jpeg"
	"image/png"
	"slices"

	_ "golang.org/x/image/bmp"  // 注册额外格式，输出前统一转换为受限静态 PNG。
	_ "golang.org/x/image/tiff" // 注册额外格式，输出前统一转换为受限静态 PNG。
	_ "golang.org/x/image/webp" // 注册额外格式，输出前统一转换为受限静态 PNG。
)

const (
	// MaxImageBytes 限制光栅预览的输入和编码输出大小。
	MaxImageBytes = 8 << 20
	// MaxImagePixels 限制光栅预览完整解码前的像素数。
	MaxImagePixels = 25000000
)

var (
	ErrImageSize       = errors.New("图片文件超出预览限制")
	ErrImageFormat     = errors.New("仅支持预览 PNG 或 JPEG 图片")
	ErrImageDimensions = errors.New("图片尺寸超出预览限制")
	ErrRasterFormat    = errors.New("仅支持预览 PNG、JPEG、GIF、BMP、TIFF 或 WebP 光栅图片")
)

// Image 是经过格式和尺寸检查的只读图片预览。
type Image struct {
	MIME string `json:"mime"`
	Data []byte `json:"data"`
}

func rasterConfig(data []byte) (image.Config, string, error) {
	return checkedConfig(data, ErrRasterFormat, "png", "jpeg", "gif", "bmp", "tiff", "webp")
}

func checkedConfig(data []byte, formatErr error, formats ...string) (image.Config, string, error) {
	if len(data) > MaxImageBytes {
		return image.Config{}, "", ErrImageSize
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || !slices.Contains(formats, format) {
		return image.Config{}, "", formatErr
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > MaxImagePixels/config.Height {
		return image.Config{}, "", ErrImageDimensions
	}
	return config, format, nil
}

// PNGJPEG 按实际内容检查 PNG/JPEG 的文件大小和尺寸，不完整解码图片。
func PNGJPEG(data []byte) (Image, error) {
	_, format, err := checkedConfig(data, ErrImageFormat, "png", "jpeg")
	if err != nil {
		return Image{}, err
	}
	return Image{MIME: "image/" + format, Data: data}, nil
}

type previewBuffer struct{ bytes.Buffer }

func (b *previewBuffer) Write(data []byte) (int, error) {
	if len(data) > MaxImageBytes-b.Len() {
		return 0, ErrImageSize
	}
	return b.Buffer.Write(data)
}

// Raster 保留 PNG/JPEG 原图；其他光栅格式只解码第一帧/页，并限制 PNG 输出大小。
func Raster(ctx context.Context, data []byte) (Image, error) {
	_, format, err := rasterConfig(data)
	if err != nil {
		return Image{}, err
	}
	if err := ctx.Err(); err != nil {
		return Image{}, err
	}
	if format == "png" || format == "jpeg" {
		return Image{MIME: "image/" + format, Data: data}, nil
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Image{}, err
	}
	if err := ctx.Err(); err != nil {
		return Image{}, err
	}
	var output previewBuffer
	if err := png.Encode(&output, source); err != nil {
		return Image{}, err
	}
	if err := ctx.Err(); err != nil {
		return Image{}, err
	}
	return Image{MIME: "image/png", Data: output.Bytes()}, nil
}
