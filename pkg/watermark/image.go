package watermark

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"

	_ "image/jpeg"
)

var pngSignature = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}

// detectImageFormat 按文件头识别图片格式，识别不了返回空字符串。
func detectImageFormat(data []byte) string {
	if len(data) >= len(pngSignature) && bytes.HasPrefix(data[:len(pngSignature)], pngSignature) {
		return "PNG"
	}
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "JPEG"
	}
	return ""
}

// formatExt 返回图片格式对应的文件名扩展名。
func formatExt(format string) string {
	switch strings.ToUpper(format) {
	case "JPEG", "JPG":
		return "jpg"
	case "PNG":
		return "png"
	default:
		return strings.ToLower(format)
	}
}

// imagePixels 返回图片的像素尺寸，无法解码时返回 0,0。
func imagePixels(data []byte) (int, int) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	return config.Width, config.Height
}

// bakeImageAlpha 按不透明度缩放 PNG 的 alpha 通道后重新编码，供图片水印
// 在不支持对象级透明度的渲染路径上呈现半透明效果。
func bakeImageAlpha(data []byte, opacity uint8) ([]byte, error) {
	if opacity >= 255 {
		return data, nil
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("解码图片失败: %w", err)
	}
	bounds := src.Bounds()
	dst := image.NewNRGBA(bounds)
	factor := float64(opacity) / 255.0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			// 取非预乘分量，避免把已预乘的 RGBA() 结果再当作非预乘颜色写回而变暗。
			pixel := color.NRGBAModel.Convert(src.At(x, y)).(color.NRGBA)
			pixel.A = uint8(float64(pixel.A) * factor)
			dst.SetNRGBA(x, y, pixel)
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, dst); err != nil {
		return nil, fmt.Errorf("编码图片失败: %w", err)
	}
	return buffer.Bytes(), nil
}
