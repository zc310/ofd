package pdf2ofd

import (
	"encoding/hex"
	"math"
	"strings"
	"unicode/utf16"

	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
)

func (p *pdfInterpreter) showText(data []byte, _ []float64) {
	if len(data) == 0 {
		return
	}
	font := p.fonts[p.state.fontName]
	text, codes := decodePDFText(data, font)
	if strings.TrimSpace(text) == "" {
		// 纯空白 Tj 不产生文字对象，但仍需推进文本矩阵，否则后续文字会
		// 丢失前导空格的位移而整体左移（如代码块中的 "    >>>"）。
		p.adjustText(pdfTextWidth(codes, font, p.state.fontSize, p.state.charSpacing, p.state.wordSpacing, p.state.hScale))
		return
	}
	x, y := transformPDFPoint(p.state.textMatrix[4], p.state.textMatrix[5], p.state.ctm)
	x, y = p.pagePoint(x, y)
	// PDF 常把实际文字缩放放在 Tm 中，而 Tf 仍为 1。文本矩阵同时决定水平
	// 推进量和字符垂直尺寸；忽略它会让这类页面的文字渲染成几乎不可见的小点。
	textScaleX := math.Hypot(p.state.textMatrix[0], p.state.textMatrix[2])
	textScaleY := math.Hypot(p.state.textMatrix[1], p.state.textMatrix[3])
	if textScaleX == 0 {
		textScaleX = 1
	}
	if textScaleY == 0 {
		textScaleY = 1
	}
	// 整页坐标用 cm 放大排版（如通勤发票把用户单位定为 1mm）时，Tf/Tm 给出的
	// “字号”仍按该坐标系计。向量与路径的线宽都已计入 CTM 平均缩放，文字的字号
	// 和推进量省略它会把字形画得过小、占位挤在左下角。这里与路径一致使用
	// pdfMatrixScale，旋转等保角变换不影响字号。
	ctmScale := pdfMatrixScale(p.state.ctm)
	width := pdfTextWidth(codes, font, p.state.fontSize*textScaleX, p.state.charSpacing, p.state.wordSpacing, p.state.hScale) * p.info.userUnit * pdfPointToMillimeter * ctmScale
	size := p.state.fontSize * textScaleY * p.info.userUnit * pdfPointToMillimeter * ctmScale
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
	item := creator.Text{X: x, Y: y - size, Width: math.Max(width, 0.001), Height: math.Max(size, 0.001), Value: text, Font: fontName, Size: size, Fill: &fill, Stroke: stroke,
		Weight: weight, Italic: font.italic,
		FillColor:   ofdColorOpacity(colorToCreator(p.state.fill), p.fillOpacity()),
		StrokeColor: ofdColorOpacity(colorToCreator(p.state.stroke), p.strokeOpacity())}
	// Pattern 颜色空间下用渐变图案填充文字（如电子发票的彩色标题）时，必须把
	// 着色转成文字对象边界的局部渐变；否则文字回退成纯色/黑色，渐变丢失。
	if fill && p.state.fillPaint != nil && p.state.fillPaint.shading != nil {
		if color := p.shadingColor(p.state.fillPaint.shading, item.X, item.Y); color != nil {
			item.FillColor = ofdColorOpacity(color, p.fillOpacity())
		}
	}
	if transforms := pdfTextGlyphTransforms(codes, text, font); len(transforms) > 0 {
		item.CGTransforms = transforms
	}
	if deltas := pdfTextDeltas(codes, text, font, p.state.fontSize*textScaleX, p.state.charSpacing, p.state.wordSpacing, p.state.hScale, p.info.userUnit, ctmScale); len(deltas) > 0 {
		item.TextCodes = []creator.TextCode{{Value: text, DeltaX: deltas}}
	}
	p.page.Items = append(p.page.Items, item)
	p.adjustText(pdfTextWidth(codes, font, p.state.fontSize, p.state.charSpacing, p.state.wordSpacing, p.state.hScale))
}

