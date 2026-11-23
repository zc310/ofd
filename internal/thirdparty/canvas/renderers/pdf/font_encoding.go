package pdf

import "fmt"

type pdfGlyphText struct {
	glyph uint16
	text  string
	known bool
}

// pdfGlyphCacheEntry 用定长直接映射缓存减少重复字形的 map 哈希开销。
// 命中时仍比较字形、原文和已知标志，不合并同字形的不同原文。
type pdfGlyphCacheEntry struct {
	key  pdfGlyphText
	code uint16
	ok   bool
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

	cache [256]pdfGlyphCacheEntry
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
	slot := &s.cache[glyph&0xff]
	if slot.ok && slot.key == key {
		return slot.code
	}
	if code, ok := s.codes[key]; ok {
		slot.key, slot.code, slot.ok = key, code, true
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
	slot.key, slot.code, slot.ok = key, code, true
	return code
}

func (s *pdfFontSubsetter) List() []uint16 { return s.glyphs }

func (s *pdfFontSubsetter) Text(code int) (string, bool) {
	return s.texts[code], s.known[code]
}
