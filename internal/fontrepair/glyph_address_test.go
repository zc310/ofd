package fontrepair

import "testing"

func TestContainsGlyphRuneMatchesEntireAddressRange(t *testing.T) {
	for _, glyph := range []uint16{0, 1, 0xfffd, 0xfffe, 0xffff} {
		value := "before" + string(GlyphRune(glyph)) + "after"
		if !ContainsGlyphRune(value) {
			t.Errorf("未识别字形 %d 的地址 %U", glyph, GlyphRune(glyph))
		}
	}
	for _, value := range []string{"", "正常文本 ABC", string(rune(0xeffff)), string(rune(0x100000))} {
		if ContainsGlyphRune(value) {
			t.Errorf("普通文本 %q 被误判为字形地址", value)
		}
	}
}
