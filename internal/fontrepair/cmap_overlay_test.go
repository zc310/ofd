package fontrepair

import (
	"bytes"
	"maps"
	"math/rand/v2"
	"sync"
	"testing"
)

// referenceOverlay 保留优化前的整表复制/覆盖规则，作为独立的合并语义对照。
func referenceOverlay(base, requested map[uint32]uint16) map[uint32]uint16 {
	replaced := replacedGlyphs(requested)
	characters := make(map[uint32]uint16, len(base)+2*len(requested))
	for code, glyph := range base {
		if code < 0xf0000 {
			if _, remove := replaced[glyph]; remove {
				continue
			}
		}
		characters[code] = glyph
	}
	for glyph := range replaced {
		address := uint32(GlyphRune(glyph))
		if _, exists := characters[address]; !exists {
			characters[address] = glyph
		}
	}
	maps.Copy(characters, requested)
	return characters
}

func replacedGlyphs(requested map[uint32]uint16) map[uint16]struct{} {
	result := make(map[uint16]struct{}, len(requested))
	for _, glyph := range requested {
		result[glyph] = struct{}{}
	}
	return result
}

func TestOrderedCmapOverlayMatchesOriginalAliasAndAddressRules(t *testing.T) {
	base := map[uint32]uint16{
		0: 1, 'A': 1, 'B': 1, 'C': 2, '文': 3, 0x1f600: 4,
		uint32(GlyphRune(1)): 1, uint32(GlyphRune(2)): 7,
	}
	repairer := &Repairer{characters: base}
	before := maps.Clone(base)
	for _, requested := range []map[uint32]uint16{
		{}, {'A': 2}, {'X': 1}, {'文': 2, uint32(GlyphRune(2)): 4},
		{'A': 2, 'B': 1, 0x1f600: 3}, {uint32(GlyphRune(1)): 3, '新': 7},
	} {
		want := replaceCharacters(nil, referenceOverlay(base, requested))
		got := encodeCharacterGroups(nil, repairer.overlayCharacterGroups(maps.Clone(requested), replacedGlyphs(requested)))
		if !bytes.Equal(got, want) {
			t.Fatalf("映射覆盖、别名删除或已有 CID 地址保留规则改变：%v", requested)
		}
	}
	if !maps.Equal(base, before) {
		t.Fatal("补字修改了共享基础映射")
	}
}

func TestOrderedCmapOverlayRandomizedByteEquivalence(t *testing.T) {
	random := rand.New(rand.NewPCG(17, 29))
	for range 100 {
		base := make(map[uint32]uint16)
		for range 512 {
			base[uint32(random.IntN(0x110000))] = uint16(1 + random.IntN(200))
		}
		repairer := &Repairer{characters: base}
		for range 10 {
			requested := make(map[uint32]uint16)
			for range 20 {
				code := uint32(random.IntN(0x110000))
				if random.IntN(2) == 0 {
					code = uint32(GlyphRune(uint16(1 + random.IntN(200))))
				}
				requested[code] = uint16(1 + random.IntN(200))
			}
			want := replaceCharacters(nil, referenceOverlay(base, requested))
			got := encodeCharacterGroups(nil, repairer.overlayCharacterGroups(maps.Clone(requested), replacedGlyphs(requested)))
			if !bytes.Equal(got, want) {
				t.Fatal("线性合并与原整表算法的字节结果不同")
			}
		}
	}
}

func TestOrderedCmapOverlayInitializesOnceUnderConcurrentUpdates(t *testing.T) {
	base := map[uint32]uint16{'A': 1, 'B': 2, 'C': 3}
	repairer := &Repairer{characters: base}
	var workers sync.WaitGroup
	for i := range 16 {
		workers.Go(func() {
			requested := map[uint32]uint16{uint32('中' + i): uint16(i + 1)}
			want := replaceCharacters(nil, referenceOverlay(base, requested))
			got := encodeCharacterGroups(nil, repairer.overlayCharacterGroups(requested, replacedGlyphs(requested)))
			if !bytes.Equal(got, want) {
				t.Error("并发首次排序或补字改变了结果")
			}
		})
	}
	workers.Wait()
}

func BenchmarkCmapMappingUpdates(b *testing.B) {
	base := make(map[uint32]uint16)
	for i := range 32768 {
		base[uint32(0x4e00+i)] = uint16(1 + i%300)
	}
	requested := map[uint32]uint16{'文': 20, '字': 30}
	for _, original := range []bool{true, false} {
		name := "ordered_merge"
		if original {
			name = "full_map_sort"
		}
		b.Run(name, func(b *testing.B) {
			repairer := &Repairer{characters: base}
			b.ReportAllocs()
			for range b.N {
				if original {
					_ = replaceCharacters(nil, referenceOverlay(base, requested))
				} else {
					_ = encodeCharacterGroups(nil, repairer.overlayCharacterGroups(maps.Clone(requested), replacedGlyphs(requested)))
				}
			}
		})
	}
}
