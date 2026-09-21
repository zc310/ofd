package pdf2ofd

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"

	gobig2 "github.com/dkrisman/gobig2"
	jpeg2000 "github.com/mrjoshuak/go-jpeg2000"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/coloricc"
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
		// 单个图像解码失败（例如 JPXDecode 等不支持的过滤器）不应中断整页
		// 转换，跳过该图像继续处理其余内容。
		_ = p.appendImage(stream)
		return nil
	}
	if *subtype == "Form" {
		if stream.Decode() != nil {
			return nil
		}
		formState := p.state
		// Form 作为透明度组绘制。OFD 没有混合模式：仅在 BM 为 Normal 时把外层
		// gs 的 ca/CA 累积为组透明度，并重置局部 ca/CA，避免 Form 内的 gs
		// （常见 ca=1）覆盖外层的不透明度。BM 为 Multiply/HardLight 等时，单独
		// 套用 ca/CA 会与混合效果叠加出更大偏差，因此保持原有叠加方式。
		if isNormalBlendMode(p.state.blendMode) {
			formState.groupAlpha = p.groupOpacity() * math.Min(p.state.fillAlpha, p.state.strokeAlpha)
			formState.fillAlpha = 1
			formState.strokeAlpha = 1
		}
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

// imageFlipCTM 判断图像是否需要镜像，并返回 OFD 图片对象的 CTM。仅处理轴对齐
// （无旋转/斜切）的 CTM：X 缩放为负表示水平镜像，Y 缩放为负表示垂直镜像。
// OFD 图片缺省 CTM 为 {Width,0,0,Height,0,0}，镜像时对相应轴取负并平移一个
// 边界长度，使图像仍落在原边界内。
func imageFlipCTM(ctm [6]float64, width, height float64) *creator.CTM {
	if ctm[1] != 0 || ctm[2] != 0 {
		return nil
	}
	vertical := ctm[3] < 0
	horizontal := ctm[0] < 0
	if !vertical && !horizontal {
		return nil
	}
	matrix := creator.CTM{width, 0, 0, height, 0, 0}
	if horizontal {
		matrix[0] = -width
		matrix[4] = width
	}
	if vertical {
		matrix[3] = -height
		matrix[5] = height
	}
	return &matrix
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
	image := creator.Image{X: minX, Y: minY, Width: width, Height: height, Data: data, Format: format, Alpha: ofdTransparency(p.fillOpacity())}
	// PDF 常通过负的缩放 CTM 翻转扫描图像（例如 595 0 0 -842 ... cm）。OFD
	// 图片缺省按边界正放，这里输出负缩放 CTM 让阅读器镜像。
	image.CTM = imageFlipCTM(p.state.ctm, width, height)
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
	// 与图像 XObject 一致：解码失败只跳过该图像。
	_ = p.appendImage(stream)
	return nil
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

// pdfImageData 编码图像为 PNG/JPEG；若图像带有 SMask（软掩码），则把掩码
// 作为 alpha 通道应用到图像上，避免透明区域被绘制成不透明色块。
func pdfImageData(ctx *model.Context, stream *types.StreamDict, maskColor pdfColor) ([]byte, string, error) {
	data, format, err := pdfImageDataRaw(ctx, stream, maskColor)
	if err != nil {
		return nil, "", err
	}
	maskStream := pdfImageSoftMask(ctx, stream)
	if maskStream == nil {
		return data, format, nil
	}
	masked, ok := applyPDFImageSoftMask(ctx, data, maskStream)
	if !ok {
		return data, format, nil
	}
	return masked, "PNG", nil
}

// pdfImageSoftMask 解析图像 /SMask 引用的软掩码流；不存在时返回 nil。
func pdfImageSoftMask(ctx *model.Context, stream *types.StreamDict) *types.StreamDict {
	object, found := stream.Find("SMask")
	if !found {
		return nil
	}
	mask, _, err := ctx.XRefTable.DereferenceStreamDict(object)
	if err != nil || mask == nil {
		return nil
	}
	return mask
}

// applyPDFImageSoftMask 把灰度软掩码作为 alpha 通道应用到已编码的图像上，
// 返回带透明度的 PNG。base 必须是可解码的 PNG/JPEG。
func applyPDFImageSoftMask(ctx *model.Context, base []byte, maskStream *types.StreamDict) ([]byte, bool) {
	baseImage, _, err := image.Decode(bytes.NewReader(base))
	if err != nil {
		return nil, false
	}
	maskWidth, okW := integerValue(maskStream.Dict["Width"])
	maskHeight, okH := integerValue(maskStream.Dict["Height"])
	if !okW || !okH || maskWidth <= 0 || maskHeight <= 0 {
		return nil, false
	}
	maskBPC, _ := integerValue(maskStream.Dict["BitsPerComponent"])
	if maskBPC == 0 {
		maskBPC = 1
	}
	if !hasJBIG2Filter(maskStream) && !hasJPXFilter(maskStream) {
		if maskBPC > 8 {
			// 高位深软掩码缺少通用样本解码路径，保持原图。
			return nil, false
		}
		if len(maskStream.Content) == 0 && maskStream.Decode() != nil {
			return nil, false
		}
	}
	samples, err := pdfImageMaskSamples(ctx, maskStream, maskWidth, maskHeight, maskBPC)
	if err != nil {
		return nil, false
	}
	bounds := baseImage.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	// 基础图像本身不透明，这里写入的是非预乘 RGB。必须使用 NRGBA：若写入
	// RGBA（预乘），PNG 编码会对低 alpha 像素做反预乘，把接近透明的颜色放大成
	// 红/品红色边缘。
	rgba := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		maskY := y * maskHeight / height
		if maskY >= maskHeight {
			maskY = maskHeight - 1
		}
		for x := 0; x < width; x++ {
			maskX := x * maskWidth / width
			if maskX >= maskWidth {
				maskX = maskWidth - 1
			}
			base := color.NRGBAModel.Convert(baseImage.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
			alpha := samples[maskY*maskWidth+maskX]
			target := rgba.PixOffset(x, y)
			rgba.Pix[target], rgba.Pix[target+1], rgba.Pix[target+2], rgba.Pix[target+3] = base.R, base.G, base.B, alpha
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, rgba); err != nil {
		return nil, false
	}
	return encoded.Bytes(), true
}

// hasJBIG2Filter 判断图像流是否使用 JBIG2Decode 过滤器。
func hasJBIG2Filter(stream *types.StreamDict) bool {
	if stream == nil {
		return false
	}
	for _, filter := range stream.FilterPipeline {
		if filter.Name == "JBIG2Decode" {
			return true
		}
	}
	return false
}

// jbig2StreamBytes 返回 JBIG2Decode 的段流字节。JBIG2Decode 是图像管线的最后
// 一层，pdfcpu 不解析它，因此其前面的过滤层（若有）需先解码。
func jbig2StreamBytes(stream *types.StreamDict) []byte {
	for index, filter := range stream.FilterPipeline {
		if filter.Name != "JBIG2Decode" {
			continue
		}
		if index == 0 {
			return stream.Raw
		}
		// 前置过滤层：解码后 Content 即为 JBIG2 段流。pdfcpu 遇到 JBIG2Decode
		// 会报错，因此这里只支持 JBIG2Decode 作为唯一过滤器。
		return nil
	}
	return nil
}

// pdfJBIG2Globals 读取 DecodeParms 的 /JBIG2Globals 段流（跨图像共享的符号字典）。
func pdfJBIG2Globals(ctx *model.Context, stream *types.StreamDict) []byte {
	for _, filter := range stream.FilterPipeline {
		if filter.Name != "JBIG2Decode" || filter.DecodeParms == nil {
			continue
		}
		object, found := filter.DecodeParms.Find("JBIG2Globals")
		if !found {
			continue
		}
		globals, _, err := ctx.XRefTable.DereferenceStreamDict(object)
		if err != nil || globals == nil {
			continue
		}
		if data := pdfStreamContent(globals); len(data) > 0 {
			return data
		}
	}
	return nil
}

// pdfJBIG2Image 用 gobig2 把 JBIG2Decode 段流解码为灰度位图（0 为墨、255 为纸）。
func pdfJBIG2Image(ctx *model.Context, stream *types.StreamDict) (*image.Gray, error) {
	data := jbig2StreamBytes(stream)
	if len(data) == 0 {
		return nil, errors.New("JBIG2 图像数据为空")
	}
	decoder, err := gobig2.NewDecoderEmbedded(bytes.NewReader(data), pdfJBIG2Globals(ctx, stream))
	if err != nil {
		return nil, fmt.Errorf("JBIG2 解码失败: %w", err)
	}
	decoded, err := decoder.Decode()
	if err != nil {
		return nil, fmt.Errorf("JBIG2 解码失败: %w", err)
	}
	gray, ok := decoded.(*image.Gray)
	if !ok || gray == nil || gray.Bounds().Empty() {
		return nil, errors.New("JBIG2 解码结果无效")
	}
	return gray, nil
}

// encodePDFJBIG2Image 把 JBIG2 灰度位图输出为 OFD 图像：ImageMask 按填充色着色，
// 普通图像按 /Decode 反相后输出灰度 PNG。
func encodePDFJBIG2Image(ctx *model.Context, stream *types.StreamDict, gray *image.Gray, maskColor pdfColor) ([]byte, string, error) {
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
	invert := decodeMax < decodeMin
	bounds := gray.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	imageMask := false
	if value, found := stream.Find("ImageMask"); found {
		if boolean, ok := value.(types.Boolean); ok {
			imageMask = boolean.Value()
		}
	}
	if imageMask {
		rgba := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				ink := gray.GrayAt(bounds.Min.X+x, bounds.Min.Y+y).Y < 128
				if invert {
					ink = !ink
				}
				if ink {
					rgba.SetRGBA(x, y, color.RGBA{R: maskColor.r, G: maskColor.g, B: maskColor.b, A: 255})
				}
			}
		}
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, rgba); err != nil {
			return nil, "", fmt.Errorf("编码 JBIG2 蒙版失败: %w", err)
		}
		return encoded.Bytes(), "PNG", nil
	}
	out := gray
	if invert {
		out = image.NewGray(bounds)
		for index, value := range gray.Pix {
			out.Pix[index] = 255 - value
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, out); err != nil {
		return nil, "", fmt.Errorf("编码 JBIG2 图像失败: %w", err)
	}
	return encoded.Bytes(), "PNG", nil
}

// hasJPXFilter 判断图像流是否使用 JPXDecode 过滤器。
func hasJPXFilter(stream *types.StreamDict) bool {
	if stream == nil {
		return false
	}
	for _, filter := range stream.FilterPipeline {
		if filter.Name == "JPXDecode" {
			return true
		}
	}
	return false
}

// jpxStreamBytes 返回 JPXDecode 的 JPEG 2000 码流。JPXDecode 是图像管线的最后
// 一层；pdfcpu 不解析它，只支持它作为唯一过滤器。
func jpxStreamBytes(stream *types.StreamDict) []byte {
	for index, filter := range stream.FilterPipeline {
		if filter.Name != "JPXDecode" {
			continue
		}
		if index == 0 {
			return stream.Raw
		}
		return nil
	}
	return nil
}

// encodePDFJPXImage 用纯 Go JPEG 2000 解码库把 JPXDecode 图像转为 PNG。库会按
// JP2 的 colr 信息做颜色转换，输出灰度或 RGBA；`/Decode [1 0]` 视为反相。
func encodePDFJPXImage(ctx *model.Context, stream *types.StreamDict, maskColor pdfColor) ([]byte, string, error) {
	data := jpxStreamBytes(stream)
	if len(data) == 0 {
		return nil, "", errors.New("JPX 图像数据为空")
	}
	decoded, err := jpeg2000.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("JPX 解码失败: %w", err)
	}
	if decoded == nil || decoded.Bounds().Empty() {
		return nil, "", errors.New("JPX 解码结果无效")
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
	invert := decodeMax < decodeMin
	imageMask := false
	if value, found := stream.Find("ImageMask"); found {
		if boolean, ok := value.(types.Boolean); ok {
			imageMask = boolean.Value()
		}
	}
	bounds := decoded.Bounds()
	if imageMask {
		rgba := image.NewRGBA(bounds)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				ink := color.GrayModel.Convert(decoded.At(x, y)).(color.Gray).Y < 128
				if invert {
					ink = !ink
				}
				if ink {
					rgba.SetRGBA(x, y, color.RGBA{R: maskColor.r, G: maskColor.g, B: maskColor.b, A: 255})
				}
			}
		}
		return encodePNGImage(rgba)
	}
	if !invert {
		return encodePNGImage(decoded)
	}
	out := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			value := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
			value.R, value.G, value.B = 255-value.R, 255-value.G, 255-value.B
			out.SetNRGBA(x, y, value)
		}
	}
	return encodePNGImage(out)
}

