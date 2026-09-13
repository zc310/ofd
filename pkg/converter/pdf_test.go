package converter

import (
	"bytes"
	"fmt"
	"image/color"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
)

func TestPDFIntroParallel(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "test", "testdata", "intro.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := ofd.Close(); err != nil {
			t.Errorf("关闭 OFD 失败: %v", err)
		}
	}()

	documents := make([]*render.Document, 0, len(ofd.Documents))
	for _, document := range ofd.Documents {
		documents = append(documents, render.NewDocument(color.Transparent, document))
	}

	var output bytes.Buffer
	if err := PDFDocuments(documents, &output, PDFParallel(true)); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(output.Bytes(), []byte("%PDF-")) {
		t.Fatalf("输出不是 PDF: %q", output.Bytes()[:min(output.Len(), 20)])
	}
	wantPages := countDocumentPages(documents)
	if !strings.Contains(output.String(), fmt.Sprintf("/Count %d", wantPages)) {
		t.Fatalf("PDF 页数不正确，want %d", wantPages)
	}
}

func BenchmarkPDFDocumentsWorkers(b *testing.B) {
	b.Run("workers=1", func(b *testing.B) {
		benchmarkPDFDocumentsWorkers(b, 1)
	})
	b.Run("workers=4", func(b *testing.B) {
		benchmarkPDFDocumentsWorkers(b, maxPDFRenderWorkers)
	})
}

func BenchmarkPDFDocumentsParallelOption(b *testing.B) {
	b.Run("parallel=false", func(b *testing.B) {
		benchmarkPDFDocumentsOption(b, PDFParallel(false))
	})
	b.Run("parallel=true", func(b *testing.B) {
		benchmarkPDFDocumentsOption(b, PDFParallel(true))
	})
}

func benchmarkPDFDocumentsOption(b *testing.B, option Option) {
	b.Helper()
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "test", "testdata", "intro.ofd"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := ofd.Close(); err != nil {
			b.Errorf("关闭 OFD 失败: %v", err)
		}
	})
	documents := makeRenderDocuments(ofd)

	var output bytes.Buffer
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		output.Reset()
		if err := PDFDocuments(documents, &output, option); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(output.Len()))
}

func benchmarkPDFDocumentsWorkers(b *testing.B, workers int) {
	b.Helper()
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "test", "testdata", "intro.ofd"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := ofd.Close(); err != nil {
			b.Errorf("关闭 OFD 失败: %v", err)
		}
	})
	documents := makeRenderDocuments(ofd)

	var output bytes.Buffer
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		output.Reset()
		if err := pdfDocumentsWithWorkers(documents, &output, workers); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(output.Len()))
}

func makeRenderDocuments(ofd *parser.OFD) []*render.Document {
	documents := make([]*render.Document, 0, len(ofd.Documents))
	for _, document := range ofd.Documents {
		documents = append(documents, render.NewDocument(color.Transparent, document))
	}
	return documents
}