func pdfTextGlyphTransforms(codes []uint16, text string, font pdfFontInfo) []creator.CGTransform {
	if len(codes) == 0 || len(font.data) == 0 || (font.codeBytes != 2 && len(font.codeToGID) == 0) {
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
		value := font.toUnicode[code]
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
		glyph := code
		mapped := font.codeBytes == 2
		if value, ok := font.cidToGID[code]; ok {
			glyph = value
			mapped = true
		}
		if value, ok := font.codeToGID[code]; ok {
			glyph = value
			mapped = true
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

func pdfTextWidth(codes []uint16, font pdfFontInfo, size, charSpacing, wordSpacing, hScale float64) float64 {
	width := 0.0
	scale := hScale / 100
	if scale == 0 {
		scale = 1
	}
	for _, code := range codes {
		width += pdfCodeWidth(code, font)/1000*size + charSpacing
		if code == ' ' {
			width += wordSpacing
		}
	}
	return width * scale
}

// pdfCodeWidth 返回单个字符在 PDF /Widths 中的宽度（千分之一 em）。
func pdfCodeWidth(code uint16, font pdfFontInfo) float64 {
	value, ok := font.widths[int(code)]
	if !ok {
		value = font.defaultW
		// CJK 变长 CMap 中的 ASCII 单字节码按半角处理，否则数字和
		// 英文会按全角宽度排开。
		if pdfCJKVariableWidth(font.encoding) && code < 0x80 {
			value = font.defaultW / 2
			if font.defaultW < 1 {
				value = 500
			}
		}
		// UniGB-UCS2-H 之类的全角 CMap 以 Unicode 编码作为字节码，而 /W 按
		// CID 给出字宽，按码位直接查表基本都会落空。此时 CJK 字符按全角
		// （1000）处理，否则套用 defaultW（常为 500 的半角）会把汉字挤压成
		// 窄字、逐字间距只有一半。
		if pdfCJKFullWidth(font.encoding) && code >= 0x80 && font.defaultW < 1000 {
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

// pdfTextDeltas 在嵌入字体的字形宽度与 PDF /Widths 不一致时，按 /Widths 生成
// 逐字符位置增量（毫米）。部分生产者（如 ReportLab）会写入与 unitsPerEm 不匹配
// 的 hmtx，阅读器若直接使用字体字宽会把代码等文字挤在一起。ctmScale 与字号统一
// 传递当前 CTM 的平均缩放，保证“整页按 1mm 排版”的文档里增量仍是物理毫米。
func pdfTextDeltas(codes []uint16, text string, font pdfFontInfo, size, charSpacing, wordSpacing, hScale, userUnit, ctmScale float64) []float64 {
	if len(codes) < 2 || len(font.glyphWidths) == 0 || len([]rune(text)) != len(codes) {
		return nil
	}
	needs := false
	for _, code := range codes {
		if fontWidth, ok := font.glyphWidths[code]; !ok || math.Abs(fontWidth-pdfCodeWidth(code, font)) > 0.5 {
			needs = true
			break
		}
	}
	if !needs {
		return nil
	}
	scale := hScale / 100
	if scale == 0 {
		scale = 1
	}
	deltas := make([]float64, len(codes)-1)
	for index, code := range codes[:len(codes)-1] {
		advance := pdfCodeWidth(code, font)/1000*size + charSpacing
		if code == ' ' {
			advance += wordSpacing
		}
		deltas[index] = advance * scale * userUnit * pdfPointToMillimeter * ctmScale
	}
	return deltas
}

func decodePDFText(data []byte, font pdfFontInfo) (string, []uint16) {
	codes := make([]uint16, 0, len(data))
	var builder strings.Builder
	step := font.codeBytes
	if step == 2 && len(data)%2 != 0 {
		step = 1
	}
	// 没有 ToUnicode 的 CID 字体（如 GBK-EUC-H）需要按预定义 CMap 的字符集
	// 解码字节，否则中文会退化成单字节乱码。
	decoder := pdfCJKDecoder(font.encoding)
	for i := 0; i < len(data); {
		// GBK/Big5/EUC-KR 等预定义 CMap 使用变长编码：ASCII 占一字节，
		// 中文占两字节，按固定两字节切分会把数字和英文拆散。
		if pdfCJKVariableWidth(font.encoding) {
			if data[i] < 0x80 {
				step = 1
			} else {
				step = 2
			}
		}
		if step == 2 && i+1 >= len(data) {
			step = 1
		}
		var code uint16
		var raw []byte
		if step == 2 {
			code = uint16(data[i])<<8 | uint16(data[i+1])
			raw = data[i : i+2]
		} else {
			code = uint16(data[i])
			raw = data[i : i+1]
		}
		i += step
		codes = append(codes, code)
		if value, ok := font.toUnicode[code]; ok {
			builder.WriteString(value)
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

func parsePDFToUnicode(data []byte) map[uint16]string {
	result := map[uint16]string{}
	tokens := tokenizePDFCMap(string(data))
	mode := ""
	for index := 0; index < len(tokens); index++ {
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
		if !ok {
			continue
		}
		switch mode {
		case "char":
			if index+1 >= len(tokens) {
				continue
			}
			if target, ok := pdfHexString(tokens[index+1]); ok {
				result[source] = target
			}
			index++
		case "range":
			if index+2 >= len(tokens) {
				continue
			}
			end, endOK := pdfHexUint(tokens[index+1])
			if !endOK || end < source {
				continue
			}
			// bfrange 的第三项为 "<start>"（连续递增）或 "[...]"（逐个列出）。
			if tokens[index+2] == "[" {
				cursor := index + 3
				for code := source; code <= end && cursor < len(tokens); code, cursor = code+1, cursor+1 {
					if tokens[cursor] == "]" {
						cursor++
						break
					}
					if value, ok := pdfHexString(tokens[cursor]); ok {
						result[code] = value
					}
				}
				index = cursor - 1
				continue
			}
			start, ok := pdfHexString(tokens[index+2])
			if !ok {
				continue
			}
			for code := source; code <= end; code++ {
				result[code] = start
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

func pdfHexUint(value string) (uint16, bool) {
	value = strings.Trim(value, "<>")
	if len(value) > 4 {
		value = value[len(value)-4:]
	}
	var result uint16
	for _, digit := range []byte(value) {
		result <<= 4
		switch {
		case digit >= '0' && digit <= '9':
			result += uint16(digit - '0')
		case digit >= 'a' && digit <= 'f':
			result += uint16(digit - 'a' + 10)
		case digit >= 'A' && digit <= 'F':
			result += uint16(digit - 'A' + 10)
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
