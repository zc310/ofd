package pdf2ofd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"

	fastjpx "github.com/ajroetker/go-jpeg2000"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// encodeJPXImage 保留原整数分量，普通 JPX 忽略 PDF Decode/BitsPerComponent。
// 只有 ImageMask 的 Decode 控制二值反相；CMYK 在原分量上转换，不误读为 RGBA。
func (p *pdfInterpreter) encodeJPXImage(stream *types.StreamDict, maskColor pdfColor) ([]byte, string, error) {
	stencil := pdfImageIsMask(p.ctx, stream)
	source := stream
	if stencil && stream.Dict["ColorSpace"] == nil {
		copyStream := *stream
		copyStream.Dict = stream.Dict.Clone().(types.Dict)
		copyStream.Dict["ColorSpace"] = types.Name("DeviceGray")
		source = &copyStream
	}
	info, err := p.jpxHeader(source, !stencil)
	if err != nil {
		return nil, "", err
	}
	m := info.metadata
	if stencil && (info.family != "DeviceGray" || m.BitsPerComponent[0] != 1) {
		return nil, "", fmt.Errorf("JPX ImageMask 需要 1 位灰度码流")
	}
	if !stencil && info.icc != nil && info.family == "DeviceRGB" && info.colours == 3 &&
		info.metadata.NumComponents == 3 && info.metadata.BitsPerComponent[0] == 8 && info.alpha == 0 && !info.converted {
		data, format, decodeErr := p.encodeICCBasedJPXRGB(source, info)
		if decodeErr == nil {
			return data, format, nil
		}
		if errors.Is(decodeErr, ErrLimitExceeded) || errors.Is(decodeErr, context.Canceled) || errors.Is(decodeErr, context.DeadlineExceeded) {
			return nil, "", decodeErr
		}
	}
	decoded, err := p.jpxImagePlanes(source, info, m.Width, m.Height, false)
	if err != nil {
		return nil, "", err
	}
	workPerPixel := int64(1)
	if info.converted {
		// 除输出写入外，三个颜色分量的矩阵转换也计入处理量。
		workPerPixel += 3
	}
	if err := p.control.consumeOperations(int64(m.Width) * int64(m.Height) * workPerPixel); err != nil {
		return nil, "", err
	}
	maximum := float64(uint32(1)<<uint(m.BitsPerComponent[0]) - 1)
	highPrecision := m.BitsPerComponent[0] > 8
	var out draw.Image
	if highPrecision {
		out = image.NewNRGBA64(image.Rect(0, 0, m.Width, m.Height))
	} else {
		out = image.NewNRGBA(image.Rect(0, 0, m.Width, m.Height))
	}
	invert := false
	if stencil {
		decode := pdfImageDecode(p.ctx, stream, 1)
		invert = decode[0] > decode[1]
	}
	converter := pdfImageCMYKConverter(p.ctx, stream)
	colourComponents := info.colours
	for y := 0; y < m.Height; y++ {
		if err := p.control.check(); err != nil {
			return nil, "", err
		}
		for x := 0; x < m.Width; x++ {
			index := y*m.Width + x
			var values [4]float64
			alpha := 1.0
			if info.alpha != 0 {
				alpha = clamp01(float64(decoded.Components[colourComponents][index]) / maximum)
			}
			for c := 0; c < colourComponents; c++ {
				values[c] = clamp01(float64(decoded.Components[c][index]) / maximum)
				if info.alpha == 2 {
					if alpha == 0 {
						values[c] = 0
					} else {
						values[c] = clamp01(values[c] / alpha)
					}
				}
			}
			if info.converted {
				values = jpxColourToRGB(values, m.ColorSpace, m.BitsPerComponent[0])
			}
			if info.icc != nil {
				r, g, b := info.icc.ToRGB([]uint8{floatToByte(values[0]), floatToByte(values[1]), floatToByte(values[2]), floatToByte(values[3])})
				values[0], values[1], values[2] = float64(r)/255, float64(g)/255, float64(b)/255
			}
			if stencil {
				ink := values[0] < .5
				if invert {
					ink = !ink
				}
				if ink {
					out.(*image.NRGBA).SetNRGBA(x, y, color.NRGBA{R: maskColor.r, G: maskColor.g, B: maskColor.b, A: 255})
				}
				continue
			}
			if highPrecision {
				r, g, b := floatToUint16(values[0]), floatToUint16(values[0]), floatToUint16(values[0])
				if info.family == "DeviceRGB" || info.icc != nil {
					g, b = floatToUint16(values[1]), floatToUint16(values[2])
				} else if info.family == "DeviceCMYK" {
					r, g, b = converter.toRGB16(floatToUint16(values[0]), floatToUint16(values[1]), floatToUint16(values[2]), floatToUint16(values[3]))
				}
				out.(*image.NRGBA64).SetNRGBA64(x, y, color.NRGBA64{R: r, G: g, B: b, A: floatToUint16(alpha)})
			} else {
				r, g, b := floatToByte(values[0]), floatToByte(values[0]), floatToByte(values[0])
				if info.family == "DeviceRGB" || info.icc != nil {
					g, b = floatToByte(values[1]), floatToByte(values[2])
				} else if info.family == "DeviceCMYK" {
					r, g, b = converter.toRGB(floatToByte(values[0]), floatToByte(values[1]), floatToByte(values[2]), floatToByte(values[3]))
				}
				out.(*image.NRGBA).SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: floatToByte(alpha)})
			}
		}
	}
	data, format, err := encodePNGImage(out)
	if err == nil {
		err = p.control.check()
	}
	return data, format, err
}

