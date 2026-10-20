package pdf2ofd

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	jpeg2000 "github.com/mrjoshuak/go-jpeg2000"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/testutil"
	"golang.org/x/image/font/gofont/goregular"
)

func budgetImagePDF(content string, images ...string) []byte {
	var resources strings.Builder
	for i := range images {
		fmt.Fprintf(&resources, "/Im%d %d 0 R ", i, i+5)
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /XObject << " + resources.String() + ">> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
	return assemblePDF(append(objects, images...))
}

func budgetImage(width, height int64, extra string, data []byte) string {
	return fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 %s /Length %d >>\nstream\n%s\nendstream", width, height, extra, len(data), data)
}

func requireBudgetError(t *testing.T, pdf []byte, limits Limits, resource string) {
	t.Helper()
	var output bytes.Buffer
	err := ConvertWithOptions(t.Context(), pdf, &output, "", Options{Limits: limits})
	var limit *LimitError
	if !errors.Is(err, ErrLimitExceeded) || !errors.As(err, &limit) || limit.Resource != resource {
		t.Fatalf("error = %v, want %s limit", err, resource)
	}
	if output.Len() != 0 {
		t.Fatalf("failed conversion wrote %d output bytes", output.Len())
	}
}

func TestPDFInputBudgetAppliesToAllInputForms(t *testing.T) {
	pdf := testutil.MinimalPDF([]byte("0 0 10 10 re f"), 200, 200)
	path := filepath.Join(t.TempDir(), "input.pdf")
	if err := os.WriteFile(path, pdf, 0600); err != nil {
		t.Fatal(err)
	}
	for _, input := range []any{pdf, bytes.NewReader(pdf), path} {
		var output bytes.Buffer
		err := ConvertWithOptions(t.Context(), input, &output, "", Options{Limits: Limits{MaxInputBytes: int64(len(pdf) - 1)}})
		if !errors.Is(err, ErrLimitExceeded) || output.Len() != 0 {
			t.Fatalf("%T: error = %v, output bytes=%d", input, err, output.Len())
		}
	}
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), pdf, &output, "", Options{Limits: Limits{MaxInputBytes: int64(len(pdf))}}); err != nil {
		t.Fatal(err)
	}
}

func TestPDFByteInputRemainsUnchangedAcrossConversions(t *testing.T) {
	pdf := embeddedTextPDF("BT /F1 12 Tf 30 100 Td (Repeated input) Tj ET")
	original := bytes.Clone(pdf)
	for i := 0; i < 2; i++ {
		var out bytes.Buffer
		if err := Convert(t.Context(), pdf, &out, ""); err != nil {
			t.Fatal(err)
		}
		if out.Len() == 0 || !bytes.Equal(pdf, original) {
			t.Fatalf("conversion %d: output=%d input was modified=%v", i+1, out.Len(), !bytes.Equal(pdf, original))
		}
	}
}

func TestPDFByteInputCanBeSharedByConcurrentConversions(t *testing.T) {
	pdf := embeddedTextPDF("BT /F1 12 Tf 30 100 Td (Shared input) Tj ET")
	original := bytes.Clone(pdf)
	t.Cleanup(func() {
		if !bytes.Equal(pdf, original) {
			t.Error("concurrent conversions modified shared input")
		}
	})
	for i := 0; i < 2; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			if err := Convert(t.Context(), pdf, &out, ""); err != nil || out.Len() == 0 {
				t.Fatalf("err=%v output=%d", err, out.Len())
			}
		})
	}
}

func TestPDFImageBudgetRejectsHugeDimensionsBeforeAllocation(t *testing.T) {
	pdf := budgetImagePDF("/Im0 Do", budgetImage(1_000_000_000, 1_000_000_000, "", []byte{0}))
	requireBudgetError(t, pdf, Limits{}, "image_pixels")
}

func TestPDFImageBudgetUsesJPEGHeaderDimensions(t *testing.T) {
	var data bytes.Buffer
	if err := jpeg.Encode(&data, image.NewRGBA(image.Rect(0, 0, 10, 10)), nil); err != nil {
		t.Fatal(err)
	}
	pdf := budgetImagePDF("/Im0 Do", budgetImage(1, 1, "/Filter /DCTDecode", data.Bytes()))
	requireBudgetError(t, pdf, Limits{MaxImagePixels: 4}, "image_pixels")
}

