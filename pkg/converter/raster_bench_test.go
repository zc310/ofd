package converter

import (
	"bytes"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"

	_ "github.com/zc310/ofd/internal/render/backends/draw2d"
	_ "github.com/zc310/ofd/internal/render/backends/fgg"
	_ "github.com/zc310/ofd/internal/render/backends/ftgg"
	_ "github.com/zc310/ofd/internal/render/backends/gg"
	_ "github.com/zc310/ofd/internal/render/backends/tinyskia"
)

// benchDoc 只解析一次文档，跨基准复用，避免把解析耗时算进栅格化对比。
func benchDoc(tb testing.TB, name string) *render.Document {
	tb.Helper()
	file := filepath.Join("..", "..", "test", "testdata", name)
	if _, err := os.Stat(file); err != nil {
		tb.Skipf("缺少测试文档: %v", err)
	}
	ofd, err := parser.NewOFD(file)
	if err != nil {
		tb.Fatalf("%s: %v", name, err)
	}
	tb.Cleanup(func() { _ = ofd.Close() })
	return render.NewDocumentWithDPI(color.White, ofd.Documents[0], dpiBench)
}

const dpiBench = 150.0

func runRasterBench(b *testing.B, backend string) {
	b.Helper()
	doc := benchDoc(b, "helloworld.ofd")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		var buf bytes.Buffer
		b.StartTimer()
		writer := Writer(func(int) (io.WriteCloser, error) { return nopWriteCloser{&buf}, nil })
		if err := ImageDocument(doc, PNG(), writer, RasterBackend(backend), DPI(dpiBench), Page(1)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRasterCanvas(b *testing.B)   { runRasterBench(b, "canvas") }
func BenchmarkRasterGG(b *testing.B)       { runRasterBench(b, "gg") }
func BenchmarkRasterFTGG(b *testing.B)     { runRasterBench(b, "ftgg") }
func BenchmarkRasterFGG(b *testing.B)      { runRasterBench(b, "fgg") }
func BenchmarkRasterTinySkia(b *testing.B) { runRasterBench(b, "tinyskia") }
func BenchmarkRasterDraw2D(b *testing.B)   { runRasterBench(b, "draw2d") }

// TestRasterBackendTimingSummary 输出人类可读的平均耗时与输出体积。
func TestRasterBackendTimingSummary(t *testing.T) {
	files := []string{"helloworld.ofd", "intro.ofd"}
	backends := []string{"canvas", "gg", "ftgg", "tinyskia", "draw2d"}
	const dpi = 150.0
	rounds := 8

	for _, name := range files {
		doc := benchDoc(t, name)
		var payload int
		for _, backend := range backends {
			var elapsed time.Duration
			for i := 0; i < rounds; i++ {
				var buf bytes.Buffer
				writer := Writer(func(int) (io.WriteCloser, error) { return nopWriteCloser{&buf}, nil })
				start := time.Now()
				if err := ImageDocument(doc, PNG(), writer, RasterBackend(backend), DPI(dpi), Page(1)); err != nil {
					t.Fatalf("%s/%s: %v", name, backend, err)
				}
				elapsed += time.Since(start)
				payload = buf.Len()
			}
			t.Logf("%-15s %-9s avg=%8s  png=%d bytes", name, backend, (elapsed / time.Duration(rounds)).Round(time.Millisecond), payload)
		}
	}
}
