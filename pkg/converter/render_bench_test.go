package converter

import (
	"bytes"
	"image/color"
	"io"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
)

func BenchmarkRenderPDF999Page1(b *testing.B) {
	benchmarkRenderFormat(b, func(output *bytes.Buffer, documents []*render.Document) error {
		return PDFDocuments(documents, output, Page(1))
	})
}

func BenchmarkRenderPNG999Page1(b *testing.B) {
	benchmarkRenderFormat(b, func(output *bytes.Buffer, documents []*render.Document) error {
		return ImageDocuments(documents,
			Page(1),
			DPI(150),
			PNG(),
			Writer(func(int) (io.WriteCloser, error) {
				return benchmarkWriteCloser{Buffer: output}, nil
			}),
		)
	})
}

func BenchmarkRenderSVG999Page1(b *testing.B) {
	benchmarkRenderFormat(b, func(output *bytes.Buffer, documents []*render.Document) error {
		return ImageDocuments(documents,
			Page(1),
			SVG(),
			Writer(func(int) (io.WriteCloser, error) {
				return benchmarkWriteCloser{Buffer: output}, nil
			}),
		)
	})
}

func benchmarkRenderFormat(b *testing.B, renderFunc func(*bytes.Buffer, []*render.Document) error) {
	b.Helper()
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "test", "testdata", "999.ofd"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := ofd.Close(); err != nil {
			b.Errorf("关闭 OFD 失败: %v", err)
		}
	})
	documents := make([]*render.Document, 0, len(ofd.Documents))
	for _, document := range ofd.Documents {
		documents = append(documents, render.NewDocument(color.Transparent, document))
	}

	var output bytes.Buffer
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		output.Reset()
		if err := renderFunc(&output, documents); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(output.Len()))
}

type benchmarkWriteCloser struct {
	*bytes.Buffer
}

func (benchmarkWriteCloser) Close() error { return nil }
