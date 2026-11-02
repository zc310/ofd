package font

import (
	"encoding/binary"
	"testing"

	"github.com/tdewolff/test"
)

// TestSFNTToleratesHmtxTrailingBytes checks that a font whose hmtx table has
// extra trailing bytes beyond what hhea/maxp require (e.g. numberOfHMetrics is
// short by one entry, as seen in some OFD subset fonts) is not rejected as a
// whole because of the harmless alignment padding.
func TestSFNTToleratesHmtxTrailingBytes(t *testing.T) {
	b, err := readFontFixture(t, "resources/DejaVuSerif.ttf")
	test.Error(t, err)

	numTables := int(binary.BigEndian.Uint16(b[4:6]))
	hmtx := -1
	var insertAt int
	for i := 0; i < numTables; i++ {
		off := 12 + i*16
		if string(b[off:off+4]) != "hmtx" {
			continue
		}
		tableOffset := int(binary.BigEndian.Uint32(b[off+8 : off+12]))
		tableLength := int(binary.BigEndian.Uint32(b[off+12 : off+16]))
		insertAt = tableOffset + tableLength
		binary.BigEndian.PutUint32(b[off+12:off+16], uint32(tableLength+2))
		hmtx = off
		break
	}
	if hmtx < 0 {
		t.Fatal("hmtx table not found")
	}

	inserted := append(append(append([]byte{}, b[:insertAt]...), 0, 0), b[insertAt:]...)
	for i := 0; i < numTables; i++ {
		off := 12 + i*16
		tableOffset := int(binary.BigEndian.Uint32(inserted[off+8 : off+12]))
		if tableOffset >= insertAt {
			binary.BigEndian.PutUint32(inserted[off+8:off+12], uint32(tableOffset+2))
		}
	}

	sfnt, err := ParseSFNT(inserted, 0)
	test.Error(t, err)
	test.T(t, sfnt.NumGlyphs(), uint16(3528))
}

// TestSFNTToleratesHeadTrailingBytes checks that a font whose head table has
// extra trailing bytes beyond the 54 bytes the specification defines (e.g.
// 56 bytes, as seen in some OFD subset fonts) is not rejected as a whole.
func TestSFNTToleratesHeadTrailingBytes(t *testing.T) {
	b, err := readFontFixture(t, "resources/DejaVuSerif.ttf")
	test.Error(t, err)

	numTables := int(binary.BigEndian.Uint16(b[4:6]))
	head := -1
	var insertAt int
	for i := 0; i < numTables; i++ {
		off := 12 + i*16
		if string(b[off:off+4]) != "head" {
			continue
		}
		tableOffset := int(binary.BigEndian.Uint32(b[off+8 : off+12]))
		tableLength := int(binary.BigEndian.Uint32(b[off+12 : off+16]))
		insertAt = tableOffset + tableLength
		binary.BigEndian.PutUint32(b[off+12:off+16], uint32(tableLength+2))
		head = off
		break
	}
	if head < 0 {
		t.Fatal("head table not found")
	}

	inserted := append(append(append([]byte{}, b[:insertAt]...), 0, 0), b[insertAt:]...)
	for i := 0; i < numTables; i++ {
		off := 12 + i*16
		tableOffset := int(binary.BigEndian.Uint32(inserted[off+8 : off+12]))
		if tableOffset >= insertAt {
			binary.BigEndian.PutUint32(inserted[off+8:off+12], uint32(tableOffset+2))
		}
	}

	sfnt, err := ParseSFNT(inserted, 0)
	test.Error(t, err)
	test.T(t, sfnt.NumGlyphs(), uint16(3528))
	test.T(t, sfnt.Head.UnitsPerEm, uint16(2048))
}

