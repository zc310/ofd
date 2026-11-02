package canvas

import (
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/tdewolff/test"
	"github.com/zc310/ofd/internal/thirdparty/canvas/text"
)

func TestGlyphPathKeyIncludesGlyphOffsets(t *testing.T) {
	family := NewFontFamily("dejavu-serif")
	if err := family.LoadFont(goregular.TTF, 0, FontRegular); err != nil {
		t.Fatal(err)
	}
	face := family.Face(12.0*ptPerMm, Black, FontRegular, FontNormal)
	base := []text.Glyph{{ID: 1, XAdvance: 10, YAdvance: 20}}
	key := makeGlyphPathKey(face, base, face.PPEM(DefaultResolution))
	base[0].XOffset = 1
	if key == makeGlyphPathKey(face, base, face.PPEM(DefaultResolution)) {
		t.Fatal("glyph path key ignored XOffset")
	}
	base[0].XOffset = 0
	base[0].YOffset = 1
	if key == makeGlyphPathKey(face, base, face.PPEM(DefaultResolution)) {
		t.Fatal("glyph path key ignored YOffset")
	}
}

// The glyph path cache must live on the font, not in a package-level store. A
// shared store keyed by *Font keeps every font (together with its parsed glyph
// tables, tens of MB for a CJK font) alive until the entry is evicted, so
// opening one document after another grows memory without bound. This guards
// that the cache is released with the font and not shared between fonts.
func TestGlyphPathCacheIsPerFont(t *testing.T) {
	loadFace := func() *FontFace {
		family := NewFontFamily("dejavu-serif")
		if err := family.LoadFont(goregular.TTF, 0, FontRegular); err != nil {
			t.Fatal(err)
		}
		return family.Face(12.0*ptPerMm, Black, FontRegular, FontNormal)
	}

	first := loadFace()
	first.toPath(first.Glyphs("A"), first.PPEM(DefaultResolution))
	if first.Font.glyphCache.lru == nil || first.Font.glyphCache.lru.Len() == 0 {
		t.Fatal("glyph cache did not record the font's outline")
	}

	second := loadFace()
	if second.Font == first.Font {
		t.Fatal("loading twice should yield distinct font instances")
	}
	if second.Font.glyphCache.lru != nil && second.Font.glyphCache.lru.Len() != 0 {
		t.Fatal("fonts share the glyph path cache")
	}
}

func TestFontFamily(t *testing.T) {
	family := NewFontFamily("dejavu-serif")
	if err := family.LoadFont(goregular.TTF, 0, FontRegular); err != nil {
		test.Error(t, err)
	}

	face := family.Face(12.0*ptPerMm, Black, FontRegular, FontNormal)
	test.Float(t, face.FauxBold, 0.0)
	test.T(t, face.Style.CSS(), 400)

	face = family.Face(12.0*ptPerMm, Black, FontBold|FontItalic, FontNormal)
	test.Float(t, face.FauxBold, 0.02)
	test.Float(t, face.FauxItalic, 0.3)
	test.T(t, face.Style.CSS(), 700)

	//face = family.Face(12.0*ptPerMm, Black, FontBold|FontItalic, FontSubscript)
	//test.T(t, face.YOffset, int32(0))
	//test.Float(t, face.FauxBold, 0.48*0.583)
	//test.Float(t, face.FauxItalic, 0.3)
	//test.T(t, face.Style.CSS(), 1000)
}

