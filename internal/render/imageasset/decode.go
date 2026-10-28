package imageasset

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"strings"

	_ "github.com/dkrisman/gobig2"
	"github.com/zc310/ofd/internal/media"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

// Reader 按文档内的规范化路径读取资源字节。
type Reader interface {
	Read(name string) ([]byte, error)
}

// IsSVG 判断声明格式或资源扩展名是否表示 SVG。
func IsSVG(format string, file models.StLoc) bool {
	return strings.EqualFold(format, "SVG") || strings.EqualFold(file.Ext(), ".svg")
}

// Load 返回缓存图片。JPEG/PNG 保留原编码并懒解码；SVG 的位图回退使用 96 DPI。
func (s *Store) Load(file models.StLoc, format string, source Reader, parse SVGParser) (image.Image, error) {
	key := file.Clean().String()
	return s.Image(key, func() (image.Image, error) {
		if IsSVG(format, file) {
			scene, err := s.LoadSVG(file, format, source, parse)
			if err != nil {
				return nil, err
			}
			return scene.Rasterize(geom.DPI(96)), nil
		}
		if source == nil {
			return nil, errors.New("图片资源读取器为空")
		}
		data, err := source.Read(key)
		if err != nil {
			return nil, err
		}
		return Decode(data)
	})
}

// LoadSVG 返回规范化资源路径对应的缓存场景。
func (s *Store) LoadSVG(file models.StLoc, format string, source Reader, parse SVGParser) (SVGScene, error) {
	key := file.Clean().String()
	return s.SVG(key, func() ([]byte, error) {
		if !IsSVG(format, file) {
			return nil, errors.New("not SVG")
		}
		if source == nil {
			return nil, errors.New("图片资源读取器为空")
		}
		return source.Read(key)
	}, parse)
}

var pngSignature = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

// Decode 对 JPEG/PNG 使用懒解码并保留原始编码字节，供 PDF 写入器直接内嵌。
// 其他格式经 media 解码，调色板图像转换为可快速采样的 RGBA。
func Decode(data []byte) (image.Image, error) {
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return NewEncodedImage("jpeg", data), nil
	}
	if len(data) >= len(pngSignature) && bytes.Equal(data[:len(pngSignature)], pngSignature) {
		return NewEncodedImage("png", data), nil
	}
	img, err := media.DecodeBytes(data)
	if err != nil {
		return nil, err
	}
	return palettedToRGBA(img), nil
}

// 调色板图像一次转为 RGBA，让后续缩放使用类型化快速路径，避免逐像素接口分发。
func palettedToRGBA(img image.Image) image.Image {
	pal, ok := img.(*image.Paletted)
	if !ok {
		return img
	}
	bounds := pal.Bounds()
	if bounds.Empty() {
		return img
	}
	out := image.NewRGBA(bounds)
	draw.Draw(out, bounds, pal, bounds.Min, draw.Src)
	return out
}

// 懒加载图片同时保留编码字节和按需解码的像素，两部分都计入缓存预算。
func imageCacheWeight(img image.Image) int64 {
	if img == nil {
		return 1
	}
	if lazy, ok := img.(*EncodedImage); ok {
		return lazy.Weight()
	}
	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return 1
	}
	return int64(bounds.Dx()) * int64(bounds.Dy()) * imageBytesPerPixel(img.ColorModel())
}

func imageBytesPerPixel(model color.Model) int64 {
	switch model {
	case color.GrayModel, color.AlphaModel:
		return 1
	case color.Gray16Model, color.Alpha16Model, color.YCbCrModel:
		return 2
	case color.RGBA64Model, color.NRGBA64Model:
		return 8
	default:
		return 4
	}
}

// SVG 场景内部结构无法直接度量，使用源字节数作为与复杂度相关的权重。
func svgCanvasWeight(data []byte) int64 { return max(1, int64(len(data))) }
