package pdf2ofd

import (
	"context"
	"strings"
	"testing"

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
