package pdf2ofd

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/pkg/creator"
)

func (p *pdfInterpreter) xobject(name string, resources types.Dict, depth int) error {
	dict, ok := dereferencedSubDict(p.ctx, resources, "XObject")
	if !ok {
		return nil
	}
	object, found := dict.Find(name)
	if !found {
		return nil
	}
	stream, _, err := p.ctx.XRefTable.DereferenceStreamDict(object)
	if err != nil || stream == nil {
		return err
	}
	subtype := stream.NameEntry("Subtype")
	if subtype == nil {
		return nil
	}
	if *subtype == "Image" {
		return p.appendImage(stream)
	}
	if *subtype == "Form" {
		if stream.Decode() != nil {
			return nil
		}
		formState := p.state
		matrix := identityPDFMatrix()
		if array := stream.ArrayEntry("Matrix"); len(array) == 6 {
			for i := range matrix {
				matrix[i] = anyFloat(array[i])
			}
		}
		formState.ctm = multiplyPDFMatrix(formState.ctm, matrix)
		formResources := resources
		if object, found := stream.Find("Resources"); found {
			if value, err := p.ctx.XRefTable.DereferenceDict(object); err == nil {
				formResources = value
			}
		}
		parentState := p.state
		parentStackLength := len(p.stack)
		err := p.parse(stream.Content, formResources, &formState, depth+1)
		p.state = parentState
		if len(p.stack) > parentStackLength {
			p.stack = p.stack[:parentStackLength]
		}
		return err
	}
	return nil
}

// appendImage 把解码后的图像按当前 CTM 的包围盒放入页面。内联图像与图像
// XObject 共用该逻辑。
func (p *pdfInterpreter) appendImage(stream *types.StreamDict) error {
	data, format, err := pdfImageData(p.ctx, stream, p.state.fill)
	if err != nil {
		return err
	}
	points := make([][2]float64, 4)
	points[0][0], points[0][1] = transformPDFPoint(0, 0, p.state.ctm)
	points[1][0], points[1][1] = transformPDFPoint(1, 0, p.state.ctm)
	points[2][0], points[2][1] = transformPDFPoint(0, 1, p.state.ctm)
	points[3][0], points[3][1] = transformPDFPoint(1, 1, p.state.ctm)
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, point := range points {
		x, y := p.pagePoint(point[0], point[1])
		minX, minY, maxX, maxY = math.Min(minX, x), math.Min(minY, y), math.Max(maxX, x), math.Max(maxY, y)
	}
	width, height := math.Max(maxX-minX, 0.001), math.Max(maxY-minY, 0.001)
	image := creator.Image{X: minX, Y: minY, Width: width, Height: height, Data: data, Format: format}
	// 图片对象在阅读器侧默认使用 {Width,0,0,Height,0,0} 的 CTM，
	// 裁剪区坐标需要抵消该缩放后才能使用毫米坐标。
	if clips := p.buildClips(minX, minY, true, width, height); clips != nil {
		image.Clips = clips
	}
	p.page.Items = append(p.page.Items, image)
	return nil
}

// inlineImageKeys 把内联图像字典的缩写键展开为 pdfImageData 使用的完整键。
var inlineImageKeys = map[string]string{
	"BPC": "BitsPerComponent",
	"CS":  "ColorSpace",
	"D":   "Decode",
	"DP":  "DecodeParms",
	"F":   "Filter",
	"H":   "Height",
	"IM":  "ImageMask",
	"I":   "Interpolate",
	"W":   "Width",
}

// inlineImageFilterNames 展开内联图像过滤器的缩写名称。
var inlineImageFilterNames = map[string]string{
	"Fl":  "FlateDecode",
	"AHx": "ASCIIHexDecode",
	"A85": "ASCII85Decode",
	"LZW": "LZWDecode",
	"RL":  "RunLengthDecode",
	"CCF": "CCITTFaxDecode",
	"DCT": "DCTDecode",
	"JPX": "JPXDecode",
}

