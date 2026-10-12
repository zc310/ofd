package pdf2ofd

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"

	jpeg2000 "github.com/mrjoshuak/go-jpeg2000"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// imageMatteData 在原颜色空间中还原预混合样本：C = (C' - (1-a)M) / a。
// 尤其不能在 CMYK 已转为 RGB 的 PNG 上还原，因为颜色转换不是线性的。
// Matte 要求掩码与基础图同尺寸；压缩图像也必须先恢复原颜色分量。
func (p *pdfInterpreter) imageMatteData(base, mask *types.StreamDict) ([]byte, error) {
	if err := p.control.check(); err != nil {
		return nil, err
	}
	family, ok := dereferenceName(p.ctx, base.Dict["ColorSpace"])
	var baseJPX, maskJPX *matteJPXInfo
	var err error
	if hasJPXFilter(base) {
		baseJPX, err = p.matteJPXHeader(base)
		if err != nil {
			return nil, err
		}
		family, ok = baseJPX.family, true
	}
	components := 0
	switch family {
	case "DeviceGray":
		components = 1
	case "DeviceRGB":
		components = 3
	case "DeviceCMYK":
		components = 4
	}
	if !ok || components == 0 || pdfImageIsMask(p.ctx, base) {
		return nil, fmt.Errorf("Matte 当前只支持 DeviceGray/RGB/CMYK")
	}
	object, err := dereferencePDFObject(p.ctx, mask.Dict["Matte"])
	if err != nil {
		return nil, err
	}
	array, ok := object.(types.Array)
	if !ok || len(array) != components {
		return nil, fmt.Errorf("Matte 分量数与基础图颜色空间不一致")
	}
	var matte [4]float64
	for i, object := range array {
		value, ok := dereferencedPDFNumber(p.ctx, object)
		if !ok || !calculatorFinite(value) || value < 0 || value > 1 {
			return nil, fmt.Errorf("Matte 分量必须是 0 到 1 的有限数值")
		}
		matte[i] = value
	}
	width, wok := integerValue(base.Dict["Width"])
	height, hok := integerValue(base.Dict["Height"])
	maskWidth, mwok := integerValue(mask.Dict["Width"])
	maskHeight, mhok := integerValue(mask.Dict["Height"])
	if !wok || !hok || !mwok || !mhok || width <= 0 || height <= 0 || width != maskWidth || height != maskHeight {
		return nil, fmt.Errorf("带 Matte 的软掩码必须与基础图同尺寸")
	}
	if mask.Dict["SMask"] != nil || mask.Dict["Mask"] != nil || pdfImageIsMask(p.ctx, mask) {
		return nil, fmt.Errorf("Matte 软掩码不能包含嵌套遮罩或 ImageMask")
	}
	maskFamily, maskCSOK := dereferenceName(p.ctx, mask.Dict["ColorSpace"])
	if hasJPXFilter(mask) {
		maskJPX, err = p.matteJPXHeader(mask)
		if err != nil {
			return nil, err
		}
		maskFamily, maskCSOK = maskJPX.family, true
	}
	if !maskCSOK || maskFamily != "DeviceGray" {
		return nil, fmt.Errorf("Matte 软掩码需要 DeviceGray 颜色空间")
	}
	bpc, _ := integerValue(base.Dict["BitsPerComponent"])
	maskBPC, _ := integerValue(mask.Dict["BitsPerComponent"])
	if baseJPX != nil {
		bpc = baseJPX.metadata.BitsPerComponent[0]
	}
	if maskJPX != nil {
		maskBPC = maskJPX.metadata.BitsPerComponent[0]
	}
	validBits := func(bits int) bool { return bits == 1 || bits == 2 || bits == 4 || bits == 8 || bits == 16 }
	if (baseJPX == nil && !validBits(bpc)) || (maskJPX == nil && !validBits(maskBPC)) {
		return nil, fmt.Errorf("Matte 图像或软掩码位深无效")
	}
	if (hasDCTFilter(base) && bpc != 8) || (hasDCTFilter(mask) && maskBPC != 8) {
		return nil, fmt.Errorf("Matte 的 JPEG 解码仅支持 8 位采样")
	}
	if (hasJBIG2Filter(base) && (bpc != 1 || components != 1)) || (hasJBIG2Filter(mask) && maskBPC != 1) {
		return nil, fmt.Errorf("Matte 的 JBIG2 图像需要 1 位 DeviceGray 采样")
	}
	if p.control != nil {
		if err := p.preflightImage(mask, 1); err != nil {
			return nil, err
		}
	} else if err := p.decodeStream(mask, 0); err != nil {
		return nil, err
	}
	maximum := int(^uint(0) >> 1)
	if width > (maximum-7)/components/bpc || width > maximum/8/height {
		return nil, fmt.Errorf("Matte 图像尺寸超出当前平台范围")
	}
	rowBytes := (width*components*bpc + 7) / 8
	if baseJPX == nil && !hasDCTFilter(base) && !hasJBIG2Filter(base) && height > len(base.Content)/rowBytes {
		return nil, fmt.Errorf("Matte 基础图采样数据不完整")
	}
	pixels := int64(width) * int64(height)
	if p.control != nil {
		count, err := p.control.imagePixels(int64(width), int64(height))
		if err != nil {
			return nil, err
		}
		if err := p.control.addImagePixels(count); err != nil {
			return nil, err
		}
	}
	var compressedBase image.Image
	var jpxBase *jpeg2000.FloatImage
	if hasDCTFilter(base) {
		compressedBase, err = p.rawJPEGImage(base, width, height, components)
	} else if hasJBIG2Filter(base) {
		compressedBase, err = p.rawJBIG2Image(base, width, height)
	} else if baseJPX != nil {
		jpxBase, err = p.matteJPXImage(base, baseJPX, width, height)
	}
	if err != nil {
		return nil, err
	}
	precision16 := bpc > 8 || maskBPC > 8
	var alpha16 []uint16
	var alpha8 []byte
	if maskJPX != nil {
		alpha16, err = p.matteJPXAlpha(mask, maskJPX, width, height)
	} else if hasJBIG2Filter(mask) {
		var gray *image.Gray
		gray, err = p.rawJBIG2Image(mask, width, height)
		if err != nil {
			return nil, err
		}
		alpha16, err = p.matteJBIG2Alpha(mask, gray)
	} else if maskBPC == 16 {
		alpha16, err = decodePDFImageSamples16(p.ctx, mask, width, height, 1, p.control)
	} else {
		if err = p.control.consumeOperations(pixels); err == nil {
			if hasDCTFilter(mask) {
				alpha8, err = p.matteJPEGAlpha(mask, width, height)
			} else {
				alpha8, err = pdfImageMaskSamples(p.ctx, mask, width, height, maskBPC, p.control)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	// 颜色还原逐分量计数，另外计入每像素的输出写入；分配输出前预留。
	if err := p.control.consumeOperations(pixels * int64(components+1)); err != nil {
		return nil, err
	}
	var output draw.Image
	if precision16 {
		output = image.NewNRGBA64(image.Rect(0, 0, width, height))
	} else {
		output = image.NewNRGBA(image.Rect(0, 0, width, height))
	}
	decode := pdfImageDecode(p.ctx, base, components)
	if baseJPX != nil {
		// PDF JPX 图像的 Decode 被忽略；原颜色分量按码流位深归一化。
		for c := 0; c < components; c++ {
			decode[c*2], decode[c*2+1] = 0, 1
		}
	}
	maxSample := float64(uint32(1)<<uint(bpc) - 1)
	converter := pdfImageCMYKConverter(p.ctx, base)
	for y := 0; y < height; y++ {
		if err := p.control.check(); err != nil {
			return nil, err
		}
		var row []byte
		if compressedBase == nil && jpxBase == nil {
			row = base.Content[y*rowBytes : (y+1)*rowBytes]
		}
		for x := 0; x < width; x++ {
			index := y*width + x
			var a uint16
			if alpha16 != nil {
				a = alpha16[index]
			} else {
				a = uint16(alpha8[index]) * 257
			}
			if a == 0 {
				continue // 透明像素颜色无法反推，置零并避免除以零。
			}
			alpha := float64(a) / 65535
			var values [4]float64
			if compressedBase != nil {
				values = rawJPEGComponents(compressedBase, x, y)
			}
			for c := 0; c < components; c++ {
				sample := values[c]
				if jpxBase != nil {
					sample = float64(jpxBase.Components[c][index]) / maxSample
				} else if compressedBase == nil {
					sample = float64(readPDFImageSample(row, x*components+c, bpc)) / maxSample
				}
				preblended := clamp01(decode[c*2] + sample*(decode[c*2+1]-decode[c*2]))
				values[c] = clamp01((preblended - (1-alpha)*matte[c]) / alpha)
			}
			if precision16 {
				r, g, b := floatToUint16(values[0]), floatToUint16(values[0]), floatToUint16(values[0])
				if components == 3 {
					g, b = floatToUint16(values[1]), floatToUint16(values[2])
				} else if components == 4 {
					r, g, b = converter.toRGB16(floatToUint16(values[0]), floatToUint16(values[1]), floatToUint16(values[2]), floatToUint16(values[3]))
				}
				output.(*image.NRGBA64).SetNRGBA64(x, y, color.NRGBA64{R: r, G: g, B: b, A: a})
			} else {
				r, g, b := floatToByte(values[0]), floatToByte(values[0]), floatToByte(values[0])
				if components == 3 {
					g, b = floatToByte(values[1]), floatToByte(values[2])
				} else if components == 4 {
					r, g, b = converter.toRGB(floatToByte(values[0]), floatToByte(values[1]), floatToByte(values[2]), floatToByte(values[3]))
				}
				output.(*image.NRGBA).SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: floatToByte(alpha)})
			}
		}
	}
	data, _, err := encodePNGImage(output)
	if err == nil {
		err = p.control.check()
	}
	return data, err
}
