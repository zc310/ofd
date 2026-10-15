package pdf2ofd

import (
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"unicode/utf16"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/fontfix"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/spec"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
)

func (p *pdfInterpreter) showText(data []byte, resources types.Dict, depth int) {
	if len(data) == 0 {
		return
	}
	if p.control != nil {
		remaining := p.control.options.Limits.MaxOperations - p.control.operations
		unicodeBytes := fontUnicodeExpansion(p.fonts[p.state.fontName])
		if int64(len(data)) > remaining/unicodeBytes {
			p.control.limit("operations", p.control.options.Limits.MaxOperations+1, p.control.options.Limits.MaxOperations)
			return
		}
		p.control.operations += int64(len(data))
	}
	font := p.fonts[p.state.fontName]
	if font.encodingCMap != nil {
		// Decoding and the subsequent Unicode/CID/glyph queries scan code spaces
		// along the inheritance chain. Reserve this work before decoding text.
		cost := int64(0)
		for cmap := font.encodingCMap; cmap != nil; cmap = cmap.base {
			cost += int64(len(cmap.spaces)) * 8
		}
		if p.control.consumeOperations(int64(len(data))*cost) != nil {
			return
		}
	}
	text, codes := decodePDFText(data, font)
	if p.control != nil {
		p.control.operations += int64(len(text))
	}
	if font.type3 == nil && p.state.renderMode >= 4 && p.state.renderMode <= 7 {
		p.textClipActive = true
	}
	if font.vertical || font.type3 != nil {
		p.showPositionedText(text, codes, font, resources, depth)
		return
	}
	if strings.TrimSpace(text) == "" {
		// 纯空白 Tj 不产生文字对象，但仍需推进文本矩阵，否则后续文字会
		// 丢失前导空格的位移而整体左移（如代码块中的 "    >>>"）。
		p.adjustText(pdfTextWidth(codes, font, p.state.fontSize, p.state.charSpacing, p.state.wordSpacing, p.state.hScale))
		return
	}
	item := p.makeText(text, codes, font)
	if p.textClipping() {
		p.addTextClip(text, codes, font, item)
	}
	p.appendText(item, font)
	p.adjustText(pdfTextWidth(codes, font, p.state.fontSize, p.state.charSpacing, p.state.wordSpacing, p.state.hScale))
}

