package gg_test

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/tdewolff/canvas"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/testscene"
)

// TestGGCanvasParity 验证 gg 光栅后端（BackendGG）与 canvas 光栅后端
// （BackendCanvas，经 render.NewBackend 创建，同时覆盖注册表路径）对同一
// DrawContext 场景的输出逐像素接近：
//   - 尺寸一致（gg 后端像素缓冲与 canvas Rasterize 取整规则相同）；
//   - 填充的内部像素应逐字节一致，差异集中于反锯齿轮廓边界；
//   - 描边/图片的半透明边缘因 rasterizer 实现与混色空间不同允许退化，
//     但大幅差异（>32/通道）像素占比必须很小。
func TestGGCanvasParity(t *testing.T) {
	fontPath := testscene.PickTestFont()
	if fontPath == "" {
		t.Skip("未找到可用测试字体")
	}

	ww, hh := 200.0, 140.0
	res := canvas.DPI(96)

	canvasBackend, err := render.NewBackend(render.BackendCanvas, ww, hh, res)
	if err != nil {
		t.Fatal(err)
	}
	ggBackend, err := render.NewBackend(render.BackendGG, ww, hh, res)
	if err != nil {
		t.Fatal(err)
	}
	if ggBackend == nil {
		t.Fatal("gg 后端注册未生效")
	}

	testscene.SceneBackend(t, ggBackend, fontPath)
	testscene.SceneBackend(t, canvasBackend, fontPath)

	a := canvasBackend.Raster()
	b := ggBackend.Raster()
	stats := testscene.CalcDiff(t, a, b)

	ink := 0
	for i := 0; i+3 < len(a.Pix); i += 4 {
		if a.Pix[i] != 255 || a.Pix[i+1] != 255 || a.Pix[i+2] != 255 || a.Pix[i+3] != 255 {
			ink++
		}
	}
	if ink == 0 {
		t.Fatal("场景内容为空，对比无意义")
	}

	totalPx := a.Bounds().Dx() * a.Bounds().Dy()
	if stats.Changed > totalPx*3/4 {
		t.Fatalf("差异像素过多: %d/%d", stats.Changed, totalPx)
	}
	if stats.Hot > totalPx/100 {
		t.Fatalf("大幅差异像素过多: %d/%d（期望 <= %d）", stats.Hot, totalPx, totalPx/100)
	}
	if stats.MaxDelta > 200 {
		t.Fatalf("最大通道差异过大: %d（出现整块错位或缺失）", stats.MaxDelta)
	}
	if stats.AvgDelta > 2.0 {
		t.Fatalf("平均通道差异过大: %.2f", stats.AvgDelta)
	}
	t.Logf("gg/canvas 逐像素对照: 墨水 %d, 差异像素 %d, 差异>32/通道 %d, 最大通道差 %d, 平均通道差 %.2f",
		ink, stats.Changed, stats.Hot, stats.MaxDelta, stats.AvgDelta)
}

// 端到端后端正交性阈值。剩余偏差的主要来源是亚像素（<1px，如 0.25mm
// 表格线）的覆盖分布差异：canvas 光栅器把覆盖率分散到相邻两行（90%+5%），
// gg 解析光栅器集中到单行（纯色）。这属于两个独立光栅器的固有差异，无可用
// 开关对齐（Skia/cairo/PDFium 之间同样如此），且 DPI 越高差异越小。文档级
// 容差只约束统计接近度：
//   - changed：有差异像素不超过页面的 3/4，防止整块错位/缺失；
//   - hot：任通道差 >32 的像素不超过页面的 5%（实测最差的表格页约 3.8%）；
//   - avgDelta：逐字节平均差（含 4 通道）有界，反映整体亮度接近度；
//   - maxDelta：单通道最大差不超过 200（高对比线条交替的单像素可能差很大）。
const (
	e2eMaxHotFrac = 0.05
	e2eMaxAvg     = 4.0
	e2eMaxDelta   = 200
)

func TestGGBackendMatchesCanvasOnRealOFD(t *testing.T) {
	fixtures := []string{
		"ano.ofd",
		"intro.ofd",
		"999.ofd",
		"magazine.ofd",
		"radial_demo.ofd",
	}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("..", "..", "..", "..", "test", "testdata", name)
			ofd, err := parser.NewOFD(path)
			if err != nil {
				t.Skipf("解析失败（环境相关）: %v", err)
			}
			defer ofd.Close()
			if len(ofd.Documents) == 0 || len(ofd.Documents[0].Pages) == 0 {
				t.Skip("文档无页面")
			}
			doc := render.NewDocumentWithDPI(color.White, ofd.Documents[0], canvas.DPI(96))
			page := doc.Pages[0]

			res := canvas.DPI(96)
			canvasImg, err := doc.RasterizePage(page, render.BackendCanvas, res)
			if err != nil {
				t.Skipf("canvas 后端渲染失败（环境相关）: %v", err)
			}
			ggImg, err := doc.RasterizePage(page, render.BackendGG, res)
			if err != nil {
				t.Fatalf("gg 后端渲染失败: %v", err)
			}
			if dir := os.Getenv("STRESS_PNG_DIR"); dir != "" {
				_ = os.MkdirAll(dir, 0o755)
				writePNG(t, filepath.Join(dir, name+"_gg.png"), ggImg)
				writePNG(t, filepath.Join(dir, name+"_canvas.png"), canvasImg)
			}
			stats := testscene.CalcDiff(t, canvasImg, ggImg)
			totalPx := canvasImg.Bounds().Dx() * canvasImg.Bounds().Dy()
			if stats.Changed > totalPx*3/4 {
				t.Fatalf("差异像素过多: %d/%d", stats.Changed, totalPx)
			}
			if float64(stats.Hot) > float64(totalPx)*e2eMaxHotFrac {
				t.Fatalf("大幅差异像素过多: %d/%d（期望 <= %d）",
					stats.Hot, totalPx, int(float64(totalPx)*e2eMaxHotFrac))
			}
			if stats.MaxDelta > e2eMaxDelta {
				t.Fatalf("单通道差异过大: %d", stats.MaxDelta)
			}
			if stats.AvgDelta > e2eMaxAvg {
				t.Fatalf("平均通道差异过大: %.2f", stats.AvgDelta)
			}
			t.Logf("%s: %dx%d, 差异像素 %d(%.1f%%), 差异>32/通道 %d(%.2f%%), 最大通道差 %d, 平均通道差 %.2f",
				name, canvasImg.Bounds().Dx(), canvasImg.Bounds().Dy(),
				stats.Changed, 100*float64(stats.Changed)/float64(totalPx),
				stats.Hot, 100*float64(stats.Hot)/float64(totalPx),
				stats.MaxDelta, stats.AvgDelta)
		})
	}
}

// writePNG 用标准库把 RGBA 图为 PNG 落盘（替代 gg.SavePNG，避免测试
// 额外依赖 sbinet/gg）。
func writePNG(t *testing.T, path string, img *image.RGBA) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建 %s 失败: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("写 PNG 失败: %v", err)
	}
}