// encodeICCBasedJPXRGB uses the full-quality RGB decoder for ordinary ICCBased
// JP2 images. The common decoder remains responsible for masks, Matte, and
// component-level operations that need the original integer planes.
func (p *pdfInterpreter) encodeICCBasedJPXRGB(stream *types.StreamDict, info *matteJPXInfo) ([]byte, string, error) {
	decoded, err := fastjpx.Decode(bytes.NewReader(jpxStreamBytes(stream)))
	if err != nil {
		return nil, "", err
	}
	width, height := info.metadata.Width, info.metadata.Height
	if decoded.Bounds().Dx() != width || decoded.Bounds().Dy() != height {
		return nil, "", fmt.Errorf("JPX 解码尺寸 %v 与头部声明 %dx%d 不一致", decoded.Bounds().Size(), width, height)
	}
	pixels := int64(width) * int64(height)
	if err := p.control.consumeOperations(pixels * int64(info.metadata.NumComponents+1)); err != nil {
		return nil, "", err
	}
	output := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		if err := p.control.check(); err != nil {
			return nil, "", err
		}
		for x := 0; x < width; x++ {
			pixel := color.NRGBAModel.Convert(decoded.At(decoded.Bounds().Min.X+x, decoded.Bounds().Min.Y+y)).(color.NRGBA)
			red, green, blue := info.icc.ToRGB([]uint8{pixel.R, pixel.G, pixel.B})
			output.SetNRGBA(x, y, color.NRGBA{R: red, G: green, B: blue, A: pixel.A})
		}
	}
	data, format, err := encodePNGImage(output)
	if err == nil {
		err = p.control.check()
	}
	return data, format, err
}

// applyJPXSoftMask 单独读取灰度原分量并按实际掩码尺寸对齐；PDF Decode 和
// 字典位深不改变 JPX alpha。全程保留高位深，避免先缩到 8 位再扩成 16 位。
func (p *pdfInterpreter) applyJPXSoftMask(base image.Image, mask *types.StreamDict) ([]byte, bool) {
	info, err := p.jpxHeader(mask, false)
	if err != nil || info.family != "DeviceGray" {
		return nil, false
	}
	m := info.metadata
	decoded, err := p.jpxImagePlanes(mask, info, m.Width, m.Height, false)
	if err != nil {
		return nil, false
	}
	bounds := base.Bounds()
	width, height, err := pdfSoftMaskCompositionSize(bounds, m.Width, m.Height, p.control)
	if err != nil {
		return nil, false
	}
	if p.control.consumeOperations(int64(width)*int64(height)) != nil {
		return nil, false
	}
	highPrecision := m.BitsPerComponent[0] > 8
	switch base.(type) {
	case *image.Gray16, *image.NRGBA64, *image.RGBA64:
		highPrecision = true
	}
	var out draw.Image
	if highPrecision {
		out = image.NewNRGBA64(image.Rect(0, 0, width, height))
	} else {
		out = image.NewNRGBA(image.Rect(0, 0, width, height))
	}
	maximum := float64(uint32(1)<<uint(m.BitsPerComponent[0]) - 1)
	for y := 0; y < height; y++ {
		if p.control.check() != nil {
			return nil, false
		}
		row := pdfSoftMaskRow(y, m.Height, height) * m.Width
		sourceY := bounds.Min.Y + pdfSoftMaskRow(y, bounds.Dy(), height)
		for x := 0; x < width; x++ {
			sourceX := bounds.Min.X + pdfSoftMaskColumn(x, bounds.Dx(), width)
			v := pdfImageNRGBA64At(base, sourceX, sourceY)
			alpha := float64(decoded.Components[0][row+pdfSoftMaskColumn(x, m.Width, width)]) / maximum
			v.A = floatToUint16(alpha)
			if highPrecision {
				out.(*image.NRGBA64).SetNRGBA64(x, y, v)
			} else {
				out.(*image.NRGBA).SetNRGBA(x, y, color.NRGBA{R: byte(v.R / 257), G: byte(v.G / 257), B: byte(v.B / 257), A: floatToByte(alpha)})
			}
		}
	}
	data, _, err := encodePNGImage(out)
	return data, err == nil && p.control.check() == nil
}