// encodePNGImage 把图像编码为 PNG。
func encodePNGImage(img image.Image) ([]byte, string, error) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		return nil, "", fmt.Errorf("编码 PNG 图像失败: %w", err)
	}
	return encoded.Bytes(), "PNG", nil
}

// pdfImageMaskSamples 解码软掩码样本：JBIG2 掩码用 gobig2，其余走通用样本解码。
func pdfImageMaskSamples(ctx *model.Context, stream *types.StreamDict, width, height, bpc int) ([]byte, error) {
	if hasJBIG2Filter(stream) {
		gray, err := pdfJBIG2Image(ctx, stream)
		if err != nil {
			return nil, err
		}
		return resampleGrayMask(gray, width, height), nil
	}
	if hasJPXFilter(stream) {
		data := jpxStreamBytes(stream)
		if len(data) == 0 {
			return nil, errors.New("JPX 掩码数据为空")
		}
		decoded, err := jpeg2000.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("JPX 掩码解码失败: %w", err)
		}
		return resampleImageGray(decoded, width, height), nil
	}
	return decodePDFImageSamples(ctx, stream, width, height, 1, bpc)
}

// resampleImageGray 把任意图像按最近邻缩放为 width×height 的灰度样本，用于软掩码。
func resampleImageGray(img image.Image, width, height int) []byte {
	bounds := img.Bounds()
	sourceWidth, sourceHeight := bounds.Dx(), bounds.Dy()
	samples := make([]byte, width*height)
	for y := 0; y < height; y++ {
		sourceY := bounds.Min.Y
		if sourceHeight > 0 {
			sourceY += y * sourceHeight / height
		}
		for x := 0; x < width; x++ {
			sourceX := bounds.Min.X
			if sourceWidth > 0 {
				sourceX += x * sourceWidth / width
			}
			samples[y*width+x] = color.GrayModel.Convert(img.At(sourceX, sourceY)).(color.Gray).Y
		}
	}
	return samples
}