func TestPDFTotalImageBudgetCountsDecodesAndHonorsCache(t *testing.T) {
	data := bytes.Repeat([]byte{255}, 12)
	image := budgetImage(2, 2, "", data)
	requireBudgetError(t, budgetImagePDF("/Im0 Do /Im1 Do", image, image), Limits{MaxTotalImagePixels: 7}, "total_image_pixels")
	var output bytes.Buffer
	pdf := budgetImagePDF("/Im0 Do /Im0 Do", image)
	if err := ConvertWithOptions(t.Context(), pdf, &output, "", Options{Limits: Limits{MaxTotalImagePixels: 4}}); err != nil {
		t.Fatal(err)
	}
}

func TestPDFStreamBudgetStopsFlateExpansion(t *testing.T) {
	var data bytes.Buffer
	zw := zlib.NewWriter(&data)
	if _, err := zw.Write(bytes.Repeat([]byte{255}, 300)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	pdf := budgetImagePDF("/Im0 Do", budgetImage(10, 10, "/Filter /FlateDecode", data.Bytes()))
	requireBudgetError(t, pdf, Limits{MaxDecodedStreamBytes: 100}, "decoded_stream_bytes")
}

func TestPDFTotalStreamBudgetIncludesContentAndImages(t *testing.T) {
	content := "/Im0 Do"
	data := bytes.Repeat([]byte{255}, 12)
	pdf := budgetImagePDF(content, budgetImage(2, 2, "", data))
	requireBudgetError(t, pdf, Limits{MaxTotalDecodedBytes: int64(len(content) + len(data) - 1)}, "total_decoded_bytes")
}

func TestPDFFontBudgetRejectsProgramBeforeRepair(t *testing.T) {
	content := "BT /F1 12 Tf 30 50 Td (A) Tj ET"
	pdf := assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /TrueType /BaseFont /GoRegular /Encoding /WinAnsiEncoding /FirstChar 65 /LastChar 65 /Widths [600] /FontDescriptor 6 0 R >>",
		"<< /Type /FontDescriptor /FontName /GoRegular /Flags 32 /Ascent 900 /Descent -200 /FontBBox [0 -200 1000 900] /CapHeight 700 /ItalicAngle 0 /StemV 80 /FontFile2 7 0 R >>",
		fmt.Sprintf("<< /Length %d /Length1 %d >>\nstream\n%s\nendstream", len(goregular.TTF), len(goregular.TTF), goregular.TTF),
	})
	requireBudgetError(t, pdf, Limits{MaxFontBytes: int64(len(goregular.TTF) - 1)}, "font_bytes")
}

func TestPDFOutputAndOperationBudgetsAbortWithoutOutput(t *testing.T) {
	pdf := testutil.MinimalPDF([]byte("0 0 10 10 re f 20 20 10 10 re f"), 200, 200)
	requireBudgetError(t, pdf, Limits{MaxOutputObjects: 1}, "output_objects")
	requireBudgetError(t, pdf, Limits{MaxOperations: 5}, "operations")
}

func TestPDFDecodeBudgetChecksAlreadyDecodedStreams(t *testing.T) {
	opts, err := (Options{Limits: Limits{MaxDecodedStreamBytes: 3}}).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	c := &conversionControl{ctx: t.Context(), options: opts}
	s := &types.StreamDict{Content: []byte{1, 2, 3, 4}}
	if err := c.decodeStream(s, 0); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("error = %v", err)
	}
}

func TestPDFPixelBudgetAvoidsIntegerOverflow(t *testing.T) {
	opts, _ := (Options{}).Normalize()
	c := &conversionControl{ctx: t.Context(), options: opts}
	if _, err := c.imagePixels(math.MaxInt64, math.MaxInt64); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("error = %v", err)
	}
}

func TestPDFRepairBudgetRejectsSparseObjectNumbers(t *testing.T) {
	opts, _ := (Options{Limits: Limits{MaxObjects: 10}}).Normalize()
	_, err := repairPDFXRefWithLimits([]byte("%PDF-1.7\n1 0 obj\n<< /Type /Catalog >>\nendobj\n1000000000 0 obj\n<< >>\nendobj\ntrailer << /Root 1 0 R >>"), opts.Limits)
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("error = %v", err)
	}
}

func TestPDFImageDiagnosticsIncludeSourceAndStrictMode(t *testing.T) {
	pdf := budgetImagePDF("/Im0 Do", budgetImage(2, 2, "", []byte{0}))
	for _, strict := range []bool{false, true} {
		var diagnostics []Diagnostic
		var output bytes.Buffer
		err := ConvertWithOptions(t.Context(), pdf, &output, "", Options{Strict: strict, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }})
		if strict && (!errors.Is(err, ErrUnsupported) || output.Len() != 0) {
			t.Fatalf("strict: error=%v, output=%d", err, output.Len())
		}
		if !strict && (err != nil || output.Len() == 0) {
			t.Fatalf("lenient: error=%v, output=%d", err, output.Len())
		}
		if len(diagnostics) != 1 || diagnostics[0].Code != "image.decode" || diagnostics[0].Page != 1 || diagnostics[0].Object != 5 || diagnostics[0].Resource != "Im0" {
			t.Fatalf("diagnostics=%+v", diagnostics)
		}
	}
}