// TestSFNTToleratesUnsupportedCmapSubtable checks that a font carrying a legacy
// cmap subtable that cannot be decoded, such as the Macintosh format 2
// high-byte mapping, is not rejected as a whole when it also has a usable
// subtable. Such records are common in OFD subset fonts and the rendering only
// needs the Unicode subtable.
func TestSFNTToleratesUnsupportedCmapSubtable(t *testing.T) {
	b, err := readFontFixture(t, "resources/DejaVuSerif.ttf")
	test.Error(t, err)

	orig, err := ParseSFNT(b, 0)
	test.Error(t, err)
	glyphA := orig.Cmap.Get('A')
	test.T(t, glyphA, uint16(36))
	numSubtables := len(orig.Cmap.Subtables)

	// append a legacy format 2 subtable record, the way OFD subset fonts often
	// carry a Macintosh high-byte mapping next to a usable Unicode subtable
	cmap := orig.Tables["cmap"]
	numRecords := int(binary.BigEndian.Uint16(cmap[2:4]))
	header := 4 + 8*(numRecords+1) // the extra record shifts all subtables
	legacy := make([]byte, 518)
	binary.BigEndian.PutUint16(legacy[0:2], 2)   // format
	binary.BigEndian.PutUint16(legacy[2:4], 518) // length
	binary.BigEndian.PutUint16(legacy[4:6], 0)   // language

	legacyOffset := header + len(cmap) - (4 + 8*numRecords)
	for legacyOffset%4 != 0 {
		legacyOffset++
	}
	newCmap := make([]byte, legacyOffset+len(legacy))
	binary.BigEndian.PutUint16(newCmap[0:2], 0)
	binary.BigEndian.PutUint16(newCmap[2:4], uint16(numRecords+1))
	copy(newCmap[header:legacyOffset], cmap[4+8*numRecords:])
	for i := 0; i < numRecords; i++ {
		off := 4 + i*8
		copy(newCmap[off:off+8], cmap[off:off+8])
		binary.BigEndian.PutUint32(newCmap[off+4:off+8], binary.BigEndian.Uint32(cmap[off+4:off+8])+8)
	}
	off := 4 + numRecords*8
	binary.BigEndian.PutUint16(newCmap[off:off+2], 1)    // platformID: Macintosh
	binary.BigEndian.PutUint16(newCmap[off+2:off+4], 25) // encodingID
	binary.BigEndian.PutUint32(newCmap[off+4:off+8], uint32(legacyOffset))
	copy(newCmap[legacyOffset:], legacy)
	orig.Tables["cmap"] = newCmap

	sfnt, err := ParseSFNT(orig.Write(), 0)
	test.Error(t, err)
	test.T(t, sfnt.NumGlyphs(), orig.NumGlyphs())
	test.T(t, sfnt.Cmap.Get('A'), glyphA)
	test.T(t, len(sfnt.Cmap.Subtables), numSubtables)
	test.T(t, len(sfnt.Cmap.EncodingRecords), numRecords)
}

func TestSFNTDejaVuSerifTTF(t *testing.T) {
	b, err := readFontFixture(t, "resources/DejaVuSerif.ttf")
	test.Error(t, err)

	sfnt, err := ParseSFNT(b, 0)
	test.Error(t, err)

	test.T(t, sfnt.Head.UnitsPerEm, uint16(2048))
	test.T(t, sfnt.Hhea.Ascender, int16(1901))
	test.T(t, sfnt.Hhea.Descender, int16(-483))
	test.T(t, sfnt.OS2.SCapHeight, int16(1493)) // height of H glyph
	test.T(t, sfnt.Head.XMin, int16(-1576))
	test.T(t, sfnt.Head.YMin, int16(-710))
	test.T(t, sfnt.Head.XMax, int16(4312))
	test.T(t, sfnt.Head.YMax, int16(2272))

	id := sfnt.GlyphIndex(' ')
	contour, err := sfnt.Glyf.Contour(id)
	test.Error(t, err)
	test.T(t, contour.GlyphID, id)
	test.T(t, len(contour.XCoordinates), 0)
}

func TestSFNTWrite(t *testing.T) {
	b, err := readFontFixture(t, "resources/DejaVuSerif.ttf")
	test.Error(t, err)

	sfnt, err := ParseSFNT(b, 0)
	test.Error(t, err)

	b2 := sfnt.Write()
	sfnt2, err := ParseSFNT(b2, 0)
	test.Error(t, err)

	test.T(t, sfnt2.GlyphIndex('A'), sfnt.GlyphIndex('A'))
	test.T(t, sfnt2.GlyphIndex('B'), sfnt.GlyphIndex('B'))
	test.T(t, sfnt2.GlyphIndex('C'), sfnt.GlyphIndex('C'))

	//ioutil.WriteFile("out.otf", subset, 0644)
}

func TestSFNTSubset(t *testing.T) {
	b, err := readFontFixture(t, "resources/DejaVuSerif.ttf")
	test.Error(t, err)

	sfnt, err := ParseSFNT(b, 0)
	test.Error(t, err)

	sfntSubset, err := sfnt.Subset([]uint16{0, 3, 6, 36, 37, 38, 55, 131}, SubsetOptions{Tables: KeepAllTables}) // .notdef, space, #, A, B, C, T, Á
	test.Error(t, err)

	test.T(t, sfntSubset.NumGlyphs(), uint16(9)) // Á is a composite glyph containing two simple glyphs: 36 and 3452

	test.T(t, sfntSubset.GlyphIndex('A'), uint16(3))
	test.T(t, sfntSubset.GlyphIndex('B'), uint16(4))
	test.T(t, sfntSubset.GlyphIndex('C'), uint16(5))

	//ioutil.WriteFile("out.otf", subset, 0644)
}
