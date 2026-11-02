package canvas

import (
	"bytes"
	"errors"
	"image/color"
	"os"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/pdfcpuwriter"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

// Measure drawing and complete PDF writing, excluding input parsing and external
// validation tools. Each iteration starts with fresh document/font resources.
// Allocated bytes are cumulative allocations, not peak resident memory.
func BenchmarkPDFCPUPrototypeIntroFullDocument(b *testing.B) {
	for _, backend := range []string{"canvas", "pdfcpu"} {
		b.Run(backend, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			b.StopTimer()
			size := 0
			for range b.N {
				const input = "../../../../testdata/ofdrw/intro.ofd"
				ofd, err := parser.NewOFD(input)
				if err != nil {
					if errors.Is(err, os.ErrNotExist) {
						b.Skipf("缺少样例 %s", input)
					}
					b.Fatal(err)
				}
				document := render.NewDocument(color.Transparent, ofd.Documents[0])
				b.StartTimer()
				var output bytes.Buffer
				var prototype *pdfcpuwriter.Document
				var reference drawing.PDFDocument
				if backend == "canvas" {
					reference, err = newCanvasPDFDocument(&output, drawing.PDFOptions{Compress: true, SubsetFonts: true})
					if err != nil {
						b.Fatal(err)
					}
				}
				for _, page := range document.Pages {
					box, err := page.PhysicalBox()
					if err != nil {
						b.Fatal(err)
					}
					if backend == "pdfcpu" {
						if prototype == nil {
							prototype, err = pdfcpuwriter.New(b.Context(), box.Width, box.Height)
						} else {
							err = prototype.NextPage(box.Width, box.Height)
						}
						if err != nil {
							b.Fatal(err)
						}
						if err := document.Draw(newCanvasBackend(canvas.NewContext(prototype)), page); err != nil {
							b.Fatal(err)
						}
					} else {
						scene := canvas.New(box.Width, box.Height)
						if err := document.Draw(newCanvasBackend(canvas.NewContext(scene)), page); err != nil {
							b.Fatal(err)
						}
						if err := reference.AddPage(canvasVectorSurface{c: scene}, nil); err != nil {
							b.Fatal(err)
						}
					}
				}
				if prototype != nil {
					err = prototype.Write(&output)
				} else {
					err = reference.Close()
				}
				if err != nil {
					b.Fatal(err)
				}
				size = output.Len()
				b.StopTimer()
				document.Close()
				if err := ofd.Close(); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(size), "PDF-bytes/op")
		})
	}
}
