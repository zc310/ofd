package pdf2ofd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/canvas"
)

func TestVerticalMissingSubstituteDiagnosesOnceAndHonorsStrictAndCancel(t *testing.T) {
	previous := pdfVerticalFontPath
	pdfVerticalFontPath = func(string, canvas.FontStyle) (string, bool) { return "", false }
	t.Cleanup(func() { pdfVerticalFontPath = previous })
	content := "BT /F1 20 Tf 100 180 Td <ff08ff09> Tj <ff08ff09> Tj ET"
	pdf := assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type0 /BaseFont /MissingCJK /Encoding /UniGB-UCS2-V /DescendantFonts [6 0 R] >>",
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /MissingCJK /DW 1000 /CIDSystemInfo << /Registry (Adobe) /Ordering (GB1) /Supplement 5 >> /FontDescriptor 7 0 R >>",
		"<< /Type /FontDescriptor /FontName /MissingCJK /Flags 4 /Ascent 1000 /Descent -200 /FontBBox [0 -200 1000 1000] /CapHeight 800 /ItalicAngle 0 /StemV 80 >>",
	})
	for _, strict := range []bool{false, true} {
		var out bytes.Buffer
		var ds []Diagnostic
		err := ConvertWithOptions(t.Context(), pdf, &out, "", Options{Strict: strict, OnDiagnostic: func(d Diagnostic) { ds = append(ds, d) }})
		if len(ds) != 1 || ds[0].Code != "font.vertical_glyph" {
			t.Fatalf("strict=%t diagnostics=%+v", strict, ds)
		}
		if strict {
			if !errors.Is(err, ErrUnsupported) || out.Len() != 0 {
				t.Fatalf("strict error=%v output=%d", err, out.Len())
			}
		} else if err != nil || out.Len() == 0 {
			t.Fatalf("ordinary error=%v output=%d", err, out.Len())
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var out bytes.Buffer
	err := ConvertWithOptions(ctx, pdf, &out, "", Options{OnDiagnostic: func(Diagnostic) { cancel() }})
	if err != context.Canceled || out.Len() != 0 {
		t.Fatalf("callback cancellation=%v output=%d", err, out.Len())
	}
}

func TestVerticalSubstituteReadHonorsFontAndTotalBudgets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "font.ttf")
	if err := os.WriteFile(path, make([]byte, 64), 0600); err != nil {
		t.Fatal(err)
	}
	for _, limits := range []Limits{{MaxFontBytes: 32}, {MaxTotalDecodedBytes: 32}} {
		options, err := (Options{Limits: limits}).Normalize()
		if err != nil {
			t.Fatal(err)
		}
		c := &conversionControl{ctx: t.Context(), options: options}
		p := pdfInterpreter{control: c}
		if _, err := p.readVerticalSubstitute(path); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("font read budget=%v", err)
		}
	}
}
