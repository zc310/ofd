package pdf2ofd

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"math"
	"strings"
	"sync"

	gobig2 "github.com/dkrisman/gobig2"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/coloricc"
	"github.com/zc310/ofd/internal/media"
	"github.com/zc310/ofd/internal/pdf2ofd/config"
	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/image/draw"
)

const cmykRGBAPoolMaxBytes = 32 << 20

var cmykRGBABufferPool sync.Pool

func acquireCMYKRGBA(bounds image.Rectangle) *image.RGBA {
	size := 4 * bounds.Dx() * bounds.Dy()
	if size > 0 && size <= cmykRGBAPoolMaxBytes {
		if value := cmykRGBABufferPool.Get(); value != nil {
			buffer := value.([]byte)
			if cap(buffer) >= size {
				return &image.RGBA{Pix: buffer[:size], Stride: 4 * bounds.Dx(), Rect: bounds}
			}
		}
	}
	return image.NewRGBA(bounds)
}

func releaseCMYKRGBA(img *image.RGBA) {
	if img == nil || len(img.Pix) > cmykRGBAPoolMaxBytes {
		return
	}
	cmykRGBABufferPool.Put(img.Pix[:0])
}

func (p *pdfInterpreter) xobject(name string, resources types.Dict, depth int) error {
	previousSource := p.setSource(nil, name)
	defer func() { p.source = previousSource }()
	dict, ok := dereferencedSubDict(p.ctx, resources, "XObject")
	if !ok {
		p.report("resource.missing", "找不到 XObject 资源字典")
		return nil
	}
	object, found := dict.Find(name)
	if !found {
		p.report("resource.missing", "找不到 XObject 资源 "+name)
		return nil
	}
	return p.paintXObject(object, name, resources, depth)
}

// 按对象引用执行 XObject，资源名称只用于诊断；软掩码可直接执行 G，
// 无需在继承资源中插入临时名称而覆盖 Form 内部的同名资源。
func (p *pdfInterpreter) paintXObject(object types.Object, name string, resources types.Dict, depth int) error {
	previousSource := p.setSource(object, name)
	defer func() { p.source = previousSource }()
	stream, _, err := p.ctx.XRefTable.DereferenceStreamDict(object)
	if err != nil || stream == nil {
		p.report("resource.invalid", fmt.Sprintf("无法读取 XObject: %v", err))
		return err
	}
	subtype := stream.NameEntry("Subtype")
	if subtype == nil {
		return nil
	}
	if *subtype == "Image" {
		if p.type3Uncolored && !pdfImageIsMask(p.ctx, stream) {
			p.report("font.type3_color", "d1 未着色字形只能绘制 ImageMask 图像")
			return p.control.check()
		}
		// 单个图像解码失败（例如 JPXDecode 等不支持的过滤器）不应中断整页
		// 转换，跳过该图像继续处理其余内容。
		if reference, ok := object.(types.IndirectRef); ok && !pdfImageIsMask(p.ctx, stream) {
			if p.imageCache == nil {
				p.imageCache = make(map[types.IndirectRef]pdfImageDataCache)
			}
			// 图像自身的 Mask/SMask 与图形状态无关，合成结果可缓存；ExtGState
			// 软掩码则必须在取出缓存后应用，同一图像可能在两种图形状态下绘制。
			cached, hit := p.imageCache[reference]
			if !hit {
				data, format, imageErr := p.imageData(stream, p.state.fill)
				if imageErr != nil {
					p.report("image.decode", imageErr.Error())
					return nil
				}
				cached = pdfImageDataCache{data: data, format: format}
				p.imageCache[reference] = cached
			}
			_ = p.appendImageWithGraphicsMask(stream, cached.data, cached.format)
			return nil
		}
		if err := p.appendImage(stream); err != nil {
			p.report("image.decode", err.Error())
		}
		return nil
	}
	if *subtype == "Form" {
		if err := p.decodeStream(stream, 0); err != nil {
			p.report("form.decode", err.Error())
			return nil
		}
		formState := p.state
		formResources := resources
		if object, found := stream.Find("Resources"); found {
			if value, err := p.ctx.XRefTable.DereferenceDict(object); err == nil {
				formResources = value
			}
		}
		matrix := identityPDFMatrix()
		if array := stream.ArrayEntry("Matrix"); len(array) == 6 {
			for i := range matrix {
				matrix[i] = anyFloat(array[i])
			}
		}
		formState.ctm = multiplyPDFMatrix(formState.ctm, matrix)
		group, groupErr := p.beginTransparencyGroup(stream, formResources, formState)
		if groupErr != nil {
			return groupErr
		}
		parentGroup := p.transparency
		groupDict, _ := p.ctx.XRefTable.DereferenceDict(stream.Dict["Group"])
		if groupDict != nil && groupDict.NameEntry("S") != nil && *groupDict.NameEntry("S") == "Transparency" {
			if group != nil {
				p.transparency = group
			}
			formState.groupAlpha, formState.fillAlpha, formState.strokeAlpha = 1, 1, 1
			formState.groupAlphaSet = true
			if group == nil {
				formState.groupAlpha = p.fillOpacity()
			}
			formState.blendMode, formState.lumMask, formState.softMask = "Normal", nil, nil
			formState.constantMaskBlack = false
		}
		// Form XObject 的 BBox 是隐式裁剪区；只应用 Matrix 而不应用 BBox 会让
		// 表单内容溢出其声明边界（常见于图章、剪切模板与重复图块）。
		if bbox := pdfNumberArray(p.ctx, stream.Dict["BBox"]); len(bbox) >= 4 {
			corners := [][2]float64{{bbox[0], bbox[1]}, {bbox[2], bbox[1]}, {bbox[2], bbox[3]}, {bbox[0], bbox[3]}}
			commands := make([]pdfPathCommand, 0, len(corners)+1)
			for index, corner := range corners {
				x, y := transformPDFPoint(corner[0], corner[1], formState.ctm)
				op := "L"
				if index == 0 {
					op = "M"
				}
				commands = append(commands, pdfPathCommand{op: op, values: []float64{x, y}})
			}
			commands = append(commands, pdfPathCommand{op: "Z"})
			formState.clips = append(append([]pdfClipRegion(nil), formState.clips...), pdfClipRegion{commands: commands})
		}
		parentState := p.state
		parentStack := p.stack
		parentTextClip, parentTextClipActive := p.textClip, p.textClipActive
		parentPath, parentPendingClip := p.path, p.pendingClip
		parentHasPendingClip, parentPendingEvenOdd := p.hasPendingClip, p.pendingClipEvenOdd
		parentFonts, parentAliases := p.fonts, p.fontAliases
		p.textClip, p.textClipActive = pdfClipRegion{}, false
		p.path, p.pendingClip, p.hasPendingClip = nil, nil, false
		p.fonts, p.fontAliases = map[string]pdfFontInfo{}, map[string]string{}
		p.stack = nil
		err := p.parse(stream.Content, formResources, &formState, depth+1)
		p.state = parentState
		p.stack = parentStack
		p.textClip, p.textClipActive = parentTextClip, parentTextClipActive
		p.path, p.pendingClip = parentPath, parentPendingClip
		p.hasPendingClip, p.pendingClipEvenOdd = parentHasPendingClip, parentPendingEvenOdd
		p.fonts, p.fontAliases = parentFonts, parentAliases
		p.transparency = parentGroup
		if err == nil && group != nil {
			err = p.finishTransparencyGroup(group)
		}
		return err
	}
	p.report("xobject.unsupported", "不支持的 XObject 类型 "+*subtype)
	return nil
}

