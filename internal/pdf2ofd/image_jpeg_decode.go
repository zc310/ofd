package pdf2ofd

import (
	"fmt"
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// jpegDecodeData 将非恒等 PDF Decode 烘进 PNG：原 JPEG 分量 -> Decode -> RGB。
// Adobe CMYK/YCCK 由共用原分量路径撤销 Go 反相，不能在 RGB PNG 上反相。
// 普通图像首次解码及输出合成计入预算；恒等 Decode 保留原 JPEG 处理路径。
func (p *pdfInterpreter) jpegDecodeData(stream *types.StreamDict, jpegData []byte) ([]byte, bool, error) {
	components, indexed, _, err := pdfImageColorSpace(p.ctx, stream)
	if err != nil {
		return nil, false, err
	}
	bits, _ := dereferencedPDFNumber(p.ctx, stream.Dict["BitsPerComponent"])
	if indexed || bits != 8 || (components != 1 && components != 3 && components != 4) {
		return nil, false, fmt.Errorf("JPEG Decode 当前需要 8 位 Gray/RGB/CMYK 分量")
	}
	object, err := dereferencePDFObject(p.ctx, stream.Dict["Decode"])
	if err != nil {
		return nil, false, err
	}
	array, ok := object.(types.Array)
	if !ok || len(array) != components*2 {
		return nil, false, fmt.Errorf("JPEG Decode 分量数量无效")
	}
	identity := true
	decode := make(types.Array, len(array))
	for i, object := range array {
		value, ok := dereferencedPDFNumber(p.ctx, object)
		if !ok || !calculatorFinite(value) {
			return nil, false, fmt.Errorf("JPEG Decode 端点不是有限数值")
		}
		decode[i] = types.Float(value)
		if value != float64(i%2) {
			identity = false
		}
	}
	if identity {
		return nil, false, nil
	}
	// 使用已解开外层过滤器的 JPEG 字节和已解引用端点，不改变源流或遮罩。
	source := *stream
	source.Dict = stream.Dict.Clone().(types.Dict)
	source.Dict["Decode"] = decode
	source.Content = jpegData
	widthValue, wok := dereferencedPDFNumber(p.ctx, source.Dict["Width"])
	heightValue, hok := dereferencedPDFNumber(p.ctx, source.Dict["Height"])
	// JPEG SOF 的两个尺寸字段是 16 位；先校验再转 int，间接尺寸也须保留。
	if !wok || !hok || !calculatorFinite(widthValue) || !calculatorFinite(heightValue) || widthValue < 1 || heightValue < 1 || widthValue > 65535 || heightValue > 65535 || widthValue != math.Trunc(widthValue) || heightValue != math.Trunc(heightValue) {
		return nil, false, fmt.Errorf("JPEG Decode 尺寸无效")
	}
	width, height := int(widthValue), int(heightValue)
	decoded, err := p.jpegImagePlanes(&source, width, height, components, false)
	if err != nil {
		return nil, false, err
	}
	data, err := p.encodeCompressedImage(&source, width, height, components, 255, nil, func(x, y int) [4]uint32 {
		values := rawJPEGComponents(decoded, x, y)
		var samples [4]uint32
		for c := 0; c < components; c++ {
			samples[c] = uint32(floatToByte(values[c]))
		}
		return samples
	})
	return data, err == nil, err
}
