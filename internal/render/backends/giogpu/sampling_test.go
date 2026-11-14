package giogpu

import (
	"image/color"
	"math"
	"testing"

	"gioui.org/op"

	"github.com/zc310/ofd/internal/render/geom"
	tcanvas "github.com/zc310/ofd/internal/thirdparty/canvas"
)

// 条纹画笔不走 Gio 原生渐变。每条纹在 20px/mm 输出中宽 4 像素，而固定
// 150 DPI 只采样约 1.2 像素，放大后会模糊或混叠。
type stripedGradient struct {
	density float64
	samples int
}

func (g *stripedGradient) At(x, _ float64) color.RGBA {
	g.samples++
	if int(math.Floor(x*g.density))%2 == 0 {
		return color.RGBA{R: 255, G: 255, B: 255, A: 255}
	}
	return color.RGBA{A: 255}
}

func (g *stripedGradient) SetColorSpace(_ tcanvas.ColorSpace) tcanvas.Gradient { return g }

func stripedScene() (*tcanvas.Canvas, *stripedGradient) {
	scene := tcanvas.New(10, 10)
	gradient := &stripedGradient{density: 5}
	style := tcanvas.DefaultStyle
	style.Fill = tcanvas.Paint{Gradient: gradient}
	scene.RenderPath(tcanvas.Rectangle(10, 10), style, tcanvas.Identity)
	return scene, gradient
}

func TestSceneRecordingPassesOutputResolutionToPaintFallback(t *testing.T) {
	scene, gradient := stripedScene()
	var ops op.Ops
	recordScene(&ops, scene, 20, 200, 200)
	if gradient.samples != 200*200 {
		t.Fatalf("局部画笔未按输出像素密度采样: got=%d want=%d", gradient.samples, 200*200)
	}
}

func TestRasterizeHighDPIPaintFallbackPreservesFineStripes(t *testing.T) {
	requireGPU(t)
	scene, _ := stripedScene()
	img, err := Rasterize(scene, geom.DPMM(20))
	if err != nil {
		t.Fatal(err)
	}
	for x := 24; x < 176; x += 8 {
		light, dark := img.RGBAAt(x+1, 100), img.RGBAAt(x+5, 100)
		// 取条纹内部，避开 GPU 纹理插值边缘，不要求两个后端逐像素一致。
		if int(light.R)-int(dark.R) < 180 || light.A != 255 || dark.A != 255 {
			t.Fatalf("像素 %d 的黑白条纹对比丢失: light=%v dark=%v", x, light, dark)
		}
	}
}
