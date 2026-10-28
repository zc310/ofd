package pdf2ofd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type cancelCoreWidthsContext struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (c *cancelCoreWidthsContext) Err() error {
	c.checks++
	if c.checks == 5 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestCoreFontWidthsStopOnCancellationWithoutPartialMetrics(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	checking := &cancelCoreWidthsContext{Context: ctx, cancel: cancel}
	options, _ := (Options{}).Normalize()
	p := pdfInterpreter{ctx: testPDFContext(t), control: &conversionControl{ctx: checking, options: options}}
	font := pdfFontInfo{codeBytes: 1, defaultW: 500, widths: map[int]float64{}}
	p.loadFont(&font, types.Dict{"Subtype": types.Name("Type1"), "BaseFont": types.Name("Helvetica"), "Encoding": types.Name("WinAnsiEncoding")})
	if err := p.control.check(); err != context.Canceled || len(font.widths) != 0 || checking.checks > 10 {
		t.Fatalf("err=%v widths=%d context checks=%d", err, len(font.widths), checking.checks)
	}
}

func TestCoreFontWidthsRejectAlreadyCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	font := pdfFontInfo{familyName: "Helvetica", widths: map[int]float64{65: 123}}
	if err := applyCoreFontWidths(ctx, &font, nil); err != context.Canceled || len(font.widths) != 1 || font.widths[65] != 123 {
		t.Fatalf("err=%v widths=%v", err, font.widths)
	}
}

func TestEmbeddedSimpleFontMappingAndAdvancesMatchSource(t *testing.T) {
	for _, tc := range []struct{ name, encoding, text, want string }{
		{"base_encoding", "/WinAnsiEncoding", "gjXYZ", "gjXYZ"},
		{"differences", "<< /BaseEncoding /WinAnsiEncoding /Differences [65 /Z] >>", "ABC", "ZBC"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pdf := embeddedTextPDFWithEncoding("BT /F1 16 Tf 30 100 Td ("+tc.text+") Tj ET", tc.encoding)
			doc, page, _ := textFidelityConvert(t, pdf)
			assertPDFImportMatchesSource(t, pdf, doc, page)
			for name, data := range map[string][]byte{"source": pdf, "roundtrip": textFidelityPDFOutput(t, doc, page)} {
				text := strings.Join(strings.Fields(textFidelityExtractPDF(t, data)), "")
				if text != tc.want {
					t.Fatalf("%s extracted %q, want %q", name, text, tc.want)
				}
			}
		})
	}
}

func TestDirectFontDictionaryDoesNotPanic(t *testing.T) {
	content := "BT /F1 12 Tf 30 100 Td (A) Tj ET"
	pdf := assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >> /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + content + "\nendstream",
	})
	var output bytes.Buffer
	if err := Convert(t.Context(), pdf, &output, ""); err != nil {
		t.Fatal(err)
	}
	if output.Len() == 0 {
		t.Fatal("direct font dictionary produced empty output")
	}
}

func TestCIDToGIDMapIsBoundedToSixteenBitCIDs(t *testing.T) {
	data := make([]byte, 2*(maxCIDToGIDEntries+2))
	data[0], data[1] = 0, 1
	last := (maxCIDToGIDEntries - 1) * 2
	data[last], data[last+1] = 0x12, 0x34
	extra := maxCIDToGIDEntries * 2
	data[extra], data[extra+1] = 0xff, 0xff
	mapping := pdfCIDToGIDMap(data)
	if len(mapping) != maxCIDToGIDEntries {
		t.Fatalf("entries=%d, want %d", len(mapping), maxCIDToGIDEntries)
	}
	if mapping[0] != 1 || mapping[maxCIDToGIDEntries-1] != 0x1234 {
		t.Fatalf("mapping[0]=%d mapping[last]=%d", mapping[0], mapping[maxCIDToGIDEntries-1])
	}
}

func TestBuiltinSymbolAndZapfEncodingsMapGlyphNames(t *testing.T) {
	for _, tc := range []struct {
		name string
		code uint32
		want string
	}{
		{"SymbolEncoding", 65, "\u0391"},
		{"Symbol", 34, "\u2200"},
		{"ZapfDingbatsEncoding", 33, "\u2701"},
		{"ZapfDingbats", 33, "\u2701"},
	} {
		mapping := map[uint32]string{}
		fillPDFBaseEncoding(tc.name, mapping)
		if got := mapping[tc.code]; got != tc.want {
			t.Fatalf("%s code %d = %q, want %q", tc.name, tc.code, got, tc.want)
		}
	}
}

func TestSimpleFontDefaultsSymbolAndZapfEncodings(t *testing.T) {
	for _, tc := range []struct {
		name     string
		encoding string
		code     uint32
		want     string
	}{
		{"Symbol", "SymbolEncoding", 34, "\u2200"},
		{"ZapfDingbats", "ZapfDingbatsEncoding", 33, "\u2701"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			font := pdfFontInfo{familyName: tc.name}
			p := pdfInterpreter{}
			mapping := p.applySimpleEncoding(&font, types.Dict{"BaseFont": types.Name(tc.name)})
			if font.encoding != tc.encoding || mapping[tc.code] != tc.want || font.toUnicode[tc.code] != tc.want {
				t.Fatalf("encoding=%q mapping=%v toUnicode=%v", font.encoding, mapping[tc.code], font.toUnicode[tc.code])
			}
		})
	}
}

func TestLoadFontDereferencesIndirectAttributes(t *testing.T) {
	ctx := &model.Context{XRefTable: &model.XRefTable{Table: map[int]*model.XRefTableEntry{
		1: model.NewXRefTableEntryGen0(types.Name("Type1")),
		2: model.NewXRefTableEntryGen0(types.Name("Helvetica")),
		3: model.NewXRefTableEntryGen0(types.Name("WinAnsiEncoding")),
		4: model.NewXRefTableEntryGen0(types.Integer(65)),
		5: model.NewXRefTableEntryGen0(types.Array{*types.NewIndirectRef(6, 0), *types.NewIndirectRef(7, 0)}),
		6: model.NewXRefTableEntryGen0(types.Integer(601)),
		7: model.NewXRefTableEntryGen0(types.Integer(701)),
	}}}
	options, _ := (Options{}).Normalize()
	p := pdfInterpreter{ctx: ctx, control: &conversionControl{ctx: t.Context(), options: options}}
	font := pdfFontInfo{codeBytes: 1, widths: map[int]float64{}}
	p.loadFont(&font, types.Dict{
		"Subtype":   *types.NewIndirectRef(1, 0),
		"BaseFont":  *types.NewIndirectRef(2, 0),
		"Encoding":  *types.NewIndirectRef(3, 0),
		"FirstChar": *types.NewIndirectRef(4, 0),
		"Widths":    *types.NewIndirectRef(5, 0),
		"LastChar":  types.Integer(66),
	})
	if font.encoding != "WinAnsiEncoding" || font.familyName != "Helvetica" {
		t.Fatalf("encoding=%q family=%q", font.encoding, font.familyName)
	}
	if font.widths[65] != 601 || font.widths[66] != 701 {
		t.Fatalf("widths=%v", font.widths)
	}
	if font.toUnicode[65] != "A" {
		t.Fatalf("toUnicode[65]=%q", font.toUnicode[65])
	}
}
