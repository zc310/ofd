package pdf2ofd

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	xdraw "golang.org/x/image/draw"
)

// imageMaskData 将图像自身的遮罩烘进 PNG。内嵌 JPX opacity 与外置 SMask
// 冲突时，普通模式优先保留常见阅读器的外置 SMask 外观并报告诊断。
// SMask 优先于 Mask；忽略的遮罩不解码，也不扣除图片预算。
// 损坏的有效遮罩在普通模式保留基础图，严格模式报告失败。
func (p *pdfInterpreter) imageMaskData(stream *types.StreamDict, data []byte, format string) ([]byte, string) {
	embedded := p.imageUsesEmbeddedSoftMask(stream)
	if embedded && stream.Dict["SMask"] == nil {
		return data, format // 没有外置 SMask 时，使用已经编码的内嵌 opacity。
	}
	if object := stream.Dict["SMask"]; object != nil {
		resolved, err := dereferencePDFObject(p.ctx, object)
		if name, ok := resolved.(types.Name); err == nil && ok && name.Value() == "None" {
			if embedded {
				return data, format
			}
			// 没有有效软掩码时继续使用 Mask。
		} else {
			mask := pdfMaskStream(resolved)
			if err != nil || mask == nil {
				p.report("image.mask", "软掩码不是可用的图像流，保留原图")
				return data, format
			}
			if embedded {
				// 非零 SMaskInData 本不应与 SMask 同时出现。此处复现 MuPDF/
				// Poppler 的外观选择；外置 alpha 替换内嵌 alpha，不相乘。
				p.report("image.mask_priority", "JPX 同时含内嵌 opacity 与外置 SMask，采用外置 SMask 以保留常见阅读器外观")
				if p.control.check() != nil {
					return data, format
				}
			}
			if mask.Dict["Matte"] != nil {
				masked, err := p.imageMatteData(stream, mask)
				if err != nil {
					p.report("image.mask_matte", err.Error()+"，保留基础图")
					return data, format
				}
				return masked, "PNG"
			}
			if masked, ok := p.applyImageSoftMask(data, mask); ok {
				return masked, "PNG"
			}
			return data, format
		}
	}
	object := stream.Dict["Mask"]
	if object == nil {
		return data, format
	}
	resolved, err := dereferencePDFObject(p.ctx, object)
	var masked []byte
	if err == nil {
		if ranges, ok := resolved.(types.Array); ok {
			masked, err = p.applyImageColorKey(data, stream, ranges)
		} else if mask := pdfMaskStream(resolved); mask != nil {
			masked, err = p.applyImageExplicitMask(data, mask)
		} else {
			err = fmt.Errorf("Mask 不是颜色键数组或图像流")
		}
	}
	if err != nil {
		p.report("image.mask_explicit", err.Error()+"，保留原图")
		return data, format
	}
	return masked, "PNG"
}

func (p *pdfInterpreter) imageUsesEmbeddedSoftMask(stream *types.StreamDict) bool {
	if !hasJPXFilter(stream) || pdfImageIsMask(p.ctx, stream) {
		return false
	}
	value, ok := dereferencedPDFNumber(p.ctx, stream.Dict["SMaskInData"])
	return ok && (value == 1 || value == 2)
}

// 图像关联的软掩码覆盖当前图形状态软掩码；显式二值 Mask 不属于软掩码。
// /SMask /None 不覆盖图形状态。非法 SMask 已由 imageMaskData 报告，不能因此
// 擅自改用另一来源的 alpha。此选择只用于当前绘制，不改变 q/Q 保存的状态。
func (p *pdfInterpreter) imageHasOwnSoftMask(stream *types.StreamDict) bool {
	if p.imageUsesEmbeddedSoftMask(stream) {
		return true
	}
	object := stream.Dict["SMask"]
	if object == nil {
		return false
	}
	resolved, err := dereferencePDFObject(p.ctx, object)
	if name, ok := resolved.(types.Name); err == nil && ok && name.Value() == "None" {
		return false
	}
	return true
}

func pdfMaskStream(object types.Object) *types.StreamDict {
	switch value := object.(type) {
	case types.StreamDict:
		return &value
	case *types.StreamDict:
		return value
	default:
		return nil
	}
}

// reserveImageDecode 在重新解码已编码的基础图之前计入像素预算。
func (p *pdfInterpreter) reserveImageDecode(data []byte) error {
	if p.control == nil {
		return nil
	}
	if err := p.control.check(); err != nil {
		return err
	}
	configuration, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return err
	}
	pixels, err := p.control.imagePixels(int64(configuration.Width), int64(configuration.Height))
	if err != nil {
		return err
	}
	return p.control.addImagePixels(pixels)
}