// imageObjectCTM 从已经换算到页面毫米坐标的四角恢复完整仿射变换。
// points 按 PDF 的左下、右下、左上、右上排列；OFD 图像空间则以左上为原点、
// y 向下。以包围盒为局部原点，保留旋转、错切、镜像和页面旋转后的轴方向。
func imageObjectCTM(points [4][2]float64, minX, minY float64) *creator.CTM {
	topLeft, topRight, bottomLeft := points[2], points[3], points[0]
	matrix := creator.CTM{
		topRight[0] - topLeft[0], topRight[1] - topLeft[1],
		bottomLeft[0] - topLeft[0], bottomLeft[1] - topLeft[1],
		topLeft[0] - minX, topLeft[1] - minY,
	}
	return &matrix
}

// appendImage 把解码后的图像按当前 CTM 的包围盒放入页面。内联图像与图像
// XObject 共用该逻辑。
func (p *pdfInterpreter) appendImage(stream *types.StreamDict) error {
	if p.type3Uncolored && !pdfImageIsMask(p.ctx, stream) {
		p.report("font.type3_color", "d1 未着色字形只能绘制 ImageMask 图像")
		return p.control.check()
	}
	data, format, err := p.imageData(stream, p.state.fill)
	if err != nil {
		return err
	}
	return p.appendImageWithGraphicsMask(stream, data, format)
}

// 内联、非缓存和缓存图像共用绘制时的软掩码选择。只在图像没有关联软掩码
// 时应用图形状态掩码，缓存内容本身不受当前状态影响；整体 ca 在图元上保留。
func (p *pdfInterpreter) appendImageWithGraphicsMask(stream *types.StreamDict, data []byte, format string) error {
	// 图片关联的软掩码只在本次绘制覆盖图形状态软掩码；q/Q 与后续矢量图元
	// 仍保留原有状态，避免污染同一图片的缓存或后续绘制。
	if p.state.softMask != nil && p.imageHasOwnSoftMask(stream) {
		mask := p.state.softMask
		p.state.softMask = nil
		defer func() { p.state.softMask = mask }()
	}
	if mask := p.state.lumMask; mask != nil && !p.imageHasOwnSoftMask(stream) {
		if masked, ok := p.applyImageSoftMask(data, mask); ok {
			data, format = masked, "PNG"
		}
	}
	if pdfImageInterpolate(p.ctx, stream) {
		var err error
		data, format, err = interpolateTinyPDFImage(data, format, p.control)
		if err != nil {
			return err
		}
	}
	return p.appendImageData(data, format)
}

func pdfImageInterpolate(ctx *model.Context, stream *types.StreamDict) bool {
	value, found := stream.Find("Interpolate")
	if !found {
		return false
	}
	value, err := dereferencePDFObject(ctx, value)
	if err != nil {
		return false
	}
	flag, ok := value.(types.Boolean)
	return ok && flag.Value()
}

// OFD 没有图像插值标志；对 PDF 显式要求插值的极小图像预放大，避免输出阅读器
// 将其按最近邻放大，保留源 PDF 要求的平滑采样效果。
func interpolateTinyPDFImage(data []byte, format string, control *conversionControl) ([]byte, string, error) {
	configuration, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("读取待插值的 PDF 图像尺寸失败: %w", err)
	}
	if configuration.Width <= 0 || configuration.Height <= 0 || int64(configuration.Width)*int64(configuration.Height) > 64 {
		return data, format, nil
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("解码待插值的 PDF 图像失败: %w", err)
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 || width != configuration.Width || height != configuration.Height {
		return data, format, nil
	}
	factor := int(math.Sqrt(65 / float64(width*height)))
	for int64(width)*int64(height)*int64(factor)*int64(factor) <= 64 {
		factor++
	}
	width, height = width*factor, height*factor
	pixels := int64(width) * int64(height)
	if control != nil {
		checked, err := control.imagePixels(int64(width), int64(height))
		if err != nil {
			return nil, "", err
		}
		if err := control.addImagePixels(checked - int64(bounds.Dx())*int64(bounds.Dy())); err != nil {
			return nil, "", err
		}
		if err := control.consumeOperations(pixels); err != nil {
			return nil, "", err
		}
	}
	output := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.ApproxBiLinear.Scale(output, output.Bounds(), source, bounds, draw.Src, nil)
	encoded, format, err := encodePNGImage(output)
	if err != nil {
		return nil, "", err
	}
	return encoded, format, nil
}

