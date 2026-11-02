package pdfcpuwriter

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"unicode/utf16"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	ctext "github.com/zc310/ofd/internal/thirdparty/canvas/text"
	cfont "github.com/zc310/ofd/internal/thirdparty/font"
)

// A distinct CID per (glyph, source text) preserves aliases and ligatures in
// ToUnicode without changing the supplied glyph ID or requiring a Unicode cmap.
type prototypeGlyph struct {
	id   uint16
	text string
}

type prototypeFont struct {
	font   *canvas.Font
	name   string
	dict   types.Dict
	glyphs []prototypeGlyph
	codes  map[prototypeGlyph]uint16
}

func (r *Document) font(font *canvas.Font) *prototypeFont {
	if existing := r.fonts[font]; existing != nil {
		return existing
	}
	f := &prototypeFont{
		font: font, name: fmt.Sprintf("F%d", len(r.fonts)), dict: types.Dict{},
		glyphs: []prototypeGlyph{{}}, codes: map[prototypeGlyph]uint16{},
	}
	r.fonts[font] = f
	r.fontOrder = append(r.fontOrder, f)
	r.fontResources[f.name] = r.object(f.dict)
	return f
}

// RenderText 绘制已整形的文字，保留字形及源文本语义。
func (r *Document) RenderText(text *canvas.Text, matrix canvas.Matrix) {
	r.renderText(text, matrix, nil, matrix)
}

func (r *Document) renderText(text *canvas.Text, matrix canvas.Matrix, sharedPaint *canvas.Paint, paintMatrix canvas.Matrix) {
	if !r.ready() || text == nil || text.Empty() {
		return
	}
	if text.WritingMode != canvas.HorizontalTB {
		r.err = fmt.Errorf("%w：竖排文字", ErrUnsupported)
		return
	}
	text.RenderDecorationsTo(r, matrix, 0)
	text.WalkSpans(func(x, y float64, span canvas.TextSpan) {
		if r.err != nil {
			return
		}
		if !span.IsText() || span.Face.FauxBold != 0 || span.Direction != ctext.LeftToRight || span.Rotation != ctext.NoRotation {
			r.err = fmt.Errorf("%w：嵌入对象、合成字重或非横向文字", ErrUnsupported)
			return
		}
		if !span.Face.Font.IsTrueType && !span.Face.Font.IsCFF {
			r.err = fmt.Errorf("%w：字体格式 %s", ErrUnsupported, span.Face.Font.Name())
			return
		}
		font := r.font(span.Face.Font)
		base := matrix.Translate(x, y).Shear(span.Face.FauxItalic, 0)
		r.content.WriteString("q\n")
		fill := span.Face.Fill
		if sharedPaint != nil {
			fill = *sharedPaint
		}
		if !fill.Has() {
			// Invisible text still carries semantics without changing visible ink.
			r.content.WriteString("BT 3 Tr\n")
		} else {
			r.paint(fill, false, paintMatrix)
			r.content.WriteString("BT 0 Tr\n")
		}
		fmt.Fprintf(&r.content, "/%s %.12g Tf\n", font.name, span.Face.Size)
		penX, penY := 0.0, 0.0
		for _, glyph := range span.Glyphs {
			key := prototypeGlyph{id: glyph.ID, text: glyph.Text}
			code, ok := font.codes[key]
			if !ok {
				limit := 65536
				if font.simpleCFF() {
					limit = 256
				}
				if len(font.glyphs) >= limit {
					r.err = fmt.Errorf("%w：CID数量超限", ErrUnsupported)
					return
				}
				code = uint16(len(font.glyphs))
				font.codes[key] = code
				font.glyphs = append(font.glyphs, key)
			}
			scale := span.Face.Size / float64(span.Face.Font.UnitsPerEm())
			pos := base.Translate(penX+float64(glyph.XOffset)*scale, penY+float64(glyph.YOffset)*scale)
			width := 4
			if font.simpleCFF() {
				width = 2
			}
			fmt.Fprintf(&r.content, "%.12g %.12g %.12g %.12g %.12g %.12g Tm <%0*X> Tj\n",
				pos[0][0], pos[1][0], pos[0][1], pos[1][1], pos[0][2], pos[1][2], width, code)
			penX += float64(glyph.XAdvance) * scale
			penY += float64(glyph.YAdvance) * scale
		}
		r.content.WriteString("ET\nQ\n")
	})
}