// applyImageColorKey 按 Decode 和颜色转换之前的整数采样值逐分量匹配闭区间。
// Indexed 匹配调色板索引，不能匹配转换后的 RGB（不同索引可能有相同颜色）。
func (p *pdfInterpreter) applyImageColorKey(data []byte, stream *types.StreamDict, ranges types.Array) ([]byte, error) {
	if hasJPXFilter(stream) {
		return p.applyJPXColorKey(data, stream, ranges)
	}
	if hasDCTFilter(stream) {
		return p.applyJPEGColorKey(stream, ranges)
	}
	if hasJBIG2Filter(stream) {
		return p.applyJBIG2ColorKey(stream, ranges)
	}
	components, indexed, _, err := pdfImageColorSpace(p.ctx, stream)
	if err != nil {
		return nil, err
	}
	if indexed {
		components = 1
	}
	bpc, _ := integerValue(stream.Dict["BitsPerComponent"])
	if bpc != 1 && bpc != 2 && bpc != 4 && bpc != 8 && bpc != 16 {
		return nil, fmt.Errorf("颜色键的分量数或图像位深无效")
	}
	limits, err := p.imageColorKeyLimits(ranges, components, bpc)
	if err != nil {
		return nil, err
	}
	width, _ := integerValue(stream.Dict["Width"])
	height, _ := integerValue(stream.Dict["Height"])
	if width > (int(^uint(0)>>1)-7)/components/bpc {
		return nil, fmt.Errorf("颜色键图像行长度溢出")
	}
	rowBytes := (width*components*bpc + 7) / 8
	if width <= 0 || height <= 0 || rowBytes <= 0 || height > len(stream.Content)/rowBytes {
		return nil, fmt.Errorf("颜色键图像采样数据不完整")
	}
	return p.encodeImageWithMask(data, width, height, components, func(x, y int) bool {
		row := stream.Content[y*rowBytes : (y+1)*rowBytes]
		for component := 0; component < components; component++ {
			sample := readPDFImageSample(row, x*components+component, bpc)
			if sample < limits[component*2] || sample > limits[component*2+1] {
				return true // 任一分量在区间外，像素仍可见。
			}
		}
		return false
	})
}

// JPX 按码流位深校验区间，普通栅格按字典位深；区间数只包含颜色分量。
func (p *pdfInterpreter) imageColorKeyLimits(ranges types.Array, components, bits int) ([8]uint32, error) {
	var limits [8]uint32
	if components < 1 || components > 4 || len(ranges) != components*2 || bits < 1 || bits > 16 {
		return limits, fmt.Errorf("颜色键的分量数或图像位深无效")
	}
	maximum := float64(uint32(1)<<uint(bits) - 1)
	for i, object := range ranges {
		value, ok := dereferencedPDFNumber(p.ctx, object)
		if !ok || math.IsNaN(value) || value < 0 || value > maximum || value != math.Trunc(value) {
			return limits, fmt.Errorf("颜色键区间不是有效的整数采样值")
		}
		limits[i] = uint32(value)
		if i%2 == 1 && limits[i] < limits[i-1] {
			return limits, fmt.Errorf("颜色键区间上下限颠倒")
		}
	}
	return limits, nil
}

