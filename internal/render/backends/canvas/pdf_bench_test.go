package canvas

import (
	"bytes"
	"errors"
	"image/color"
	"os"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

// BenchmarkPDFIntroFullDocument 测量绘制及完整 PDF 写出，不计输入解析和关闭。
// 每次使用新的文档及文档级字体资源；B/op 为累计分配，不是峰值内存。
func BenchmarkPDFIntroFullDocument(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	b.StopTimer()
	size := 0
	for range b.N {
		const input = "../../../../testdata/ofdrw/intro.ofd"
		ofd, err := parser.NewOFD(input)
		if errors.Is(err, os.ErrNotExist) {
			b.Skipf("缺少样例 %s", input)
		}
		if err != nil {
			b.Fatal(err)
		}
		document := render.NewDocument(color.Transparent, ofd.Documents[0], 0)
		b.StartTimer()
		var output bytes.Buffer
		writer, err := newCanvasPDFDocument(&output, drawing.PDFOptions{Compress: true, SubsetFonts: true})
		if err != nil {
			b.Fatal(err)
		}
		for _, page := range document.Pages {
			box, err := page.PhysicalBox()
			if err != nil {
				b.Fatal(err)
			}
			scene := canvas.New(box.Width, box.Height)
			if err := document.Draw(newCanvasBackend(canvas.NewContext(scene)), page); err != nil {
				b.Fatal(err)
			}
			if err := writer.AddPage(canvasVectorSurface{c: scene}, nil); err != nil {
				b.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			b.Fatal(err)
		}
		size = output.Len()
		b.StopTimer()
		document.Close()
		if err := ofd.Close(); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(size), "PDF-bytes/op")
}
