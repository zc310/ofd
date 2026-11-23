package fontrepair

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"maps"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
)

// referenceGlyphAddresses 保留逐条写入 map 的原算法，独立核对连续段生成的字节结果。
func referenceGlyphAddresses(b []byte, count uint16) ([]byte, error) {
	entries, err := encodings(b)
	if err != nil {
		return nil, err
	}
	characters := cmapCharacters(b)
	if characters == nil {
		characters = make(map[uint32]uint16)
	}
	for glyph := uint32(1); glyph < uint32(count); glyph++ {
		characters[uint32(GlyphRune(uint16(glyph)))] = uint16(glyph)
	}
	return replaceCharacters(entries, characters), nil
}

func TestContinuousGlyphAddressesMatchOriginalMapExpansion(t *testing.T) {
	random := rand.New(rand.NewPCG(31, 47))
	for _, count := range []uint16{0, 1, 2, 200, 51835, 65535} {
		for range 10 {
			characters := map[uint32]uint16{
				0: 1, '文': 5, 0xeffff: 1, 0xf0000: 2, 0xf0001: 9,
				uint32(GlyphRune(count)): 7, 0xfffff: 3, 0x100000: 4, 0x10ffff: 5,
			}
			for range 100 {
				characters[uint32(random.IntN(0x110000))] = uint16(random.IntN(65536))
			}
			input := encodeCharacters(characters)
			before := bytes.Clone(input)
			want, err := referenceGlyphAddresses(input, count)
			if err != nil {
				t.Fatal(err)
			}
			got, err := withGlyphAddresses(input, count)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("字形数 %d 的连续地址段改变了 cmap 字节：%v", count, err)
			}
			if !bytes.Equal(input, before) {
				t.Fatal("生成字形地址修改了共享输入")
			}
		}
	}
}

func TestOrderedCmapLoadingMatchesMapParsingAndGlyphLookup(t *testing.T) {
	for _, groups := range [][]cmapGroup{
		{},
		{{0xf0001, 0xffffe, 1}},
		{{0, 5, 0}, {0xd7fe, 0xe002, 20}, {0x10fffe, 0x10ffff, 65535}},
		{{10, 20, 0xffffffff}, {0x10000, 0x10005, 65533}},
		{{'A', 'C', 1}, {0xf0001, 0xf0001, 9}, {0xf0050, 0xf0050, 2}},
	} {
		input := encodeCharacterGroups(nil, groups)
		before := bytes.Clone(input)
		ordered, ok := orderedCmapCharacters(input)
		if !ok {
			t.Fatal("有效且唯一的 format 12 未走有序读取")
		}
		want := cmapCharacters(input)
		if !slices.Equal(ordered, sortedMappings(want)) {
			t.Fatal("有序读取改变了 Unicode、GID 边界或无效标量的过滤")
		}
		repairer := &Repairer{ordered: ordered}
		for code, glyph := range want {
			if repairer.characterGlyph(code) != glyph {
				t.Fatalf("%U 的二分查询结果不同", code)
			}
		}
		for _, code := range []uint32{0, 7, 0xd800, 0xf0000, 0xfffff, 0x110000, 0xffffffff} {
			if repairer.characterGlyph(code) != want[code] {
				t.Fatalf("%U 的边界查询结果不同", code)
			}
		}
		var workers sync.WaitGroup
		for range 4 {
			workers.Go(func() {
				requested := map[uint32]uint16{'中': 2, 0xf0001: 3, 0xf0003: 4}
				got := encodeCharacterGroups(nil, repairer.overlayCharacterGroups(maps.Clone(requested), replacedGlyphs(requested)))
				want := replaceCharacters(nil, referenceOverlay(want, requested))
				if !bytes.Equal(got, want) {
					t.Error("并发有序补字改变了旧别名或私有地址覆盖规则")
				}
			})
		}
		workers.Wait()
		if !bytes.Equal(input, before) {
			t.Fatal("读取或补字修改了共享 cmap")
		}
	}
}