func (p *pdfInterpreter) makeText(text string, codes []uint64, font pdfFontInfo) creator.Text {
	// Ts 沿文字空间的 y 轴平移基线，不改变后续字符推进的文本矩阵。
	x, y := transformPDFPoint(p.state.textMatrix[4]+p.state.textMatrix[2]*p.state.textRise, p.state.textMatrix[5]+p.state.textMatrix[3]*p.state.textRise, p.state.ctm)
	x, y = p.pagePoint(x, y)
	// 按合成矩阵的 y 轴长度换算字号和本地推进量，其余缩放、错切、镜像及页面
	// 旋转保留在 OFD CTM 中。仅输出旋转角会丢失字形的横向拉伸和倾斜。
	matrix := multiplyPDFMatrix(p.state.ctm, p.state.textMatrix)
	textScale := math.Hypot(matrix[2], matrix[3])
	if textScale == 0 {
		textScale = 1
	}
	factor := textScale * p.info.userUnit * pdfPointToMillimeter
	px, py := p.pagePoint(0, 0)
	xx, xy := p.pagePoint(matrix[0], matrix[1])
	yx, yy := p.pagePoint(-matrix[2], -matrix[3])
	textCTM := creator.CTM{(xx - px) / factor, (xy - py) / factor, (yx - px) / factor, (yy - py) / factor, 0, 0}
	width := pdfTextWidth(codes, font, p.state.fontSize, p.state.charSpacing, p.state.wordSpacing, p.state.hScale) * factor
	size := p.state.fontSize * factor
	fontName := p.state.fontName
	if alias := p.fontAliases[fontName]; alias != "" {
		fontName = alias
	}
	if fontName == "" {
		fontName = "Helvetica"
		p.ensureFont(nil, fontName)
	}
	fill := p.state.renderMode == 0 || p.state.renderMode == 2 || p.state.renderMode == 4 || p.state.renderMode == 6
	stroke := p.state.renderMode == 1 || p.state.renderMode == 2 || p.state.renderMode == 5 || p.state.renderMode == 6
	weight := 0
	if font.bold {
		weight = 700
	}
	// Boundary 的底边必须包含基线下的笔画。基线显式写入 TextCode，避免扩展
	// Height 后 creator 的默认基线随之下移；字体度量与字号共用 Tm/CTM 缩放。
	baseline := math.Max(size, size*font.ascent/1000)
	descent := size * font.descent / 1000
	item := creator.Text{Value: text, Font: fontName, Size: size, Fill: &fill, Stroke: stroke,
		Weight: weight, Italic: font.italic,
		TextCodes:   []creator.TextCode{{Value: text, Y: &baseline}},
		FillColor:   ofdColorOpacity(colorToCreator(p.state.fill), p.fillOpacity()),
		StrokeColor: ofdColorOpacity(colorToCreator(p.state.stroke), p.strokeOpacity())}
	placePDFText(&item, x, y, math.Max(width, 0.001), baseline, descent, textCTM)
	if stroke {
		item.DrawParam = p.textStrokeDrawParam()
	}
	item.Clips = p.buildClips(item.X, item.Y)
	// PDF 的 Tz 同时缩放字形宽度和推进量。这里只把推进量计入 Width/DeltaX，
	// 若不同时输出 OFD 的 HScale，字形本身会按原宽绘制（横向未拉伸），
	// 导致 Tz≠100 的标题等文字偏窄、与相邻行字距失真。OFD HScale 默认 1。
	if h := p.state.hScale; h > 0 && h != 100 {
		item.HScale = h / 100
	}
	// Pattern 颜色空间下用渐变图案填充文字（如电子发票的彩色标题）时，必须把
	// 着色转成文字对象边界的局部渐变；否则文字回退成纯色/黑色，渐变丢失。
	if fill && p.state.fillPaint != nil && p.state.fillPaint.shading != nil {
		if color := p.shadingColor(p.state.fillPaint.shading, item.X, item.Y); color != nil {
			item.FillColor = ofdColorOpacity(color, p.fillOpacity())
		}
	}
	transforms := pdfTextGlyphTransforms(codes, text, font)
	if len(transforms) > 0 {
		item.CGTransforms = transforms
	}
	if deltas := pdfTextDeltas(codes, text, font, p.state.fontSize, p.state.charSpacing, p.state.wordSpacing, p.state.hScale, p.info.userUnit, textScale, transforms); len(deltas) > 0 {
		item.TextCodes[0].DeltaX = deltas
	}
	return item
}

func fontUnicodeExpansion(font pdfFontInfo) int64 {
	if font.maxUnicodeBytes > 0 {
		return font.maxUnicodeBytes + 1
	}
	return 5
}

// textStrokeDrawParam 保留 PDF 用描边模拟加粗或绘制空心字的线宽与连接样式。
// 线宽属于 PDF 用户空间，不随字号或 Tm 放大；相同参数在文档内复用资源。
func (p *pdfInterpreter) textStrokeDrawParam() string {
	lineWidth := p.state.lineWidth * p.info.userUnit * pdfPointToMillimeter * pdfMatrixScale(p.state.ctm)
	if p.state.lineWidth <= 0 {
		lineWidth = hairlineLineWidthMM
	}
	param := creator.DrawParam{
		LineWidth:  lineWidth,
		Cap:        [...]string{spec.LineCapButt, spec.LineCapRound, spec.LineCapSquare}[p.state.lineCap],
		Join:       [...]string{spec.LineJoinMiter, spec.LineJoinRound, spec.LineJoinBevel}[p.state.lineJoin],
		MiterLimit: p.state.miterLimit * lineWidth / 2,
	}
	for _, existing := range p.document.DrawParams {
		if existing.LineWidth == param.LineWidth && existing.Cap == param.Cap && existing.Join == param.Join && existing.MiterLimit == param.MiterLimit {
			return existing.Name
		}
	}
	param.Name = fmt.Sprintf("pdf-text-stroke-%d", len(p.document.DrawParams)+1)
	p.document.DrawParams = append(p.document.DrawParams, param)
	return param.Name
}