func (p *pdfInterpreter) inlineImage(tokens *pdfContentTokenizer) error {
	dict, data, err := tokens.inlineImage()
	if err != nil {
		return err
	}
	stream := &types.StreamDict{Dict: types.Dict{}}
	for key, value := range dict {
		name := key
		if full, ok := inlineImageKeys[key]; ok {
			name = full
		}
		if name == "ColorSpace" {
			value = inlineImageColorSpace(value)
		}
		stream.Insert(name, inlineImageObject(value))
	}
	if _, ok := stream.Find("ColorSpace"); !ok {
		// 内联图像未给出颜色空间时默认 DeviceGray（PDF 32000-1 表 93）。
		stream.Insert("ColorSpace", types.Name("DeviceGray"))
	}
	stream.Raw = data
	if pipeline := inlineImageFilterPipeline(stream); len(pipeline) > 0 {
		stream.FilterPipeline = pipeline
	}
	return p.appendImage(stream)
}

func inlineImageColorSpace(value any) any {
	switch typed := value.(type) {
	case pdfName:
		return pdfName(inlineImageColorSpaceName(string(typed)))
	case []any:
		for index, item := range typed {
			typed[index] = inlineImageColorSpace(item)
		}
		return typed
	default:
		return value
	}
}

func inlineImageColorSpaceName(name string) string {
	switch name {
	case "G":
		return "DeviceGray"
	case "RGB":
		return "DeviceRGB"
	case "CMYK":
		return "DeviceCMYK"
	case "I":
		return "Indexed"
	default:
		return name
	}
}

func inlineImageObject(value any) types.Object {
	switch typed := value.(type) {
	case float64:
		if typed == math.Trunc(typed) {
			return types.Integer(int(typed))
		}
		return types.Float(typed)
	case int:
		return types.Integer(typed)
	case pdfName:
		return types.Name(string(typed))
	case pdfString:
		return types.StringLiteral(string(typed))
	case []any:
		array := make(types.Array, 0, len(typed))
		for _, item := range typed {
			array = append(array, inlineImageObject(item))
		}
		return array
	case map[string]any:
		dict := types.Dict{}
		for key, item := range typed {
			if value := inlineImageObject(item); value != nil {
				dict[key] = value
			}
		}
		return dict
	case string:
		switch typed {
		case "true":
			return types.Boolean(true)
		case "false":
			return types.Boolean(false)
		default:
			return types.Name(typed)
		}
	default:
		return nil
	}
}

func inlineImageFilterPipeline(stream *types.StreamDict) []types.PDFFilter {
	object, found := stream.Find("Filter")
	if !found {
		return nil
	}
	var names []string
	switch value := object.(type) {
	case types.Name:
		names = append(names, value.Value())
	case types.Array:
		for _, item := range value {
			if name, ok := item.(types.Name); ok {
				names = append(names, name.Value())
			}
		}
	}
	pipeline := make([]types.PDFFilter, 0, len(names))
	for _, name := range names {
		if full, ok := inlineImageFilterNames[name]; ok {
			name = full
		}
		pipeline = append(pipeline, types.PDFFilter{Name: name})
	}
	return pipeline
}

func isJPEGData(data []byte) bool {
	return len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF
}

