package pdf2ofd

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"strings"

	"github.com/benoitkugler/textlayout/fonts/glyphsnames"
	"github.com/benoitkugler/textlayout/fonts/simpleencodings"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	fontparser "github.com/tdewolff/font"
	"github.com/zc310/fontfix"
)

func (p *pdfInterpreter) loadFont(font *pdfFontInfo, object types.Object) {
	dict, err := p.ctx.XRefTable.DereferenceDict(object)
	if err != nil || dict == nil {
		return
	}
	if subtype := dict.NameEntry("Subtype"); subtype != nil && *subtype == "Type0" {
		font.codeBytes = 2
	}
	if encoding, found := dict.Find("Encoding"); found {
		if name, ok := encoding.(types.Name); ok {
			font.encoding = name.Value()
		}
	}
	if baseFont := dict.NameEntry("BaseFont"); baseFont != nil {
		font.familyName = decodePDFName(strings.TrimPrefix(*baseFont, "/"))
	}
	firstCharObject, found := dict.Find("FirstChar")
	if value, ok := numberValue(firstCharObject); found && ok {
		widthsObject, found := dict.Find("Widths")
		if found {
			if widths, err := p.ctx.XRefTable.DereferenceArray(widthsObject); err == nil {
				for i, object := range widths {
					if width, ok := numberValue(object); ok {
						font.widths[int(value)+i] = width
					}
				}
			}
		}
	}
	descriptorObject, found := dict.Find("FontDescriptor")
	if !found && len(dict.ArrayEntry("DescendantFonts")) > 0 {
		if descendant, descendantErr := p.ctx.XRefTable.DereferenceDict(dict.ArrayEntry("DescendantFonts")[0]); descendantErr == nil && descendant != nil {
			descriptorObject, found = descendant.Find("FontDescriptor")
		}
	}
	if descriptor, err := p.ctx.XRefTable.DereferenceDict(descriptorObject); found && err == nil && descriptor != nil {
		missingWidthObject, found := descriptor.Find("MissingWidth")
		if value, ok := numberValue(missingWidthObject); found && ok {
			font.defaultW = value
		}
		p.loadEmbeddedFont(font, descriptor)
	}
	if object, found := dict.Find("ToUnicode"); found {
		if stream, _, err := p.ctx.XRefTable.DereferenceStreamDict(object); err == nil && stream != nil {
			if stream.Decode() == nil {
				font.toUnicode = parsePDFToUnicode(stream.Content)
			}
		}
	}
	// 部分字体（尤其 Type3 与无 ToUnicode 的子集字体）只提供 /Encoding 的
	// /Differences 字形名。必须按标准字形表补出 Unicode，否则解码会退化成
	// 原始字节，正文出现乱码。ToUnicode 优先，仅补齐缺失的编码。
	if font.codeBytes == 1 {
		p.applySimpleEncoding(font, dict)
	}
	font.codeToGID = p.pdfFontCodeToGID(*font, dict)
	if len(font.data) > 0 {
		if sfnt, err := fontparser.ParseSFNT(font.data, 0); err == nil && sfnt != nil {
			font.sfnt = sfnt
		}
	}
	font.glyphWidths = pdfFontGlyphWidths(font.data, font.codeToGID)
	if descendants := dict.ArrayEntry("DescendantFonts"); len(descendants) > 0 {
		if descendant, err := p.ctx.XRefTable.DereferenceDict(descendants[0]); err == nil && descendant != nil {
			defaultWidthObject, found := descendant.Find("DW")
			if value, ok := numberValue(defaultWidthObject); found && ok {
				font.defaultW = value
			}
			if object, found := descendant.Find("W"); found {
				p.loadCIDWidths(font, object)
			}
			p.loadCIDToGID(font, descendant)
		}
	}
	// CID 字体没有 /Encoding + /Widths 布局，/W 按 CID 给出字宽。嵌入字体的
	// hmtx 可能只是占位值（如 CFF 包装后统一为默认字宽），必须用 PDF /W 生成
	// DeltaX，否则阅读器按内嵌字体字宽排版会把文字挤在一起或错位。
	if font.codeBytes == 2 && font.sfnt != nil {
		font.glyphWidths = pdfCIDFontGlyphWidths(font.widths, font.sfnt)
	}
}