// RenderClippedText 在文字局部坐标定义的裁剪路径内绘制文字。
func (r *Document) RenderClippedText(text *canvas.Text, matrix canvas.Matrix, clip *canvas.Path) {
	r.clip(matrix, clip, func() { r.RenderText(text, matrix) })
}

func (f *prototypeFont) embed(r *Document) {
	if f.font.IsCFF {
		f.embedCFF(r)
		return
	}
	// Glyph subsetting is provided by the existing font dependency, not pdfcpu.
	ids := []uint16{0}
	remap := map[uint16]uint16{0: 0}
	for _, glyph := range f.glyphs {
		if _, ok := remap[glyph.id]; !ok {
			remap[glyph.id] = uint16(len(ids))
			ids = append(ids, glyph.id)
		}
	}
	subset, err := f.font.Subset(ids, cfont.SubsetOptions{Tables: cfont.KeepPDFTables})
	if err != nil {
		r.err = err
		return
	}
	data := subset.Write()
	file := r.fontProgram(data, "", true)
	name := types.Name("OFDPDF+Prototype" + f.name)
	descriptor := f.descriptor(r, name, "FontFile2", file)
	scale := 1000 / float64(f.font.UnitsPerEm())
	widths := make(types.Array, 0, len(f.glyphs))
	gids := make([]byte, 2*len(f.glyphs))
	for code, glyph := range f.glyphs {
		binary.BigEndian.PutUint16(gids[2*code:], remap[glyph.id])
		widths = append(widths, types.Float(float64(f.font.GlyphAdvance(glyph.id))*scale))
	}
	cidMap := r.stream(gids, nil)
	descendant := r.object(types.Dict{
		"Type": types.Name("Font"), "Subtype": types.Name("CIDFontType2"), "BaseFont": name,
		"CIDSystemInfo":  types.Dict{"Registry": types.StringLiteral("Adobe"), "Ordering": types.StringLiteral("Identity"), "Supplement": types.Integer(0)},
		"FontDescriptor": descriptor, "CIDToGIDMap": cidMap, "DW": types.Integer(1000),
		"W": types.Array{types.Integer(0), widths},
	})
	f.dict["Type"], f.dict["Subtype"], f.dict["BaseFont"] = types.Name("Font"), types.Name("Type0"), name
	f.dict["Encoding"], f.dict["DescendantFonts"] = types.Name("Identity-H"), types.Array{descendant}
	f.dict["ToUnicode"] = r.stream(f.toUnicode(), nil)
}

func (f *prototypeFont) descriptor(r *Document, name types.Name, fileKey string, file types.IndirectRef) types.IndirectRef {
	scale := 1000 / float64(f.font.UnitsPerEm())
	head, hhea := f.font.Head, f.font.Hhea
	return r.object(types.Dict{
		"Type": types.Name("FontDescriptor"), "FontName": name, "Flags": types.Integer(4),
		"FontBBox": types.Array{types.Float(float64(head.XMin) * scale), types.Float(float64(head.YMin) * scale),
			types.Float(float64(head.XMax) * scale), types.Float(float64(head.YMax) * scale)},
		"Ascent": types.Float(float64(hhea.Ascender) * scale), "Descent": types.Float(float64(hhea.Descender) * scale),
		"CapHeight": types.Float(float64(hhea.Ascender) * scale), "ItalicAngle": types.Float(0),
		"StemV": types.Integer(80), fileKey: file,
	})
}

func (f *prototypeFont) toUnicode() []byte {
	var b bytes.Buffer
	b.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n")
	b.WriteString("/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n")
	b.WriteString("/CMapName /OFDPrototypeUnicode def\n/CMapType 2 def\n1 begincodespacerange\n")
	width := 4
	if f.simpleCFF() {
		width = 2
		b.WriteString("<00> <FF>\n")
	} else {
		b.WriteString("<0000> <FFFF>\n")
	}
	b.WriteString("endcodespacerange\n")
	for first := 1; first < len(f.glyphs); first += 100 {
		last := min(first+100, len(f.glyphs))
		fmt.Fprintf(&b, "%d beginbfchar\n", last-first)
		for code := first; code < last; code++ {
			text := f.glyphs[code].text
			if text == "" {
				text = "\uFFFD"
			}
			fmt.Fprintf(&b, "<%0*X> <", width, code)
			for _, unit := range utf16.Encode([]rune(text)) {
				fmt.Fprintf(&b, "%04X", unit)
			}
			b.WriteString(">\n")
		}
		b.WriteString("endbfchar\n")
	}
	b.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\n")
	b.WriteString("end\n")
	b.WriteString("end\n")
	return b.Bytes()
}