func pdfImageData(ctx *model.Context, stream *types.StreamDict, maskColor pdfColor) ([]byte, string, error) {
	for index, filter := range stream.FilterPipeline {
		if filter.Name != "DCTDecode" {
			continue
		}
		// DCTDecode 是图像管线的最后一层。pdfcpu 的 Decode 只应用它之前的
		// 过滤层（例如 FlateDecode），把未解码的 JPEG 数据留在 Content，
		// 而 Raw 仍是外层过滤后的字节，因此优先使用 Content。
		var jpegData []byte
		if index > 0 && stream.Decode() == nil && isJPEGData(stream.Content) {
			jpegData = append([]byte(nil), stream.Content...)
		} else if isJPEGData(stream.Raw) {
			jpegData = append([]byte(nil), stream.Raw...)
		} else {
			return nil, "", errors.New("JPEG 图像解码失败")
		}
		// DeviceCMYK JPEG 直接嵌入 OFD 时，多数渲染器会按 Adobe 约定解码成
		// 反相颜色（整幅变黑）。这里转换为 RGB PNG，保证红章等彩色图像正确。
		if encoded, format, ok, err := encodePDFCMYKJPEG(jpegData); err != nil {
			return nil, "", err
		} else if ok {
			return encoded, format, nil
		}
		return jpegData, "JPEG", nil
	}
	if stream.Decode() != nil {
		return nil, "", errors.New("图像流解码失败")
	}
	widthObject, wok := stream.Find("Width")
	heightObject, hok := stream.Find("Height")
	bpcObject, _ := stream.Find("BitsPerComponent")
	width, widthOK := integerValue(widthObject)
	height, heightOK := integerValue(heightObject)
	bpc, _ := integerValue(bpcObject)
	if !wok || !hok || !widthOK || !heightOK {
		return nil, "", errors.New("PNG 图像尺寸无效")
	}
	if imageMask, found := stream.Find("ImageMask"); found {
		if value, ok := imageMask.(types.Boolean); ok && value.Value() {
			if bpc == 0 {
				bpc = 1
			}
			if bpc != 1 {
				return nil, "", errors.New("ImageMask 仅支持 1 位图像")
			}
			return encodePDFImageMask(ctx, stream, maskColor, width, height)
		}
	}
	components, indexed, palette, err := pdfImageColorSpace(ctx, stream)
	if err != nil {
		return nil, "", err
	}
	if bpc != 8 && !indexed {
		return nil, "", errors.New("仅支持 8 位 PNG 图像")
	}
	if indexed {
		return encodePDFIndexedImage(stream.Content, width, height, bpc, components, palette)
	}
	raw := stream.Content
	if len(raw) != width*height*components {
		return nil, "", errors.New("PNG 图像数据长度无效")
	}
	if components == 4 {
		// 非 DCT 编码的 DeviceCMYK 样本已是油墨值（0 表示无油墨），无需反相。
		return encodePDFCMYKImage(&image.CMYK{Pix: raw, Stride: width * 4, Rect: image.Rect(0, 0, width, height)}, false)
	}
	if components == 1 {
		gray := image.NewGray(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			copy(gray.Pix[y*gray.Stride:], raw[y*width:(y+1)*width])
		}
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, gray); err != nil {
			return nil, "", fmt.Errorf("编码 PNG 图像失败: %w", err)
		}
		return encoded.Bytes(), "PNG", nil
	}
	rgba := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			index := (y*width + x) * components
			rgba.SetRGBA(x, y, color.RGBA{R: raw[index], G: raw[index+1], B: raw[index+2], A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, rgba); err != nil {
		return nil, "", fmt.Errorf("编码 PNG 图像失败: %w", err)
	}
	return encoded.Bytes(), "PNG", nil
}