// placePDFText 把字形框变换为页面包围盒，并以 CTM 平移补偿边界原点的变化。
// TextCode 的基线偏移保持本地坐标值，变换后仍落在源 PDF 的基线起点。
func placePDFText(item *creator.Text, x, y, width, ascent, descent float64, matrix creator.CTM) {
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, corner := range [4][2]float64{{0, -ascent}, {width, -ascent}, {0, descent}, {width, descent}} {
		cx := matrix[0]*corner[0] + matrix[2]*corner[1]
		cy := matrix[1]*corner[0] + matrix[3]*corner[1]
		minX, minY = math.Min(minX, cx), math.Min(minY, cy)
		maxX, maxY = math.Max(maxX, cx), math.Max(maxY, cy)
	}
	item.X, item.Y = x+minX, y+minY
	item.Width, item.Height = math.Max(maxX-minX, 0.001), math.Max(maxY-minY, 0.001)
	matrix[4], matrix[5] = -minX-matrix[2]*ascent, -minY-matrix[3]*ascent
	if math.Abs(matrix[0]-1) > 1e-9 || math.Abs(matrix[1]) > 1e-9 || math.Abs(matrix[2]) > 1e-9 || math.Abs(matrix[3]-1) > 1e-9 || math.Abs(matrix[4]) > 1e-9 || math.Abs(matrix[5]) > 1e-9 {
		item.CTM = &matrix
	}
}

func pdfTextGlyphTransforms(codes []uint64, text string, font pdfFontInfo) []creator.CGTransform {
	if len(codes) == 0 || len(font.data) == 0 || (font.codeBytes != 2 && len(font.codeToGID) == 0 && font.predefined == nil && font.encodingCMap == nil) {
		return nil
	}
	glyphCount := embeddedFontGlyphCount(font.data)
	if glyphCount == 0 {
		return nil
	}
	values := []rune(text)
	transforms := make([]creator.CGTransform, 0, len(codes))
	position := 0
	for _, code := range codes {
		value := pdfCodeUnicode(code, font)
		count := len([]rune(value))
		if count == 0 {
			count = 1
		}
		if position+count > len(values) {
			count = len(values) - position
		}
		if count <= 0 {
			break
		}
		cid := pdfCIDCode(code, font)
		glyph := cid
		mapped := font.codeBytes == 2 || font.predefined != nil || font.encodingCMap != nil
		if value, ok := font.cidToGID[cid]; ok {
			glyph = value
			mapped = true
		}
		if value, ok := font.codeToGID[uint32(code)]; ok {
			glyph = value
			mapped = true
		}
		if font.sfnt != nil {
			glyph = pdfCodeGlyph(code, string(values[position:position+count]), font)
		}
		// 单个字符编码缺少字形时跳过该位置，保留其余已映射字形的
		// CGTransform；若整段都无法映射则返回 nil，退回按 Unicode 排版。
		if mapped && pdfFontCoversGlyph(font, uint32(glyph)) {
			transforms = append(transforms, creator.CGTransform{CodePosition: position, CodeCount: count, GlyphCount: 1, Glyphs: []int{int(glyph)}})
		}
		position += count
	}
	if len(transforms) == 0 {
		return nil
	}
	return transforms
}

func pdfTextWidth(codes []uint64, font pdfFontInfo, size, charSpacing, wordSpacing, hScale float64) float64 {
	width := 0.0
	scale := hScale / 100
	if scale == 0 {
		scale = 1
	}
	for _, code := range codes {
		width += pdfCodeWidth(code, font)/1000*size + charSpacing
		if pdfCodeWordSpacing(code, font) {
			width += wordSpacing
		}
	}
	return width * scale
}

// pdfCodeWidth 返回单个字符在 PDF /Widths 中的宽度（千分之一 em）。
func pdfCodeWidth(code uint64, font pdfFontInfo) float64 {
	value, ok := font.widths[int(pdfCIDCode(code, font))]
	if !ok {
		value = font.defaultW
		// CJK 变长 CMap 中的 ASCII 单字节码按半角处理，否则数字和
		// 英文会按全角宽度排开。
		if font.predefined == nil && pdfCJKVariableWidth(font.encoding) && code < 0x80 {
			value = font.defaultW / 2
			if font.defaultW < 1 {
				value = 500
			}
		}
		// UniGB-UCS2-H 之类的全角 CMap 以 Unicode 编码作为字节码，而 /W 按
		// CID 给出字宽，按码位直接查表基本都会落空。此时 CJK 字符按全角
		// （1000）处理，否则套用 defaultW（常为 500 的半角）会把汉字挤压成
		// 窄字、逐字间距只有一半。
		if font.predefined == nil && pdfCJKFullWidth(font.encoding) && code >= 0x80 && font.defaultW < 1000 {
			value = 1000
		}
	}
	return value
}