// appendImageData 把已经完成解码的图像数据按当前 CTM 放入页面。
func (p *pdfInterpreter) appendImageData(data []byte, format string) error {
	var points [4][2]float64
	points[0][0], points[0][1] = transformPDFPoint(0, 0, p.state.ctm)
	points[1][0], points[1][1] = transformPDFPoint(1, 0, p.state.ctm)
	points[2][0], points[2][1] = transformPDFPoint(0, 1, p.state.ctm)
	points[3][0], points[3][1] = transformPDFPoint(1, 1, p.state.ctm)
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for i, point := range points {
		x, y := p.pagePoint(point[0], point[1])
		points[i] = [2]float64{x, y}
		minX, minY, maxX, maxY = math.Min(minX, x), math.Min(minY, y), math.Max(maxX, x), math.Max(maxY, y)
	}
	width, height := math.Max(maxX-minX, 0.001), math.Max(maxY-minY, 0.001)
	image := creator.Image{X: minX, Y: minY, Width: width, Height: height, Data: data, Format: format, Alpha: ofdAlpha(p.fillOpacity())}
	// 一律显式输出 CTM，兼容未实现图片缺省矩阵的阅读器。
	image.CTM = imageObjectCTM(points, minX, minY)
	if clips := p.buildClips(minX, minY); clips != nil {
		image.Clips = clips
	}
	p.appendItem(image)
	return nil
}

