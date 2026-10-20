package converter

import (
	"image/color"
	"io"
	"path/filepath"
	"testing"
)

// 包含 OFD 打开、模板绘制、PDF 写出及资源释放，避免只测已预热的画布。
func BenchmarkPDF1000Pages(b *testing.B) {
	input := filepath.Join("..", "..", "testdata", "ofd-creator", "1000-pages.ofd")
	b.ReportAllocs()
	for b.Loop() {
		if err := PDF(b.Context(), input, io.Discard, BgColor(color.White), DPI(150)); err != nil {
			b.Fatal(err)
		}
	}
}
