package fontrepair

import "slices"

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

// overlayCharacterGroups 只排序本次新增/覆盖的映射，再与只读基础字符表线性合并。
// 保持显式映射优先、删除旧 Unicode 别名及保留已有 CID 地址的既有规则；
// 不在每次逐页补字时复制并重新排序整份 CJK cmap。
func (r *Repairer) overlayCharacterGroups(requested map[uint32]uint16, replaced map[uint16]struct{}) []cmapGroup {
	r.orderedOnce.Do(func() { r.ordered = sortedMappings(r.characters) })
	for glyph := range replaced {
		address := uint32(GlyphRune(glyph))
		if _, exists := r.characters[address]; exists {
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