// applySimpleEncoding 按简单字体的 /Encoding 补齐 code→Unicode 映射。Type3 与
// 部分子集字体没有 /ToUnicode，只给出 /Encoding 的 /Differences（标准字形名）。
// 不补齐时 decodePDFText 会退回原始字节，正文出现乱码。已有 ToUnicode 的编码
// 保持不变，仅补充缺失项。
func (p *pdfInterpreter) applySimpleEncoding(font *pdfFontInfo, dict types.Dict) {
	encodingObject, found := dict.Find("Encoding")
	if !found {
		return
	}
	mapping := map[uint16]string{}
	switch value := encodingObject.(type) {
	case types.Name:
		fillPDFBaseEncoding(value.Value(), mapping)
	default:
		encodingDict, err := p.ctx.XRefTable.DereferenceDict(encodingObject)
		if err != nil || encodingDict == nil {
			return
		}
		base := "StandardEncoding"
		if baseName := encodingDict.NameEntry("BaseEncoding"); baseName != nil {
			base = *baseName
		}
		fillPDFBaseEncoding(base, mapping)
		differences, found := encodingDict.Find("Differences")
		if !found {
			break
		}
		values, err := p.ctx.XRefTable.DereferenceArray(differences)
		if err != nil {
			break
		}
		code := -1
		for _, item := range values {
			if number, ok := integerValue(item); ok {
				code = number
				continue
			}
			name, ok := item.(types.Name)
			if !ok || code < 0 || code > 255 {
				continue
			}
			if r, ok := glyphsnames.GlyphToRune(strings.TrimPrefix(name.Value(), "/")); ok {
				mapping[uint16(code)] = string(r)
			}
			code++
		}
	}
	if len(mapping) == 0 {
		return
	}
	if font.toUnicode == nil {
		font.toUnicode = map[uint16]string{}
	}
	for code, value := range mapping {
		if _, exists := font.toUnicode[code]; !exists {
			font.toUnicode[code] = value
		}
	}
}

// fillPDFBaseEncoding 填充简单字体的基础编码（WinAnsi/MacRoman/标准编码等）。
func fillPDFBaseEncoding(name string, out map[uint16]string) {
	var table simpleencodings.Encoding
	switch strings.TrimPrefix(name, "/") {
	case "WinAnsiEncoding":
		table = simpleencodings.WinAnsi
	case "MacRomanEncoding":
		table = simpleencodings.MacRoman
	case "MacExpertEncoding":
		table = simpleencodings.MacExpert
	case "SymbolEncoding", "Symbol":
		table = simpleencodings.Symbol
	case "ZapfDingbatsEncoding", "ZapfDingbats":
		table = simpleencodings.ZapfDingbats
	default:
		table = simpleencodings.AdobeStandard
	}
	for code, r := range table.ByteToRune() {
		out[uint16(code)] = string(r)
	}
}

func (p *pdfInterpreter) loadCIDToGID(font *pdfFontInfo, descendant types.Dict) {
	object, found := descendant.Find("CIDToGIDMap")
	if !found {
		return
	}
	if name, ok := object.(types.Name); ok && name.Value() == "Identity" {
		return
	}
	stream, _, err := p.ctx.XRefTable.DereferenceStreamDict(object)
	if err != nil || stream == nil || stream.Decode() != nil {
		return
	}
	font.cidToGID = make(map[uint16]uint16, len(stream.Content)/2)
	for index := 0; index+1 < len(stream.Content); index += 2 {
		font.cidToGID[uint16(index/2)] = uint16(stream.Content[index])<<8 | uint16(stream.Content[index+1])
	}
}

