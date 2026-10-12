package pdf2ofd

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-text/typesetting/font/opentype/tables"
	"github.com/zc310/canvas"
	fontparser "github.com/zc310/font"
	"golang.org/x/image/font/gofont/goregular"
)

func TestCJKVerticalLogicalFontEmbedsSelectedSubstitute(t *testing.T) {
	data := cjkVerticalFont(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "CJKVerticalTest.ttf")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	previous := pdfVerticalFontPath
	pdfVerticalFontPath = func(name string, style canvas.FontStyle) (string, bool) { return path, true }
	t.Cleanup(func() { pdfVerticalFontPath = previous })
	// Fontconfig is confined to the reference reader's test environment.
	config := filepath.Join(dir, "fonts.conf")
	if err := os.WriteFile(config, []byte("<?xml version=\"1.0\"?><!DOCTYPE fontconfig SYSTEM \"fonts.dtd\"><fontconfig><dir>"+dir+"</dir><cachedir>"+dir+"</cachedir></fontconfig>"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FONTCONFIG_FILE", config)
	pdf := cjkVerticalPDF(t, "UniGB-UCS2-V", "BT /F1 28 Tf 100 180 Td <ff084e2dff09ff0c3002> Tj ET", false)
	doc, page, ofd := textFidelityConvert(t, pdf)
	texts := layerTexts(page.Content().Layer[0])
	if len(texts) != 5 {
		t.Fatal("logical font text missing")
	}
	font, err := fontparser.ParseSFNT(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range []rune("（中），。") {
		if len(texts[i].CGTransform) != 1 {
			t.Fatal("logical font has no resolved glyph")
		}
		g := texts[i].CGTransform[0].Glyphs[0]
		if r != '中' && g == int(font.GlyphIndex(r)) {
			t.Fatalf("%c still uses horizontal glyph", r)
		}
	}
	img := textFidelityRaster(t, doc, page)
	textFidelityCompareSourceReader(t, pdf, img, "pdftoppm")
	roundtrip := textFidelityPDFOutput(t, doc, page)
	textFidelityCompareSource(t, roundtrip, img)
	if text := strings.Join(strings.Fields(textFidelityExtractPDF(t, roundtrip)), ""); text != "（中），。" {
		t.Fatalf("substitute semantic text=%q", text)
	}
	if dir := os.Getenv("OFD_CJK_EVIDENCE_DIR"); dir != "" {
		for suffix, data := range map[string][]byte{"-source.pdf": pdf, ".ofd": ofd, "-roundtrip.pdf": roundtrip} {
			if err := os.WriteFile(filepath.Join(dir, "substitute"+suffix), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}

	var out bytes.Buffer
	err = ConvertWithOptions(t.Context(), pdf, &out, "", Options{Limits: Limits{MaxFontBytes: 100}})
	if !errors.Is(err, ErrLimitExceeded) || out.Len() != 0 {
		t.Fatalf("substitute font budget=%v output=%d", err, out.Len())
	}
}

func TestVerticalGSUBPrefersVrt2AndKeepsLookupAlternatives(t *testing.T) {
	sfnt, err := fontparser.ParseSFNT(goregular.TTF, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Two features select distinct single substitutions for the same glyph.
	// vrt2 must win; applying both would incorrectly produce g+3.
	g := sfnt.GlyphIndex('(')
	u16 := func(v ...uint16) []byte {
		b := make([]byte, len(v)*2)
		for i, n := range v {
			binary.BigEndian.PutUint16(b[i*2:], n)
		}
		return b
	}
	gs := u16(1, 0, 10, 32, 58)
	gs = append(gs, u16(1)...)
	gs = append(gs, []byte("DFLT")...)
	gs = append(gs, u16(8, 4, 0, 0, 65535, 2, 0, 1)...)
	gs = append(gs, u16(2)...)
	gs = append(gs, []byte("vert")...)
	gs = append(gs, u16(14)...)
	gs = append(gs, []byte("vrt2")...)
	gs = append(gs, u16(20, 0, 1, 0, 0, 1, 1)...)
	gs = append(gs, u16(2, 6, 26)...)
	for _, delta := range []uint16{1, 2} {
		gs = append(gs, u16(1, 0, 1, 8, 1, 6, delta, 1, 1, g)...)
	}
	sfnt.Tables["GSUB"] = gs
	after, err := pdfVerticalGSUB(sfnt, nil, "Adobe-GB1")
	if err != nil || len(after) == 0 {
		t.Fatalf("vrt2=%v", err)
	}
	f := pdfFontInfo{sfnt: sfnt, verticalSubstitutions: after}
	if pdfVerticalSubstituteGlyph("(", f) != g+2 {
		t.Fatal("vrt2 did not replace vert")
	}
	// Two subtables in one lookup are alternatives: the second must not
	// substitute the output of the first a second time.
	f.verticalSubstitutions = [][]tables.SingleSubs{{
		{Data: tables.SingleSubstData1{Coverage: tables.Coverage1{Glyphs: []tables.GlyphID{tables.GlyphID(g)}}, DeltaGlyphID: 1}},
		{Data: tables.SingleSubstData1{Coverage: tables.Coverage1{Glyphs: []tables.GlyphID{tables.GlyphID(g + 1)}}, DeltaGlyphID: 1}},
	}}
	if pdfVerticalSubstituteGlyph("(", f) != g+1 {
		t.Fatal("GSUB subtable alternatives chained")
	}
	options, _ := (Options{Limits: Limits{MaxOperations: 1}}).Normalize()
	c := &conversionControl{ctx: t.Context(), options: options}
	if _, err := pdfVerticalGSUB(sfnt, c); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("GSUB budget=%v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	c = &conversionControl{ctx: ctx, options: options}
	if _, err := pdfVerticalGSUB(sfnt, c); err != context.Canceled {
		t.Fatalf("GSUB cancellation=%v", err)
	}
}