func pdfImageColorSpace(ctx *model.Context, stream *types.StreamDict) (components int, indexed bool, palette []byte, err error) {
	components = 3
	object, found := stream.Find("ColorSpace")
	if !found {
		return components, false, nil, nil
	}
	object, err = dereferencePDFObject(ctx, object)
	if err != nil {
		return 0, false, nil, err
	}
	if name, ok := object.(types.Name); ok {
		return pdfNamedColorSpace(name.Value())
	}
	array, ok := object.(types.Array)
	if !ok || len(array) == 0 {
		return 0, false, nil, errors.New("图像颜色空间无效")
	}
	first, err := dereferencePDFObject(ctx, array[0])
	if err != nil {
		return 0, false, nil, err
	}
	name, ok := first.(types.Name)
	if !ok {
		return 0, false, nil, errors.New("图像颜色空间名称无效")
	}
	switch name.Value() {
	case "Indexed":
		if len(array) < 4 {
			return 0, false, nil, errors.New("Indexed 图像颜色空间参数不足")
		}
		base, baseIndexed, _, err := pdfArrayColorSpace(ctx, array[1])
		if err != nil || baseIndexed {
			return 0, false, nil, errors.New("Indexed 基础颜色空间不支持")
		}
		high, ok := dereferencedPDFNumber(ctx, array[2])
		if !ok || high < 0 || high > 255 {
			return 0, false, nil, errors.New("Indexed 颜色空间索引范围无效")
		}
		lookup, err := dereferencePDFObject(ctx, array[3])
		if err != nil {
			return 0, false, nil, err
		}
		if stream, ok := lookup.(types.StreamDict); ok {
			if err := stream.Decode(); err != nil {
				return 0, false, nil, fmt.Errorf("解码 Indexed 图像调色板失败: %w", err)
			}
			lookup = stream
		}
		palette, ok := pdfBytesObject(lookup)
		if !ok || len(palette) < (int(high)+1)*base {
			return 0, false, nil, errors.New("Indexed 图像调色板无效")
		}
		return base, true, palette, nil
	case "ICCBased":
		if len(array) < 2 {
			return 0, false, nil, errors.New("ICCBased 图像颜色空间参数不足")
		}
		profile, err := dereferencePDFObject(ctx, array[1])
		if err != nil {
			return 0, false, nil, err
		}
		if dict, ok := profile.(types.Dict); ok {
			if value, ok := dereferencedPDFNumber(ctx, dict["N"]); ok {
				return int(value), false, nil, nil
			}
		}
		return 3, false, nil, nil
	default:
		return pdfNamedColorSpace(name.Value())
	}
}

func pdfArrayColorSpace(ctx *model.Context, object types.Object) (int, bool, []byte, error) {
	object, err := dereferencePDFObject(ctx, object)
	if err != nil {
		return 0, false, nil, err
	}
	if name, ok := object.(types.Name); ok {
		components, _, _, err := pdfNamedColorSpace(name.Value())
		return components, false, nil, err
	}
	array, ok := object.(types.Array)
	if !ok || len(array) == 0 {
		return 0, false, nil, errors.New("基础颜色空间无效")
	}
	name, ok := array[0].(types.Name)
	if !ok {
		return 0, false, nil, errors.New("基础颜色空间名称无效")
	}
	if name.Value() == "ICCBased" && len(array) > 1 {
		profile, err := dereferencePDFObject(ctx, array[1])
		if err != nil {
			return 0, false, nil, err
		}
		if dict, ok := profile.(types.Dict); ok {
			if value, ok := dereferencedPDFNumber(ctx, dict["N"]); ok {
				return int(value), false, nil, nil
			}
		}
		return 3, false, nil, nil
	}
	return pdfNamedColorSpace(name.Value())
}

func pdfNamedColorSpace(name string) (int, bool, []byte, error) {
	switch name {
	case "DeviceGray", "CalGray":
		return 1, false, nil, nil
	case "DeviceRGB", "CalRGB":
		return 3, false, nil, nil
	case "DeviceCMYK":
		return 4, false, nil, nil
	default:
		return 0, false, nil, fmt.Errorf("不支持的图像颜色空间 %s", name)
	}
}

func dereferencePDFObject(ctx *model.Context, object types.Object) (types.Object, error) {
	if object == nil {
		return nil, errors.New("PDF 图像对象为空")
	}
	if ctx == nil || ctx.XRefTable == nil {
		return object, nil
	}
	return ctx.XRefTable.Dereference(object)
}

func dereferencedPDFNumber(ctx *model.Context, object types.Object) (float64, bool) {
	value, err := dereferencePDFObject(ctx, object)
	if err != nil {
		return 0, false
	}
	return numberValue(value)
}