func (p *pdfInterpreter) pdfFontCodeToGID(font pdfFontInfo, dict types.Dict) map[uint16]uint16 {
	if len(font.data) == 0 || font.codeBytes != 1 {
		return nil
	}
	sfnt, err := fontparser.ParseSFNT(font.data, 0)
	if err != nil || sfnt == nil {
		return nil
	}
	names := make(map[int]string, 256)
	for code := 32; code <= 126; code++ {
		names[code] = pdfStandardGlyphName(rune(code))
	}
	if encoding, found := dict.Find("Encoding"); found {
		if encodingDict, dictErr := p.ctx.XRefTable.DereferenceDict(encoding); dictErr == nil && encodingDict != nil {
			if differences, found := encodingDict.Find("Differences"); found {
				if values, arrayErr := p.ctx.XRefTable.DereferenceArray(differences); arrayErr == nil {
					code := -1
					for _, value := range values {
						if number, ok := integerValue(value); ok {
							code = number
							continue
						}
						if name, ok := value.(types.Name); ok && code >= 0 && code <= 255 {
							names[code] = strings.TrimPrefix(name.Value(), "/")
							code++
						}
					}
				}
			}
		}
	}
	result := make(map[uint16]uint16)
	for code, name := range names {
		if name == "" {
			continue
		}
		if glyph := sfnt.FindGlyphName(name); glyph != 0 {
			result[uint16(code)] = glyph
		}
	}
	// 子集字体可能使用非标准字形名（如 ReportLab 的 uF00XX），无法按标准
	// 字形名解析。此时改用字体自身的 cmap：按字符编码（优先 ToUnicode）
	// 查询 glyph，使转换器能输出 CGTransform，让阅读器直接使用内嵌字形。
	for code := 0; code <= 255; code++ {
		if _, ok := result[uint16(code)]; ok {
			continue
		}
		value := rune(code)
		if mapped, ok := font.toUnicode[uint16(code)]; ok {
			if runes := []rune(mapped); len(runes) == 1 {
				value = runes[0]
			}
		}
		if glyph := sfnt.GlyphIndex(value); glyph != 0 {
			result[uint16(code)] = glyph
			continue
		}
		// 部分子集字体的 cmap 使用平台 1（Mac Roman）字节键，字符编码本身
		// 就是键；ToUnicode 给出的 Unicode 值反而查不到字形时回退到原始编码。
		if value != rune(code) {
			if glyph := sfnt.GlyphIndex(rune(code)); glyph != 0 {
				result[uint16(code)] = glyph
			}
		}
	}
	return result
}

// pdfFontGlyphWidths 返回字体自身 cmap 给出的逐字符宽度，换算到与 PDF
// /Widths 相同的千分之一 em 单位，用于检测内嵌字体字宽是否可信。
func pdfFontGlyphWidths(data []byte, codeToGID map[uint16]uint16) map[uint16]float64 {
	if len(data) == 0 || len(codeToGID) == 0 {
		return nil
	}
	sfnt, err := fontparser.ParseSFNT(data, 0)
	if err != nil || sfnt == nil || sfnt.Head == nil {
		return nil
	}
	upem := float64(sfnt.Head.UnitsPerEm)
	if upem <= 0 {
		return nil
	}
	widths := make(map[uint16]float64, len(codeToGID))
	for code, glyph := range codeToGID {
		widths[code] = float64(sfnt.GlyphAdvance(glyph)) / upem * 1000
	}
	return widths
}

