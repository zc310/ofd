package pdf2ofd

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
)

func TestInlineImageRawDataPreservesEmbeddedEIAndRowPadding(t *testing.T) {
	for _, tc := range []struct {
		name, dict, data string
	}{
		{"rgb", "/W 2 /H 2 /CS /RGB /BPC 8", "\xff EI \x00\xff\x00\x00\x00\xff\x00"},
		{"gray", "/W 5 /H 1 /BPC 8", " EI \r"},
		{"packed_rows", "/W 9 /H 2 /BPC 1", " EI "},
		{"mask", "/W 9 /H 2 /IM true", " EI "},
		{"full_keys", "/Width 2 /Height 2 /ColorSpace /DeviceRGB /BitsPerComponent 8", "\xff EI \x00\xff\x00\x00\x00\xff\x00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := "q 100 0 0 100 30 30 cm BI " + tc.dict + " ID " + tc.data + "\r\nEI Q 10 10 5 5 re f"
			tokens := newPDFContentTokenizer([]byte(content[bytes.Index([]byte(content), []byte("BI "))+3:]))
			p := &pdfInterpreter{}
			_, data, err := tokens.inlineImage(func(dict map[string]any) (int64, error) {
				return p.inlineImageRawLength(inlineImageStream(dict))
			})
			if err != nil || !bytes.Equal(data, []byte(tc.data)) {
				t.Fatalf("data = %x, %v; want %x", data, err, tc.data)
			}
			pdf := embeddedTextPDF(content)
			_, page, _ := textFidelityConvert(t, pdf)
			if got := len(layerImages(page.Content().Layer[0])); got != 1 {
				t.Fatalf("images = %d, want 1", got)
			}
			if got := len(layerPaths(page.Content().Layer[0])); got != 1 {
				t.Fatalf("following paths = %d, want 1", got)
			}
		})
	}
}

func TestInlineImageRawLengthChecksBeforeReadingData(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		limit         int64
	}{
		{"limit", "BI /W 5 /H 1 /BPC 8 ID  EI \r EI", 4},
		{"truncated", "BI /W 100 /H 100 /BPC 8 ID x EI", 0},
		{"missing_end", "BI /W 1 /H 1 /BPC 8 ID x Q", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options, err := (Options{Limits: Limits{MaxStreamBytes: tc.limit}}).Normalize()
			if err != nil {
				t.Fatal(err)
			}
			control := &conversionControl{ctx: t.Context(), options: options}
			tokens := newPDFContentTokenizer([]byte(tc.content[len("BI "):]))
			tokens.control = control
			p := &pdfInterpreter{control: control}
			_, _, err = tokens.inlineImage(func(dict map[string]any) (int64, error) {
				return p.inlineImageRawLength(inlineImageStream(dict))
			})
			if err == nil {
				t.Fatal("expected inline data framing to fail")
			}
			if tc.limit > 0 {
				var limit *LimitError
				if !errors.As(err, &limit) || limit.Resource != "stream_bytes" || limit.Actual != 5 {
					t.Fatalf("err=%v, want stream_bytes limit at 5 bytes", err)
				}
			}
			var out bytes.Buffer
			err = ConvertWithOptions(t.Context(), embeddedTextPDF(tc.content), &out, "", options)
			if err == nil || out.Len() != 0 {
				t.Fatalf("err=%v output=%d, want failure before output", err, out.Len())
			}
			if tc.limit > 0 && !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("err=%v, want limit error", err)
			}
		})
	}
}

func TestInlineImageResolvesNamedColorSpaceForRawLength(t *testing.T) {
	data := bytes.Repeat([]byte{255, 0, 0}, 100*100)
	copy(data, " EI ")
	content := "q 100 0 0 100 30 30 cm BI /W 100 /H 100 /CS /Custom /BPC 8 ID " + string(data) + " EI Q"
	pdf := assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /ColorSpace << /Custom /DeviceRGB >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	})
	doc, page, _ := textFidelityConvert(t, pdf)
	assertPDFImportMatchesSource(t, pdf, doc, page)
}
