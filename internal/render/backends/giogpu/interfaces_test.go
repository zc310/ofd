package giogpu

import (
	"image"
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
	tcanvas "github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/internal/thirdparty/canvas/renderers/rasterizer"
)

func TestBackendExposesActualRasterResolution(t *testing.T) {
	resolution := geom.DPI(96)
	backend, err := newBackend(20, 10, resolution)
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := backend.(interface{ RasterResolution() geom.Resolution })
	if !ok {
		t.Fatal("缺少 RasterResolution 可选接口")
	}
	if got := provider.RasterResolution(); got != resolution {
		t.Fatalf("分辨率错误: got=%v want=%v", got, resolution)
	}
}

func TestBackendPreservesSVGVectorSceneRecording(t *testing.T) {
	backend, err := newBackend(20, 10, geom.DPMM(10))
	if err != nil {
		t.Fatal(err)
	}
	parse := drawing.SVGSceneFactory()
	if parse == nil {
		t.Fatal("SVG 工厂未注册")
	}
	scene, err := parse([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10mm" height="6mm" viewBox="0 0 10 6"><rect width="10" height="6" fill="red"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	if !scene.RenderVector(backend, geom.Identity.Translate(5, 2)) {
		t.Fatal("SVG 应直接记录为矢量场景，不能退回预栅格化图片")
	}
	// 直接检查共享页面记录，不依赖 GPU 是否可用。
	gpu, ok := backend.(*gpuBackend)
	if !ok {
		t.Fatalf("后端类型错误: %T", backend)
	}
	img := rasterizer.Draw(gpu.scene, tcanvas.Resolution(geom.DPMM(10)), tcanvas.DefaultColorSpace)
	if got, want := img.RGBAAt(100, 50), (color.RGBA{R: 255, A: 255}); got != want {
		t.Errorf("SVG 场景位置或颜色错误: got=%v want=%v", got, want)
	}
	if got := img.RGBAAt(20, 20); got != (color.RGBA{}) {
		t.Errorf("SVG 不应绘制到变换后范围外: %v", got)
	}
}

func TestBackendExposesTextPathWithoutMutatingSharedGeometry(t *testing.T) {
	backend, err := newBackend(20, 10, geom.DPMM(10))
	if err != nil {
		t.Fatal(err)
	}
	drawer, ok := backend.(interface {
		TextPath(path *geom.Path, x, y float64)
	})
	if !ok {
		t.Fatal("缺少 TextPath 可选接口")
	}
	path := geom.Rectangle(10, 6)
	before := path.String()
	backend.SetFillColor(color.Black)
	drawer.TextPath(path, 5, 2)
	if path.String() != before {
		t.Fatal("文字轮廓被原地修改")
	}
	gpu, ok := backend.(*gpuBackend)
	if !ok {
		t.Fatalf("后端类型错误: %T", backend)
	}
	img := rasterizer.Draw(gpu.scene, tcanvas.Resolution(geom.DPMM(10)), tcanvas.DefaultColorSpace)
	if got := img.RGBAAt(100, 50); got != (color.RGBA{A: 255}) {
		t.Errorf("文字轮廓未写入页面记录: %v", got)
	}
	if img.Bounds() != image.Rect(0, 0, 200, 100) {
		t.Errorf("页面记录尺寸错误: %v", img.Bounds())
	}
}
