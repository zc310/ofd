package pdf2ofd

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// rawJPEGImage 为 Matte/颜色键读取原 JPEG，不能从 RGB PNG 反推 CMYK。
// 额外解码计入像素与处理量预算，实际尺寸必须与声明尺寸一致。
func (p *pdfInterpreter) rawJPEGImage(stream *types.StreamDict, width, height, components int) (image.Image, error) {
	return p.jpegImagePlanes(stream, width, height, components, true)
}

// 普通 JPEG 的首轮解码已由 preflightImage 计入像素；Matte/颜色键再次读取
// 原分量时需额外预留，两个路径共用结构校验和取消检查。
func (p *pdfInterpreter) jpegImagePlanes(stream *types.StreamDict, width, height, components int, extraDecode bool) (image.Image, error) {
	data := stream.Content
	if !isJPEGData(data) {
		data = stream.Raw
	}
	configuration, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("读取 JPEG 原采样头失败: %w", err)
	}
	if configuration.Width != width || configuration.Height != height {
		return nil, fmt.Errorf("JPEG 实际尺寸与声明尺寸不一致")
	}
	if p.control != nil {
		pixels, err := p.control.imagePixels(int64(width), int64(height))
		if err != nil {
			return nil, err
		}
		if extraDecode {
			if err := p.control.addImagePixels(pixels); err != nil {
				return nil, err
			}
		}
	}
	if err := p.control.consumeOperations(int64(width) * int64(height) * int64(components)); err != nil {
		return nil, err
	}
	decoded, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("解码 JPEG 原采样失败: %w", err)
	}
	if err := p.control.check(); err != nil {
		return nil, err
	}
	count, transform := 0, 0
	switch decoded.(type) {
	case *image.Gray:
		count = 1
	case *image.YCbCr:
		count, transform = 3, 1
	case *image.RGBA:
		count = 3
	case *image.CMYK:
		count = 4
		if marker, _ := jpegAdobeTransform(data); marker == 2 {
			transform = 1
		}
	}
	if count != components {
		return nil, fmt.Errorf("JPEG 分量数与颜色空间不一致")
	}
	for _, filter := range stream.FilterPipeline {
		if filter.Name != "DCTDecode" || filter.DecodeParms == nil || filter.DecodeParms["ColorTransform"] == nil {
			continue
		}
		value, ok := dereferencedPDFNumber(p.ctx, filter.DecodeParms["ColorTransform"])
		if !ok || (value != 0 && value != 1) || (count != 1 && int(value) != transform) {
			return nil, fmt.Errorf("JPEG 的 ColorTransform 覆盖与码流颜色变换不一致，当前无法还原")
		}
	}
	return decoded, nil
}

// rawJPEGComponents 返回应用 PDF Decode 之前的分量。Go 将 Adobe CMYK/YCCK
// 解码为油墨型 image.CMYK，这里撤销其反相，随后由调用方统一应用 PDF Decode。
func rawJPEGComponents(img image.Image, x, y int) [4]float64 {
	var values [4]float64
	switch source := img.(type) {
	case *image.Gray:
		values[0] = float64(source.GrayAt(x, y).Y) / 255
	case *image.YCbCr:
		v := source.YCbCrAt(x, y)
		r, g, b := color.YCbCrToRGB(v.Y, v.Cb, v.Cr)
		values[0], values[1], values[2] = float64(r)/255, float64(g)/255, float64(b)/255
	case *image.RGBA:
		v := source.RGBAAt(x, y)
		values[0], values[1], values[2] = float64(v.R)/255, float64(v.G)/255, float64(v.B)/255
	case *image.CMYK:
		v := source.CMYKAt(x, y)
		values[0], values[1], values[2], values[3] = float64(255-v.C)/255, float64(255-v.M)/255, float64(255-v.Y)/255, float64(255-v.K)/255
	}
	return values
}

func (p *pdfInterpreter) matteJPEGAlpha(mask *types.StreamDict, width, height int) ([]byte, error) {
	decoded, err := p.rawJPEGImage(mask, width, height, 1)
	if err != nil {
		return nil, err
	}
	gray := decoded.(*image.Gray)
	decode := pdfImageDecode(p.ctx, mask, 1)
	alpha := make([]byte, width*height)
	for y := 0; y < height; y++ {
		if err := p.control.check(); err != nil {
			return nil, err
		}
		for x := 0; x < width; x++ {
			value := float64(gray.GrayAt(x, y).Y) / 255
			alpha[y*width+x] = floatToByte(decode[0] + value*(decode[1]-decode[0]))
		}
	}
	return alpha, nil
}
