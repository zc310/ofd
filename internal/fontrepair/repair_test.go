package fontrepair

import (
	"bytes"
	"slices"
	"sync"
	"testing"

	fontparser "github.com/zc310/font"
	"golang.org/x/image/font/gofont/goregular"
)

func TestSFNTRepairPreservesOutlinesAndChecksums(t *testing.T) {
	d, err := readDirectory(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	d.tables = slices.DeleteFunc(d.tables, func(t table) bool { return t.tag == "OS/2" || t.tag == "name" || t.tag == "post" })
	input := d.encode()
	saved := slices.Clone(input)
	fixed, err := Repair(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(input, saved) {
		t.Fatal("input mutated")
	}
	if checksum(fixed) != 0xb1b0afba {
		t.Fatalf("font checksum = %08x", checksum(fixed))
	}
	parsed, err := fontparser.ParseSFNT(fixed, 0)
	if err != nil {
		t.Fatal(err)
	}
	original, err := fontparser.ParseSFNT(goregular.TTF, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range "Abg中" {
		if parsed.GlyphIndex(r) != original.GlyphIndex(r) {
			t.Fatalf("mapping changed for %c", r)
		}
	}
	for g := uint16(1); g < parsed.NumGlyphs(); g++ {
		if parsed.GlyphIndex(GlyphRune(g)) != g {
			t.Fatalf("missing glyph address %d", g)
		}
	}
	rebuilt, _ := readDirectory(fixed)
	for _, tag := range []string{"glyf", "loca", "hmtx", "hhea"} {
		if !bytes.Equal(d.get(tag), rebuilt.get(tag)) {
			t.Fatalf("%s changed", tag)
		}
	}
	for _, tab := range rebuilt.tables {
		body := slices.Clone(tab.data)
		if tab.tag == "head" {
			clear(body[8:12])
		}
		if tab.sum != checksum(body) {
			t.Fatalf("invalid %s checksum", tab.tag)
		}
	}
	again, err := Repair(fixed)
	if err != nil || !bytes.Equal(fixed, again) {
		t.Fatalf("repair is not idempotent: %v", err)
	}
}

func TestExplicitMappingsOverrideAliasesAndRemainConcurrent(t *testing.T) {
	original, err := fontparser.ParseSFNT(goregular.TTF, 0)
	if err != nil {
		t.Fatal(err)
	}
	a, b := original.GlyphIndex('A'), original.GlyphIndex('B')
	mappings := []GlyphMapping{{Rune: '中', Glyph: a}, {Rune: GlyphRune(a), Glyph: b}, {Rune: '文', Glyph: b}}
	r := NewRepairer(goregular.TTF)
	fixed, err := r.WithGlyphs(mappings)
	if err != nil {
		t.Fatal(err)
	}
	f, err := fontparser.ParseSFNT(fixed, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range mappings {
		if got := f.GlyphIndex(m.Rune); got != m.Glyph {
			t.Fatalf("%U = %d, want %d", m.Rune, got, m.Glyph)
		}
	}
	if f.GlyphIndex('A') != 0 || f.GlyphIndex('B') != 0 {
		t.Fatal("stale Unicode aliases retained")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			got, e := r.WithGlyphs(mappings)
			if e != nil || !bytes.Equal(got, fixed) {
				t.Errorf("concurrent repair differs: %v", e)
			}
		})
	}
	wg.Wait()
	whole, err := RepairWithGlyphs(goregular.TTF, mappings)
	if err != nil || !bytes.Equal(whole, fixed) {
		t.Fatalf("cached repair differs: %v", err)
	}
	if r.font.get("glyf") == nil {
		t.Fatal("missing outlines")
	}
}

func TestRepairKeepsVariationSelectorSubtable(t *testing.T) {
	d, err := readDirectory(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := encodings(d.get("cmap"))
	if err != nil {
		t.Fatal(err)
	}
	variation := make([]byte, 10)
	be.PutUint16(variation, 14)
	be.PutUint32(variation[2:], 10)
	entries = append(entries, encoding{0, 5, variation})
	d.put("cmap", replaceCharacters(entries, cmapCharacters(d.get("cmap"))))
	input := d.encode()
	for _, explicit := range []bool{false, true} {
		var fixed []byte
		if explicit {
			fixed, err = RepairWithGlyphs(input, []GlyphMapping{{Rune: '中', Glyph: 1}})
		} else {
			fixed, err = Repair(input)
		}
		if err != nil {
			t.Fatal(err)
		}
		result, e := readDirectory(fixed)
		if e != nil {
			t.Fatal(e)
		}
		subtables, e := encodings(result.get("cmap"))
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, subtable := range subtables {
			if subtable.platform == 0 && subtable.id == 5 && bytes.Equal(subtable.data, variation) {
				found = true
			}
		}
		if !found {
			t.Fatalf("variation selectors lost (explicit=%v)", explicit)
		}
	}
}

// 合成 CFF 使用固定宽度的 DICT 整数，便于直接回填偏移，
// 测试数据无需复用生产序列化逻辑或外部字体文件。
func cffTestIndex(objects ...[]byte) []byte {
	if len(objects) == 0 {
		return []byte{0, 0}
	}
	b := make([]byte, 3+4*(len(objects)+1))
	be.PutUint16(b, uint16(len(objects)))
	b[2] = 4
	pos := uint32(1)
	for i, obj := range objects {
		be.PutUint32(b[3+4*i:], pos)
		pos += uint32(len(obj))
	}
	be.PutUint32(b[3+4*len(objects):], pos)
	for _, obj := range objects {
		b = append(b, obj...)
	}
	return b
}

func cffTestDict(op int, values ...int) []byte {
	b := make([]byte, 0, 5*len(values)+2)
	for _, v := range values {
		b = append(b, 29, byte(uint32(v)>>24), byte(uint32(v)>>16), byte(uint32(v)>>8), byte(v))
	}
	if op >= 0xc00 {
		return append(b, 12, byte(op-0xc00))
	}
	return append(b, byte(op))
}

func syntheticCFF(cid bool, orderings ...string) []byte {
	names := cffTestIndex([]byte("SpecificationFixture"))
	ordering := "GB1"
	if len(orderings) > 0 {
		ordering = orderings[0]
	}
	stringsIndex := cffTestIndex([]byte("Adobe"), []byte(ordering))
	global := cffTestIndex()
	// FontMatrix 的 0.0005 对应每 em 2000 个设计单位。
	matrix := []byte{30, 0x0a, 0x00, 0x05, 0xff, 139, 139, 30, 0x0a, 0x00, 0x05, 0xff, 139, 139, 12, 7}
	var top []byte
	if cid {
		top = append(top, cffTestDict(0xc1e, 391, 392, 5)...)
	}
	top = append(top, matrix...)
	var appendOffset = func(op int, values ...int) int {
		start := len(top)
		top = append(top, cffTestDict(op, values...)...)
		return start + 1
	}
	charsetRef := appendOffset(15, 0)
	charsRef := appendOffset(17, 0)
	var privateRef, fdRef, selectRef int
	if cid {
		fdRef = appendOffset(0xc24, 0)
		selectRef = appendOffset(0xc25, 0)
	} else {
		privateRef = appendOffset(18, 0, 0)
	}
	base := 4 + len(names) + len(cffTestIndex(top)) + len(stringsIndex) + len(global)
	charset := []byte{0, 0, 34, 0, 35}
	if cid {
		charset = []byte{0, 0x02, 0x44, 0x56, 0x0f}
	} // CID 分别为 580 和 22031。
	chars := cffTestIndex([]byte{14}, []byte{189, 139, 22, 14}, []byte{139, 22, 14})
	private := append(cffTestDict(20, 900), cffTestDict(21, 400)...)
	be.PutUint32(top[charsetRef:], uint32(base))
	be.PutUint32(top[charsRef:], uint32(base+len(charset)))
	tail := append(slices.Clone(charset), chars...)
	if cid {
		fd0, fd1 := cffTestDict(18, len(private), 0), cffTestDict(18, len(private), 0)
		fds := cffTestIndex(fd0, fd1)
		fdPos := base + len(tail)
		selectPos := fdPos + len(fds)
		privatePos := selectPos + 4
		be.PutUint32(fd0[6:], uint32(privatePos))
		be.PutUint32(fd1[6:], uint32(privatePos+len(private)))
		fds = cffTestIndex(fd0, fd1)
		be.PutUint32(top[fdRef:], uint32(fdPos))
		be.PutUint32(top[selectRef:], uint32(selectPos))
		tail = append(tail, fds...)
		tail = append(tail, 0, 0, 0, 1)
		tail = append(tail, private...)
		tail = append(tail, append(cffTestDict(20, 1200), cffTestDict(21, 600)...)...)
	} else {
		be.PutUint32(top[privateRef:], uint32(len(private)))
		be.PutUint32(top[privateRef+5:], uint32(base+len(tail)))
		tail = append(tail, private...)
	}
	b := []byte{1, 0, 4, 4}
	b = append(b, names...)
	b = append(b, cffTestIndex(top)...)
	b = append(b, stringsIndex...)
	b = append(b, global...)
	return append(b, tail...)
}

func TestCFFMatrixCharsetAndPerFDWidths(t *testing.T) {
	for _, cid := range []bool{false, true} {
		t.Run(map[bool]string{false: "names", true: "CID-multiple-FD"}[cid], func(t *testing.T) {
			input := syntheticCFF(cid)
			saved := slices.Clone(input)
			fixed, err := Repair(input)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(input, saved) {
				t.Fatal("CFF input mutated")
			}
			if checksum(fixed) != 0xb1b0afba {
				t.Fatal("invalid OTF checksum")
			}
			f, err := fontparser.ParseSFNT(fixed, 0)
			if err != nil {
				t.Fatal(err)
			}
			if f.UnitsPerEm() != 2000 {
				t.Fatalf("unitsPerEm = %d", f.UnitsPerEm())
			}
			d, _ := readDirectory(fixed)
			want := []uint16{900, 450, 900}
			if cid {
				want[2] = 1200
			}
			for g, w := range want {
				if got := be.Uint16(d.get("hmtx")[4*g:]); got != w {
					t.Fatalf("glyph %d width=%d, want %d", g, got, w)
				}
			}
			if cid {
				if f.GlyphIndex('︵') != 1 || f.GlyphIndex(GlyphRune(580)) != 1 || f.GlyphIndex(GlyphRune(22031)) != 2 {
					t.Fatal("CID mapping lost")
				}
				if got, ok := f.CFF.GlyphCID(2); !ok || got != 22031 {
					t.Fatalf("CID = %d, %v", got, ok)
				}
				explicit, err := RepairWithGlyphs(input, []GlyphMapping{{Rune: '中', Glyph: 580}})
				if err != nil {
					t.Fatal(err)
				}
				mapped, err := fontparser.ParseSFNT(explicit, 0)
				if err != nil || mapped.GlyphIndex('中') != 0 || mapped.GlyphIndex(GlyphRune(580)) != 1 || mapped.GlyphIndex('︵') != 1 {
					t.Fatalf("out-of-range GID changed sparse CID aliases: %v", err)
				}
			} else if f.GlyphIndex('A') != 1 || f.GlyphIndex('B') != 2 {
				t.Fatal("standard SID mapping lost")
			}
		})
	}
}

func TestType2SubroutineWidthAndCounterMask(t *testing.T) {
	program := []byte{32, 10, 14}
	subr := []byte{189, 139, 149, 1, 20, 0x80, 139, 22, 11}
	v := type2State{private: privateDict{nominalWidth: 400, defaultWidth: 900, subrs: [][]byte{subr}}, budget: 1000}
	if err := v.run(program, 0); err != nil {
		t.Fatal(err)
	}
	if v.width != 450 || subr[4] != 19 || subr[5] != 0x80 {
		t.Fatalf("width=%v subroutine=%x", v.width, subr)
	}
}

func TestAdobeIdentitySubsetKeepsGB1Compatibility(t *testing.T) {
	fixed, err := Repair(syntheticCFF(true, "Identity"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := fontparser.ParseSFNT(fixed, 0)
	if err != nil {
		t.Fatal(err)
	}
	if f.GlyphIndex('︵') != 1 {
		t.Fatal("Adobe-Identity GB1 fallback lost")
	}
}

func TestMalformedFontsAreBoundedAndReadOnly(t *testing.T) {
	for _, b := range [][]byte{nil, []byte("wOFF"), {0, 1, 0, 0}, {1, 0, 255, 4}, syntheticCFF(true)} {
		for i := 0; i <= len(b); i++ {
			input := slices.Clone(b[:i])
			saved := slices.Clone(input)
			_, _ = Repair(input)
			if !bytes.Equal(input, saved) {
				t.Fatal("malformed input mutated")
			}
		}
	}
}

func TestCFFIndexRejectsInteriorOffsetBeyondPayload(t *testing.T) {
	// 最后一个偏移落在缓冲区内，但中间偏移已越界。
	b := []byte{0, 2, 4, 0, 0, 0, 1, 0x30, 0x30, 0x30, 0x30, 0, 0, 0, 2, 14}
	if _, _, err := cffIndex(b, 0); err == nil {
		t.Fatal("out-of-bounds intermediate offset accepted")
	}
}

func FuzzRepair(f *testing.F) {
	f.Add(syntheticCFF(false))
	f.Add(syntheticCFF(true))
	f.Add(goregular.TTF)
	f.Add([]byte{0, 1, 0, 0})
	f.Fuzz(func(t *testing.T, input []byte) {
		saved := slices.Clone(input)
		_, _ = RepairWithGlyphs(input, []GlyphMapping{{Rune: '中', Glyph: 1}})
		if !bytes.Equal(input, saved) {
			t.Fatal("input mutated")
		}
	})
}
