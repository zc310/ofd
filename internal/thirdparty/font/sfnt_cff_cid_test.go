package font

import (
	"sort"
	"testing"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/test"
)

func TestParseCIDFontDICTLegacyOperators(t *testing.T) {
	// Integer operands encode PaintType=2, CharstringType=2, and StrokeWidth=10.
	dict, err := parseFontDICT([]byte{141, 12, 5, 141, 12, 6, 149, 12, 8, 139, 139, 18}, false)
	if err != nil {
		t.Fatal(err)
	}
	if dict.PrivateLength != 0 || dict.PrivateOffset != 0 {
		t.Fatalf("private dict = (%d, %d), want (0, 0)", dict.PrivateLength, dict.PrivateOffset)
	}
	if dict.PaintType != 2 || dict.CharstringType != 2 || dict.StrokeWidth != 10 {
		t.Fatalf("Font DICT operators = paint %d, charstring %d, stroke %g", dict.PaintType, dict.CharstringType, dict.StrokeWidth)
	}

	if _, err := parsePrivateDICT([]byte{139, 12, 15, 139, 12, 16}, false); err != nil {
		t.Fatalf("legacy Type 1 private operators: %v", err)
	}
}

// TestCFFCIDMultiFDSubset keeps subsetting working for CID CFF fonts with multiple
// Font DICTs. These fonts previously failed with "only single-font CFFs are supported",
// preventing PDF font embedding and turning text into non-copyable vector paths.
func TestCFFCIDMultiFDSubset(t *testing.T) {
	b, err := readFontFixture(t, "resources/cid-cff-subset.otf")
	test.Error(t, err)

	sfnt, err := ParseSFNT(b, 0)
	test.Error(t, err)
	if sfnt.CFF == nil || !sfnt.CFF.top.IsCID {
		t.Fatal("expected a CID CFF font")
	}
	if len(sfnt.CFF.fonts.private) < 2 {
		t.Fatalf("expected multiple Font DICTs, got %d", len(sfnt.CFF.fonts.private))
	}

	// Select .notdef and CID glyphs mapped through private-use characters.
	glyphs := []uint16{0}
	for _, cid := range []uint16{19, 97, 1063, 1720, 2169, 4605} {
		if g := sfnt.GlyphIndex(rune(0xF0000 + int(cid))); g != 0 {
			glyphs = append(glyphs, g)
		}
	}

	subset, err := sfnt.Subset(glyphs, SubsetOptions{Tables: KeepAllTables})
	test.Error(t, err)
	test.T(t, subset.NumGlyphs(), uint16(len(glyphs)))

	out := subset.Write()
	reparsed, err := ParseSFNT(out, 0)
	test.Error(t, err)
	if reparsed.CFF == nil || !reparsed.CFF.top.IsCID {
		t.Fatal("subset output is not a CID CFF")
	}
	if len(reparsed.CFF.fonts.private) != len(sfnt.CFF.fonts.private) {
		t.Fatalf("subset lost Font DICTs: got %d, want %d", len(reparsed.CFF.fonts.private), len(sfnt.CFF.fonts.private))
	}
	test.T(t, reparsed.NumGlyphs(), uint16(len(glyphs)))
}

