package pdfcpuwriter

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// ISO 32000-1 sections 9.6.6, 9.7.4.2 and 9.9:
// name-keyed CFF uses Type1C plus glyph-name Encoding; CID-keyed CFF uses
// CIDFontType0C plus a character-code-to-original-CID CMap. CIDToGIDMap belongs
// only to TrueType descendants. Keep the complete original CFF program so that
// FDSelect, local subroutines and nonidentity CID charsets cannot be reordered.
func (f *prototypeFont) simpleCFF() bool {
	if !f.font.IsCFF || f.font.CFF == nil {
		return false
	}
	_, _, _, cid := f.font.CFF.CIDSystemInfo() //nolint:dogsled // The branch needs only the CID-keyed flag, not the ROS strings.
	return !cid
}

func (f *prototypeFont) embedCFF(r *Document) {
	data := f.font.Tables["CFF "]
	if len(data) == 0 || f.font.CFF == nil || f.font.CFF.Version() != 1 {
		r.err = fmt.Errorf("%w：CFF2或缺少CFF程序", ErrUnsupported)
		return
	}
	programName, err := prototypeCFFProgramName(data)
	if err != nil {
		r.err = err
		return
	}
	name := types.Name(programName)
	subtype := types.Name("CIDFontType0C")
	if f.simpleCFF() {
		subtype = types.Name("Type1C")
	}
	file := r.fontProgram(data, string(subtype), false)
	descriptor := f.descriptor(r, name, "FontFile3", file)
	f.dict["Type"], f.dict["BaseFont"], f.dict["ToUnicode"] = types.Name("Font"), name, r.stream(f.toUnicode(), nil)
	if f.simpleCFF() {
		f.embedSimpleCFF(r, descriptor)
		return
	}
	f.embedCIDCFF(r, name, descriptor)
}

// The wrapper's SFNT name table may be synthesized by font repair. FontName must
// instead follow the embedded CFF's original Name INDEX (Adobe 5176 section 7).
func prototypeCFFProgramName(data []byte) (string, error) {
	if len(data) < 4 || int(data[2]) > len(data)-3 {
		return "", fmt.Errorf("%w：CFF Name INDEX截断", ErrUnsupported)
	}
	index := data[int(data[2]):]
	size := int(index[2])
	if binary.BigEndian.Uint16(index) != 1 || size < 1 || size > 4 || len(index) < 3+2*size {
		return "", fmt.Errorf("%w：CFF Name INDEX格式", ErrUnsupported)
	}
	start, end := uint32(0), uint32(0)
	for i := range size {
		start = start<<8 | uint32(index[3+i])
		end = end<<8 | uint32(index[3+size+i])
	}
	base := 3 + 2*size
	if start != 1 || end <= start || uint64(end)-1 > uint64(len(index)-base) {
		return "", fmt.Errorf("%w：CFF Name INDEX偏移", ErrUnsupported)
	}
	return string(index[base : base+int(end)-1]), nil
}

func (f *prototypeFont) embedSimpleCFF(r *Document, descriptor types.IndirectRef) {
	differences := types.Array{types.Integer(1)}
	widths := make(types.Array, 0, len(f.glyphs)-1)
	seen := map[string]uint16{}
	scale := 1000 / float64(f.font.UnitsPerEm())
	for _, glyph := range f.glyphs[1:] {
		name, ok := f.font.CFF.GlyphName(glyph.id)
		previous, duplicate := seen[name]
		if !ok || name == "" || (duplicate && previous != glyph.id) {
			r.err = fmt.Errorf("%w：CFF字形名缺失或重复，GID=%d", ErrUnsupported, glyph.id)
			return
		}
		seen[name] = glyph.id
		differences = append(differences, types.Name(name))
		widths = append(widths, types.Float(float64(f.font.GlyphAdvance(glyph.id))*scale))
	}
	f.dict["Subtype"], f.dict["FontDescriptor"] = types.Name("Type1"), descriptor
	f.dict["FirstChar"], f.dict["LastChar"], f.dict["Widths"] = types.Integer(1), types.Integer(len(f.glyphs)-1), widths
	f.dict["Encoding"] = types.Dict{"Type": types.Name("Encoding"), "Differences": differences}
}

func (f *prototypeFont) embedCIDCFF(r *Document, name types.Name, descriptor types.IndirectRef) {
	registry, ordering, supplement, _ := f.font.CFF.CIDSystemInfo()
	ros := types.Dict{"Registry": types.StringLiteral(registry), "Ordering": types.StringLiteral(ordering), "Supplement": types.Integer(supplement)}
	cids := make([]uint16, len(f.glyphs))
	widthByCID := map[uint16]types.Float{}
	scale := 1000 / float64(f.font.UnitsPerEm())
	for code, glyph := range f.glyphs {
		cid, ok := f.font.CFF.GlyphCID(glyph.id)
		if !ok {
			r.err = fmt.Errorf("%w：CFF缺少CID，GID=%d", ErrUnsupported, glyph.id)
			return
		}
		cids[code] = cid
		widthByCID[cid] = types.Float(float64(f.font.GlyphAdvance(glyph.id)) * scale)
	}
	ordered := make([]uint16, 0, len(widthByCID))
	for cid := range widthByCID {
		ordered = append(ordered, cid)
	}
	slices.Sort(ordered)
	widths := make(types.Array, 0, len(ordered)*2)
	for _, cid := range ordered {
		widths = append(widths, types.Integer(cid), types.Array{widthByCID[cid]})
	}
	descendant := r.object(types.Dict{
		"Type": types.Name("Font"), "Subtype": types.Name("CIDFontType0"), "BaseFont": name,
		"CIDSystemInfo": ros, "FontDescriptor": descriptor, "W": widths,
	})
	f.dict["Subtype"], f.dict["DescendantFonts"] = types.Name("Type0"), types.Array{descendant}
	f.dict["Encoding"] = r.stream(prototypeCIDEncoding(ros, cids), types.Dict{
		"Type": types.Name("CMap"), "CMapName": types.Name("OFDPrototypeCID"), "CIDSystemInfo": ros, "WMode": types.Integer(0),
	})
}

func prototypeCIDEncoding(ros types.Dict, cids []uint16) []byte {
	var b bytes.Buffer
	b.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n")
	fmt.Fprintf(&b, "/CIDSystemInfo %s def\n", ros.PDFString())
	b.WriteString("/CMapName /OFDPrototypeCID def\n/CMapType 1 def\n/WMode 0 def\n1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")
	for first := 1; first < len(cids); first += 100 {
		last := min(first+100, len(cids))
		fmt.Fprintf(&b, "%d begincidchar\n", last-first)
		for code := first; code < last; code++ {
			fmt.Fprintf(&b, "<%04X> %d\n", code, cids[code])
		}
		b.WriteString("endcidchar\n")
	}
	b.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\n")
	b.WriteString("end\n")
	b.WriteString("end\n")
	return b.Bytes()
}