// resampleGrayMask 把灰度位图按最近邻缩放到 width×height，作为软掩码的 alpha。
func resampleGrayMask(gray *image.Gray, width, height int) []byte {
	bounds := gray.Bounds()
	sourceWidth, sourceHeight := bounds.Dx(), bounds.Dy()
	samples := make([]byte, width*height)
	for y := 0; y < height; y++ {
		sourceY := 0
		if sourceHeight > 0 {
			sourceY = y * sourceHeight / height
		}
		sourceY += bounds.Min.Y
		for x := 0; x < width; x++ {
			sourceX := 0
			if sourceWidth > 0 {
				sourceX = x * sourceWidth / width
			}
			samples[y*width+x] = gray.GrayAt(bounds.Min.X+sourceX, sourceY).Y
		}
	}
	return samples
}

func pdfImageDataRaw(ctx *model.Context, stream *types.StreamDict, maskColor pdfColor) ([]byte, string, error) {
	// JBIG2Decode（T.88）由 gobig2 解码为灰度位图，再按 ImageMask 或 1 位图像
	// 输出。pdfcpu 不处理该过滤器，必须在此之前拦截。
	if hasJBIG2Filter(stream) {
		gray, err := pdfJBIG2Image(ctx, stream)
		if err != nil {
			return nil, "", err
		}
		return encodePDFJBIG2Image(ctx, stream, gray, maskColor)
	}
	// JPXDecode（JPEG 2000）由纯 Go 解码库处理。
	if hasJPXFilter(stream) {
		return encodePDFJPXImage(ctx, stream, maskColor)
	}
	// 图像颜色空间可能内嵌 ICC 配置文件；CMYK 图像优先用它转换到 RGB。
	cmyk := pdfImageCMYKConverter(ctx, stream)
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
		if encoded, format, ok, err := encodePDFCMYKJPEG(jpegData, cmyk); err != nil {
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
	if indexed {
		return encodePDFIndexedImage(stream.Content, width, height, bpc, components, palette, cmyk)
	}
	if bpc == maxPDFImageBitsPerComponent {
		return encodePDFImage16(ctx, stream, width, height, components, cmyk)
	}
	raw, err := decodePDFImageSamples(ctx, stream, width, height, components, bpc)
	if err != nil {
		return nil, "", err
	}
	if components == 4 {
		// 非 DCT 编码的 DeviceCMYK 样本已是油墨值（0 表示无油墨），无需反相。
		return encodePDFCMYKImage(&image.CMYK{Pix: raw, Stride: width * 4, Rect: image.Rect(0, 0, width, height)}, false, cmyk)
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

// maxPDFImageBitsPerComponent 是 PDF 图像支持的每分量最大位数。
const maxPDFImageBitsPerComponent = 16

// decodePDFImageSamples 把 PDF 图像样本解码为每分量 8 位的交织缓冲区。
// 支持 BitsPerComponent 1/2/4/8/16，并按 /Decode 数组（默认 [0 1]）映射取值。
func decodePDFImageSamples(ctx *model.Context, stream *types.StreamDict, width, height, components, bpc int) ([]byte, error) {
	if bpc < 1 || bpc > maxPDFImageBitsPerComponent {
		return nil, fmt.Errorf("不支持的图像位深 %d", bpc)
	}
	if components < 1 || components > 4 {
		return nil, errors.New("图像颜色分量数量无效")
	}
	rowBytes := (width*components*bpc + 7) / 8
	data := stream.Content
	if width <= 0 || height <= 0 || rowBytes <= 0 || len(data) < rowBytes*height {
		return nil, errors.New("PNG 图像数据长度无效")
	}
	decode := pdfImageDecode(ctx, stream, components)
	maxValue := float64(uint32(1)<<uint(bpc) - 1)
	samples := make([]byte, width*height*components)
	for y := 0; y < height; y++ {
		row := data[y*rowBytes : (y+1)*rowBytes]
		for x := 0; x < width; x++ {
			for c := 0; c < components; c++ {
				normalized := float64(readPDFImageSample(row, x*components+c, bpc)) / maxValue
				low, high := decode[c*2], decode[c*2+1]
				samples[(y*width+x)*components+c] = floatToByte(low + normalized*(high-low))
			}
		}
	}
	return samples, nil
}

// readPDFImageSample 从一行解包后的图像数据中读取第 index 个样本（高位在前）。
func readPDFImageSample(row []byte, index, bpc int) uint32 {
	switch bpc {
	case 16:
		offset := index * 2
		return uint32(row[offset])<<8 | uint32(row[offset+1])
	case 8:
		return uint32(row[index])
	default:
		value := uint32(0)
		position := index * bpc
		remaining := bpc
		for remaining > 0 {
			byteIndex := position / 8
			bitInByte := position % 8
			take := 8 - bitInByte
			if take > remaining {
				take = remaining
			}
			shift := uint(8 - bitInByte - take)
			mask := byte(1<<uint(take) - 1)
			value = value<<uint(take) | uint32((row[byteIndex]>>shift)&mask)
			position += take
			remaining -= take
		}
		return value
	}
}

// pdfImageDecode 读取 /Decode 数组；缺省为每个分量 [0 1]。
func pdfImageDecode(ctx *model.Context, stream *types.StreamDict, components int) []float64 {
	values := make([]float64, components*2)
	for i := 0; i < components; i++ {
		values[i*2+1] = 1
	}
	object, found := stream.Find("Decode")
	if !found {
		return values
	}
	resolved, err := dereferencePDFObject(ctx, object)
	if err != nil {
		return values
	}
	array, ok := resolved.(types.Array)
	if !ok || len(array) < components*2 {
		return values
	}
	for i := 0; i < components*2; i++ {
		if value, ok := numberValue(array[i]); ok {
			values[i] = value
		}
	}
	return values
}

func floatToByte(value float64) byte {
	if value <= 0 {
		return 0
	}
	if value >= 1 {
		return 255
	}
	return byte(value*255 + 0.5)
}

// encodePDFImage16 把 16 位样本直接编码为 16 位 PNG，保留位深。
// DeviceCMYK 没有 16 位 PNG 通道，转换为 16 位 RGB。
func encodePDFImage16(ctx *model.Context, stream *types.StreamDict, width, height, components int, converter cmykConverter) ([]byte, string, error) {
	if components < 1 || components > 4 {
		return nil, "", errors.New("图像颜色分量数量无效")
	}
	samples, err := decodePDFImageSamples16(ctx, stream, width, height, components)
	if err != nil {
		return nil, "", err
	}
	put := func(pixel []byte, value uint16) { binary.BigEndian.PutUint16(pixel, value) }
	var img image.Image
	switch components {
	case 1:
		gray := image.NewGray16(image.Rect(0, 0, width, height))
		for index, sample := range samples {
			put(gray.Pix[index*2:index*2+2], sample)
		}
		img = gray
	case 4:
		rgba := image.NewRGBA64(image.Rect(0, 0, width, height))
		for index := 0; index < width*height; index++ {
			r, g, b := converter.toRGB16(samples[index*4], samples[index*4+1], samples[index*4+2], samples[index*4+3])
			base := index * 8
			put(rgba.Pix[base:base+2], r)
			put(rgba.Pix[base+2:base+4], g)
			put(rgba.Pix[base+4:base+6], b)
			put(rgba.Pix[base+6:base+8], 0xffff)
		}
		img = rgba
	default:
		rgba := image.NewRGBA64(image.Rect(0, 0, width, height))
		for index := 0; index < width*height; index++ {
			base := index * 8
			put(rgba.Pix[base:base+2], samples[index*3])
			put(rgba.Pix[base+2:base+4], samples[index*3+1])
			put(rgba.Pix[base+4:base+6], samples[index*3+2])
			put(rgba.Pix[base+6:base+8], 0xffff)
		}
		img = rgba
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		return nil, "", fmt.Errorf("编码 PNG 图像失败: %w", err)
	}
	return encoded.Bytes(), "PNG", nil
}

func decodePDFImageSamples16(ctx *model.Context, stream *types.StreamDict, width, height, components int) ([]uint16, error) {
	rowBytes := width * components * 2
	data := stream.Content
	if width <= 0 || height <= 0 || rowBytes <= 0 || len(data) < rowBytes*height {
		return nil, errors.New("PNG 图像数据长度无效")
	}
	decode := pdfImageDecode(ctx, stream, components)
	samples := make([]uint16, width*height*components)
	for y := 0; y < height; y++ {
		row := data[y*rowBytes : (y+1)*rowBytes]
		for x := 0; x < width; x++ {
			for c := 0; c < components; c++ {
				offset := (x*components + c) * 2
				sample := uint16(row[offset])<<8 | uint16(row[offset+1])
				normalized := float64(sample) / 65535
				low, high := decode[c*2], decode[c*2+1]
				samples[(y*width+x)*components+c] = floatToUint16(low + normalized*(high-low))
			}
		}
	}
	return samples, nil
}

func floatToUint16(value float64) uint16 {
	if value <= 0 {
		return 0
	}
	if value >= 1 {
		return 0xffff
	}
	return uint16(value*65535 + 0.5)
}

func cmykToRGB16(c, m, y, k uint16) (uint16, uint16, uint16) {
	// 与 8 位 CMYK 使用同一套 Adobe/poppler 矩阵模型，只是分量精度为 16 位。
	r, g, b := coloricc.DeviceCMYKToRGB(float64(c)/65535, float64(m)/65535, float64(y)/65535, float64(k)/65535)
	return uint16(math.Round(65535 * r)), uint16(math.Round(65535 * g)), uint16(math.Round(65535 * b))
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

func encodePDFIndexedImage(data []byte, width, height, bpc, components int, palette []byte, converter cmykConverter) ([]byte, string, error) {
	if bpc <= 0 || bpc > 8 {
		return nil, "", errors.New("Indexed 图像位深无效")
	}
	// 基础颜色空间允许灰度、RGB 与 CMYK；CMYK 调色板按油墨值转为 RGB。
	if components != 1 && components != 3 && components != 4 {
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
			switch components {
			case 1:
				rgba.SetRGBA(x, y, color.RGBA{R: palette[paletteIndex], G: palette[paletteIndex], B: palette[paletteIndex], A: 255})
			case 4:
				r, g, b := converter.toRGB(palette[paletteIndex], palette[paletteIndex+1], palette[paletteIndex+2], palette[paletteIndex+3])
				rgba.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
			default:
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
// 对 Adobe CMYK JPEG（transform=0 的 CMYK 与 transform=2 的 YCbCrK/YCCK）会
// 做一次反相，而 PDF 中 DeviceCMYK 样本本身就是油墨值，因此需要再反相还原。
// 返回 ok=false 表示该 JPEG 不是 CMYK 图像，调用方应按原始字节处理。
func encodePDFCMYKJPEG(data []byte, converter cmykConverter) ([]byte, string, bool, error) {
	transform, adobe := jpegAdobeTransform(data)
	// transform 0 = CMYK，2 = YCbCrK（YCCK），两者都是 4 分量 CMYK JPEG。
	if !adobe || (transform != 0 && transform != 2) {
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
	encoded, format, err := encodePDFCMYKImage(cmyk, true, converter)
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

// cmykConverter 负责把 CMYK 图像样本转换为 RGB。优先使用 ICC 配置文件
// （图像内嵌的 ICCBased 配置文件，或环境变量 OFD_CMYK_ICC 指定的默认配置），
// 不可用时回退到近似油墨模型。渲染端 internal/render 采用同样的优先级。
type cmykConverter struct {
	icc *coloricc.Transformer
}

func (c cmykConverter) toRGB(cyan, magenta, yellow, black uint8) (uint8, uint8, uint8) {
	if c.icc != nil {
		return c.icc.ToRGB([]uint8{cyan, magenta, yellow, black})
	}
	return cmykInkToRGB(cyan, magenta, yellow, black)
}

func (c cmykConverter) toRGB16(cyan, magenta, yellow, black uint16) (uint16, uint16, uint16) {
	if c.icc != nil {
		to8 := func(value uint16) uint8 { return uint8(value >> 8) }
		r, g, b := c.icc.ToRGB([]uint8{to8(cyan), to8(magenta), to8(yellow), to8(black)})
		to16 := func(value uint8) uint16 { return uint16(value)<<8 | uint16(value) }
		return to16(r), to16(g), to16(b)
	}
	return cmykToRGB16(cyan, magenta, yellow, black)
}

// pdfImageCMYKConverter 根据图像的颜色空间选择 CMYK 转换器：ICCBased 图像使用
// 内嵌配置文件，其余 CMYK 图像使用 OFD_CMYK_ICC 默认配置（未设置则为近似模型）。
func pdfImageCMYKConverter(ctx *model.Context, stream *types.StreamDict) cmykConverter {
	if transformer := pdfImageICCProfile(ctx, stream); transformer != nil {
		return cmykConverter{icc: transformer}
	}
	transformer, _ := coloricc.DefaultCMYK()
	return cmykConverter{icc: transformer}
}

// pdfImageICCProfile 读取 ICCBased 图像颜色空间内嵌的 4 分量 ICC 配置文件。
// 返回 nil 表示该图像没有可用的内嵌 CMYK 配置文件。
func pdfImageICCProfile(ctx *model.Context, stream *types.StreamDict) *coloricc.Transformer {
	object, found := stream.Find("ColorSpace")
	if !found {
		return nil
	}
	resolved, err := dereferencePDFObject(ctx, object)
	if err != nil {
		return nil
	}
	array, ok := resolved.(types.Array)
	if !ok || len(array) < 2 {
		return nil
	}
	name, ok := array[0].(types.Name)
	if !ok || name.Value() != "ICCBased" {
		return nil
	}
	profileObject, err := dereferencePDFObject(ctx, array[1])
	if err != nil {
		return nil
	}
	var profile *types.StreamDict
	switch value := profileObject.(type) {
	case types.StreamDict:
		profile = &value
	case *types.StreamDict:
		profile = value
	default:
		return nil
	}
	if profile == nil {
		return nil
	}
	if n, ok := dereferencedPDFNumber(ctx, profile.Dict["N"]); !ok || int(n) != 4 {
		return nil
	}
	transformer, err := coloricc.New(pdfStreamContent(profile), 4)
	if err != nil {
		return nil
	}
	return transformer
}

// pdfStreamContent 返回流解码后的字节：优先已解码的 Content，其次按过滤器
// 解码 Raw，最后在没有过滤器时直接使用 Raw。
func pdfStreamContent(stream *types.StreamDict) []byte {
	if stream == nil {
		return nil
	}
	if len(stream.Content) > 0 {
		return stream.Content
	}
	if len(stream.FilterPipeline) == 0 {
		return stream.Raw
	}
	if stream.Decode() == nil {
		return stream.Content
	}
	return nil
}

// cmykInkToRGB 在没有可用 ICC 配置文件时，用 Adobe/poppler 的 4 色印刷矩阵模型
// 把 CMYK 油墨量合成为 RGB。纯色结果与印刷色一致：纯青 (0,173,239)、
// 纯品红 (236,0,140)、纯黄 (255,242,0)、纯黑 (35,31,32)。
//
// 这里不加载 ICC profile，属于近似转换；渲染端 internal/render 使用同一套模型，
// 保证同一 CMYK 颜色在转换与渲染两条路径上一致。
func cmykInkToRGB(c, m, y, k uint8) (uint8, uint8, uint8) {
	r, g, b := coloricc.DeviceCMYKToRGB(float64(c)/255, float64(m)/255, float64(y)/255, float64(k)/255)
	return uint8(math.Round(255 * r)), uint8(math.Round(255 * g)), uint8(math.Round(255 * b))
}

// encodePDFCMYKImage 将 CMYK 图像转换为 RGB PNG；invert 为 true 时先对四个
// 分量取反，用于还原 Go 解码 Adobe CMYK JPEG 时引入的反相。
func encodePDFCMYKImage(img *image.CMYK, invert bool, converter cmykConverter) ([]byte, string, error) {
	bounds := img.Bounds()
	rgba := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			index := img.PixOffset(x, y)
			c, m, yellow, k := img.Pix[index], img.Pix[index+1], img.Pix[index+2], img.Pix[index+3]
			if invert {
				c, m, yellow, k = 255-c, 255-m, 255-yellow, 255-k
			}
			r, g, b := converter.toRGB(c, m, yellow, k)
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
