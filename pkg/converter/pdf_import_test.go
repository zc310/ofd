package converter_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/zc310/ofd/internal/testutil"
	"github.com/zc310/ofd/pkg/converter"
	pdfimport "github.com/zc310/ofd/pkg/converter/import/pdf"
)

func TestPDFImportOptionsReachBothPublicEntrypoints(t *testing.T) {
	input := testutil.MinimalPDF([]byte("0 0 1 1 re f"), 200, 200)
	options := converter.PDFImportOptions{Limits: converter.PDFImportLimits{MaxInputBytes: 1}}
	for _, convert := range []func(*bytes.Buffer) error{
		func(out *bytes.Buffer) error {
			return converter.Convert(t.Context(), "pdf", "ofd", input, out, converter.WithPDFImportOptions(options))
		},
		func(out *bytes.Buffer) error {
			return pdfimport.ConvertWithOptions(t.Context(), input, out, "", options)
		},
	} {
		var output bytes.Buffer
		err := convert(&output)
		var limit *converter.PDFImportLimitError
		if !errors.Is(err, converter.ErrPDFImportLimitExceeded) || !errors.As(err, &limit) || limit.Resource != "input_bytes" || output.Len() != 0 {
			t.Fatalf("error=%v output=%d", err, output.Len())
		}
	}
}

func TestPDFStrictImportSupportsGlyphClipping(t *testing.T) {
	input := testutil.MinimalPDF([]byte("BT /F1 12 Tf 7 Tr (A) Tj ET"), 200, 200)
	var diagnostics []converter.PDFImportDiagnostic
	var output bytes.Buffer
	err := converter.Convert(t.Context(), "pdf", "ofd", input, &output, converter.WithPDFImportOptions(converter.PDFImportOptions{
		Strict: true, OnDiagnostic: func(d converter.PDFImportDiagnostic) { diagnostics = append(diagnostics, d) },
	}))
	if err != nil || output.Len() == 0 || len(diagnostics) != 0 {
		t.Fatalf("error=%v diagnostics=%+v output=%d", err, diagnostics, output.Len())
	}
}

func TestPDFStrictImportReportsMissingResourceAndStopsOutput(t *testing.T) {
	input := testutil.MinimalPDF([]byte("/Missing Do"), 200, 200)
	var diagnostics []converter.PDFImportDiagnostic
	var output bytes.Buffer
	err := converter.Convert(t.Context(), "pdf", "ofd", input, &output, converter.WithPDFImportOptions(converter.PDFImportOptions{Strict: true, OnDiagnostic: func(d converter.PDFImportDiagnostic) { diagnostics = append(diagnostics, d) }}))
	if !errors.Is(err, converter.ErrPDFImportUnsupported) || output.Len() != 0 || len(diagnostics) != 1 || diagnostics[0].Code != "resource.missing" || diagnostics[0].Page != 1 {
		t.Fatalf("error=%v diagnostics=%+v output=%d", err, diagnostics, output.Len())
	}
}