func pdfBytesObject(object types.Object) ([]byte, bool) {
	switch value := object.(type) {
	case types.StreamDict:
		return append([]byte(nil), value.Content...), true
	case *types.StreamDict:
		if value == nil {
			return nil, false
		}
		return append([]byte(nil), value.Content...), true
	case types.StringLiteral:
		return []byte(value.Value()), true
	case types.HexLiteral:
		bytes, err := value.Bytes()
		return bytes, err == nil
	default:
		return nil, false
	}
}

func encodePDFIndexedImage(data []byte, width, height, bpc, components int, palette []byte) ([]byte, string, error) {
	if bpc <= 0 || bpc > 8 {
		return nil, "", errors.New("Indexed 图像位深无效")
	}
	if components != 1 && components != 3 {
		return nil, "", errors.New("Indexed 基础颜色空间不支持")
	}
	rowBytes := (width*bpc + 7) / 8
	if len(data) < rowBytes*height {
		return nil, "", errors.New("Indexed 图像数据长度无效")
	}
	rgba := image.NewRGBA(image.Rect(0, 0, width, height))
	mask := byte((1 << bpc) - 1)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			bit := x * bpc
			value := (data[y*rowBytes+bit/8] >> uint(8-bpc-bit%8)) & mask
			paletteIndex := int(value) * components
			if paletteIndex+components-1 >= len(palette) {
				return nil, "", errors.New("Indexed 图像索引超出调色板")
			}
			if components == 1 {
				rgba.SetRGBA(x, y, color.RGBA{R: palette[paletteIndex], G: palette[paletteIndex], B: palette[paletteIndex], A: 255})
			} else {
				rgba.SetRGBA(x, y, color.RGBA{R: palette[paletteIndex], G: palette[paletteIndex+1], B: palette[paletteIndex+2], A: 255})
			}
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, rgba); err != nil {
		return nil, "", fmt.Errorf("编码 Indexed PNG 图像失败: %w", err)
	}
	return encoded.Bytes(), "PNG", nil
}

// encodePDFCMYKJPEG 把 DeviceCMYK 的 JPEG 转换为 RGB PNG。Go 的 jpeg 解码器
// 对 Adobe APP14 transform=0 的 CMYK JPEG 会做一次反相，而 PDF 中 DeviceCMYK
// 样本本身就是油墨值，因此需要再反相还原。返回 ok=false 表示该 JPEG 不是
// CMYK 图像，调用方应按原始字节处理。
func encodePDFCMYKJPEG(data []byte) ([]byte, string, bool, error) {
	transform, adobe := jpegAdobeTransform(data)
	if !adobe || transform != 0 {
		return nil, "", false, nil
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", false, nil
	}
	cmyk, ok := img.(*image.CMYK)
	if !ok {
		return nil, "", false, nil
	}
	encoded, format, err := encodePDFCMYKImage(cmyk, true)
	if err != nil {
		return nil, "", false, err
	}
	return encoded, format, true, nil
}

// jpegAdobeTransform 返回 JPEG 的 Adobe APP14 颜色变换类型。第二个返回值为
// false 表示不存在 Adobe APP14 标记；transform 0 表示 CMYK，2 表示 YCbCrK。
func jpegAdobeTransform(data []byte) (byte, bool) {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 0, false
	}
	for offset := 2; offset+4 <= len(data); {
		if data[offset] != 0xFF {
			return 0, false
		}
		marker := data[offset+1]
		if marker == 0xD8 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			offset += 2
			continue
		}
		if marker == 0xDA || marker == 0xD9 {
			return 0, false
		}
		length := int(data[offset+2])<<8 | int(data[offset+3])
		if length < 2 || offset+2+length > len(data) {
			return 0, false
		}
		if marker == 0xEE && length >= 12 {
			segment := data[offset+4 : offset+2+length]
			if len(segment) >= 12 && string(segment[:5]) == "Adobe" {
				return segment[11], true
			}
		}
		offset += 2 + length
	}
	return 0, false
}

