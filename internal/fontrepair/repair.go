// Package fontrepair 修复内嵌 SFNT 字体，并将独立 CFF 数据封装为 OTF。
// 二进制格式按 OpenType 1.9.1 和 Adobe 技术文档 5176、5177 实现；
// 规范依据与兼容策略见 README.md。
package fontrepair

import (
	"fmt"
	"slices"
	"unicode/utf8"
)

// GlyphMapping 将文档中的 Unicode 码位绑定到实际字形 ID。
type GlyphMapping struct {
	Rune  rune
	Glyph uint16
}

// GlyphRune 返回为内嵌字形引用预留的私有码位。
// 该码位仅用于寻址，文档原文另行保留。
func GlyphRune(glyphID uint16) rune { return 0xf0000 + rune(glyphID) }

// Repair 保留已有字体表，补齐缺失的 OS/2、name、post 和字形寻址 cmap 数据。
// 未知格式原样返回；已识别格式的结构损坏时，返回原数据和错误。
// 修复过程不修改输入字节。
func Repair(data []byte) ([]byte, error) {
	if isCFF(data) {
		out, err := wrapCFF(data)
		if err != nil {
			return data, err
		}
		return out, nil
	}
	if !sfntSignature(data) {
		return data, nil
	}
	d, err := readDirectory(data)
	if err != nil {
		return data, err
	}
	changed := false
	units := uint16(1000)
	if head := d.get("head"); len(head) >= 20 && be.Uint16(head[18:]) != 0 {
		units = be.Uint16(head[18:])
	}
	for _, tag := range []string{"OS/2", "name", "post"} {
		if d.get(tag) != nil {
			continue
		}
		var body []byte
		switch tag {
		case "OS/2":
			body = os2Table(units)
		case "name":
			body = nameTable()
		case "post":
			body = postTable()
		}
		d.put(tag, body)
		changed = true
	}
	if maxp := d.get("maxp"); len(maxp) >= 6 {
		count := be.Uint16(maxp[4:])
		if count != 0 && !hasGlyphAddress(d.get("cmap")) {
			body, err := withGlyphAddresses(d.get("cmap"), count)
			if err != nil {
				return data, fmt.Errorf("repair cmap: %w", err)
			}
			d.put("cmap", body)
			changed = true
		}
	}
	if !changed {
		return data, nil
	}
	return d.encode(), nil
}

// Repairer 缓存只读的字体表和字符映射，供同一字体反复修复时复用。
// WithGlyphs 支持并发调用；调用方须在修复器存活期间保持输入字节不变。
type Repairer struct {
	base       []byte
	font       directory
	characters map[uint32]uint16
	err        error
}

func NewRepairer(data []byte) *Repairer {
	r := &Repairer{}
	r.base, r.err = Repair(data)
	if r.err != nil || !sfntSignature(r.base) {
		return r
	}
	r.font, r.err = readDirectory(r.base)
	if r.err == nil {
		r.characters = cmapCharacters(r.font.get("cmap"))
	}
	return r
}

// RepairWithGlyphs 修复字体，并应用文档提供的权威 Unicode 到 GID 映射。
// 无效 Unicode 标量和超出 maxp.numGlyphs 范围的 GID 会被忽略。
// 结果与 NewRepairer(data).WithGlyphs 相同。
func RepairWithGlyphs(data []byte, mappings []GlyphMapping) ([]byte, error) {
	return NewRepairer(data).WithGlyphs(mappings)
}

func (r *Repairer) WithGlyphs(mappings []GlyphMapping) ([]byte, error) {
	if r == nil {
		return nil, fmt.Errorf("nil font repairer")
	}
	if r.err != nil {
		return r.base, r.err
	}
	if r.characters == nil || len(mappings) == 0 {
		return slices.Clone(r.base), nil
	}
	requested := make(map[uint32]uint16, len(mappings))
	var glyphCount uint16
	if maxp := r.font.get("maxp"); len(maxp) >= 6 {
		glyphCount = be.Uint16(maxp[4:])
	}
	for _, m := range mappings {
		if m.Rune > 0 && utf8.ValidRune(m.Rune) && m.Glyph != 0 && m.Glyph < glyphCount {
			requested[uint32(m.Rune)] = m.Glyph
		}
	}
	if len(requested) == 0 {
		return slices.Clone(r.base), nil
	}
	// 先移除显式指定字形的旧别名，再统一应用全部请求映射，
	// 避免 map 遍历顺序影响结果。
	byGlyph := make(map[uint16]struct{}, len(requested))
	for _, glyph := range requested {
		byGlyph[glyph] = struct{}{}
	}
	characters := make(map[uint32]uint16, len(r.characters)+2*len(requested))
	for code, glyph := range r.characters {
		if code < 0xf0000 {
			if _, replaced := byGlyph[glyph]; replaced {
				continue
			}
		}
		characters[code] = glyph
	}
	for glyph := range byGlyph {
		characters[uint32(GlyphRune(glyph))] = glyph
	}
	for code, glyph := range requested {
		characters[code] = glyph
	}
	d := r.font
	d.tables = slices.Clone(d.tables)
	entries, err := encodings(d.get("cmap"))
	if err != nil {
		return slices.Clone(r.base), err
	}
	// 保留异体选择符子表。其他编码可能让字体消费者在回退查询时命中旧别名，
	// 因此将权威映射统一写入一个 Unicode 子表。
	entries = slices.DeleteFunc(entries, func(e encoding) bool { return be.Uint16(e.data) != 14 })
	d.put("cmap", replaceCharacters(entries, characters))
	return d.encode(), nil
}
