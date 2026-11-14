package giogpu

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
	tcanvas "github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/internal/thirdparty/canvas/renderers/rasterizer"
)

// 用独立的线性光公式核对透明合成，避免把 Gio 与默认 CPU 混色差异误判为
// Alpha 丢失或重复预乘。intro 第 14、18 页的半透明遮罩属于这种情况。
// GB/T 33190—2016 §8.5 表 34 定义 Alpha 范围；本测试不额外解释标准的混色空间。
func TestTranslucentImagesAndPathsUseSameLinearLightCompositing(t *testing.T) {
	requireGPU(t)
	backgrounds := []color.NRGBA{
		{A: 255},
		{R: 255, G: 255, B: 255, A: 255},
		{R: 128, G: 128, B: 128, A: 255},
		{R: 200, G: 120, B: 60, A: 255},
	}
	foregrounds := []color.NRGBA{{}, {R: 255}, {R: 255, G: 255, B: 255}, {R: 80, G: 160, B: 200}}
	alphas := []uint8{0, 64, 102, 128, 192, 255}
	for _, background := range backgrounds {
		for _, foreground := range foregrounds {
			for _, alpha := range alphas {
				foreground.A = alpha
				t.Run(fmt.Sprintf("background%v/foreground%v", background, foreground), func(t *testing.T) {
					scene := tcanvas.New(24, 8)
					ctx := tcanvas.NewContext(scene)
					ctx.SetFillColor(background)
					ctx.DrawPath(0, 0, tcanvas.Rectangle(24, 8))
					ctx.SetFillColor(foreground)
					ctx.DrawPath(0, 0, tcanvas.Rectangle(8, 8))
					rgba := image.NewRGBA(image.Rect(0, 0, 4, 4))
					nrgba := image.NewNRGBA(rgba.Bounds())
					for y := range 4 {
						for x := range 4 {
							rgba.Set(x, y, foreground)
							nrgba.SetNRGBA(x, y, foreground)
						}
					}
					beforeRGBA, beforeNRGBA := bytes.Clone(rgba.Pix), bytes.Clone(nrgba.Pix)
					ctx.RenderImage(rgba, tcanvas.Identity.Translate(8, 0).Scale(2, 2))
					ctx.RenderImage(nrgba, tcanvas.Identity.Translate(16, 0).Scale(2, 2))
					gpu, err := Rasterize(scene, geom.DPMM(2))
					if err != nil {
						t.Fatal(err)
					}
					want := linearLightOverOpaque(foreground, background)
					for i, name := range []string{"原生路径", "RGBA 图片", "NRGBA 图片"} {
						if got := gpu.RGBAAt(i*16+8, 8); !colorNear(got, want, 2) {
							t.Errorf("%s 透明合成不符合线性光公式: got=%v want=%v", name, got, want)
						}
					}
					if !bytes.Equal(rgba.Pix, beforeRGBA) || !bytes.Equal(nrgba.Pix, beforeNRGBA) {
						t.Fatal("透明合成修改了共享源图")
					}
					if background == (color.NRGBA{R: 255, G: 255, B: 255, A: 255}) && foreground == (color.NRGBA{A: 102}) {
						cpu := rasterizer.Draw(scene, tcanvas.DPMM(2), tcanvas.DefaultColorSpace)
						if got := cpu.RGBAAt(8, 8); got != (color.RGBA{R: 153, G: 153, B: 153, A: 255}) {
							t.Fatalf("CPU 默认通道混色示例改变: %v", got)
						}
						t.Logf("白底叠加 Alpha=102 的黑色: Gio=%v CPU=%v", gpu.RGBAAt(8, 8), cpu.RGBAAt(8, 8))
					}
				})
			}
		}
	}
}

// 独立计算 sRGB 解码、线性光 Over、sRGB 编码，不复用待测的上传或回读查表。
func linearLightOverOpaque(source, background color.NRGBA) color.RGBA {
	decode := func(value uint8) float64 {
		v := float64(value) / 255
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	alpha := float64(source.A) / 255
	channel := func(s, b uint8) uint8 {
		v := decode(s)*alpha + decode(b)*(1-alpha)
		if v <= 0.0031308 {
			v *= 12.92
		} else {
			v = 1.055*math.Pow(v, 1/2.4) - 0.055
		}
		return uint8(math.Round(v * 255))
	}
	return color.RGBA{R: channel(source.R, background.R), G: channel(source.G, background.G), B: channel(source.B, background.B), A: 255}
}