func TestPDFDiagnosticCancellationIsReturnedUnwrapped(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	pdf := budgetImagePDF("/Im0 Do", budgetImage(2, 2, "", []byte{0}))
	var output bytes.Buffer
	err := ConvertWithOptions(ctx, pdf, &output, "", Options{OnDiagnostic: func(Diagnostic) { cancel() }})
	if err != context.Canceled || output.Len() != 0 {
		t.Fatalf("error=%v, output=%d", err, output.Len())
	}
}

func TestPDFInvalidLimitsDoNotReadInput(t *testing.T) {
	called := false
	input := readFunc(func([]byte) (int, error) { called = true; return 0, errors.New("must not read") })
	err := ConvertWithOptions(t.Context(), input, &bytes.Buffer{}, "", Options{Limits: Limits{MaxInputBytes: -1}})
	if err == nil || called {
		t.Fatalf("error=%v, read=%v", err, called)
	}
}

type readFunc func([]byte) (int, error)

func (f readFunc) Read(data []byte) (int, error) { return f(data) }

func TestPDFParserLimitsReturnTypedErrors(t *testing.T) {
	pdf := budgetImagePDF("/Im0 Do", budgetImage(2, 2, "", bytes.Repeat([]byte{255}, 12)))
	requireBudgetError(t, pdf, Limits{MaxObjects: 3}, "objects")
	requireBudgetError(t, pdf, Limits{MaxStreamBytes: 8}, "stream_bytes")
}

func TestPDFPageAndFormBudgetsAbortBeforeOutput(t *testing.T) {
	content := "0 0 1 1 re f"
	pdf := assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 5 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << >> /Contents 4 0 R >>",
	})
	requireBudgetError(t, pdf, Limits{MaxPages: 1}, "pages")
	// 直接验证内容解释器的 Form 深度；循环对象图已由 pdfcpu 在读取阶段拒绝。
	opts, _ := (Options{Limits: Limits{MaxDepth: 2}}).Normalize()
	control := &conversionControl{ctx: t.Context(), options: opts}
	p := &pdfInterpreter{control: control}
	if err := p.parse(nil, nil, nil, 3); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("error=%v", err)
	}
}

func TestPDFEmptyStreamDoesNotConsumeExhaustedDecodeBudget(t *testing.T) {
	opts, _ := (Options{Limits: Limits{MaxTotalDecodedBytes: 1}}).Normalize()
	control := &conversionControl{ctx: t.Context(), options: opts}
	if err := control.decodeStream(&types.StreamDict{Content: []byte{1}}, 0); err != nil {
		t.Fatal(err)
	}
	if err := control.decodeStream(&types.StreamDict{}, 0); err != nil {
		t.Fatal(err)
	}
}

func TestPDFParserLimitAdapterKeepsOrdinaryErrors(t *testing.T) {
	opts, _ := (Options{}).Normalize()
	for _, item := range []struct{ message, resource string }{
		{`"Size" 1000001 exceeds limit 1000000`, "objects"},
		{"object stream N 1000001 exceeds limit 1000000", "objects"},
		{"xref entry count 1000001 exceeds limit 1000000", "objects"},
		{"stream content exceeds maximum blind read length 268435456", "stream_bytes"},
	} {
		control := &conversionControl{ctx: t.Context(), options: opts}
		err := control.parserError(errors.New(item.message))
		var limit *LimitError
		if !errors.As(err, &limit) || limit.Resource != item.resource {
			t.Fatalf("%s: %v", item.message, err)
		}
	}
	control := &conversionControl{ctx: t.Context(), options: opts}
	err := errors.New("unrelated metadata exceeds limit 123")
	if got := control.parserError(err); got != err || control.check() != nil {
		t.Fatalf("ordinary error mapped to %v", got)
	}
}