func TestCFFCIDMultiFDLocalSubrSubset(t *testing.T) {
	b, err := readFontFixture(t, "resources/cid-cff-subset.otf")
	test.Error(t, err)

	sfnt, err := ParseSFNT(b, 0)
	test.Error(t, err)
	cff := sfnt.CFF
	fdCount := len(cff.fonts.private)
	if fdCount < 2 {
		t.Fatalf("expected multiple Font DICTs, got %d", fdCount)
	}

	localSubrs := make([]*cffINDEX, fdCount)
	for fd := range localSubrs {
		localSubrs[fd] = &cffINDEX{}
		for index := 0; index < 3; index++ {
			// A stem declaration is geometrically inert but gives each FD and index
			// distinct bytes so cross-FD index collisions are observable.
			localSubrs[fd].Add([]byte{byte(fd + 139), byte(index + 139), byte(cffHstem), byte(cffReturn)})
		}
	}

	firstGlyphByFD := make(map[uint16]uint16, fdCount)
	zeroFD, ok := cff.fonts.Index(0)
	if !ok {
		t.Fatal("missing Font DICT for .notdef")
	}
	charStrings := &cffINDEX{}
	for glyphID := uint16(0); glyphID < uint16(cff.charStrings.Len()); glyphID++ {
		fd, ok := cff.fonts.Index(uint32(glyphID))
		if !ok {
			t.Fatalf("missing Font DICT for glyph %d", glyphID)
		}
		if _, exists := firstGlyphByFD[fd]; !exists && glyphID != 0 {
			firstGlyphByFD[fd] = glyphID
		}

		// Different FDs deliberately reuse local-subroutine indexes. The frequently
		// called index 2 sorts ahead of index 0 or 1, forcing independent remapping.
		calls := []byte{}
		appendCall := func(index int) {
			calls = append(calls, byte(index-107+139), byte(cffCallsubr))
		}
		if glyphID != 0 {
			rareIndex := 1
			if fd == 0 {
				rareIndex = 0
			}
			appendCall(rareIndex)
			appendCall(2)
			appendCall(2)
			appendCall(2)
		}
		charString := append(calls, cff.charStrings.Get(glyphID)...)
		charStrings.Add(charString)
	}
	if len(firstGlyphByFD) < 2 {
		t.Fatal("test font does not map glyphs to multiple Font DICTs")
	}
	cff.charStrings = charStrings
	cff.fonts.localSubrs = localSubrs

	selectedFDs := make([]uint16, 0, 2)
	for fd := range firstGlyphByFD {
		selectedFDs = append(selectedFDs, fd)
		if len(selectedFDs) == 2 {
			break
		}
	}
	if len(selectedFDs) < 2 {
		t.Fatalf("test font does not map glyphs to multiple Font DICTs: %v", firstGlyphByFD)
	}
	sort.Slice(selectedFDs, func(i, j int) bool { return selectedFDs[i] < selectedFDs[j] })
	fdGlyphs := []uint16{firstGlyphByFD[selectedFDs[0]], firstGlyphByFD[selectedFDs[1]]}

	subset, err := sfnt.Subset([]uint16{0, fdGlyphs[0], fdGlyphs[1]}, SubsetOptions{Tables: KeepAllTables})
	test.Error(t, err)
	if subset.NumGlyphs() != 3 {
		t.Fatalf("subset glyph count = %d, want 3", subset.NumGlyphs())
	}

	reparsed, err := ParseSFNT(subset.Write(), 0)
	test.Error(t, err)
	for subsetGlyph, sourceFD := range []uint16{zeroFD, selectedFDs[0], selectedFDs[1]} {
		gotFD, ok := reparsed.CFF.fonts.Index(uint32(subsetGlyph))
		if !ok || gotFD != sourceFD {
			t.Fatalf("subset glyph %d Font DICT = %d, %v; want %d", subsetGlyph, gotFD, ok, sourceFD)
		}
		if err := reparsed.CFF.parseCharString(uint16(subsetGlyph), func(*parse.BinaryReader, int32, []int32) error { return nil }); err != nil {
			t.Fatalf("subset glyph %d has invalid local subroutine references: %v", subsetGlyph, err)
		}
	}
	for _, fd := range selectedFDs {
		if got := reparsed.CFF.fonts.localSubrs[fd].Len(); got != 2 {
			t.Fatalf("FD %d local subroutines = %d, want 2", fd, got)
		}
		rareIndex := 1
		if fd == 0 {
			rareIndex = 0
		}
		want := []byte{byte(fd + 139), byte(rareIndex + 139), byte(cffHstem), byte(cffReturn)}
		if got := reparsed.CFF.fonts.localSubrs[fd].Get(1); string(got) != string(want) {
			t.Fatalf("FD %d remapped local subroutine = %v, want %v", fd, got, want)
		}
	}
}

func TestCFFCIDMultiFDUnusedLocalSubrsAreDropped(t *testing.T) {
	b, err := readFontFixture(t, "resources/cid-cff-subset.otf")
	test.Error(t, err)
	sfnt, err := ParseSFNT(b, 0)
	test.Error(t, err)

	for fd := range sfnt.CFF.fonts.localSubrs {
		sfnt.CFF.fonts.localSubrs[fd] = &cffINDEX{}
		sfnt.CFF.fonts.localSubrs[fd].Add([]byte{byte(cffReturn)})
	}
	// The glyphs do not call their Local Subrs, so a valid subset has none.
	usedGlyph := uint16(1)
	subset, err := sfnt.Subset([]uint16{0, usedGlyph}, SubsetOptions{Tables: KeepAllTables})
	test.Error(t, err)
	for fd, subrs := range subset.CFF.fonts.localSubrs {
		if subrs.Len() != 0 {
			t.Fatalf("FD %d retained %d unused local subroutines", fd, subrs.Len())
		}
	}
}

