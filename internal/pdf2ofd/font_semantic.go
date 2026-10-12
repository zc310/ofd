package pdf2ofd

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/zc310/ofd/pkg/creator"
)

// pdfSemanticFont 构造无笔画的合法 TrueType 字体。每个 Unicode 码位使用不同
// glyph ID，PDF 子集化的 ToUnicode 才不会把多个字符合并成同一个字符。
func pdfSemanticFont(mapping map[uint16]string, control *conversionControl) ([]byte, error) {
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
		value, ok := mapping[uint16(code)]
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
	for i := 0; i < count; i++ {
		u16(hmtx, i*4, 1000)
	}
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
	nameString := "PDFType3Text"
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
		mapping := make(map[uint16]string, len(font.toUnicode)+1)
		for code, value := range font.toUnicode {
			mapping[code] = value
		}
		mapping[0] = item.Value
		data, err := pdfSemanticFont(mapping, p.control)
		if err != nil {
			p.report("text.semantic", err.Error())
			return
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
			p.document.Fonts = append(p.document.Fonts, creator.Font{Name: name, FamilyName: "PDFType3Text", Charset: "unicode", Format: "ttf", Data: data})
		}
		fill := true
		item.Fill = &fill
		item.Font = name
		item.CGTransforms = nil
	}
	p.appendItem(item)
}
