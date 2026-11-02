package pdf

import "fmt"

type pdfGlyphText struct {
	glyph uint16
	text  string
	known bool
}

// A PDF character code identifies both the drawn glyph and its source text.
// Different Unicode strings can share a CFF glyph (or a ligature), so a GID
// alone cannot identify the character for ToUnicode.
type pdfFontSubsetter struct {
	codes  map[pdfGlyphText]uint16
	glyphs []uint16
	texts  []string
	known  []bool
	err    error
}

func newPDFFontSubsetter() *pdfFontSubsetter {
	s := &pdfFontSubsetter{codes: map[pdfGlyphText]uint16{}}
	s.Get(0)
	return s
}

func (s *pdfFontSubsetter) Get(glyph uint16, source ...string) uint16 {
	key := pdfGlyphText{glyph: glyph}
	if len(source) > 0 {
		key.text, key.known = source[0], true
	}
	if code, ok := s.codes[key]; ok {
		return code
	}
	if len(s.glyphs) >= 65536 {
		s.err = fmt.Errorf("too many character codes for one PDF font")
		return 0
	}
	code := uint16(len(s.glyphs))
	s.codes[key] = code
	s.glyphs = append(s.glyphs, glyph)
	s.texts = append(s.texts, key.text)
	s.known = append(s.known, key.known)
	return code
}

func (s *pdfFontSubsetter) List() []uint16 { return s.glyphs }

func (s *pdfFontSubsetter) Text(code int) (string, bool) {
	return s.texts[code], s.known[code]
}