// pdfCJKFullWidth 判断 CMap 是否以 Unicode 全角字形编码（UCS-2/UTF-16 的 Uni*
// 系列）。这类编码中非 ASCII 字符几乎都是全角 CJK 字符。
func pdfCJKFullWidth(name string) bool {
	switch name {
	case "UniGB-UCS2-H", "UniGB-UCS2-V", "UniGB-UTF16-H", "UniGB-UTF16-V",
		"UniCNS-UCS2-H", "UniCNS-UCS2-V", "UniCNS-UTF16-H", "UniCNS-UTF16-V",
		"UniJIS-UCS2-H", "UniJIS-UCS2-V", "UniJIS-UTF16-H", "UniJIS-UTF16-V",
		"UniKS-UCS2-H", "UniKS-UCS2-V", "UniKS-UTF16-H", "UniKS-UTF16-V":
		return true
	default:
		return false
	}
}

// pdfTextDeltas 按 PDF /Widths 生成逐字符位置增量（毫米）。ctmScale 与字号统一
// 传递当前 CTM 的平均缩放，保证“整页按 1mm 排版”的文档里增量仍是物理毫米。
//
// 一律输出 DeltaX，不因内嵌字体字宽与 /Widths 一致就省略：OFD 的字符推进量由
// TextCode@DeltaX 给出，阅读器可以只按 DeltaX 排版而完全不查字体 hmtx。缺失
// DeltaX 时这类阅读器拿到 0 步进，整段文字会叠在首字符位置——
// intro.ofd 经 PDF 回转后第 3 页的 INTRODUCTION 叠在 “I” 上、“关于澎思” 叠在
// “关” 上就是这个现象。本项目渲染器另有 hmtx 回退，所以自测渲染看不出来。
func pdfTextDeltas(codes []uint64, text string, font pdfFontInfo, size, charSpacing, wordSpacing, hScale, userUnit, ctmScale float64, transforms []creator.CGTransform) []float64 {
	runes := []rune(text)
	if len(codes) < 2 || len(runes) < 2 {
		return nil
	}
	spans := make([][2]int, len(codes))
	if len(runes) == len(codes) {
		for index := range codes {
			spans[index] = [2]int{index, index + 1}
		}
	} else {
		// ToUnicode 可以把一个 PDF 字符码映射为连字字符串（例如单个 fi 字形）。
		// 仅在 CGTransform 确认每个 PDF 字符码对应一个输出字形时保留字宽，并将推进量
		// 放在该字符码对应的 Unicode 区间末尾。
		if len(transforms) != len(codes) {
			return nil
		}
		position := 0
		for index, transform := range transforms {
			count := len([]rune(pdfCodeUnicode(codes[index], font)))
			if count == 0 || transform.CodePosition != position || transform.CodeCount != count || transform.GlyphCount != 1 {
				return nil
			}
			spans[index] = [2]int{position, position + count}
			position += count
		}
		if position != len(runes) {
			return nil
		}
	}
	scale := hScale / 100
	if scale == 0 {
		scale = 1
	}
	deltas := make([]float64, len(runes)-1)
	for index, code := range codes[:len(codes)-1] {
		advance := pdfCodeWidth(code, font)/1000*size + charSpacing
		if pdfCodeWordSpacing(code, font) {
			advance += wordSpacing
		}
		deltas[spans[index][1]-1] = advance * scale * userUnit * pdfPointToMillimeter * ctmScale
	}
	return deltas
}