// pdfCIDFontGlyphWidths 对 CID 字体按 /W 中出现的 CID 生成"内嵌字体字形宽度"
// 表，供 pdfTextDeltas 检测 hmtx 与 PDF /W 不一致后按 /W 输出 DeltaX。CFF 包装
// 的内嵌字体通常没有真实 hmtx（统一为占位字宽），CID 与字形序也不连续，因此按
// fontfix 私有区映射 F0000+CID 找字形再取字宽。
func pdfCIDFontGlyphWidths(pdfWidths map[int]float64, sfnt *fontparser.SFNT) map[uint16]float64 {
	if sfnt == nil || sfnt.Head == nil {
		return nil
	}
	upem := float64(sfnt.Head.UnitsPerEm)
	if upem <= 0 {
		return nil
	}
	widths := make(map[uint16]float64, len(pdfWidths))
	for code := range pdfWidths {
		if code < 0 || code > math.MaxUint16 {
			continue
		}
		if glyph := sfnt.GlyphIndex(fontfix.GlyphRune(uint16(code))); glyph != 0 {
			widths[uint16(code)] = float64(sfnt.GlyphAdvance(glyph)) / upem * 1000
		}
	}
	return widths
}

// pdfFontCoversGlyph 判断嵌入字体是否包含指定 glyph id。CID 字体（如 TeX
// 生产者的子集）可能使用不连续的 CID 编号：code(CID) 的数值大于 numGlyphs，
// 但字形确实存在于内嵌字体中，由 fontfix 私有区映射 F0000+CID 指向。仅凭
// code < numGlyphs 会误判字形缺失并丢弃整段 CGTransform，使斜体等文字退回
// Unicode 排版后布局与字形错乱（出现方块或错字）。
func pdfFontCoversGlyph(font pdfFontInfo, glyph uint32) bool {
	if glyph < embeddedFontGlyphCount(font.data) {
		return true
	}
	if glyph > math.MaxUint16 || font.sfnt == nil {
		return false
	}
	return font.sfnt.GlyphIndex(fontfix.GlyphRune(uint16(glyph))) != 0
}

func pdfStandardGlyphName(value rune) string {
	if value >= '0' && value <= '9' {
		return [...]string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}[value-'0']
	}
	if value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z' {
		return string(value)
	}
	switch value {
	case ' ':
		return "space"
	case '!':
		return "exclam"
	case '"':
		return "quotedbl"
	case '#':
		return "numbersign"
	case '$':
		return "dollar"
	case '%':
		return "percent"
	case '&':
		return "ampersand"
	case '\'':
		return "quotesingle"
	case '(':
		return "parenleft"
	case ')':
		return "parenright"
	case '*':
		return "asterisk"
	case '+':
		return "plus"
	case ',':
		return "comma"
	case '-':
		return "hyphen"
	case '.':
		return "period"
	case '/':
		return "slash"
	case ':':
		return "colon"
	case ';':
		return "semicolon"
	case '<':
		return "less"
	case '=':
		return "equal"
	case '>':
		return "greater"
	case '?':
		return "question"
	case '@':
		return "at"
	case '[':
		return "bracketleft"
	case '\\':
		return "backslash"
	case ']':
		return "bracketright"
	case '^':
		return "asciicircum"
	case '_':
		return "underscore"
	case '`':
		return "grave"
	case '{':
		return "braceleft"
	case '|':
		return "bar"
	case '}':
		return "braceright"
	case '~':
		return "asciitilde"
	}
	return ""
}

// pdfFontStyleFlags 依据 PDF 字体族/PostScript 名推断 OFD 逻辑字体的粗体、
// 斜体、衬线与等宽标志。非嵌入字体没有可用的字形数据，这些标志用于让阅读器
// 回退到风格一致的系统字体（如 NimbusRomNo9L-Medi 是粗体衬线）。
func pdfFontStyleFlags(name string) (bold, italic, serif, fixedWidth bool) {
	lower := strings.ToLower(name)
	bold = strings.Contains(lower, "bold") || strings.Contains(lower, "medi") ||
		strings.Contains(lower, "semi") || strings.Contains(lower, "demi") ||
		strings.Contains(lower, "black") || strings.Contains(lower, "heavy")
	italic = strings.Contains(lower, "italic") || strings.Contains(lower, "ital") ||
		strings.Contains(lower, "oblique") || strings.Contains(lower, "slant")
	fixedWidth = strings.Contains(lower, "nimbusmon") || strings.Contains(lower, "mono") ||
		strings.Contains(lower, "courier") || strings.Contains(lower, "typewriter") ||
		strings.Contains(lower, "cmtt")
	serif = !fixedWidth && (strings.Contains(lower, "nimbusrom") || strings.Contains(lower, "roman") ||
		strings.Contains(lower, "serif") || strings.Contains(lower, "times") ||
		strings.Contains(lower, "cmr") || strings.Contains(lower, "cmsy") ||
		strings.Contains(lower, "cmmi") || strings.Contains(lower, "bookman") ||
		strings.Contains(lower, "schoolbook") || strings.Contains(lower, "georgia"))
	return bold, italic, serif, fixedWidth
}

