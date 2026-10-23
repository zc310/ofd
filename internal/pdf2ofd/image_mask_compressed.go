package pdf2ofd

import (
	"fmt"
	"image"
	"image/color"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// JPEG/颜色键共用 Matte 的原分量读取：撤销 Go 对 Adobe CMYK/YCCK 的反相，
// 再匹配 Decode 之前的整数值；不能匹配颜色转换后的 PNG 或 CMYK 油墨值。
func (p *pdfInterpreter) applyJPEGColorKey(stream *types.StreamDict, ranges types.Array) ([]byte, error) {
	components, indexed, _, err := pdfImageColorSpace(p.ctx, stream)
	if err != nil {
		return nil, err
	}
	bits, _ := integerValue(stream.Dict["BitsPerComponent"])
	if indexed || pdfImageIsMask(p.ctx, stream) || bits != 8 || (components != 1 && components != 3 && components != 4) {
		return nil, fmt.Errorf("JPEG 颜色键需要 8 位 Gray/RGB/CMYK 分量")
	}
	limits, err := p.imageColorKeyLimits(ranges, components, 8)
	if err != nil {
		return nil, err
	}
	width, _ := integerValue(stream.Dict["Width"])
	height, _ := integerValue(stream.Dict["Height"])
	decoded, err := p.rawJPEGImage(stream, width, height, components)
	if err != nil {
		return nil, err
	}
	return p.encodeCompressedColorKey(stream, width, height, components, 255, limits, func(x, y int) [4]uint32 {
		values := rawJPEGComponents(decoded, x, y)
		var samples [4]uint32
		for c := 0; c < components; c++ {
			samples[c] = uint32(floatToByte(values[c]))
		}
		return samples
	})
}

// JBIG2 的 Gray 0/255 对应 PDF 的二值采样 0/1，键的合法范围也是 0..1。
func (p *pdfInterpreter) applyJBIG2ColorKey(stream *types.StreamDict, ranges types.Array) ([]byte, error) {
	family, ok := dereferenceName(p.ctx, stream.Dict["ColorSpace"])
	bits, _ := integerValue(stream.Dict["BitsPerComponent"])
	if !ok || family != "DeviceGray" || bits != 1 || pdfImageIsMask(p.ctx, stream) {
		return nil, fmt.Errorf("JBIG2 颜色键需要 1 位 DeviceGray 分量")
	}
	limits, err := p.imageColorKeyLimits(ranges, 1, 1)
	if err != nil {
		return nil, err
	}
	width, _ := integerValue(stream.Dict["Width"])
	height, _ := integerValue(stream.Dict["Height"])
	gray, err := p.rawJBIG2Image(stream, width, height)
	if err != nil {
		return nil, err
	}
	bounds := gray.Bounds()
	return p.encodeCompressedColorKey(stream, width, height, 1, 1, limits, func(x, y int) [4]uint32 {
		return [4]uint32{uint32(gray.GrayAt(bounds.Min.X+x, bounds.Min.Y+y).Y / 255)}
	})
}

// 匹配原整数采样后，应用 PDF Decode 并在原颜色空间转换可见像素。输出
// alpha 是二值；原采样解码、PNG 合成分别计入像素预算，逐行检查取消。
func (p *pdfInterpreter) encodeCompressedColorKey(stream *types.StreamDict, width, height, components int, maximum uint32, limits [8]uint32, sample func(x, y int) [4]uint32) ([]byte, error) {
	return p.encodeCompressedImage(stream, width, height, components, maximum, &limits, sample)
}

// 无颜色键时也用于普通 JPEG Decode，保持同一原颜色空间中的转换顺序。
func (p *pdfInterpreter) encodeCompressedImage(stream *types.StreamDict, width, height, components int, maximum uint32, limits *[8]uint32, sample func(x, y int) [4]uint32) ([]byte, error) {
	if err := p.control.check(); err != nil {
		return nil, err
	}
	pixels := int64(width) * int64(height)
	if p.control != nil {
		var err error
		pixels, err = p.control.imagePixels(int64(width), int64(height))
		if err != nil {
			return nil, err
		}
		if err := p.control.addImagePixels(pixels); err != nil {
			return nil, err
		}
	}
	workPerPixel := components + 1
	if limits != nil {
		workPerPixel += components
	}
	if err := p.control.consumeOperations(pixels * int64(workPerPixel)); err != nil {
		return nil, err
	}
	out := image.NewNRGBA(image.Rect(0, 0, width, height))
	decode := pdfImageDecode(p.ctx, stream, components)
	converter := pdfImageCMYKConverter(p.ctx, stream)
	for y := 0; y < height; y++ {
		if err := p.control.check(); err != nil {
			return nil, err
		}
		for x := 0; x < width; x++ {
			values := sample(x, y)
			masked := limits != nil
			var colors [4]byte
			for c := 0; c < components; c++ {
				if limits != nil && (values[c] < limits[c*2] || values[c] > limits[c*2+1]) {
					masked = false
				}
				colors[c] = floatToByte(decode[c*2] + float64(values[c])/float64(maximum)*(decode[c*2+1]-decode[c*2]))
			}
			r, g, b := colors[0], colors[0], colors[0]
			if components == 3 {
				g, b = colors[1], colors[2]
			} else if components == 4 {
				r, g, b = converter.toRGB(colors[0], colors[1], colors[2], colors[3])
			}
			alpha := byte(255)
			if masked {
				alpha = 0
			}
			out.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: alpha})
		}
	}
	data, _, err := encodePNGImage(out)
	if err == nil {
		err = p.control.check()
	}
	return data, err
}