func decodePDFText(data []byte, font pdfFontInfo) (string, []uint64) {
	codes := make([]uint64, 0, len(data))
	var builder strings.Builder
	step := font.codeBytes
	if step != 2 {
		step = 1
	}
	if step == 2 && len(data)%2 != 0 {
		step = 1
	}
	// 没有 ToUnicode 的 CID 字体（如 GBK-EUC-H）需要按预定义 CMap 的字符集
	// 解码字节，否则中文会退化成单字节乱码。
	decoder := pdfCJKDecoder(font.encoding)
	for i := 0; i < len(data); {
		// GBK/Big5/EUC-KR 等预定义 CMap 使用变长编码：ASCII 占一字节，
		// 中文占两字节，按固定两字节切分会把数字和英文拆散。
		if font.encodingCMap != nil {
			step = pdfCMapCodeLength(font.encodingCMap.spaces, data[i:])
		} else if font.predefined != nil {
			step = font.predefined.codeLength(data[i:])
		} else if pdfCJKVariableWidth(font.encoding) {
			if data[i] < 0x80 {
				step = 1
			} else {
				step = 2
			}
		}
		if step == 2 && i+1 >= len(data) {
			step = 1
		}
		var code uint64
		raw := data[i : i+step]
		for _, b := range raw {
			code = code<<8 | uint64(b)
		}
		i += step
		if font.encodingCMap != nil {
			code |= uint64(step) << 32
			if !pdfEncodingContains(font.encodingCMap.spaces, code) {
				code = pdfEncodingCode(uint32(code), 5)
			}
		}
		codes = append(codes, code)
		if value := pdfCodeUnicode(code, font); value != "" {
			builder.WriteString(value)
			continue
		}
		// 无 ToUnicode 的 CID 字体靠内嵌 cmap 的字符集映射还原，见 cidUnicode。
		if value, ok := font.cidUnicode(code); ok {
			builder.WriteString(value)
			continue
		}
		// /G36 等未知自定义字形名没有 Unicode 语义。用私有区码位保留实际
		// 字形，避免把源字节误当作无关 ASCII 字符；修复后的内嵌字体会将该码位
		// 直接映射到原始 GID。
		if glyph := font.codeToGID[uint32(code)]; glyph != 0 {
			builder.WriteRune(fontfix.GlyphRune(glyph))
			continue
		}
		if decoder != nil {
			if decoded, err := decoder.NewDecoder().Bytes(raw); err == nil && len(decoded) > 0 {
				builder.WriteString(string(decoded))
				continue
			}
		}
		builder.WriteString(fallbackPDFCode(raw[0]))
	}
	return builder.String(), codes
}

// decodePDFName 把字体名称还原为正确的 Unicode 文本。PDF 名称可能以 #XX
// 十六进制转义编码（如 /#CB#CE#CC#E5 表示"宋体"），pdfcpu 在解析时已经
// 把它解成原始字节，也可能仍保留 # 转义。两种情况都按 GBK 再解释成中文，
// 避免字体族名写成非法 UTF-8。无法解码时原样返回。
func decodePDFName(name string) string {
	var raw []byte
	if strings.Contains(name, "#") {
		raw = make([]byte, 0, len(name))
		for i := 0; i < len(name); i++ {
			if name[i] == '#' && i+2 < len(name) {
				if hi, ok1 := hexNibble(name[i+1]); ok1 {
					if lo, ok2 := hexNibble(name[i+2]); ok2 {
						raw = append(raw, hi<<4|lo)
						i += 2
						continue
					}
				}
			}
			raw = append(raw, name[i])
		}
	} else {
		raw = []byte(name)
	}
	for _, value := range raw {
		if value < 0x80 {
			continue
		}
		if decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes(raw); err == nil && len(decoded) > 0 {
			return string(decoded)
		}
		break
	}
	return string(raw)
}

func hexNibble(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10, true
	default:
		return 0, false
	}
}

// pdfCJKVariableWidth 判断预定义 CMap 是否为变长字节编码（ASCII 单字节、
// 汉字双字节）。UCS2/UTF16 等固定双字节 CMap 返回 false。
func pdfCJKVariableWidth(name string) bool {
	switch name {
	case "GBK-EUC-H", "GBK-EUC-V", "GBKp-EUC-H", "GBKp-EUC-V", "GBK2K-H", "GBK2K-V",
		"ETen-B5-H", "ETen-B5-V", "ETenms-B5-H", "ETenms-B5-V", "B5pc-H", "B5pc-V",
		"HKscs-B5-H", "HKscs-B5-V", "CNS-EUC-H", "CNS-EUC-V",
		"KSC-EUC-H", "KSC-EUC-V", "KSCms-UHC-H", "KSCms-UHC-V", "KSCpc-EUC-H", "KSCpc-EUC-V":
		return true
	default:
		return false
	}
}