// applyImageExplicitMask 的语义与 ImageMask 相同：默认采样 0 绘制、1 透明，
// Decode [1 0] 反相。遮罩与基础图分别映射到单位正方形，按最近邻对齐不同尺寸。
func (p *pdfInterpreter) applyImageExplicitMask(data []byte, mask *types.StreamDict) ([]byte, error) {
	if p.control != nil {
		if err := p.preflightImage(mask, 1); err != nil {
			return nil, err
		}
	} else if !hasJBIG2Filter(mask) && !hasJPXFilter(mask) {
		if err := p.decodeStream(mask, 0); err != nil {
			return nil, err
		}
	}
	bpc, _ := integerValue(mask.Dict["BitsPerComponent"])
	if !pdfImageIsMask(p.ctx, mask) || (bpc != 0 && bpc != 1) || mask.Dict["SMask"] != nil || mask.Dict["Mask"] != nil {
		return nil, fmt.Errorf("显式遮罩需要不带嵌套遮罩的 1 位 ImageMask")
	}
	decode := []float64{0, 1}
	if object := mask.Dict["Decode"]; object != nil {
		resolved, err := dereferencePDFObject(p.ctx, object)
		array, ok := resolved.(types.Array)
		if err != nil || !ok || len(array) != 2 {
			return nil, fmt.Errorf("1 位遮罩的 Decode 数组无效")
		}
		for i, object := range array {
			value, ok := dereferencedPDFNumber(p.ctx, object)
			if !ok {
				return nil, fmt.Errorf("1 位遮罩的 Decode 数组无效")
			}
			decode[i] = value
		}
	}
	if !((decode[0] == 0 && decode[1] == 1) || (decode[0] == 1 && decode[1] == 0)) {
		return nil, fmt.Errorf("1 位遮罩的 Decode 必须是 [0 1] 或 [1 0]")
	}
	width, _ := integerValue(mask.Dict["Width"])
	height, _ := integerValue(mask.Dict["Height"])
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("显式遮罩尺寸无效")
	}
	if err := p.control.consumeOperations(int64(width) * int64(height)); err != nil {
		return nil, err
	}
	// 样本解码器读取已解引用的 Decode 数值，避免间接数值被当作缺省值。
	sampleMask := *mask
	sampleMask.Dict = types.Dict{}
	for key, value := range mask.Dict {
		sampleMask.Dict[key] = value
	}
	sampleMask.Dict["Decode"] = types.Array{types.Float(decode[0]), types.Float(decode[1])}
	samples, err := pdfImageMaskSamples(p.ctx, &sampleMask, width, height, 1, p.control)
	if err != nil {
		return nil, err
	}
	// JPX 样本解码器尚未应用 Decode；DCT/JBIG2 与裸采样路径已经应用。
	invert := hasJPXFilter(mask) && decode[0] > decode[1]
	return p.encodeImageWithMask(data, width, height, 1, func(x, y int) bool {
		ink := samples[y*width+x] < 128
		return ink != invert
	})
}

// encodeImageWithMask 使用非预乘通道保留像素颜色；16 位基础 PNG 仍输出 16 位，
// 避免仅为添加二值透明度就损失颜色精度。逐行预留处理量并检查取消。
func (p *pdfInterpreter) encodeImageWithMask(data []byte, maskWidth, maskHeight, workPerPixel int, visible func(x, y int) bool) ([]byte, error) {
	if err := p.reserveImageDecode(data); err != nil {
		return nil, err
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	bounds := source.Bounds()
	sourceWidth, sourceHeight := bounds.Dx(), bounds.Dy()
	// 独立遮罩可比基础图更精细（例如 1×1 纯色图配高分辨率图形遮罩）。
	// 合成栅格取两者各轴的最大值，避免把遮罩降到基础图尺寸而丢掉细节。
	width, height := max(sourceWidth, maskWidth), max(sourceHeight, maskHeight)
	if p.control != nil && (width != sourceWidth || height != sourceHeight) {
		pixels, err := p.control.imagePixels(int64(width), int64(height))
		if err != nil {
			return nil, err
		}
		// reserveImageDecode 已计入源尺寸；额外预留合成栅格增加的像素。
		if err := p.control.addImagePixels(pixels - int64(sourceWidth)*int64(sourceHeight)); err != nil {
			return nil, err
		}
	}
	var target draw.Image
	switch source.(type) {
	case *image.Gray16, *image.RGBA64, *image.NRGBA64:
		target = image.NewNRGBA64(image.Rect(0, 0, width, height))
	default:
		target = image.NewNRGBA(image.Rect(0, 0, width, height))
	}
	for y := 0; y < height; y++ {
		if err := p.control.consumeOperations(int64(width) * int64(workPerPixel)); err != nil {
			return nil, err
		}
		row := image.Rect(0, y, width, y+1)
		if width == sourceWidth && height == sourceHeight {
			draw.Draw(target, row, source, image.Pt(bounds.Min.X, bounds.Min.Y+y), draw.Src)
		} else {
			sourceY := bounds.Min.Y + pdfSoftMaskRow(y, sourceHeight, height)
			xdraw.NearestNeighbor.Scale(target, row, source, image.Rect(bounds.Min.X, sourceY, bounds.Max.X, sourceY+1), draw.Src, nil)
		}
		maskY := pdfSoftMaskRow(y, maskHeight, height)
		for x := 0; x < width; x++ {
			if visible(pdfSoftMaskColumn(x, maskWidth, width), maskY) {
				continue
			}
			switch out := target.(type) {
			case *image.NRGBA:
				out.Pix[out.PixOffset(x, y)+3] = 0
			case *image.NRGBA64:
				offset := out.PixOffset(x, y) + 6
				out.Pix[offset], out.Pix[offset+1] = 0, 0
			}
		}
	}
	encoded, _, err := encodePNGImage(target)
	return encoded, err
}