func embeddedFontGlyphCount(data []byte) uint32 {
	if len(data) < 12 {
		return 0
	}
	numTables := int(binary.BigEndian.Uint16(data[4:6]))
	if 12+numTables*16 > len(data) {
		return 0
	}
	for index := 0; index < numTables; index++ {
		entry := data[12+index*16 : 28+index*16]
		if string(entry[:4]) != "maxp" {
			continue
		}
		offset := int(binary.BigEndian.Uint32(entry[8:12]))
		length := int(binary.BigEndian.Uint32(entry[12:16]))
		if offset < 0 || length < 6 || offset > len(data) || length > len(data)-offset {
			return 0
		}
		return uint32(binary.BigEndian.Uint16(data[offset+4 : offset+6]))
	}
	return 0
}

func (p *pdfInterpreter) loadEmbeddedFont(font *pdfFontInfo, descriptor types.Dict) {
	var object types.Object
	var found bool
	format := ""
	if object, found = descriptor.Find("FontFile2"); found {
		format = "ttf"
	} else if object, found = descriptor.Find("FontFile3"); found {
		stream, _, err := p.ctx.XRefTable.DereferenceStreamDict(object)
		if err != nil || stream == nil {
			return
		}
		subtype := stream.NameEntry("Subtype")
		switch {
		case subtype != nil && (*subtype == "OpenType" || *subtype == "CIDFontType0C"):
			format = "otf"
		case subtype != nil && *subtype == "Type1C":
			format = "otf"
		default:
			return
		}
	}
	if !found {
		return
	}
	stream, _, err := p.ctx.XRefTable.DereferenceStreamDict(object)
	if err != nil || stream == nil || stream.Decode() != nil || len(stream.Content) == 0 {
		return
	}
	data := append([]byte(nil), stream.Content...)
	if format == "ttf" {
		if normalized, normalizeErr := normalizeEmbeddedTrueType(data); normalizeErr == nil {
			data = normalized
		}
		if repaired, repairErr := fontfix.Repair(data); repairErr == nil {
			data = repaired
		}
	}
	if format == "otf" {
		if repaired, repairErr := fontfix.Repair(data); repairErr == nil {
			data = repaired
		} else if len(data) < 4 || string(data[:4]) != "OTTO" {
			return
		}
	}
	font.format = format
	font.data = data
}