// pdfCJKDecoder 返回预定义 CJK CMap 对应的字节解码器；未知或非 CJK 编码返回 nil。
func pdfCJKDecoder(name string) encoding.Encoding {
	switch name {
	case "GBK-EUC-H", "GBK-EUC-V", "GBKp-EUC-H", "GBKp-EUC-V", "GBK2K-H", "GBK2K-V", "GBK-EUC-Hp":
		return simplifiedchinese.GBK
	case "ETen-B5-H", "ETen-B5-V", "ETenms-B5-H", "ETenms-B5-V", "B5pc-H", "B5pc-V",
		"HKscs-B5-H", "HKscs-B5-V", "CNS-EUC-H", "CNS-EUC-V":
		return traditionalchinese.Big5
	case "KSC-EUC-H", "KSC-EUC-V", "KSCms-UHC-H", "KSCms-UHC-V", "KSCpc-EUC-H", "KSCpc-EUC-V":
		return korean.EUCKR
	case "UniGB-UCS2-H", "UniGB-UCS2-V", "UniGB-UTF16-H", "UniGB-UTF16-V",
		"UniCNS-UCS2-H", "UniCNS-UCS2-V", "UniCNS-UTF16-H", "UniCNS-UTF16-V",
		"UniJIS-UCS2-H", "UniJIS-UCS2-V", "UniJIS-UTF16-H", "UniJIS-UTF16-V",
		"UniKS-UCS2-H", "UniKS-UCS2-V", "UniKS-UTF16-H", "UniKS-UTF16-V":
		return unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)
	default:
		return nil
	}
}

func fallbackPDFCode(value byte) string {
	if value >= 0x20 && value != 0x7f {
		return string(rune(value))
	}
	return "\uFFFD"
}

// maxToUnicodeCodes 限制 ToUnicode CMap 展开出的条目数量上限。
const maxToUnicodeCodes = 1 << 16

func parsePDFToUnicode(data []byte, controls ...*conversionControl) map[uint32]string {
	var control *conversionControl
	if len(controls) > 0 {
		control = controls[0]
	}
	return parsePDFToUnicodeMapping(data, control, nil)
}

func parsePDFToUnicodeMapping(data []byte, control *conversionControl, encoded map[uint64]string) map[uint32]string {
	if control.consumeOperations(int64(len(data))) != nil {
		return nil
	}
	result := map[uint32]string{}
	count := func() int { return max(len(result), len(encoded)) }
	put := func(code uint32, length int, value string) {
		if encoded != nil {
			encoded[pdfEncodingCode(code, length)] = value
		} else {
			result[code] = value
		}
	}
	tokens := tokenizePDFCMap(string(data))
	mode := ""
	for index := 0; index < len(tokens); index++ {
		if control.check() != nil {
			return result
		}
		token := tokens[index]
		switch token {
		case "beginbfchar":
			mode = "char"
			continue
		case "beginbfrange":
			mode = "range"
			continue
		case "endbfchar", "endbfrange":
			mode = ""
			continue
		}
		if mode == "" || !strings.HasPrefix(token, "<") {
			continue
		}
		source, ok := pdfHexUint(token)
		exactSource, sourceLength, exactOK := pdfEncodingHex(token)
		if encoded != nil && !exactOK {
			continue
		}
		if encoded != nil {
			source, ok = exactSource, exactOK
		}
		if !ok {
			continue
		}
		switch mode {
		case "char":
			if index+1 >= len(tokens) {
				continue
			}
			if target, ok := pdfHexString(tokens[index+1]); ok {
				if count() >= maxToUnicodeCodes {
					return result
				}
				put(source, sourceLength, target)
			}
			index++
		case "range":
			if index+2 >= len(tokens) {
				continue
			}
			end, endOK := pdfHexUint(tokens[index+1])
			if encoded != nil {
				var endLength int
				end, endLength, endOK = pdfEncodingHex(tokens[index+1])
				endOK = endOK && endLength == sourceLength
			}
			if !endOK || end < source {
				continue
			}
			// bfrange 的第三项为 "<start>"（连续递增）或 "[...]"（逐个列出）。
			if tokens[index+2] == "[" {
				cursor := index + 3
				for code := uint64(source); code <= uint64(end) && cursor < len(tokens); code, cursor = code+1, cursor+1 {
					if tokens[cursor] == "]" {
						cursor++
						break
					}
					if value, ok := pdfHexString(tokens[cursor]); ok {
						if control.consumeOperations(int64(len(value))+1) != nil || count() >= maxToUnicodeCodes {
							return result
						}
						put(uint32(code), sourceLength, value)
					}
				}
				index = cursor - 1
				continue
			}
			start, ok := pdfHexString(tokens[index+2])
			if !ok {
				continue
			}
			// 使用更宽的循环变量，避免 FFFFFFFF 的终点回绕；展开受预算与条目上限约束。
			for code, expanded := uint64(source), 0; code <= uint64(end); code, expanded = code+1, expanded+1 {
				if control.consumeOperations(int64(len(start))+1) != nil {
					return result
				}
				if expanded >= maxToUnicodeCodes || count() >= maxToUnicodeCodes {
					return result
				}
				put(uint32(code), sourceLength, start)
				start = incrementPDFUnicode(start)
			}
			index += 2
		}
	}
	return result
}

