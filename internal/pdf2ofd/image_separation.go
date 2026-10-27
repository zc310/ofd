package pdf2ofd

import (
	"bytes"
	"fmt"
	"image"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/media"
)

func (p *pdfInterpreter) separationImageSpace(stream *types.StreamDict) (*pdfColorSpace, error) {
	if pdfImageIsMask(p.ctx, stream) || stream.Dict["ColorSpace"] == nil {
		return nil, nil
	}
	object, err := dereferencePDFObject(p.ctx, stream.Dict["ColorSpace"])
	if err != nil {
		return nil, err
	}
	array, ok := object.(types.Array)
	if !ok || len(array) == 0 {
		return nil, nil
	}
	family, _ := dereferenceName(p.ctx, array[0])
	if family != "Separation" {
		return nil, nil
	}
	if hasDCTFilter(stream) || hasJPXFilter(stream) || hasJBIG2Filter(stream) {
		return nil, fmt.Errorf("Separation 图片暂不支持 JPEG/JPX/JBIG2 编码")
	}
	space := p.parseColorSpace(object, 0)
	if space == nil {
		return nil, fmt.Errorf("Separation 图片的替代颜色空间或 tint 函数无效")
	}
	return space, nil
}

// encodeSeparationImage 按 ISO 32000-1 8.6.6.4、8.9.5 先应用 Decode，
// 再经 tint 函数和替代颜色空间转为 RGB。单通道专色不是灰度；亮度软掩码
// 内的专色图片也必须先恢复实际颜色，否则丢图会使被遮蔽的斑点完全显露。
func (p *pdfInterpreter) encodeSeparationImage(stream *types.StreamDict, space *pdfColorSpace) ([]byte, string, error) {
	width, wok := integerValue(stream.Dict["Width"])
	height, hok := integerValue(stream.Dict["Height"])
	bpc, _ := integerValue(stream.Dict["BitsPerComponent"])
	if !wok || !hok || width <= 0 || height <= 0 ||
		(bpc != 1 && bpc != 2 && bpc != 4 && bpc != 8 && bpc != 16) ||
		width > (int(^uint(0)>>1)-7)/bpc {
		return nil, "", fmt.Errorf("Separation 图片尺寸或位深无效")
	}
	if err := p.decodeStream(stream, 0); err != nil {
		return nil, "", err
	}
	rowBytes := (width*bpc + 7) / 8
	if height > len(stream.Content)/rowBytes {
		return nil, "", fmt.Errorf("Separation 图片采样数据不完整")
	}
	if err := p.control.consumeOperations(int64(width) * int64(height)); err != nil {
		return nil, "", err
	}
	decode := pdfImageDecode(p.ctx, stream, 1)
	maximum := uint32(1)<<uint(bpc) - 1
	// 相同 tint 复用求值结果；16 位输入最多 65536 项，避免逐像素解释函数。
	colors := make([]pdfColor, int(maximum)+1)
	known := make([]bool, len(colors))
	rgb := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		if err := p.control.check(); err != nil {
			return nil, "", err
		}
		row := stream.Content[y*rowBytes : (y+1)*rowBytes]
		for x := 0; x < width; x++ {
			sample := readPDFImageSample(row, x, bpc)
			if !known[sample] {
				tint := clamp01(decode[0] + float64(sample)/float64(maximum)*(decode[1]-decode[0]))
				value, ok := space.colorFromComponents([]float64{tint})
				if !ok {
					return nil, "", fmt.Errorf("Separation 图片 tint 函数求值失败")
				}
				colors[sample], known[sample] = value, true
			}
			value := colors[sample]
			offset := rgb.PixOffset(x, y)
			rgb.Pix[offset], rgb.Pix[offset+1], rgb.Pix[offset+2], rgb.Pix[offset+3] = value.r, value.g, value.b, 255
		}
	}
	var encoded bytes.Buffer
	if err := media.EncodePNG(&encoded, rgb); err != nil {
		return nil, "", fmt.Errorf("编码 Separation 图片失败: %w", err)
	}
	return encoded.Bytes(), "PNG", nil
}
