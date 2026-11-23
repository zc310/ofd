package fontrepair

import (
	"cmp"
	"slices"
)

type cmapMapping struct {
	code  uint32
	glyph uint16
}

func sortedMappings(characters map[uint32]uint16) []cmapMapping {
	ordered := make([]cmapMapping, 0, len(characters))
	for code, glyph := range characters {
		if glyph != 0 {
			ordered = append(ordered, cmapMapping{code: code, glyph: glyph})
		}
	}
	slices.SortFunc(ordered, func(a, b cmapMapping) int { return int(a.code) - int(b.code) })
	return ordered
}

// orderedCmapCharacters 读取唯一且完整有效的 Unicode format 12 子表。
// OpenType cmap §Format 12 要求段有序且不重叠，可线性读取而无需 map 与排序。
// 多个 Unicode 编码或损坏数据仍由 cmapCharacters 按既有优先级与容错规则处理。
func orderedCmapCharacters(b []byte) ([]cmapMapping, bool) {
	entries, err := encodings(b)
	if err != nil {
		return nil, false
	}
	var body []byte
	for _, e := range entries {
		if !unicodeEncoding(e) {
			continue
		}
		if body != nil || be.Uint16(e.data) != 12 {
			return nil, false
		}
		body = e.data
	}
	if len(body) < 16 {
		return nil, false
	}
	n := be.Uint32(body[12:])
	if uint64(n) > uint64((len(body)-16)/12) {
		return nil, false
	}
	size := 0
	var previous uint32
	for i := range n {
		record := body[16+12*i:]
		first, last, glyph := be.Uint32(record), be.Uint32(record[4:]), be.Uint32(record[8:])
		if first > last || last > 0x10ffff || i > 0 && first <= previous {
			return nil, false
		}
		previous = last
		if glyph <= 65535 {
			size += int(min(last-first+1, 65536-glyph))
		}
	}
	ordered := make([]cmapMapping, 0, size)
	for i := range n {
		record := body[16+12*i:]
		first, last, glyph := be.Uint32(record), be.Uint32(record[4:]), be.Uint32(record[8:])
		if glyph > 65535 {
			continue
		}
		last = min(last, first+65535-glyph)
		for code := first; code <= last; code++ {
			g := glyph + code - first
			if g != 0 && (code < 0xd800 || code > 0xdfff) {
				ordered = append(ordered, cmapMapping{code: code, glyph: uint16(g)})
			}
		}
	}
	return ordered, true
}

func (r *Repairer) characterGlyph(code uint32) uint16 {
	if r.characters != nil {
		return r.characters[code]
	}
	index, found := slices.BinarySearchFunc(r.ordered, code, func(m cmapMapping, code uint32) int {
		return cmp.Compare(m.code, code)
	})
	if found {
		return r.ordered[index].glyph
	}
	return 0
}

// overlayCharacterGroups 只排序本次新增/覆盖的映射，再与只读基础字符表线性合并。
// 保持显式映射优先、删除旧 Unicode 别名及保留已有 CID 地址的既有规则；
// 不在每次逐页补字时复制并重新排序整份 CJK cmap。
func (r *Repairer) overlayCharacterGroups(requested map[uint32]uint16, replaced map[uint16]struct{}) []cmapGroup {
	r.orderedOnce.Do(func() {
		if r.ordered == nil {
			r.ordered = sortedMappings(r.characters)
		}
	})
	for glyph := range replaced {
		address := uint32(GlyphRune(glyph))
		if r.characterGlyph(address) != 0 {
			continue
		}
		if _, explicit := requested[address]; !explicit {
			requested[address] = glyph
		}
	}
	changes := sortedMappings(requested)
	groups := make([]cmapGroup, 0, len(changes))
	next := 0
	for _, base := range r.ordered {
		for next < len(changes) && changes[next].code < base.code {
			change := changes[next]
			groups = appendCharacterGroup(groups, change.code, change.glyph)
			next++
		}
		if next < len(changes) && changes[next].code == base.code {
			groups = appendCharacterGroup(groups, base.code, changes[next].glyph)
			next++
			continue
		}
		if base.code < 0xf0000 {
			if _, remove := replaced[base.glyph]; remove {
				continue
			}
		}
		groups = appendCharacterGroup(groups, base.code, base.glyph)
	}
	for _, change := range changes[next:] {
		groups = appendCharacterGroup(groups, change.code, change.glyph)
	}
	return groups
}