// tokenizePDFCMap 把 ToUnicode CMap 文本切分为 token。CMap 允许相邻的
// "<...>" 之间没有空白（例如 <0336><0336><4e0a>），因此不能直接用
// strings.Fields，需要按分隔符切分并把十六进制串整体保留。
func tokenizePDFCMap(data string) []string {
	tokens := make([]string, 0, 256)
	for index := 0; index < len(data); {
		current := data[index]
		switch {
		case current == '%':
			// 注释到行尾。
			for index < len(data) && data[index] != '\n' {
				index++
			}
		case current == '(':
			// 跳过字面字符串（如 /Registry (Adobe)）。
			depth := 1
			index++
			for index < len(data) && depth > 0 {
				switch data[index] {
				case '\\':
					index++
				case '(':
					depth++
				case ')':
					depth--
				}
				index++
			}
		case current == '<':
			if index+1 < len(data) && data[index+1] == '<' {
				index += 2
				continue
			}
			end := strings.IndexByte(data[index:], '>')
			if end < 0 {
				return tokens
			}
			tokens = append(tokens, data[index:index+end+1])
			index += end + 1
		case current == '>' || current == ')' || current == '{' || current == '}':
			index++
		case current == '[' || current == ']':
			tokens = append(tokens, string(current))
			index++
		case isPDFCMapSpace(current):
			index++
		default:
			end := index
			for end < len(data) && !isPDFCMapDelimiter(data[end]) {
				end++
			}
			if end == index {
				index++
				continue
			}
			tokens = append(tokens, data[index:end])
			index = end
		}
	}
	return tokens
}

func isPDFCMapSpace(value byte) bool {
	switch value {
	case ' ', '\t', '\r', '\n', '\f', 0:
		return true
	}
	return false
}

func isPDFCMapDelimiter(value byte) bool {
	switch value {
	case '<', '>', '[', ']', '(', ')', '{', '}', '%':
		return true
	}
	return isPDFCMapSpace(value)
}

func pdfHexUint(value string) (uint32, bool) {
	value = strings.Trim(value, "<>")
	if len(value) == 0 || len(value) > 8 {
		return 0, false
	}
	var result uint32
	for _, digit := range []byte(value) {
		result <<= 4
		switch {
		case digit >= '0' && digit <= '9':
			result += uint32(digit - '0')
		case digit >= 'a' && digit <= 'f':
			result += uint32(digit - 'a' + 10)
		case digit >= 'A' && digit <= 'F':
			result += uint32(digit - 'A' + 10)
		default:
			return 0, false
		}
	}
	return result, true
}

func pdfHexString(value string) (string, bool) {
	value = strings.Trim(value, "<>")
	if len(value)%2 != 0 {
		return "", false
	}
	raw, err := hex.DecodeString(value)
	if err != nil {
		return "", false
	}
	if len(raw) >= 2 && raw[0] == 0xfe && raw[1] == 0xff {
		raw = raw[2:]
	}
	if len(raw)%2 == 0 && len(raw) > 0 {
		units := make([]uint16, len(raw)/2)
		for index := range units {
			units[index] = uint16(raw[index*2])<<8 | uint16(raw[index*2+1])
		}
		return string(utf16.Decode(units)), true
	}
	return string(raw), true
}

func incrementPDFUnicode(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return value
	}
	runes[len(runes)-1]++
	return string(runes)
}