func TestPDFPixelBudgetUsesJPXAndJBIG2Headers(t *testing.T) {
	jpx := encodeJPX(t, jpxTestSource(), jpeg2000.FormatJP2)
	requireBudgetError(t, budgetImagePDF("/Im0 Do", budgetImage(1, 1, "/Filter /JPXDecode", jpx)), Limits{MaxImagePixels: 4}, "image_pixels")
	// JBIG2 必须声明 1 位灰度；尺寸仍故意小于码流，保护真实尺寸预算检查。
	jbImage := fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 1 /Filter /JBIG2Decode /Length %d >>\nstream\n%s\nendstream", len(jbig2SampleMask), jbig2SampleMask)
	jb := budgetImagePDF("/Im0 Do", jbImage)
	requireBudgetError(t, jb, Limits{MaxImagePixels: 4}, "image_pixels")
	requireBudgetError(t, jb, Limits{MaxTotalImagePixels: 4}, "total_image_pixels")
}

func TestPDFImageBudgetIncludesSoftMasks(t *testing.T) {
	base := budgetImage(2, 2, "/SMask 6 0 R", bytes.Repeat([]byte{255}, 12))
	mask := budgetImage(100, 100, "", []byte{0})
	requireBudgetError(t, budgetImagePDF("/Im0 Do", base, mask), Limits{MaxImagePixels: 10}, "image_pixels")
	mask = budgetImage(2, 2, "", bytes.Repeat([]byte{255}, 12))
	requireBudgetError(t, budgetImagePDF("/Im0 Do", base, mask), Limits{MaxTotalImagePixels: 7}, "total_image_pixels")
}

func TestPDFBrokenSoftMaskReportsDiagnostic(t *testing.T) {
	pdf := budgetImagePDF("/Im0 Do", budgetImage(2, 2, "/SMask 6 0 R", bytes.Repeat([]byte{255}, 12)), budgetImage(2, 2, "", []byte{0}))
	var diagnostics []Diagnostic
	var output bytes.Buffer
	err := ConvertWithOptions(t.Context(), pdf, &output, "", Options{Strict: true, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }})
	if !errors.Is(err, ErrUnsupported) || output.Len() != 0 || len(diagnostics) != 1 || diagnostics[0].Code != "image.mask" {
		t.Fatalf("error=%v output=%d diagnostics=%+v", err, output.Len(), diagnostics)
	}
}

func TestPDFDepthBudgetCoversContentAndGraphicsState(t *testing.T) {
	for _, content := range []string{"[[[0]]]", "<< /A << /B << /C 0 >> >> >>"} {
		opts, _ := (Options{Limits: Limits{MaxDepth: 2}}).Normalize()
		control := &conversionControl{ctx: t.Context(), options: opts}
		tokens := newPDFContentTokenizer([]byte(content))
		tokens.control = control
		if _, _, err := tokens.next(); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("%s: error=%v", content, err)
		}
	}
	requireBudgetError(t, testutil.MinimalPDF([]byte(strings.Repeat("q ", 9)), 200, 200), Limits{MaxDepth: 8}, "graphics_state_depth")
}

func TestPDFUnicodeRangeExpansionUsesWorkBudget(t *testing.T) {
	data := []byte("1 beginbfrange <0000> <FFFF> <0041004200430044> endbfrange")
	opts, _ := (Options{Limits: Limits{MaxOperations: 1000}}).Normalize()
	control := &conversionControl{ctx: t.Context(), options: opts}
	mapping := parsePDFToUnicode(data, control)
	if !errors.Is(control.check(), ErrLimitExceeded) || len(mapping) >= 1000 {
		t.Fatalf("error=%v mappings=%d", control.check(), len(mapping))
	}
	// 数组目标的终点 0xFFFF 也必须停止，不能因 uint16 回绕继续覆盖码位 0。
	mapping = parsePDFToUnicode([]byte("1 beginbfrange <FFFF> <FFFF> [<0041> <0042>] endbfrange"))
	if len(mapping) != 1 || mapping[65535] != "A" {
		t.Fatalf("mapping=%v", mapping)
	}
}

func TestPDFConversionFailurePreservesExistingFile(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "input.pdf"), filepath.Join(dir, "output.ofd")
	if err := os.WriteFile(input, testutil.MinimalPDF([]byte("0 0 1 1 re f"), 200, 200), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("existing output"), 0600); err != nil {
		t.Fatal(err)
	}
	err := ConvertFileWithOptions(t.Context(), input, output, "", Options{Limits: Limits{MaxInputBytes: 1}})
	data, readErr := os.ReadFile(output)
	entries, dirErr := os.ReadDir(dir)
	if !errors.Is(err, ErrLimitExceeded) || readErr != nil || string(data) != "existing output" || dirErr != nil || len(entries) != 2 {
		t.Fatalf("error=%v read=%v data=%q entries=%d dir=%v", err, readErr, data, len(entries), dirErr)
	}
}
