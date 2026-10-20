package pdf2ofd

import (
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

var benchmarkPDFInput []byte

func BenchmarkReadPDFByteInput(b *testing.B) {
	var input any = make([]byte, 16<<20)
	b.ReportAllocs()
	b.SetBytes(16 << 20)
	for b.Loop() {
		var err error
		benchmarkPDFInput, err = readPDFInputLimited(b.Context(), input, 32<<20)
		if err != nil {
			b.Fatal(err)
		}
	}
	benchmarkPDFInput = nil
}

func BenchmarkLoadEmbeddedSimpleFont(b *testing.B) {
	conf, err := api.LoadConfiguration(api.ConfigurationOptions{Mode: api.ConfigurationModeStateless})
	if err != nil {
		b.Fatal(err)
	}
	ctx, err, _ := readPDFContext(b.Context(), embeddedTextPDF("BT /F1 12 Tf (Hello) Tj ET"), conf)
	if err != nil {
		b.Fatal(err)
	}
	p := pdfInterpreter{ctx: ctx}
	b.ReportAllocs()
	for b.Loop() {
		font := pdfFontInfo{codeBytes: 1, defaultW: 500, widths: map[int]float64{}}
		p.loadFont(&font, *types.NewIndirectRef(5, 0))
		if font.sfnt == nil || len(font.codeToGID) == 0 || len(font.glyphWidths) == 0 {
			b.Fatal("embedded font mappings missing")
		}
	}
}