func TestFontFace(t *testing.T) {
	family := NewFontFamily("dejavu-serif")
	if err := family.LoadFontFile(externalMetricFont(t), FontRegular); err != nil {
		test.Error(t, err)
	}
	pt := ptPerMm * float64(family.fonts[FontRegular].Head.UnitsPerEm)
	face := family.Face(pt, Black, FontRegular, FontNormal)

	metrics := face.Metrics()
	test.Float(t, face.Size, 2048)
	test.Float(t, metrics.LineHeight, 2384)
	test.Float(t, metrics.Ascent, 1901)
	test.Float(t, metrics.Descent, 483)
	test.Float(t, metrics.XHeight, 1063)   // height of x
	test.Float(t, metrics.CapHeight, 1493) // height of H

	test.Float(t, face.TextWidth("T"), 1366)
	test.Float(t, face.TextWidth("AV"), face.TextWidth("A")+face.TextWidth("V")-102)

	//Epsilon = 1e-3
	//p, width, err := face.ToPath("AO")
	//test.Error(t, err)
	//test.T(t, p, MustParseSVGPath("M2.4062 3.1719L5.6094 3.1719L4.0156 7.3281L2.4062 3.1719zM-0.078125 0L-0.078125 0.625L0.70312 0.625L3.8125 8.75L4.7969 8.75L7.9219 0.625L8.7812 0.625L8.7812 0L5.6094 0L5.6094 0.625L6.5781 0.625L5.8438 2.5469L2.1562 2.5469L1.4375 0.625L2.3906 0.625L2.3906 0L-0.078125 0zM13.594 0.45312Q15.031 0.45312 15.766 1.4375Q16.5 2.4375 16.5 4.3594Q16.5 6.3125 15.766 7.2969Q15.031 8.2812 13.594 8.2812Q12.156 8.2812 11.422 7.2969Q10.688 6.3125 10.688 4.3594Q10.688 2.4375 11.422 1.4375Q12.156 0.45312 13.594 0.45312zM13.594 -0.17188Q12.703 -0.17188 11.953 0.125Q11.203 0.42188 10.641 0.98438Q9.9844 1.6406 9.6562 2.4688Q9.3438 3.3125 9.3438 4.3594Q9.3438 5.4219 9.6562 6.2656Q9.9844 7.0938 10.641 7.75Q11.219 8.3281 11.953 8.6094Q12.688 8.9062 13.594 8.9062Q15.5 8.9062 16.672 7.6562Q17.844 6.4062 17.844 4.3594Q17.844 3.3125 17.516 2.4688Q17.203 1.6406 16.547 0.98438Q15.969 0.40625 15.234 0.125Q14.484 -0.17188 13.594 -0.17188z"))
	//test.Float(t, width, 18.515625)
}

func fontDecorate(face *FontFace, width float64) *Path {
	c := &Canvas{}
	for _, deco := range face.Deco {
		deco.Decorate(c, Identity, face, &Path{}, width)
	}
	p := &Path{}
	for z := range c.layers {
		for _, layer := range c.layers[z] {
			if layer.path != nil {
				p = p.Append(layer.path)
			}
		}
	}
	return p
}

func TestFontDecoration(t *testing.T) {
	family := NewFontFamily("dejavu-serif")
	if err := family.LoadFontFile(externalMetricFont(t), FontRegular); err != nil {
		test.Error(t, err)
	}
	pt := ptPerMm * float64(family.fonts[FontRegular].Head.UnitsPerEm)

	// ascent = 1901
	// underlineDistance = 130, underlineThickness = 90
	// yStrikoutSize = 102, yStrikeoutPosition = 530
	// note that we increase distance by half the thickness to match the implementation of Firefox
	face := family.Face(pt, Black, FontRegular, FontNormal, FontUnderline)
	test.T(t, fontDecorate(face, 10.0), MustParseSVGPath("M0 -265L10 -265L10 -175L0 -175z"))

	face = family.Face(pt, Black, FontRegular, FontNormal, FontOverline)
	test.T(t, fontDecorate(face, 10.0), MustParseSVGPath("M0 1811L10 1811L10 1901L0 1901z"))

	face = family.Face(pt, Black, FontRegular, FontNormal, FontStrikethrough)
	test.T(t, fontDecorate(face, 10.0), MustParseSVGPath("M0 530L10 530L10 632L0 632z"))

	face = family.Face(pt, Black, FontRegular, FontNormal, FontDoubleUnderline)
	test.T(t, fontDecorate(face, 10.0), MustParseSVGPath("M0 -400L10 -400L10 -310L0 -310zM0 -265L10 -265L10 -175L0 -175z"))

	face = family.Face(pt, Black, FontRegular, FontNormal, FontDottedUnderline)
	test.T(t, fontDecorate(face, 89.0), MustParseSVGPath(""))
	test.T(t, fontDecorate(face, 90.0), MustParseSVGPath("M90 -220A45 45 0 0 1 0 -220A45 45 0 0 1 90 -220z"))
	test.T(t, fontDecorate(face, 269.0), MustParseSVGPath("M179.5 -220A45 45 0 0 1 89.5 -220A45 45 0 0 1 179.5 -220z"))
	test.T(t, fontDecorate(face, 270.0), MustParseSVGPath("M90 -220A45 45 0 0 1 0 -220A45 45 0 0 1 90 -220zM270 -220A45 45 0 0 1 180 -220A45 45 0 0 1 270 -220z"))

	face = family.Face(pt, Black, FontRegular, FontNormal, FontDashedUnderline)
	test.T(t, fontDecorate(face, 809.0), MustParseSVGPath("M0 -265L809 -265L809 -175L0 -175z"))
	test.T(t, fontDecorate(face, 810.0), MustParseSVGPath("M0 -265L270 -265L270 -175L0 -175zM540 -265L810 -265L810 -175L540 -175z"))
}
