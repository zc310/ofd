package pdf2ofd

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	fontparser "github.com/zc310/font"
)

type semanticCharacter struct {
	X     float64 `xml:"x,attr"`
	Y     float64 `xml:"y,attr"`
	Value string  `xml:"c,attr"`
}

func semanticPDFCharacters(t *testing.T, data []byte) []semanticCharacter {
	t.Helper()
	tool, err := exec.LookPath("mutool")
	if err != nil {
		t.Skip("mutool unavailable")
	}
	input := filepath.Join(t.TempDir(), "text.pdf")
	if err := os.WriteFile(input, data, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.CommandContext(t.Context(), tool, "draw", "-q", "-F", "stext", "-o", "-", input).Output()
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Characters []semanticCharacter `xml:"page>block>line>font>char"`
	}
	if err := xml.Unmarshal(out, &document); err != nil {
		t.Fatal(err)
	}
	return document.Characters
}

func TestSemanticTextKeepsWordsAndSourceCharacterPositions(t *testing.T) {
	font := "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>"
	for _, tc := range []struct{ name, setup, show, want string }{
		{"plain", "", "(MaskText) Tj", "MaskText"},
		{"character spacing", ".4 Tc", "(MaskText) Tj", "MaskText"},
		{"word spacing", ".4 Tc 1 Tw", "(Mask Text) Tj", "Mask Text"},
		{"scale and rise", "65 Tz 3 Ts .4 Tc", "(MaskText) Tj", "MaskText"},
		{"affine basis", "1.2 0 .3 1 30 100 Tm", "(MaskText) Tj", "MaskText"},
		{"TJ adjustment", "", "[(Ma) 20 (skText)] TJ", "MaskText"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := textFidelityPDF("BT /F1 12 Tf 3 Tr 30 100 Td "+tc.setup+" "+tc.show+" ET", font, "")
			ofd, page, _ := textFidelityConvert(t, input)
			pdf := textFidelityPDFOutput(t, ofd, page)
			if text := textFidelityExtractPDF(t, pdf); strings.TrimSpace(text) != tc.want {
				t.Fatalf("semantic word split: %q", text)
			}
			source, got := semanticPDFCharacters(t, input), semanticPDFCharacters(t, pdf)
			if len(source) != len(tc.want) || len(got) != len(source) {
				t.Fatalf("character count: source=%+v roundtrip=%+v", source, got)
			}
			for i, want := range source {
				if got[i].Value != want.Value || math.Abs(got[i].X-want.X) > .02 || math.Abs(got[i].Y-want.Y) > .02 {
					t.Fatalf("character %d: source=%+v roundtrip=%+v", i, want, got[i])
				}
			}
			img := textFidelityRaster(t, ofd, page)
			for i := 0; i < len(img.Pix); i += 4 {
				if img.Pix[i] != 255 || img.Pix[i+1] != 255 || img.Pix[i+2] != 255 {
					t.Fatal("semantic text painted visible ink")
				}
			}
		})
	}
}

func TestType3SemanticWidthsKeepFontMatrixAndCharacterSpacing(t *testing.T) {
	for _, matrix := range []string{".001 0 0 .001 0 0", ".002 0 0 .001 0 0"} {
		t.Run(matrix, func(t *testing.T) {
			input := type3TextPDF("BT /F1 12 Tf 3 Tr .4 Tc 30 100 Td (ABAB) Tj ET", matrix, "600 0 d0", "600 0 d0", "")
			ofd, page, _ := textFidelityConvert(t, input)
			pdf := textFidelityPDFOutput(t, ofd, page)
			if text := textFidelityExtractPDF(t, pdf); strings.TrimSpace(text) != "ABAB" {
				t.Fatalf("Type3 semantic word split: %q", text)
			}
			source, got := semanticPDFCharacters(t, input), semanticPDFCharacters(t, pdf)
			if len(source) != 4 || len(got) != len(source) {
				t.Fatalf("Type3 characters: source=%+v roundtrip=%+v", source, got)
			}
			for i, want := range source {
				if got[i].Value != want.Value || math.Abs(got[i].X-want.X) > .02 || math.Abs(got[i].Y-want.Y) > .02 {
					t.Fatalf("Type3 character %d: source=%+v roundtrip=%+v", i, want, got[i])
				}
			}
		})
	}
}

func TestSemanticFontUsesCIDWidthsAndEncodedCodeLengths(t *testing.T) {
	cmap := &pdfEncodingCMap{
		spaces: []pdfCMapSpace{{first: 1, last: 2, bytes: 1}, {first: 1, last: 2, bytes: 2}},
	}
	// The two raw <01> values have different byte lengths and different CIDs.
	cmap.ranges = []pdfEncodingRange{
		{first: pdfEncodingCode(1, 1), last: pdfEncodingCode(1, 1), target: 10},
		{first: pdfEncodingCode(1, 2), last: pdfEncodingCode(1, 2), target: 20},
	}
	source := pdfFontInfo{encodingCMap: cmap, codeBytes: 2, defaultW: 1000,
		widths:         map[int]float64{1: 99, 10: 420, 20: 860},
		encodedUnicode: map[uint64]string{pdfEncodingCode(1, 1): "中", pdfEncodingCode(1, 2): "𠀀"},
	}
	data, err := pdfSemanticFont(map[uint32]string{1: "中𠀀"}, source, nil)
	if err != nil {
		t.Fatal(err)
	}
	sfnt, err := fontparser.ParseSFNT(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	for r, want := range map[rune]uint16{'中': 420, '𠀀': 860} {
		glyph := sfnt.GlyphIndex(r)
		if glyph == 0 || sfnt.GlyphAdvance(glyph) != want {
			t.Fatalf("%U advance=%d want=%d", r, sfnt.GlyphAdvance(glyph), want)
		}
	}
}

func TestSemanticFontConflictingCodesAndLigaturesAreDeterministic(t *testing.T) {
	source := pdfFontInfo{codeBytes: 2, defaultW: 1000,
		widths:    map[int]float64{1: 280, 2: 600, 3: 900, 4: 400, 5: 0, 6: 70000},
		toUnicode: map[uint32]string{1: "f", 2: "f", 3: "fi", 4: "i", 5: "z", 6: "W"},
	}
	data, err := pdfSemanticFont(source.toUnicode, source, nil)
	if err != nil {
		t.Fatal(err)
	}
	sfnt, err := fontparser.ParseSFNT(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	for r, want := range map[rune]uint16{'f': 280, 'i': 400, 'z': 0, 'W': 65535} {
		if got := sfnt.GlyphAdvance(sfnt.GlyphIndex(r)); got != want {
			t.Fatalf("%U advance=%d want=%d", r, got, want)
		}
	}
	for i := 0; i < 20; i++ {
		again, err := pdfSemanticFont(source.toUnicode, source, nil)
		if err != nil || !bytes.Equal(data, again) {
			t.Fatalf("font output changed: %v", err)
		}
	}
}

func TestSemanticAdvanceMappingHonorsBudgetAndCancellation(t *testing.T) {
	source := pdfFontInfo{codeBytes: 2, toUnicode: map[uint32]string{1: strings.Repeat("中", 100)}}
	control := &conversionControl{ctx: t.Context(), options: Options{Limits: Limits{MaxOperations: 10}}}
	if _, err := pdfSemanticAdvances(source, control); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("mapping budget: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	control = &conversionControl{ctx: ctx, options: Options{Limits: Limits{MaxOperations: 1000}}}
	if _, err := pdfSemanticAdvances(source, control); err != context.Canceled {
		t.Fatalf("mapping cancellation: %v", err)
	}
}