// normalizeEmbeddedTrueType 去掉损坏的 PDF 子集 hmtx 表尾部多余填充。少数
// 生产者会多写两个字节，导致原本可用的字体被严格的 SFNT 读取器和 canvas 拒绝。
func normalizeEmbeddedTrueType(data []byte) ([]byte, error) {
	if len(data) < 12 || binary.BigEndian.Uint32(data[:4]) != 0x00010000 {
		return data, nil
	}
	numTables := int(binary.BigEndian.Uint16(data[4:6]))
	directoryEnd := 12 + numTables*16
	if numTables == 0 || directoryEnd > len(data) {
		return nil, errors.New("TrueType 字体目录无效")
	}
	type table struct {
		tag  [4]byte
		data []byte
	}
	tables := make([]table, 0, numTables)
	hhea, maxp, hmtx := -1, -1, -1
	for index := 0; index < numTables; index++ {
		entry := data[12+index*16 : 28+index*16]
		offset := int(binary.BigEndian.Uint32(entry[8:12]))
		length := int(binary.BigEndian.Uint32(entry[12:16]))
		if offset < 0 || length < 0 || offset > len(data) || length > len(data)-offset {
			return nil, errors.New("TrueType 字体表超出范围")
		}
		var tag [4]byte
		copy(tag[:], entry[:4])
		tables = append(tables, table{tag: tag, data: append([]byte(nil), data[offset:offset+length]...)})
		switch string(tag[:]) {
		case "hhea":
			hhea = index
		case "maxp":
			maxp = index
		case "hmtx":
			hmtx = index
		}
	}
	if hhea < 0 || maxp < 0 || hmtx < 0 || len(tables[hhea].data) < 36 || len(tables[maxp].data) < 6 {
		return data, nil
	}
	numGlyphs := int(binary.BigEndian.Uint16(tables[maxp].data[4:6]))
	numHMetrics := int(binary.BigEndian.Uint16(tables[hhea].data[34:36]))
	if numGlyphs <= 0 || numHMetrics <= 0 || numHMetrics > numGlyphs {
		return data, nil
	}
	required := 4*numHMetrics + 2*(numGlyphs-numHMetrics)
	if len(tables[hmtx].data) < required {
		return nil, errors.New("TrueType hmtx 表长度不足")
	}
	if len(tables[hmtx].data) == required {
		return data, nil
	}
	tables[hmtx].data = tables[hmtx].data[:required]
	var output bytes.Buffer
	output.Write(data[:4])
	var header [8]byte
	binary.BigEndian.PutUint16(header[0:2], uint16(numTables))
	// 保留原有的 search 字段；表数量不变时读取器不要求重新计算这些值。
	copy(header[2:], data[6:12])
	output.Write(header[:])
	directory := make([]byte, numTables*16)
	dataOffset := 12 + len(directory)
	for index := range tables {
		dataOffset = (dataOffset + 3) &^ 3
		entry := directory[index*16 : index*16+16]
		copy(entry[:4], tables[index].tag[:])
		binary.BigEndian.PutUint32(entry[8:12], uint32(dataOffset))
		binary.BigEndian.PutUint32(entry[12:16], uint32(len(tables[index].data)))
		dataOffset += len(tables[index].data)
	}
	output.Write(directory)
	for index := range tables {
		for output.Len()%4 != 0 {
			output.WriteByte(0)
		}
		output.Write(tables[index].data)
	}
	return output.Bytes(), nil
}

func (p *pdfInterpreter) loadCIDWidths(font *pdfFontInfo, object types.Object) {
	widths, err := p.ctx.XRefTable.DereferenceArray(object)
	if err != nil {
		return
	}
	for index := 0; index < len(widths); {
		first, ok := p.dereferencedNumber(widths[index])
		if !ok {
			index++
			continue
		}
		index++
		if index >= len(widths) {
			break
		}
		if values, err := p.ctx.XRefTable.DereferenceArray(widths[index]); err == nil {
			index++
			for offset, value := range values {
				if width, ok := p.dereferencedNumber(value); ok {
					font.widths[int(first)+offset] = width
				}
			}
			continue
		}
		second, ok := p.dereferencedNumber(widths[index])
		if !ok || index+1 >= len(widths) {
			break
		}
		width, ok := p.dereferencedNumber(widths[index+1])
		if !ok {
			index++
			continue
		}
		for code := int(first); code <= int(second); code++ {
			font.widths[code] = width
		}
		index += 2
	}
}

func (p *pdfInterpreter) dereferencedNumber(object types.Object) (float64, bool) {
	value, err := p.ctx.XRefTable.Dereference(object)
	if err != nil {
		return 0, false
	}
	return numberValue(value)
}
