package giogpu

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"gioui.org/op"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
	tcanvas "github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/internal/thirdparty/canvas/renderers/rasterizer"
)

func openFixtureDocument(tb testing.TB, name string) *render.Document {
	tb.Helper()
	path := filepath.Join("../../../../testdata/ofdrw", name+".ofd")
	ofd, err := parser.NewOFD(path)
	if errors.Is(err, os.ErrNotExist) {
		tb.Skipf("缺少测试文档 %s", path)
	}
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		if err := ofd.Close(); err != nil {
			tb.Error(err)
		}
	})
	if len(ofd.Documents) == 0 {
		tb.Fatal("测试文档没有文档体")
	}
	doc := render.NewDocument(tcanvas.White, ofd.Documents[0], geom.DPI(150))
	tb.Cleanup(doc.Close)
	return doc
}

func fixtureScene(tb testing.TB, doc *render.Document, index int) *tcanvas.Canvas {
	tb.Helper()
	page := doc.Pages[index]
	box, err := page.PhysicalBox()
	if err != nil {
		tb.Fatal(err)
	}
	backend, err := newBackend(box.Width, box.Height, geom.DPI(150))
	if err != nil {
		tb.Fatal(err)
	}
	if err := doc.Draw(backend, page); err != nil {
		tb.Fatal(err)
	}
	gpu, ok := backend.(*gpuBackend)
	if !ok {
		tb.Fatal("未创建 Gio 后端")
	}
	return gpu.scene
}

// 全页使用严格 GPU 入口，不能把 CPU 自动回退当成 GPU 测试成功。
// 哈希可用于优化前后逐字节对照；仅显式设置目录时保留预览图片。
func TestOFDFixturesGPURendering(t *testing.T) {
	requireGPU(t)
	for _, name := range []string{"999", "intro"} {
		t.Run(name, func(t *testing.T) {
			doc := openFixtureDocument(t, name)
			for i := range doc.Pages {
				t.Run(fmt.Sprintf("page%02d", i+1), func(t *testing.T) {
					scene := fixtureScene(t, doc, i)
					gpu, err := Rasterize(scene, geom.DPI(150))
					if err != nil {
						t.Fatal(err)
					}
					cpu := fixtureRasterizeCPU(scene, tcanvas.DPI(150), tcanvas.DefaultColorSpace)
					if gpu.Bounds() != cpu.Bounds() {
						t.Fatalf("输出尺寸不同: GPU=%v CPU=%v", gpu.Bounds(), cpu.Bounds())
					}
					// GPU 与 CPU 的抗锯齿和合成模型不同，仅记录差异，不要求逐像素一致。
					var total uint64
					for j, value := range gpu.Pix {
						total += uint64(math.Abs(float64(value) - float64(cpu.Pix[j])))
					}
					if gpu.Opaque() != cpu.Opaque() {
						t.Fatal("GPU 输出不透明性与 CPU 不一致")
					}
					t.Logf("size=%v gpu_sha256=%x cpu_sha256=%x mean_channel_diff=%.4f", gpu.Bounds().Size(), sha256.Sum256(gpu.Pix), sha256.Sum256(cpu.Pix), float64(total)/float64(len(gpu.Pix)))
					if dir := os.Getenv("OFD_GIO_TEST_OUTPUT"); dir != "" {
						if err := os.MkdirAll(dir, 0o755); err != nil {
							t.Fatal(err)
						}
						writeFixturePreview(t, filepath.Join(dir, fmt.Sprintf("%s-page%02d.png", name, i+1)), gpu)
						writeFixturePreview(t, filepath.Join(dir, fmt.Sprintf("%s-page%02d-cpu.png", name, i+1)), cpu)
					}
				})
			}
		})
	}
}

// 按项目的页面像素尺寸创建 CPU 参考图；第三方 Draw 仍使用四舍五入。
func fixtureRasterizeCPU(scene *tcanvas.Canvas, resolution geom.Resolution, colorSpace tcanvas.ColorSpace) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, geom.RasterPixels(scene.W, resolution), geom.RasterPixels(scene.H, resolution)))
	renderer := rasterizer.FromImage(img, resolution, colorSpace)
	scene.RenderTo(renderer)
	renderer.Close()
	return img
}

func writeFixturePreview(t *testing.T, path string, img image.Image) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encodeErr := png.Encode(file, img)
	if err := errors.Join(encodeErr, file.Close()); err != nil {
		t.Fatal(err)
	}
}

// 分别测量已构建页面的操作记录、严格 GPU 栅格化及 CPU 栅格化。
// 不计解析、字体加载、OFD 页面绘制和 PNG 编码；intro 末页用于覆盖密集图元。
func BenchmarkOFDFixtures(b *testing.B) {
	requireGPU(b)
	for _, fixture := range []struct {
		name string
		page int
	}{{"999", 0}, {"intro", 0}, {"intro", 41}} {
		b.Run(fmt.Sprintf("%s/page%02d", fixture.name, fixture.page+1), func(b *testing.B) {
			doc := openFixtureDocument(b, fixture.name)
			if fixture.page >= len(doc.Pages) {
				b.Fatalf("缺少第 %d 页", fixture.page+1)
			}
			scene := fixtureScene(b, doc, fixture.page)
			resolution := geom.DPI(150)
			if _, err := Rasterize(scene, resolution); err != nil {
				b.Fatal(err)
			}
			b.Run("record", func(b *testing.B) {
				var ops op.Ops
				dpmm := resolution.DPMM()
				width, height := geom.RasterPixels(scene.W, resolution), geom.RasterPixels(scene.H, resolution)
				b.ReportAllocs()
				for b.Loop() {
					recordScene(&ops, scene, dpmm, width, height)
				}
			})
			b.Run("gpu", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, err := Rasterize(scene, resolution); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("cpu", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					_ = rasterizer.Draw(scene, tcanvas.Resolution(resolution), tcanvas.DefaultColorSpace)
				}
			})
		})
	}
}