func TestCFFCIDLocalSubrBiasChangeRewritesSameIndex(t *testing.T) {
	b, err := readFontFixture(t, "resources/cid-cff-subset.otf")
	test.Error(t, err)
	sfnt, err := ParseSFNT(b, 0)
	test.Error(t, err)
	cff := sfnt.CFF

	for fd := range cff.fonts.localSubrs {
		cff.fonts.localSubrs[fd] = &cffINDEX{}
		for range 1240 {
			cff.fonts.localSubrs[fd].Add([]byte{byte(cffReturn)})
		}
	}

	firstGlyphByFD := make(map[uint16]uint16)
	charStrings := &cffINDEX{}
	for glyphID := uint16(0); glyphID < uint16(cff.charStrings.Len()); glyphID++ {
		fd, ok := cff.fonts.Index(uint32(glyphID))
		if !ok {
			t.Fatalf("missing Font DICT for glyph %d", glyphID)
		}
		if glyphID != 0 {
			if _, exists := firstGlyphByFD[fd]; !exists {
				firstGlyphByFD[fd] = glyphID
			}
		}
		charString := append([]byte(nil), cff.charStrings.Get(glyphID)...)
		if glyphID != 0 {
			// Index zero has operand -1131 when the original bias is 1131.
			charString = append([]byte{254, 255, byte(cffCallsubr)}, charString...)
		}
		charStrings.Add(charString)
	}
	cff.charStrings = charStrings

	selectedFDs := make([]uint16, 0, 2)
	for fd := range firstGlyphByFD {
		selectedFDs = append(selectedFDs, fd)
		if len(selectedFDs) == 2 {
			break
		}
	}
	if len(selectedFDs) < 2 {
		t.Skip("fixture has fewer than two populated Font DICTs")
	}
	sort.Slice(selectedFDs, func(i, j int) bool { return selectedFDs[i] < selectedFDs[j] })
	subset, err := sfnt.Subset([]uint16{0, firstGlyphByFD[selectedFDs[0]], firstGlyphByFD[selectedFDs[1]]}, SubsetOptions{Tables: KeepAllTables})
	test.Error(t, err)
	for glyphID := uint16(0); glyphID < uint16(subset.CFF.charStrings.Len()); glyphID++ {
		if err := subset.CFF.parseCharString(glyphID, func(*parse.BinaryReader, int32, []int32) error { return nil }); err != nil {
			t.Fatalf("subset glyph %d has invalid calls after bias change: %v", glyphID, err)
		}
	}
}

func TestCFFCIDGlobalSubrLocalCallPreservesFontDictIndexes(t *testing.T) {
	b, err := readFontFixture(t, "resources/cid-cff-subset.otf")
	test.Error(t, err)
	sfnt, err := ParseSFNT(b, 0)
	test.Error(t, err)
	cff := sfnt.CFF

	fdCount := len(cff.fonts.private)
	localSubrs := make([]*cffINDEX, fdCount)
	for fd := range localSubrs {
		localSubrs[fd] = &cffINDEX{}
		for index := 0; index < 3; index++ {
			localSubrs[fd].Add([]byte{byte(fd + 139), byte(index + 139), byte(cffHstem), byte(cffReturn)})
		}
	}
	globalSubrs := &cffINDEX{}
	globalSubrs.Add([]byte{32, byte(cffCallsubr), byte(cffReturn)}) // call local subroutine 0
	cff.globalSubrs = globalSubrs
	cff.fonts.localSubrs = localSubrs

	firstGlyphByFD := make(map[uint16]uint16, fdCount)
	charStrings := &cffINDEX{}
	for glyphID := uint16(0); glyphID < uint16(cff.charStrings.Len()); glyphID++ {
		fd, ok := cff.fonts.Index(uint32(glyphID))
		if !ok {
			t.Fatalf("missing Font DICT for glyph %d", glyphID)
		}
		if glyphID != 0 {
			if _, exists := firstGlyphByFD[fd]; !exists {
				firstGlyphByFD[fd] = glyphID
			}
		}
		charString := append([]byte{32, byte(cffCallgsubr)}, cff.charStrings.Get(glyphID)...)
		charStrings.Add(charString)
	}
	cff.charStrings = charStrings

	selectedFDs := make([]uint16, 0, 2)
	for fd := range firstGlyphByFD {
		selectedFDs = append(selectedFDs, fd)
		if len(selectedFDs) == 2 {
			break
		}
	}
	if len(selectedFDs) < 2 {
		t.Skip("fixture has fewer than two populated Font DICTs")
	}
	sort.Slice(selectedFDs, func(i, j int) bool { return selectedFDs[i] < selectedFDs[j] })
	subset, err := sfnt.Subset([]uint16{0, firstGlyphByFD[selectedFDs[0]], firstGlyphByFD[selectedFDs[1]]}, SubsetOptions{Tables: KeepAllTables})
	test.Error(t, err)
	if subset.CFF.globalSubrs.Len() != 1 {
		t.Fatalf("global subroutine count = %d, want 1", subset.CFF.globalSubrs.Len())
	}
	for fd, subrs := range subset.CFF.fonts.localSubrs {
		if subrs.Len() != 3 {
			t.Fatalf("FD %d local subroutine count = %d, want preserved count 3", fd, subrs.Len())
		}
	}
	for glyphID := uint16(0); glyphID < uint16(subset.CFF.charStrings.Len()); glyphID++ {
		if err := subset.CFF.parseCharString(glyphID, func(*parse.BinaryReader, int32, []int32) error { return nil }); err != nil {
			t.Fatalf("subset glyph %d has invalid nested subroutine references: %v", glyphID, err)
		}
	}
}
