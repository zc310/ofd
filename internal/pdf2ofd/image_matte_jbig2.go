package pdf2ofd

import (
	"fmt"
	"image"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// rawJBIG2Image 为 Matte/颜色键读取未应用 PDF Decode 的二值灰度，不按声明尺寸
// 重采样；预检页面与区域尺寸后，为这次额外解码预留像素和处理量预算。
func (p *pdfInterpreter) rawJBIG2Image(stream *types.StreamDict, width, height int) (*image.Gray, error) {
	if err := p.control.check(); err != nil {
		return nil, err
	}
	pixels := int64(width) * int64(height)
	if p.control != nil {
		if err := p.preflightJBIG2(jbig2StreamBytes(stream), &pixels); err != nil {
			return nil, err
		}
		if err := p.control.addImagePixels(pixels); err != nil {
			return nil, err
		}
	}
	if err := p.control.consumeOperations(pixels); err != nil {
		return nil, err
	}
	gray, err := pdfJBIG2Image(p.ctx, stream, p.control)
	if err != nil {
		return nil, err
	}
	if gray.Bounds().Dx() != width || gray.Bounds().Dy() != height {
		return nil, fmt.Errorf("JBIG2 实际尺寸与声明尺寸不一致")
	}
	return gray, nil
}

// 二值采样的 Decode 端点可以是部分透明度；统一保留为 16 位直到去底色完成，
// 避免提前量化到 8 位影响颜色。直接读取原灰度，避免把反相应用两次。
func (p *pdfInterpreter) matteJBIG2Alpha(mask *types.StreamDict, gray *image.Gray) ([]uint16, error) {
	bounds := gray.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if err := p.control.consumeOperations(int64(width) * int64(height)); err != nil {
		return nil, err
	}
	alpha := make([]uint16, width*height)
	decode := pdfImageDecode(p.ctx, mask, 1)
	for y := 0; y < height; y++ {
		if err := p.control.check(); err != nil {
			return nil, err
		}
		for x := 0; x < width; x++ {
			sample := float64(gray.GrayAt(bounds.Min.X+x, bounds.Min.Y+y).Y) / 255
			value := decode[0] + sample*(decode[1]-decode[0])
			alpha[y*width+x] = floatToUint16(value)
		}
	}
	return alpha, nil
}