// pdfImageIsMask 判断图像是否是依赖当前填充色的 ImageMask。
func pdfImageIsMask(ctx *model.Context, stream *types.StreamDict) bool {
	if stream == nil {
		return false
	}
	value, found := stream.Find("ImageMask")
	if !found {
		return false
	}
	value, err := dereferencePDFObject(ctx, value)
	if err != nil {
		return false
	}
	boolean, ok := value.(types.Boolean)
	return ok && boolean.Value()
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

func (p *pdfInterpreter) inlineImage(tokens *pdfContentTokenizer, resources types.Dict) error {
	previousSource := p.setSource(nil, "inline image")
	defer func() { p.source = previousSource }()
	var stream *types.StreamDict
	_, data, err := tokens.inlineImage(func(dict map[string]any) (int64, error) {
		stream = inlineImageStream(dict)
		// 内联图像可引用当前资源中的颜色空间；先解析再计算原始行长度。
		if name, ok := stream.Dict["ColorSpace"].(types.Name); ok {
			if spaces, found := dereferencedSubDict(p.ctx, resources, "ColorSpace"); found {
				if object, found := spaces.Find(name.Value()); found {
					stream.Dict["ColorSpace"] = object
				}
			}
		}
		return p.inlineImageRawLength(stream)
	})
	if err != nil {
		return err
	}
	stream.Raw = data
	// 与图像 XObject 一致：解码失败只跳过该图像。
	if err := p.appendImage(stream); err != nil {
		p.report("image.decode", err.Error())
	}
	return nil
}

func inlineImageStream(dict map[string]any) *types.StreamDict {
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
	if pipeline := inlineImageFilterPipeline(stream); len(pipeline) > 0 {
		stream.FilterPipeline = pipeline
	}
	return stream
}

// inlineImageRawLength 按 ISO 32000-1 8.9.7 计算无过滤器图像的数据长度。
// 每行补齐到字节边界；像素中的空白或 EI 字节序列不能作为终止标记。
// -1 表示过滤器编码或未知颜色空间，仍需扫描结束标记。
func (p *pdfInterpreter) inlineImageRawLength(stream *types.StreamDict) (int64, error) {
	if len(stream.FilterPipeline) > 0 {
		return -1, nil
	}
	w, wok := dereferencedPDFNumber(p.ctx, stream.Dict["Width"])
	h, hok := dereferencedPDFNumber(p.ctx, stream.Dict["Height"])
	b, bok := dereferencedPDFNumber(p.ctx, stream.Dict["BitsPerComponent"])
	components := 1
	if pdfImageIsMask(p.ctx, stream) {
		b, bok = 1, true
	} else {
		var err error
		components, _, _, err = pdfImageColorSpace(p.ctx, stream)
		if err != nil {
			return -1, nil
		}
	}
	if !wok || !hok || !bok || w <= 0 || h <= 0 || w != math.Trunc(w) || h != math.Trunc(h) ||
		math.IsInf(w, 0) || math.IsInf(h, 0) || math.IsNaN(w) || math.IsNaN(h) || w >= math.MaxInt64 || h >= math.MaxInt64 ||
		(b != 1 && b != 2 && b != 4 && b != 8 && b != 16) || components <= 0 {
		return 0, errors.New("PDF 内联图像尺寸或位深无效")
	}
	width, height, bits := int64(w), int64(h), int64(b)*int64(components)
	if p.control != nil {
		if _, err := p.control.imagePixels(width, height); err != nil {
			return 0, err
		}
	}
	if width > (math.MaxInt64-7)/bits {
		return 0, errors.New("PDF 内联图像行长度溢出")
	}
	row := (width*bits + 7) / 8
	if height > math.MaxInt64/row {
		return 0, errors.New("PDF 内联图像数据长度溢出")
	}
	return row * height, nil
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

// pdfImageData 编码图像为 PNG/JPEG；若图像带有 Mask 或 SMask，则把掩码
// 作为 alpha 通道应用到图像上，避免透明区域被绘制成不透明色块。
func pdfImageData(ctx *model.Context, stream *types.StreamDict, maskColor pdfColor) ([]byte, string, error) {
	p := pdfInterpreter{ctx: ctx}
	return p.imageData(stream, maskColor)
}

// pdfExtGStateSoftMask 解析 ExtGState 的 /SMask，返回可用的掩码图像流。
//
// 只处理 /S /Luminosity 且掩码由图像生成的情况：Illustrator CS 导出的高光与
// 柔边图层都是这个形态（三层嵌套：Form XObject → gs /SMask → 亮度掩码 → 又一个
// 绘制灰度图像的 Form）。OFD 没有软掩码语义，返回的图像会被当作 alpha 烘进被
// 遮蔽的图像里，因此无法还原成图像的掩码一律返回 nil，保持原有行为。
func pdfExtGStateSoftMask(ctx *model.Context, dict types.Dict, controls ...*conversionControl) *types.StreamDict {
	if dict == nil {
		return nil
	}
	object, found := dict.Find("SMask")
	if !found || object == nil {
		return nil
	}
	resolved, err := ctx.XRefTable.Dereference(object)
	if err != nil || resolved == nil {
		return nil
	}
	if _, isName := resolved.(types.Name); isName {
		// /SMask /None：显式清除软掩码。
		return nil
	}
	// 亮度掩码是普通字典（/Type /Mask），不是流字典。
	mask, err := ctx.XRefTable.DereferenceDict(resolved)
	if err != nil || mask == nil {
		return nil
	}
	if subtype, found := mask.Find("S"); found {
		if name, isName := subtype.(types.Name); isName && name.Value() != "Luminosity" {
			return nil
		}
	}
	group, found := mask.Find("G")
	if !found {
		return nil
	}
	return pdfLuminosityMaskImage(ctx, group, controls...)
}

// pdfLuminosityMaskImage 从亮度掩码的 /G 取出实际承载灰度信息的图像流。
// /G 可以直接是图像，也可以是仅调用一次 Do 绘制图像的 Form XObject。
func pdfLuminosityMaskImage(ctx *model.Context, object types.Object, controls ...*conversionControl) *types.StreamDict {
	stream, _, err := ctx.XRefTable.DereferenceStreamDict(object)
	if err != nil || stream == nil {
		return nil
	}
	subtype := stream.NameEntry("Subtype")
	if subtype == nil {
		return nil
	}
	if *subtype == "Image" {
		return stream
	}
	if *subtype != "Form" || stream.Decode() != nil {
		return nil
	}
	resources, ok := pdfStreamResources(ctx, stream)
	if !ok {
		return nil
	}
	xobjects, ok := dereferencedSubDict(ctx, resources, "XObject")
	if !ok {
		return nil
	}
	return pdfFormDrawnImage(ctx, xobjects, stream.Content, controls...)
}

// pdfFormDrawnImage 扫描 Form 内容，返回其中第一个 Do 操作绘制的图像 XObject。
// 亮度掩码的 Form 只做一次矩阵变换加一次 Do，直接扫描比完整解释内容流更稳。
func pdfFormDrawnImage(ctx *model.Context, xobjects types.Dict, content []byte, controls ...*conversionControl) *types.StreamDict {
	tokens := newPDFContentTokenizer(content)
	if len(controls) > 0 {
		tokens.control = controls[0]
	}
	operands := []any{}
	for {
		value, ok, err := tokens.next()
		if err != nil || !ok {
			return nil
		}
		operator, isOperator := value.(string)
		if !isOperator || strings.HasPrefix(operator, "/") {
			operands = append(operands, value)
			continue
		}
		if operator == "Do" && len(operands) > 0 {
			// 词法器把 /Name 产出为 pdfName，不是 types.Name。
			if name := anyName(operands[len(operands)-1]); name != "" {
				if image := pdfXObjectImage(ctx, xobjects, name); image != nil {
					return image
				}
			}
		}
		operands = operands[:0]
	}
}

// pdfXObjectImage 按名字从 XObject 资源字典取出图像流；不是图像则返回 nil。
func pdfXObjectImage(ctx *model.Context, xobjects types.Dict, name string) *types.StreamDict {
	object, found := xobjects.Find(name)
	if !found {
		return nil
	}
	stream, _, err := ctx.XRefTable.DereferenceStreamDict(object)
	if err != nil || stream == nil {
		return nil
	}
	if subtype := stream.NameEntry("Subtype"); subtype == nil || *subtype != "Image" {
		return nil
	}
	return stream
}

// pdfStreamResources 取出流的 /Resources 字典，缺省时返回 nil。
func pdfStreamResources(ctx *model.Context, stream *types.StreamDict) (types.Dict, bool) {
	object, found := stream.Find("Resources")
	if !found {
		return nil, false
	}
	value, err := ctx.XRefTable.DereferenceDict(object)
	return value, err == nil && value != nil
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
func applyPDFImageSoftMask(ctx *model.Context, base []byte, maskStream *types.StreamDict, controls ...*conversionControl) ([]byte, bool) {
	var control *conversionControl
	if len(controls) > 0 {
		control = controls[0]
	}
	if control.check() != nil {
		return nil, false
	}
	baseImage, _, err := image.Decode(bytes.NewReader(base))
	if err != nil {
		return nil, false
	}
	if hasJPXFilter(maskStream) {
		p := pdfInterpreter{ctx: ctx, control: control}
		return p.applyJPXSoftMask(baseImage, maskStream)
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
		if maskBPC > 8 && maskBPC != 16 {
			return nil, false
		}
		if len(maskStream.Content) == 0 && maskStream.Decode() != nil {
			return nil, false
		}
	}
	// 任一输入使用 16 位通道时，颜色与 alpha 都写入非预乘的 16 位 PNG。
	// 不能先把掩码压到 8 位，否则相邻 alpha 样本会合并、渐变出现色阶。
	highPrecision := maskBPC == 16
	switch baseImage.(type) {
	case *image.Gray16, *image.RGBA64, *image.NRGBA64:
		highPrecision = true
	}
	if highPrecision {
		return applyPDFImageSoftMask16(ctx, baseImage, maskStream, maskWidth, maskHeight, maskBPC, control)
	}
	if control.consumeOperations(int64(maskWidth)*int64(maskHeight)) != nil {
		return nil, false
	}
	samples, err := pdfImageMaskSamples(ctx, maskStream, maskWidth, maskHeight, maskBPC, control)
	if err != nil {
		return nil, false
	}
	bounds := baseImage.Bounds()
	width, height, err := pdfSoftMaskCompositionSize(bounds, maskWidth, maskHeight, control)
	if err != nil {
		return nil, false
	}
	if control.consumeOperations(int64(width)*int64(height)) != nil {
		return nil, false
	}
	// 基础图像本身不透明，这里写入的是非预乘 RGB。必须使用 NRGBA：若写入
	// RGBA（预乘），PNG 编码会对低 alpha 像素做反预乘，把接近透明的颜色放大成
	// 红/品红色边缘。
	rgba := image.NewNRGBA(image.Rect(0, 0, width, height))
	// 逐像素调用 image.At 会为每个像素在堆上装箱一个 color.Color，是软掩码
	// 合成的最大分配来源。这里改用不装箱的取值路径。
	for y := 0; y < height; y++ {
		if control.check() != nil {
			return nil, false
		}
		maskY := pdfSoftMaskRow(y, maskHeight, height)
		maskBase := maskY * maskWidth
		row := rgba.PixOffset(0, y)
		sy := bounds.Min.Y + pdfSoftMaskRow(y, bounds.Dy(), height)
		for x := 0; x < width; x++ {
			maskX := pdfSoftMaskColumn(x, maskWidth, width)
			sx := bounds.Min.X + pdfSoftMaskColumn(x, bounds.Dx(), width)
			var r, g, b uint8
			switch source := baseImage.(type) {
			case *image.YCbCr:
				value := source.YCbCrAt(sx, sy)
				r, g, b = color.YCbCrToRGB(value.Y, value.Cb, value.Cr)
			case *image.Gray:
				value := source.GrayAt(sx, sy).Y
				r, g, b = value, value, value
			case *image.NRGBA:
				pix := source.PixOffset(sx, sy)
				r, g, b = source.Pix[pix], source.Pix[pix+1], source.Pix[pix+2]
			case *image.RGBA:
				// 预乘颜色必须先反预乘，否则半透明像素会被放大成亮边。
				value := source.RGBAAt(sx, sy)
				r, g, b = value.R, value.G, value.B
				if alpha := value.A; alpha != 0 && alpha != 0xff {
					r, g, b = unpremultiplyPDF(r, alpha), unpremultiplyPDF(g, alpha), unpremultiplyPDF(b, alpha)
				}
			default:
				converted := color.NRGBAModel.Convert(baseImage.At(sx, sy)).(color.NRGBA)
				r, g, b = converted.R, converted.G, converted.B
			}
			target := row + x*4
			rgba.Pix[target], rgba.Pix[target+1], rgba.Pix[target+2], rgba.Pix[target+3] = r, g, b, samples[maskBase+maskX]
		}
	}
	var encoded bytes.Buffer
	if err := media.EncodePNG(&encoded, rgba); err != nil {
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
func pdfJBIG2Image(ctx *model.Context, stream *types.StreamDict, controls ...*conversionControl) (*image.Gray, error) {
	var control *conversionControl
	if len(controls) > 0 {
		control = controls[0]
	}
	if err := control.check(); err != nil {
		return nil, err
	}
	data := jbig2StreamBytes(stream)
	if len(data) == 0 {
		return nil, errors.New("JBIG2 图像数据为空")
	}
	decoder, err := gobig2.NewDecoderEmbedded(bytes.NewReader(data), pdfJBIG2Globals(ctx, stream))
	if err != nil {
		return nil, fmt.Errorf("JBIG2 解码失败: %w", err)
	}
	var decoded image.Image
	if control != nil {
		decoded, err = decoder.DecodeContext(control.ctx)
	} else {
		decoded, err = decoder.Decode()
	}
	// gobig2 包装取消错误；转换入口必须原样返回调用方的 context 错误。
	if cancelled := control.check(); cancelled != nil {
		return nil, cancelled
	}
	if err != nil {
		return nil, fmt.Errorf("JBIG2 解码失败: %w", err)
	}
	gray, ok := decoded.(*image.Gray)
	if !ok || gray == nil || gray.Bounds().Empty() {
		return nil, errors.New("JBIG2 解码结果无效")
	}
	return gray, nil
}

// pdfJBIG2Standalone 把不依赖全局段的 PDF JBIG2 图像段流封装为独立 JBIG2 文件。
func pdfJBIG2Standalone(ctx *model.Context, stream *types.StreamDict) ([]byte, bool) {
	if stream == nil || len(pdfJBIG2Globals(ctx, stream)) != 0 {
		return nil, false
	}
	colorSpace, found := stream.Find("ColorSpace")
	if !found {
		return nil, false
	}
	if ctx != nil {
		resolved, err := dereferencePDFObject(ctx, colorSpace)
		if err != nil {
			return nil, false
		}
		colorSpace = resolved
	}
	name, ok := colorSpace.(types.Name)
	if !ok || name.Value() != "DeviceGray" {
		return nil, false
	}
	bpc, ok := integerValue(stream.Dict["BitsPerComponent"])
	if !ok || bpc != 1 {
		return nil, false
	}
	data := jbig2StreamBytes(stream)
	if len(data) == 0 {
		return nil, false
	}
	if pdfImageIsMask(ctx, stream) {
		return nil, false
	}
	if _, found := stream.Find("SMask"); found {
		return nil, false
	}
	if _, found := stream.Find("Mask"); found {
		return nil, false
	}
	if decode, found := stream.Find("Decode"); found {
		if values, ok := decode.(types.Array); ok && len(values) >= 2 {
			decodeMin, minOK := dereferencedPDFNumber(ctx, values[0])
			decodeMax, maxOK := dereferencedPDFNumber(ctx, values[1])
			if !minOK || !maxOK || decodeMin != 0 || decodeMax != 1 {
				return nil, false
			}
		} else {
			return nil, false
		}
	}
	// PDF embeds a headerless segment stream and omits EOP/EOF; standalone JBIG2
	// needs a file header, page count, and terminal page/document segments.
	result := make([]byte, 0, len(data)+27)
	result = append(result, 0x97, 'J', 'B', '2', '\r', '\n', 0x1a, '\n', 0x01, 0, 0, 0, 1)
	result = append(result, data...)
	result = append(result,
		0, 0, 0, 0, 49, 0, 1, 0, 0, 0, 0,
		0, 0, 0, 0, 51, 0, 0, 0, 0, 0,
	)
	return result, true
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
	imageMask = pdfImageIsMask(ctx, stream)
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
		if err := media.EncodePNG(&encoded, rgba); err != nil {
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
	if err := media.EncodePNG(&encoded, out); err != nil {
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

// encodePDFJPXImage 将上下文及预算传入统一的原分量解码路径。
func encodePDFJPXImage(ctx *model.Context, stream *types.StreamDict, maskColor pdfColor, controls ...*conversionControl) ([]byte, string, error) {
	var control *conversionControl
	if len(controls) > 0 {
		control = controls[0]
	}
	p := pdfInterpreter{ctx: ctx, control: control}
	return p.encodeJPXImage(stream, maskColor)
}

// encodePNGImage 把图像编码为 PNG。
func encodePNGImage(img image.Image) ([]byte, string, error) {
	var encoded bytes.Buffer
	if err := media.EncodePNG(&encoded, img); err != nil {
		return nil, "", fmt.Errorf("编码 PNG 图像失败: %w", err)
	}
	return encoded.Bytes(), "PNG", nil
}

// pdfImageMaskSamples 解码软掩码样本：JBIG2 掩码用 gobig2，其余走通用样本解码。
func pdfImageMaskSamples(ctx *model.Context, stream *types.StreamDict, width, height, bpc int, controls ...*conversionControl) ([]byte, error) {
	if hasJBIG2Filter(stream) {
		gray, err := pdfJBIG2Image(ctx, stream, controls...)
		if err != nil {
			return nil, err
		}
		return resampleGrayMask(gray, width, height), nil
	}
	if hasJPXFilter(stream) {
		var control *conversionControl
		if len(controls) > 0 {
			control = controls[0]
		}
		p := pdfInterpreter{ctx: ctx, control: control}
		source := *stream
		if pdfImageIsMask(ctx, stream) && stream.Dict["ColorSpace"] == nil {
			source.Dict = stream.Dict.Clone().(types.Dict)
			source.Dict["ColorSpace"] = types.Name("DeviceGray")
		}
		info, err := p.jpxHeader(&source, false)
		if err != nil {
			return nil, err
		}
		if info.family != "DeviceGray" || (pdfImageIsMask(ctx, stream) && info.metadata.BitsPerComponent[0] != 1) {
			return nil, fmt.Errorf("JPX 掩码需要灰度分量，ImageMask 必须为 1 位")
		}
		m := info.metadata
		decoded, err := p.jpxImagePlanes(&source, info, m.Width, m.Height, false)
		if err != nil {
			return nil, err
		}
		maximum := float64(uint32(1)<<uint(m.BitsPerComponent[0]) - 1)
		samples := make([]byte, width*height)
		for y := 0; y < height; y++ {
			if err := control.check(); err != nil {
				return nil, err
			}
			row := pdfSoftMaskRow(y, m.Height, height) * m.Width
			for x := 0; x < width; x++ {
				samples[y*width+x] = floatToByte(float64(decoded.Components[0][row+pdfSoftMaskColumn(x, m.Width, width)]) / maximum)
			}
		}
		return samples, nil
	}
	// DCTDecode（JPEG）掩码：按图像解码成灰度样本。此前只处理 JBIG2/JPX 与未压缩
	// 样本，JPEG 掩码只能走到 decodePDFImageSamples（按裸样解读码）而失败，掩码被
	// 静默丢弃——带掩码的图像整幅不透明。建筑图纸常把「白线黑底的图案 + 灰度掩码」
	// 存成一对 JPEG，丢掉掩码后整片铺成黑块。
	if hasDCTFilter(stream) {
		jpegData := stream.Content
		if !isJPEGData(jpegData) {
			jpegData = stream.Raw
		}
		if !isJPEGData(jpegData) {
			return nil, errors.New("JPEG 掩码数据无效")
		}
		decoded, err := jpeg.Decode(bytes.NewReader(jpegData))
		if err != nil {
			return nil, fmt.Errorf("JPEG 掩码解码失败: %w", err)
		}
		return resampleImageGray(decoded, width, height), nil
	}
	return decodePDFImageSamples(ctx, stream, width, height, 1, bpc)
}

// hasDCTFilter 判断流的过滤管线里是否含 DCTDecode（JPEG）。
func hasDCTFilter(stream *types.StreamDict) bool {
	for _, filter := range stream.FilterPipeline {
		if filter.Name == "DCTDecode" {
			return true
		}
	}
	return false
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

func pdfImageDataRaw(ctx *model.Context, stream *types.StreamDict, maskColor pdfColor, controls ...*conversionControl) ([]byte, string, error) {
	// JBIG2Decode（T.88）由 gobig2 解码为灰度位图，再按 ImageMask 或 1 位图像
	// 输出。pdfcpu 不处理该过滤器，必须在此之前拦截。
	if hasJBIG2Filter(stream) {
		if data, ok := pdfJBIG2Standalone(ctx, stream); ok {
			return data, "JBIG2", nil
		}
		gray, err := pdfJBIG2Image(ctx, stream, controls...)
		if err != nil {
			return nil, "", err
		}
		return encodePDFJBIG2Image(ctx, stream, gray, maskColor)
	}
	// JPXDecode（JPEG 2000）由纯 Go 解码库处理。
	if hasJPXFilter(stream) {
		return encodePDFJPXImage(ctx, stream, maskColor, controls...)
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
		if isJPEGData(stream.Content) {
			jpegData = append([]byte(nil), stream.Content...)
		} else if index > 0 && stream.Decode() == nil && isJPEGData(stream.Content) {
			jpegData = append([]byte(nil), stream.Content...)
		} else if isJPEGData(stream.Raw) {
			jpegData = append([]byte(nil), stream.Raw...)
		} else {
			return nil, "", errors.New("JPEG 图像解码失败")
		}
		if stream.Dict["Decode"] != nil && !pdfImageIsMask(ctx, stream) {
			var control *conversionControl
			if len(controls) > 0 {
				control = controls[0]
			}
			p := pdfInterpreter{ctx: ctx, control: control}
			if data, changed, err := p.jpegDecodeData(stream, jpegData); err != nil {
				return nil, "", err
			} else if changed {
				return data, "PNG", nil
			}
		}
		// DeviceCMYK JPEG 直接嵌入 OFD 时，多数渲染器会按 Adobe 约定解码成
		// 反相颜色（整幅变黑）。这里转换为 RGB PNG，保证红章等彩色图像正确。
		components, _, _, colorSpaceErr := pdfImageColorSpace(ctx, stream)
		forceCMYK := colorSpaceErr == nil && components == 4
		compactJPEG, jpegQuality := false, config.DefaultJPEGQuality
		if len(controls) > 0 && controls[0] != nil {
			options := controls[0].options
			compactJPEG = options.ImageMode == config.ImageModeCompact && stream.Dict["Mask"] == nil && stream.Dict["SMask"] == nil
			jpegQuality = options.JPEGQuality
		}
		if jpegQuality == 0 {
			jpegQuality = config.DefaultJPEGQuality
		}
		if encoded, format, ok, err := encodePDFCMYKJPEG(jpegData, cmyk, forceCMYK, compactJPEG, jpegQuality); err != nil {
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
	if pdfImageIsMask(ctx, stream) {
		if bpc == 0 {
			bpc = 1
		}
		if bpc != 1 {
			return nil, "", errors.New("ImageMask 仅支持 1 位图像")
		}
		return encodePDFImageMask(ctx, stream, maskColor, width, height)
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
	if components == 1 {
		gray, err := decodePDFGrayImage(ctx, stream, width, height, bpc)
		if err != nil {
			return nil, "", err
		}
		var encoded bytes.Buffer
		if err := media.EncodePNG(&encoded, gray); err != nil {
			return nil, "", fmt.Errorf("编码 PNG 图像失败: %w", err)
		}
		return encoded.Bytes(), "PNG", nil
	}
	raw, err := decodePDFImageSamples(ctx, stream, width, height, components, bpc)
	if err != nil {
		return nil, "", err
	}
	if components == 4 {
		// 非 DCT 编码的 DeviceCMYK 样本已是油墨值（0 表示无油墨），无需反相。
		return encodePDFCMYKImage(&image.CMYK{Pix: raw, Stride: width * 4, Rect: image.Rect(0, 0, width, height)}, false, cmyk)
	}
	rgba := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			index := (y*width + x) * components
			rgba.SetRGBA(x, y, color.RGBA{R: raw[index], G: raw[index+1], B: raw[index+2], A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := media.EncodePNG(&encoded, rgba); err != nil {
		return nil, "", fmt.Errorf("编码 PNG 图像失败: %w", err)
	}
	return encoded.Bytes(), "PNG", nil
}

// decodePDFGrayImage 把单通道 PDF 图像转换为 Gray。8 位默认灰度图直接复用
// PDF 解码后的 Content，避免先分配交错样本缓冲区再复制到 image.Gray.Pix。
func decodePDFGrayImage(ctx *model.Context, stream *types.StreamDict, width, height, bpc int) (*image.Gray, error) {
	if bpc < 1 || bpc > maxPDFImageBitsPerComponent {
		return nil, fmt.Errorf("不支持的图像位深 %d", bpc)
	}
	rowBytes := (width*bpc + 7) / 8
	data := stream.Content
	if width <= 0 || height <= 0 || rowBytes <= 0 || len(data) < rowBytes*height {
		return nil, errors.New("PNG 图像数据长度无效")
	}
	if bpc == 8 && pdfGrayDecodeIsIdentity(ctx, stream) {
		return &image.Gray{
			Pix:    data[:width*height],
			Stride: width,
			Rect:   image.Rect(0, 0, width, height),
		}, nil
	}
	decode := pdfImageDecode(ctx, stream, 1)
	maxValue := float64(uint32(1)<<uint(bpc) - 1)
	gray := image.NewGray(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		row := data[y*rowBytes : (y+1)*rowBytes]
		for x := 0; x < width; x++ {
			normalized := float64(readPDFImageSample(row, x, bpc)) / maxValue
			gray.Pix[y*gray.Stride+x] = floatToByte(decode[0] + normalized*(decode[1]-decode[0]))
		}
	}
	return gray, nil
}

func pdfGrayDecodeIsIdentity(ctx *model.Context, stream *types.StreamDict) bool {
	object, found := stream.Find("Decode")
	if !found {
		return true
	}
	resolved, err := dereferencePDFObject(ctx, object)
	if err != nil {
		return false
	}
	array, ok := resolved.(types.Array)
	if !ok || len(array) < 2 {
		return false
	}
	low, lowOK := numberValue(array[0])
	high, highOK := numberValue(array[1])
	return lowOK && highOK && low == 0 && high == 1
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
		if value, ok := dereferencedPDFNumber(ctx, array[i]); ok {
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
	if err := media.EncodePNG(&encoded, img); err != nil {
		return nil, "", fmt.Errorf("编码 PNG 图像失败: %w", err)
	}
	return encoded.Bytes(), "PNG", nil
}

func decodePDFImageSamples16(ctx *model.Context, stream *types.StreamDict, width, height, components int, controls ...*conversionControl) ([]uint16, error) {
	if components < 1 || components > 4 || width <= 0 || height <= 0 || width > int(^uint(0)>>1)/components/2 {
		return nil, errors.New("16 位图像尺寸或颜色分量数量无效")
	}
	rowBytes := width * components * 2
	data := stream.Content
	if height > len(data)/rowBytes {
		return nil, errors.New("PNG 图像数据长度无效")
	}
	var control *conversionControl
	if len(controls) > 0 {
		control = controls[0]
	}
	if err := control.consumeOperations(int64(width) * int64(height) * int64(components)); err != nil {
		return nil, err
	}
	decode := pdfImageDecode(ctx, stream, components)
	samples := make([]uint16, width*height*components)
	for y := 0; y < height; y++ {
		if err := control.check(); err != nil {
			return nil, err
		}
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
	if err := media.EncodePNG(&encoded, rgba); err != nil {
		return nil, "", fmt.Errorf("编码 Indexed PNG 图像失败: %w", err)
	}
	return encoded.Bytes(), "PNG", nil
}

// encodePDFCMYKJPEG 把 PDF 四分量 JPEG 转换为 RGB 图像。Go 的 jpeg 解码器
// 对 Adobe CMYK JPEG（transform=0 的 CMYK 与 transform=2 的 YCbCrK/YCCK）会
// 做一次反相，而 PDF 中 DeviceCMYK 样本本身就是油墨值，因此需要再反相还原。
// 没有 APP14 标记时，仅当 PDF 颜色空间确认是四分量，才补充 CMYK 标记供 Go 解码。
// 返回 ok=false 表示无法确认这是 CMYK 图像，调用方应按原始字节处理。
func encodePDFCMYKJPEG(data []byte, converter cmykConverter, forceCMYK, compact bool, quality int) ([]byte, string, bool, error) {
	transform, adobe := jpegAdobeTransform(data)
	// transform 0 = CMYK，2 = YCbCrK（YCCK），两者都是 4 分量 CMYK JPEG。
	if adobe && transform != 0 && transform != 2 {
		return nil, "", false, nil
	}
	decodeData := data
	if !adobe {
		if !forceCMYK {
			return nil, "", false, nil
		}
		decodeData = append([]byte(nil), data[:2]...)
		decodeData = append(decodeData, []byte{0xff, 0xee, 0x00, 0x0e, 'A', 'd', 'o', 'b', 'e', 0x00, 0x64, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}...)
		decodeData = append(decodeData, data[2:]...)
	}
	img, err := jpeg.Decode(bytes.NewReader(decodeData))
	if err != nil {
		return nil, "", false, nil
	}
	cmyk, ok := img.(*image.CMYK)
	if !ok {
		return nil, "", false, nil
	}
	format := "PNG"
	if compact {
		format = "JPEG"
	}
	encoded, format, err := encodePDFCMYKImageFormat(cmyk, true, converter, format, quality)
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
	icc   *coloricc.Transformer
	fast8 *coloricc.CMYK8Converter
}

func (c cmykConverter) toRGB(cyan, magenta, yellow, black uint8) (uint8, uint8, uint8) {
	if c.icc != nil {
		return c.icc.ToRGB([]uint8{cyan, magenta, yellow, black})
	}
	if c.fast8 == nil {
		return coloricc.DeviceCMYK8ToRGB(cyan, magenta, yellow, black)
	}
	return c.fast8.ToRGB(cyan, magenta, yellow, black)
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
	fast8 := coloricc.NewCMYK8Converter()
	return cmykConverter{icc: transformer, fast8: &fast8}
}

// pdfImageICCProfile 读取 ICCBased 图像颜色空间内嵌的 4 分量 ICC 配置文件。
// 返回 nil 表示该图像没有可用的内嵌 CMYK 配置文件。
func pdfImageICCProfile(ctx *model.Context, stream *types.StreamDict) *coloricc.Transformer {
	return pdfImageICCProfileChannels(ctx, stream, 4)
}

func pdfImageICCProfileChannels(ctx *model.Context, stream *types.StreamDict, channels int) *coloricc.Transformer {
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
	if n, ok := dereferencedPDFNumber(ctx, profile.Dict["N"]); !ok || int(n) != channels {
		return nil
	}
	transformer, err := coloricc.New(pdfStreamContent(profile), channels)
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
	return coloricc.DeviceCMYK8ToRGB(c, m, y, k)
}

// encodePDFCMYKImage 将 CMYK 图像转换为 RGB PNG；invert 为 true 时先对四个
// 分量取反，用于还原 Go 解码 Adobe CMYK JPEG 时引入的反相。
func encodePDFCMYKImage(img *image.CMYK, invert bool, converter cmykConverter) ([]byte, string, error) {
	return encodePDFCMYKImageFormat(img, invert, converter, "PNG", config.DefaultJPEGQuality)
}

func encodePDFCMYKImageFormat(img *image.CMYK, invert bool, converter cmykConverter, format string, quality int) ([]byte, string, error) {
	bounds := img.Bounds()
	rgba := acquireCMYKRGBA(bounds)
	defer releaseCMYKRGBA(rgba)
	width := bounds.Dx()
	if converter.icc == nil {
		fastConverter := coloricc.NewCMYK8Converter()
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			source := img.Pix[img.PixOffset(bounds.Min.X, y):]
			target := rgba.Pix[rgba.PixOffset(bounds.Min.X, y):]
			for x := 0; x < width; x++ {
				index := x * 4
				c, m, yellow, k := source[index], source[index+1], source[index+2], source[index+3]
				if invert {
					c, m, yellow, k = 255-c, 255-m, 255-yellow, 255-k
				}
				r, g, b := fastConverter.ToRGB(c, m, yellow, k)
				target[index], target[index+1], target[index+2], target[index+3] = r, g, b, 255
			}
		}
	} else {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			source := img.Pix[img.PixOffset(bounds.Min.X, y):]
			target := rgba.Pix[rgba.PixOffset(bounds.Min.X, y):]
			for x := 0; x < width; x++ {
				index := x * 4
				c, m, yellow, k := source[index], source[index+1], source[index+2], source[index+3]
				if invert {
					c, m, yellow, k = 255-c, 255-m, 255-yellow, 255-k
				}
				r, g, b := converter.toRGB(c, m, yellow, k)
				target[index], target[index+1], target[index+2], target[index+3] = r, g, b, 255
			}
		}
	}
	var encoded bytes.Buffer
	if format == "JPEG" {
		if err := jpeg.Encode(&encoded, rgba, &jpeg.Options{Quality: quality}); err != nil {
			return nil, "", fmt.Errorf("编码 CMYK 转换后的 JPEG 图像失败: %w", err)
		}
		return encoded.Bytes(), "JPEG", nil
	}
	if err := media.EncodePNG(&encoded, rgba); err != nil {
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
	if err := media.EncodePNG(&encoded, rgba); err != nil {
		return nil, "", fmt.Errorf("编码 ImageMask PNG 失败: %w", err)
	}
	return encoded.Bytes(), "PNG", nil
}

// unpremultiplyPDF 把预乘通道还原为非预乘值，与 color.NRGBAModel 的行为一致。
func unpremultiplyPDF(value, alpha uint8) uint8 {
	return uint8((uint32(value) * 0xffff / uint32(alpha)) >> 8)
}
