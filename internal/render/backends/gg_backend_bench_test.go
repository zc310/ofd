package backends_test

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tdewolff/canvas"

	"github.com/zc310/ofd/internal/render"

	_ "github.com/zc310/ofd/internal/render/backends/ftgg"
	_ "github.com/zc310/ofd/internal/render/backends/gg"
	_ "github.com/zc310/ofd/internal/render/backends/tinyskia"
)

// TestStress999Page1PNG 对 999.ofd 第 1 页做全部已注册后端的重复渲染压力
// 测试，比较耗时并把第 1 页 PNG 输出到 STRESS_PNG_DIR（默认 /tmp/ofd_test）
// 供目视对照。
func TestStress999Page1PNG(t *testing.T) {
	for _, dpiVal := range []float64{96, 150, 300} {
		dpi := canvas.DPI(dpiVal)
		doc, ofd := stress999Document(t, dpi)
		page := doc.Pages[0]

		iters := 20
		measure := func(name string) time.Duration {
			start := time.Now()
			for i := 0; i < iters; i++ {
				if _, err := doc.RasterizePage(page, name, dpi); err != nil {
					t.Fatalf("%s 渲染失败: %v", name, err)
				}
			}
			return time.Since(start) / time.Duration(iters)
		}

		canvasDur := measure(render.BackendCanvas)
		ggDur := measure(render.BackendGG)
		ftggDur := measure(render.BackendFTGG)
		tskDur := measure(render.BackendTinySkia)
		if err := ofd.Close(); err != nil {
			t.Errorf("关闭 OFD 失败: %v", err)
		}
		t.Logf("dpi=%.0f  canvas %v   gg %v   ftgg %v   tinyskia %v"+
			"   加速比 gg/canvas=%.2fx ftgg/canvas=%.2fx tinyskia/canvas=%.2fx",
			dpiVal, canvasDur.Round(time.Microsecond), ggDur.Round(time.Microsecond),
			ftggDur.Round(time.Microsecond), tskDur.Round(time.Microsecond),
			float64(canvasDur)/float64(ggDur), float64(canvasDur)/float64(ftggDur),
			float64(canvasDur)/float64(tskDur))
	}

	// 150dpi 输出四份 PNG 到临时目录供目视对比。
	dir := os.Getenv("STRESS_PNG_DIR")
	if dir == "" {
		dir = "/tmp/ofd_test"
	}
	_ = os.MkdirAll(dir, 0o755)
	doc, ofd := stress999Document(t, canvas.DPI(150))
	page := doc.Pages[0]
	for name, out := range map[string]string{
		render.BackendCanvas:   filepath.Join(dir, "999_p1_canvas.png"),
		render.BackendGG:       filepath.Join(dir, "999_p1_gg.png"),
		render.BackendFTGG:     filepath.Join(dir, "999_p1_ftgg.png"),
		render.BackendTinySkia: filepath.Join(dir, "999_p1_tinyskia.png"),
	} {
		img, err := doc.RasterizePage(page, name, canvas.DPI(150))
		if err != nil {
			t.Fatalf("%s 渲染失败: %v", name, err)
		}
		f, err := os.Create(out)
		if err != nil {
			t.Fatalf("创建 %s 失败: %v", out, err)
		}
		if err := png.Encode(f, img); err != nil {
			f.Close()
			t.Fatalf("写 PNG 失败: %v", err)
		}
		f.Close()
		t.Logf("已输出 %s（%dx%d）", out, img.Bounds().Dx(), img.Bounds().Dy())
	}
	if err := ofd.Close(); err != nil {
		t.Errorf("关闭 OFD 失败: %v", err)
	}
}

// BenchmarkRasterize999Page1 提供 999.ofd 第 1 页各已注册后端的渲染基准。
func BenchmarkRasterize999Page1(b *testing.B) {
	for _, dpiVal := range []float64{96, 150, 300} {
		dpi := canvas.DPI(dpiVal)
		for _, name := range []string{render.BackendCanvas, render.BackendGG, render.BackendFTGG, render.BackendTinySkia} {
			b.Run(fmt.Sprintf("%s_%.0fdpi", name, dpiVal), func(b *testing.B) {
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
	}
}
