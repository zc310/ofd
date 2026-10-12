package pdf2ofd

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"unicode/utf8"

	"github.com/zc310/ofd/pkg/creator"
)

const pdfSemanticFontFamily = "PDFType3Text"

// pdfSemanticFont 构造无笔画的合法 TrueType 字体。每个 Unicode 码位使用不同
// glyph ID，并保留源水平字宽，供 PDF 子集化的 ToUnicode 与提取框使用。
func pdfSemanticFont(mapping map[uint32]string, source pdfFontInfo, control *conversionControl) ([]byte, error) {
	advances, err := pdfSemanticAdvances(source, control)
	if err != nil {
		return nil, err
	}
	set := map[rune]bool{' ': true, '\uFFFD': true}
	for _, value := range mapping {
		if control.consumeOperations(int64(len(value))) != nil {
			return nil, control.check()
		}
		for _, r := range value {
			set[r] = true
		}
	}
	for code := 0; code < 256; code++ {
		value, ok := mapping[uint32(code)]
		if !ok {
			value = fallbackPDFCode(byte(code))
		}
		if control.consumeOperations(int64(len(value))) != nil {
			return nil, control.check()
		}
		for _, r := range value {
			set[r] = true
		}
	}
	if len(set) >= 65535 {
		return nil, fmt.Errorf("Type3 Unicode 字形数量过多")
	}
	runes := make([]rune, 0, len(set))
	for r := range set {
		runes = append(runes, r)
	}
	sort.Slice(runes, func(i, j int) bool { return runes[i] < runes[j] })
	count := len(runes) + 1
	u16 := func(b []byte, o int, v uint16) { binary.BigEndian.PutUint16(b[o:], v) }
	u32 := func(b []byte, o int, v uint32) { binary.BigEndian.PutUint32(b[o:], v) }
	tables := map[string][]byte{}
	head := make([]byte, 54)
	u32(head, 0, 0x10000)
	u32(head, 4, 0x10000)
	u32(head, 12, 0x5f0f3cf5)
	u16(head, 18, 1000)
	u16(head, 46, 8)
	u16(head, 48, 2)
	tables["head"] = head
	hhea := make([]byte, 36)
	u32(hhea, 0, 0x10000)
	u16(hhea, 4, 1000)
	u16(hhea, 6, 0xff38)
	u16(hhea, 10, 1000)
	u16(hhea, 18, 1)
	u16(hhea, 34, uint16(count))
	tables["hhea"] = hhea
	maxp := make([]byte, 32)
	u32(maxp, 0, 0x10000)
	u16(maxp, 4, uint16(count))
	u16(maxp, 14, 2)
	tables["maxp"] = maxp
	tables["glyf"] = make([]byte, 4)
	tables["loca"] = make([]byte, (count+1)*2)
	hmtx := make([]byte, count*4)
	defaultAdvance := source.defaultW
	if source.widths == nil && defaultAdvance == 0 {
		defaultAdvance = 1000
	}
	if source.type3 != nil {
		defaultAdvance *= math.Hypot(source.type3.matrix[0], source.type3.matrix[1]) * 1000
	}
	width := func(v float64) uint16 {
		if !calculatorFinite(v) {
			return 1000
		}
		return uint16(math.Round(math.Max(0, math.Min(math.MaxUint16, v))))
	}
	u16(hmtx, 0, width(defaultAdvance))
	maxAdvance := width(defaultAdvance)
	for i, r := range runes {
		advance, ok := advances[r]
		if !ok {
			advance = defaultAdvance
			if source.type3 == nil && source.sfnt != nil && source.sfnt.Head != nil && source.sfnt.Head.UnitsPerEm > 0 {
				if glyph := source.sfnt.GlyphIndex(r); glyph != 0 {
					advance = float64(source.sfnt.GlyphAdvance(glyph)) * 1000 / float64(source.sfnt.Head.UnitsPerEm)
				}
			}
		}
		w := width(advance)
		u16(hmtx, (i+1)*4, w)
		maxAdvance = max(maxAdvance, w)
	}
	u16(hhea, 10, maxAdvance)
	tables["hmtx"] = hmtx
	cmap := make([]byte, 12+16+len(runes)*12)
	u16(cmap, 2, 1)
	u16(cmap, 4, 3)
	u16(cmap, 6, 10)
	u32(cmap, 8, 12)
	u16(cmap, 12, 12)
	u32(cmap, 16, uint32(len(cmap)-12))
	u32(cmap, 24, uint32(len(runes)))
	for i, r := range runes {
		o := 28 + i*12
		u32(cmap, o, uint32(r))
		u32(cmap, o+4, uint32(r))
		u32(cmap, o+8, uint32(i+1))
	}
	tables["cmap"] = cmap
	nameString := pdfSemanticFontFamily
	name := make([]byte, 6+4*12+len(nameString)*2)
	u16(name, 2, 4)
	u16(name, 4, 54)
	for i, id := range []uint16{1, 2, 4, 6} {
		o := 6 + i*12
		u16(name, o, 3)
		u16(name, o+2, 1)
		u16(name, o+4, 0x409)
		u16(name, o+6, id)
		u16(name, o+8, uint16(len(nameString)*2))
	}
	for i, r := range nameString {
		u16(name, 54+i*2, uint16(r))
	}
	tables["name"] = name
	post := make([]byte, 32)
	u32(post, 0, 0x30000)
	tables["post"] = post
	os2 := make([]byte, 78)
	u16(os2, 2, 1000)
	u16(os2, 4, 400)
	u16(os2, 6, 5)
	copy(os2[58:], "OFD ")
	u16(os2, 62, 0x40)
	u16(os2, 66, 0xffff)
	u16(os2, 68, 1000)
	u16(os2, 70, 0xff38)
	u16(os2, 74, 1000)
	u16(os2, 76, 200)
	tables["OS/2"] = os2
	tags := make([]string, 0, len(tables))
	for tag := range tables {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	size := 12 + len(tags)*16
	for _, tag := range tags {
		size += (len(tables[tag]) + 3) &^ 3
	}
	if control != nil && int64(size) > control.options.Limits.MaxFontBytes {
		return nil, control.limit("font_bytes", int64(size), control.options.Limits.MaxFontBytes)
	}
	data := make([]byte, size)
	u32(data, 0, 0x10000)
	u16(data, 4, uint16(len(tags)))
	power, selector := 1, 0
	for power*2 <= len(tags) {
		power *= 2
		selector++
	}
	u16(data, 6, uint16(power*16))
	u16(data, 8, uint16(selector))
	u16(data, 10, uint16(len(tags)*16-power*16))
	offset, headOffset := 12+len(tags)*16, 0
	checksum := func(b []byte) uint32 {
		var s uint32
		for i := 0; i < len(b); i += 4 {
			var v [4]byte
			copy(v[:], b[i:min(i+4, len(b))])
			s += binary.BigEndian.Uint32(v[:])
		}
		return s
	}
	for i, tag := range tags {
		table := tables[tag]
		entry := 12 + i*16
		copy(data[entry:], tag)
		u32(data, entry+4, checksum(table))
		u32(data, entry+8, uint32(offset))
		u32(data, entry+12, uint32(len(table)))
		copy(data[offset:], table)
		if tag == "head" {
			headOffset = offset
		}
		offset += (len(table) + 3) &^ 3
	}
	u32(data, headOffset+8, 0xb1b0afba-checksum(data))
	return data, nil
}

// 空轮廓也需要源字体的水平字宽：固定全角宽会让比例文字的提取框互相
// 重叠，导致阅读器把同一基线上的单词拆行。实际位置仍由 TextCode 增量控制。
func pdfSemanticAdvances(source pdfFontInfo, control *conversionControl) (map[rune]float64, error) {
	type candidate struct {
		code   uint64
		width  float64
		single bool
	}
	selected := make(map[rune]candidate)
	add := func(code uint64, value string) error {
		if err := control.consumeOperations(int64(len(value)) + 1); err != nil {
			return err
		}
		count := utf8.RuneCountInString(value)
		if count == 0 {
			return nil
		}
		advance := pdfCodeWidth(code, source) / float64(count)
		if source.type3 != nil {
			advance *= math.Hypot(source.type3.matrix[0], source.type3.matrix[1]) * 1000
		}
		if !calculatorFinite(advance) {
			return nil
		}
		for _, r := range value {
			old, found := selected[r]
			// 一对一映射优先于连字拆分；同一 Unicode 的不同源代码按最小
			// 代码确定参考字宽，保证 map 遍历顺序不影响输出和字体复用。
			if !found || (count == 1 && !old.single) || ((count == 1) == old.single && code < old.code) {
				selected[r] = candidate{code: code, width: advance, single: count == 1}
			}
		}
		return nil
	}
	if source.encodingCMap == nil {
		for code, value := range source.toUnicode {
			if err := add(uint64(code), value); err != nil {
				return nil, err
			}
		}
		if source.codeBytes != 2 && source.predefined == nil && (source.widths != nil || source.defaultW != 0) {
			for code := uint32(0); code < 256; code++ {
				if _, found := source.toUnicode[code]; !found {
					if err := add(uint64(code), fallbackPDFCode(byte(code))); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	for code, value := range source.encodedUnicode {
		if err := add(code, value); err != nil {
			return nil, err
		}
	}
	advances := make(map[rune]float64, len(selected))
	for r, c := range selected {
		advances[r] = c.width
	}
	return advances, nil
}

// 语义文字不需要整形或轮廓合并；显式增量逐字写成定位段，避免通用
// 渲染器合并文本串时把小幅 Tc/Tw/TJ 调整吸收到字体的自然步进中。
func pdfSemanticTextCodes(item creator.Text, control *conversionControl) ([]creator.TextCode, error) {
	positioned := false
	for _, code := range item.TextCodes {
		positioned = positioned || len(code.DeltaX) > 0 || len(code.DeltaY) > 0
	}
	if !positioned {
		return item.TextCodes, nil
	}
	codes := make([]creator.TextCode, 0, len(item.TextCodes))
	for _, code := range item.TextCodes {
		if len(code.DeltaX) == 0 && len(code.DeltaY) == 0 {
			codes = append(codes, code)
			continue
		}
		if err := control.consumeOperations(int64(len(code.Value))); err != nil {
			return nil, err
		}
		x, y := 0.0, item.Height
		if code.X != nil {
			x = *code.X
		}
		if code.Y != nil {
			y = *code.Y
		}
		index := 0
		for _, r := range code.Value {
			if index > 0 {
				if index-1 < len(code.DeltaX) {
					x += code.DeltaX[index-1]
				}
				if index-1 < len(code.DeltaY) {
					y += code.DeltaY[index-1]
				}
			}
			px, py := x, y
			codes = append(codes, creator.TextCode{Value: string(r), X: &px, Y: &py})
			index++
		}
	}
	return codes, nil
}

// 在单字图元合并之后固定语义层的位置，Type3 的逐字语义对象也会在合并时
// 重新形成 DeltaX。只处理本转换器生成的空轮廓字体，常规文字保留原有整形路径。
func positionPDFSemanticTexts(items []creator.Item, fonts []creator.Font, control *conversionControl) error {
	if err := control.consumeOperations(int64(len(fonts))); err != nil {
		return err
	}
	semantic := make(map[string]bool)
	for _, font := range fonts {
		if font.FamilyName == pdfSemanticFontFamily && len(font.Data) > 0 {
			semantic[font.Name] = true
		}
	}
	if len(semantic) == 0 {
		return control.check()
	}
	if err := control.consumeOperations(int64(len(items))); err != nil {
		return err
	}
	for i, item := range items {
		text, ok := item.(creator.Text)
		if !ok || !semantic[text.Font] {
			continue
		}
		codes, err := pdfSemanticTextCodes(text, control)
		if err != nil {
			return err
		}
		text.TextCodes = codes
		items[i] = text
	}
	return control.check()
}

// Tr=3/7 的不可见文字仍须保留往返 PDF 文字层。空轮廓字体允许后端按原生
// 文字输出，无需依赖透明颜色，也不会绘制字形。生成字体按内容摘要复用。
func (p *pdfInterpreter) appendText(item creator.Text, font pdfFontInfo) {
	for _, clip := range p.state.clips {
		if clip.empty {
			fill := false
			item.Fill = &fill
			item.Stroke = false
			break
		}
	}
	if item.Fill != nil && !*item.Fill && !item.Stroke {
		var ok bool
		item, ok = p.semanticText(item, font)
		if !ok {
			return
		}
	}
	p.appendItem(item)
}

func (p *pdfInterpreter) semanticText(item creator.Text, font pdfFontInfo) (creator.Text, bool) {
	mapping := make(map[uint32]string, len(font.toUnicode)+len(font.encodedUnicode)+1)
	for code, value := range font.toUnicode {
		mapping[code] = value
	}
	// 空轮廓字体只需要 Unicode 覆盖；把带代码长度的映射放入独立空闲键，
	// 避免同值不同长度的 PDF 字符代码覆盖彼此的文字。
	nextCode := uint32(256)
	for _, value := range font.encodedUnicode {
		for mapping[nextCode] != "" {
			nextCode++
		}
		mapping[nextCode] = value
		nextCode++
	}
	mapping[0] = item.Value
	data, err := pdfSemanticFont(mapping, font, p.control)
	if err != nil {
		p.report("text.semantic", err.Error())
		return item, false
	}
	digest := sha256.Sum256(data)
	name := fmt.Sprintf("pdf-semantic-%x", digest[:8])
	found := false
	for _, f := range p.document.Fonts {
		if bytes.Equal(f.Data, data) {
			name = f.Name
			found = true
			break
		}
	}
	if !found {
		p.document.Fonts = append(p.document.Fonts, creator.Font{Name: name, FamilyName: pdfSemanticFontFamily, Charset: "unicode", Format: "ttf", Data: data})
	}
	fill := true
	item.Fill = &fill
	item.Font = name
	item.CGTransforms = nil
	item.FillColor = &creator.Color{}
	return item, true
}