func TestOrderedCmapLoadingFallsBackForCompetingEncodingsAndMalformedGroups(t *testing.T) {
	valid := encodeCharacterGroups(nil, []cmapGroup{{'A', 'C', 1}})
	entries, err := encodings(valid)
	if err != nil {
		t.Fatal(err)
	}
	variation := make([]byte, 10)
	be.PutUint16(variation, 14)
	be.PutUint32(variation[2:], 10)
	multiple := encodeCharacterGroups([]encoding{{0, 4, entries[0].data}}, []cmapGroup{{'A', 'A', 7}})
	truncated := bytes.Clone(valid)
	be.PutUint32(truncated[24:], 2)
	for _, input := range [][]byte{
		nil, {0}, {0, 0, 0, 0}, truncated,
		multiple,
		encodeCharacterGroups([]encoding{{0, 5, variation}}, []cmapGroup{{'A', 'C', 1}}),
		encodeCharacterGroups(nil, []cmapGroup{{20, 10, 1}}),
		encodeCharacterGroups(nil, []cmapGroup{{0x110000, 0x110000, 1}}),
		encodeCharacterGroups(nil, []cmapGroup{{10, 20, 1}, {20, 30, 2}}),
		encodeCharacterGroups(nil, []cmapGroup{{10, 20, 1}, {1, 5, 2}}),
	} {
		if _, ok := orderedCmapCharacters(input); ok {
			t.Fatal("竞争编码或损坏的字符段不应绕过原解析逻辑")
		}
	}
	// 原算法优先使用完整 Unicode 子表，不能让新增快路径改变优先级。
	if got := cmapCharacters(multiple)['A']; got != 7 {
		t.Fatalf("全字符集映射优先级改变：%d", got)
	}
}

func introFontSources(tb testing.TB) map[string][]byte {
	tb.Helper()
	const input = "../../testdata/ofdrw/intro.ofd"
	archive, err := zip.OpenReader(input)
	if errors.Is(err, os.ErrNotExist) {
		tb.Skipf("缺少字体样例 %s", input)
	}
	if err != nil {
		tb.Fatal(err)
	}
	defer archive.Close()
	sources := make(map[string][]byte)
	for _, file := range archive.File {
		if !strings.HasPrefix(file.Name, "Doc_0/Res/font_") {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			tb.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		closeErr := reader.Close()
		if err != nil || closeErr != nil {
			tb.Fatal(errors.Join(err, closeErr))
		}
		sources[file.Name] = data
	}
	if len(sources) == 0 {
		tb.Fatal("样例中未找到内嵌字体")
	}
	return sources
}

func TestIntroOrderedFontLoadingMatchesOriginalMappingRepair(t *testing.T) {
	for name, data := range introFontSources(t) {
		t.Run(name, func(t *testing.T) {
			repairer := NewRepairer(data)
			if repairer.err != nil {
				t.Fatal(repairer.err)
			}
			reference := &Repairer{
				base: repairer.base, font: repairer.font,
				characters: cmapCharacters(repairer.font.get("cmap")),
			}
			for _, mappings := range [][]GlyphMapping{
				nil, {{'文', 1}, {'字', 2}}, {{GlyphRune(1), 2}, {'中', 65534}, {'A', 20}},
			} {
				got, err := repairer.WithGlyphReferences(mappings)
				want, wantErr := reference.WithGlyphReferences(mappings)
				if err != nil || wantErr != nil || !bytes.Equal(got, want) {
					t.Fatalf("有序加载改变了真实字体的修复字节或 CID 映射：%v / %v", err, wantErr)
				}
			}
			if strings.HasSuffix(name, "font_938_938.ttf") && (repairer.characters != nil || len(repairer.ordered) < 50000) {
				t.Fatal("大字体仍在展开并排序私有地址 map")
			}
		})
	}
}

func BenchmarkGlyphAddressConstruction(b *testing.B) {
	input := []byte{0, 0, 0, 0}
	for _, original := range []bool{true, false} {
		name := "continuous_group"
		if original {
			name = "map_expansion"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				var err error
				if original {
					_, err = referenceGlyphAddresses(input, 51835)
				} else {
					_, err = withGlyphAddresses(input, 51835)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCmapInitialCharacterLoading(b *testing.B) {
	input := encodeCharacterGroups(nil, []cmapGroup{{0xf0001, 0xf0000 + 51834, 1}})
	for _, original := range []bool{true, false} {
		name := "ordered_read"
		if original {
			name = "map_sort"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if original {
					_ = sortedMappings(cmapCharacters(input))
				} else if _, ok := orderedCmapCharacters(input); !ok {
					b.Fatal("有效的字符表未走有序读取")
				}
			}
		})
	}
}
