package backends_test

import (
	"fmt"
	"image"
	"io"
	"testing"

	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/converter"
)

// discardWriterCloser 丢弃写入内容，避免基准中包含真实文件 I/O。
type discardWriterCloser struct{}

func (discardWriterCloser) Write(p []byte) (int, error) { return len(p), nil }
func (discardWriterCloser) Close() error                { return nil }

// BenchmarkConvertPngPage1 对比同一份 stress999Document 上两条取图路径的速度：
//   - RasterizePage/backend：render.Document.RasterizePage 各后端，返回原始 RGBA；
//   - converter_Writer：pkg/converter.ImageDocument + Writer，经 encoder_image 的
//     PNG 输出（render.Rasterize + woozymasta/png（klauspost zlib）level 7）；
//   - converter_ImageWriter：pkg/converter.ImageDocument + ImageWriter，仅光栅化，
//     结果以 ImageWriter 回调交付，不含 PNG 编码。
//
// 全部限制在第 1 页，便于与 RasterizePage 直接对齐。
func BenchmarkConvertPngPage1(b *testing.B) {
	for _, dpiVal := range []float64{150, 300} {
		dpi := geom.DPI(dpiVal)
		for _, name := range []string{render.BackendCanvas, drawing.BackendGG, drawing.BackendFTGG, drawing.BackendFGG, drawing.BackendTinySkia, drawing.BackendDraw2D} {
			b.Run(fmt.Sprintf("RasterizePage/%s_%.0fdpi", name, dpiVal), func(b *testing.B) {
				doc, ofd := stress999Document(b, dpi)
				defer ofd.Close()
				page := doc.Pages[0]
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := doc.RasterizePage(page, name, dpi); err != nil {
						b.Fatal(err)
					}
				}
			})
		}

		b.Run(fmt.Sprintf("converter_Writer_%.0fdpi", dpiVal), func(b *testing.B) {
			doc, ofd := stress999Document(b, dpi)
			defer ofd.Close()
			fw := func(int) (io.WriteCloser, error) { return discardWriterCloser{}, nil }
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := converter.ImageDocument(doc, converter.DPI(dpiVal), converter.Page(1), converter.Writer(fw)); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(fmt.Sprintf("converter_ImageWriter_%.0fdpi", dpiVal), func(b *testing.B) {
			doc, ofd := stress999Document(b, dpi)
			defer ofd.Close()
			iw := func(int, image.Image) error { return nil }
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := converter.ImageDocument(doc, converter.DPI(dpiVal), converter.Page(1), converter.ImageWriter(iw)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