// 标准四色印刷油墨在白纸上的近似 sRGB 表现。直接用 color.CMYKToRGB 会把
// 纯品红画成 (255,0,255)，比实际印刷色偏亮偏紫，因此按油墨减色模型合成。
var (
	processInkCyan    = [3]float64{0, 174, 239}
	processInkMagenta = [3]float64{236, 0, 139}
	processInkYellow  = [3]float64{255, 242, 0}
)

// cmykInkToRGB 用减色模型把 CMYK 油墨量合成为 RGB：每种油墨按其在某个通道
// 上对白光的吸收量线性叠加，最后用黑色油墨吸收全部通道。
func cmykInkToRGB(c, m, y, k uint8) (uint8, uint8, uint8) {
	cyan, magenta, yellow, black := float64(c)/255, float64(m)/255, float64(y)/255, float64(k)/255
	absorb := func(cyanSolid, magentaSolid, yellowSolid float64) uint8 {
		value := cyan*(255-cyanSolid) + magenta*(255-magentaSolid) + yellow*(255-yellowSolid) + black*255
		if value > 255 {
			value = 255
		}
		return uint8(math.Round(255 - value))
	}
	return absorb(processInkCyan[0], processInkMagenta[0], processInkYellow[0]),
		absorb(processInkCyan[1], processInkMagenta[1], processInkYellow[1]),
		absorb(processInkCyan[2], processInkMagenta[2], processInkYellow[2])
}

// encodePDFCMYKImage 将 CMYK 图像转换为 RGB PNG；invert 为 true 时先对四个
// 分量取反，用于还原 Go 解码 Adobe CMYK JPEG 时引入的反相。
func encodePDFCMYKImage(img *image.CMYK, invert bool) ([]byte, string, error) {
	bounds := img.Bounds()
	rgba := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			index := img.PixOffset(x, y)
			c, m, yellow, k := img.Pix[index], img.Pix[index+1], img.Pix[index+2], img.Pix[index+3]
			if invert {
				c, m, yellow, k = 255-c, 255-m, 255-yellow, 255-k
			}
			r, g, b := cmykInkToRGB(c, m, yellow, k)
			target := rgba.PixOffset(x, y)
			rgba.Pix[target], rgba.Pix[target+1], rgba.Pix[target+2], rgba.Pix[target+3] = r, g, b, 255
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, rgba); err != nil {
		return nil, "", fmt.Errorf("编码 CMYK PNG 图像失败: %w", err)
	}
	return encoded.Bytes(), "PNG", nil
}

func encodePDFImageMask(ctx *model.Context, stream *types.StreamDict, maskColor pdfColor, width, height int) ([]byte, string, error) {
	rgba := image.NewRGBA(image.Rect(0, 0, width, height))
	rowBytes := (width + 7) / 8
	if len(stream.Content) < rowBytes*height {
		return nil, "", errors.New("ImageMask 图像数据长度无效")
	}
	decodeMin, decodeMax := 0.0, 1.0
	if decode, found := stream.Find("Decode"); found {
		if array, ok := decode.(types.Array); ok && len(array) >= 2 {
			if value, ok := dereferencedPDFNumber(ctx, array[0]); ok {
				decodeMin = value
			}
			if value, ok := dereferencedPDFNumber(ctx, array[1]); ok {
				decodeMax = value
			}
		}
	}
	// 掩码样本解码值 0 表示以当前填充色着色，1 表示透明（PDF 8.9.6.2）。
	// 默认 Decode [0 1] 因此着色 0 位；[1 0] 着色 1 位。
	paintOne := decodeMax < decodeMin
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			bit := (stream.Content[y*rowBytes+x/8] >> uint(7-x%8)) & 1
			paint := (bit == 1) == paintOne
			alpha := uint8(0)
			if paint {
				alpha = 255
			}
			rgba.SetRGBA(x, y, color.RGBA{R: maskColor.r, G: maskColor.g, B: maskColor.b, A: alpha})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, rgba); err != nil {
		return nil, "", fmt.Errorf("编码 ImageMask PNG 失败: %w", err)
	}
	return encoded.Bytes(), "PNG", nil
}
