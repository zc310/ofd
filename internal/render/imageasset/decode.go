package imageasset

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"

	_ "github.com/dkrisman/gobig2"
	"github.com/zc310/ofd/internal/media"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/renderstate"
	"github.com/zc310/ofd/pkg/spec"
)

// Reader 按文档内的规范化路径读取资源字节。
type Reader interface {
	Read(name string) ([]byte, error)
}

// IsSVG 判断声明格式或资源扩展名是否表示 SVG。
func IsSVG(format string, file spec.StLoc) bool {
	return strings.EqualFold(format, "SVG") || strings.EqualFold(file.Ext(), ".svg")
}

// Load 返回缓存图片。无目标尺寸时 SVG 使用 96 DPI；所有格式均限制像素尺寸。
func (s *Store) Load(file spec.StLoc, format string, source Reader, parse SVGParser) (image.Image, error) {
	return s.LoadWithOptions(file, format, source, parse, LoadOptions{})
}

// ErrPixelBudget 表示当前绘制额度不足，不能作为资源自身的永久失败缓存。
var ErrPixelBudget = errors.New("图像分配超出当前绘制预算")

var (
	errImageSize  = errors.New("图像尺寸超限或无效")
	errSVGSize    = errors.New("SVG 画布或栅格尺寸无效或超限")
	errEmptyImage = errors.New("图像像素为空")
	errLazyImage  = errors.New("图像延迟解码未返回像素或嵌套过深")
)

// LoadOptions 在实际缓存未命中时预留解码像素；SVG 按最终设备像素需求栅格化。
// AllowPixels 不会保存在共享图片中，延迟解码使用已预留的额度和独立硬上限。
type LoadOptions struct {
	AllowPixels               func(width, height int) bool
	TargetWidth, TargetHeight float64
}

// CheckSize 在任何像素分配之前验证尺寸，除像素数外也防止行步长及权重溢出。
func CheckSize(width, height int) error {
	if width <= 0 || height <= 0 || width > renderstate.MaxOffscreenPixels/height ||
		int64(width) > int64(^uint(0)>>1)/8/int64(height) {
		return fmt.Errorf("%w：%dx%d", errImageSize, width, height)
	}
	return nil
}

func (o LoadOptions) allow(width, height int) error {
	if err := CheckSize(width, height); err != nil {
		return err
	}
	if o.AllowPixels != nil && !o.AllowPixels(width, height) {
		return ErrPixelBudget
	}
	return nil
}

// LoadWithOptions 将图片解码及 SVG 栅格化纳入调用方预算，命中缓存时不重复计量。
func (s *Store) LoadWithOptions(file spec.StLoc, format string, source Reader, parse SVGParser, options LoadOptions) (image.Image, error) {
	key := file.Clean().String()
	if IsSVG(format, file) {
		scene, err := s.LoadSVG(file, format, source, parse)
		if err != nil {
			return nil, err
		}
		width, height := scene.Width(), scene.Height()
		if width <= 0 || height <= 0 || math.IsNaN(width) || math.IsNaN(height) || math.IsInf(width, 0) || math.IsInf(height, 0) {
			return nil, errSVGSize
		}
		scale := geom.DPI(96).DPMM()
		if options.TargetWidth > 0 && options.TargetHeight > 0 {
			scale = math.Max(options.TargetWidth/width, options.TargetHeight/height)
		}
		w, h := math.Ceil(width*scale), math.Ceil(height*scale)
		if math.IsNaN(w) || math.IsNaN(h) || math.IsInf(w, 0) || math.IsInf(h, 0) ||
			w <= 0 || h <= 0 || w > renderstate.MaxOffscreenPixels || h > renderstate.MaxOffscreenPixels {
			return nil, errSVGSize
		}
		key = fmt.Sprintf("\x00svg-raster:%s:%gx%g", key, w, h)
		return s.Image(key, func() (image.Image, error) {
			if err := options.allow(int(w), int(h)); err != nil {
				return nil, err
			}
			img := scene.Rasterize(geom.DPMM(scale))
			if img == nil || img.Bounds().Empty() {
				return nil, permanentError{errEmptyImage}
			}
			return img, nil
		})
	}
	return s.Image(key, func() (image.Image, error) {
		if source == nil {
			return nil, errors.New("图片资源读取器为空")
		}
		data, err := source.Read(key)
		if err != nil {
			return nil, err
		}
		img, err := decode(data, options)
		if err != nil && !errors.Is(err, ErrPixelBudget) {
			return nil, permanentError{err}
		}
		return img, err
	})
}

// LoadSVG 返回规范化资源路径对应的缓存场景。
func (s *Store) LoadSVG(file spec.StLoc, format string, source Reader, parse SVGParser) (SVGScene, error) {
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

// Decoded 展开延迟解码包装并验证像素，避免将隐藏的解码错误当作可绘制图片。
func Decoded(img image.Image) (image.Image, error) {
	for range 4 {
		if img == nil || img.Bounds().Empty() {
			return nil, errEmptyImage
		}
		lazy, ok := img.(interface{ Image() (image.Image, error) })
		if !ok {
			return img, nil
		}
		decoded, err := lazy.Image()
		if err != nil {
			return nil, err
		}
		if decoded == img {
			return nil, errLazyImage
		}
		img = decoded
	}
	return nil, errLazyImage
}

// Decode 对 JPEG/PNG 使用懒解码并保留原始编码字节，供 PDF 写入器直接内嵌。
// 其他格式经 media 解码，调色板图像转换为可快速采样的 RGBA。
func Decode(data []byte) (image.Image, error) {
	return decode(data, LoadOptions{})
}

func decode(data []byte, options LoadOptions) (image.Image, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if err := options.allow(config.Width, config.Height); err != nil {
		return nil, err
	}
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
	if img == nil {
		return nil, errEmptyImage
	}
	if err := CheckSize(img.Bounds().Dx(), img.Bounds().Dy()); err != nil {
		return nil, err
	}
	if _, paletted := img.(*image.Paletted); paletted {
		if err := options.allow(img.Bounds().Dx(), img.Bounds().Dy()); err != nil {
			return nil, err
		}
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
