package canvas

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
)

// makeTestImage 返回带半透明与不同颜色的测试图，覆盖 draw 的常见源类型。
func makeTestImage(kind string) image.Image {
	const w, h = 40, 30
	switch kind {
	case "rgba":
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				img.Set(x, y, color.RGBA{R: uint8(x * 6), G: uint8(y * 8), B: 90, A: uint8(120 + x)})
			}
		}
		return img
	case "nrgba":
		img := image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				img.Set(x, y, color.NRGBA{R: uint8(x * 6), G: uint8(y * 8), B: 200, A: uint8(100 + y)})
			}
		}
		return img
	default: // ycbcr
		img := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				img.Y[img.YOffset(x, y)] = uint8((x*7 + y*3) % 255)
			}
		}
		for i := range img.Cb {
			img.Cb[i] = uint8((i * 5) % 255)
			img.Cr[i] = uint8((i * 11) % 255)
		}
		return img
	}
}

// TestAdaptiveRasterizerRenderImageMatchesCanvas 回归图片合成快路径：
// adaptiveRasterizer 把绘制目标换成底层 *image.RGBA 以命中 x/image/draw
// 的直接采样路径后，输出必须与 canvas 原始 Rasterizer.RenderImage 逐字节
// 一致。覆盖 RGBA/NRGBA/YCbCr 三种源、平移/缩放/旋转三种矩阵。
func TestAdaptiveRasterizerRenderImageMatchesCanvas(t *testing.T) {
	res := canvas.DPI(200)
	pageW, pageH := 60.0, 50.0

	matrices := map[string]canvas.Matrix{
		"translate": canvas.Identity.Translate(3, 4),
		"scale":     canvas.Identity.Translate(3, 4).Scale(0.4, 0.7),
		"rotate":    canvas.Identity.Translate(20, 15).Rotate(30),
		"shear":     canvas.Matrix{{1, 0.3, 5}, {0.2, 1, 6}},
	}

	for _, kind := range []string{"rgba", "nrgba", "ycbcr"} {
		for name, m := range matrices {
			t.Run(kind+"/"+name, func(t *testing.T) {
				src := makeTestImage(kind)

				// 原始路径：canvas.Rasterizer.RenderImage 直接合成。
				origImg := image.NewRGBA(image.Rect(0, 0, int(pageW*res.DPMM()+0.5), int(pageH*res.DPMM()+0.5)))
				origBase := rasterizer.FromImage(origImg, res, canvas.DefaultColorSpace)
				origBase.RenderImage(src, m)
				origBase.Close()

				// 快路径：adaptiveRasterizer.RenderImage。
				fastImg := image.NewRGBA(image.Rect(0, 0, int(pageW*res.DPMM()+0.5), int(pageH*res.DPMM()+0.5)))
				fastBase := rasterizer.FromImage(fastImg, res, canvas.DefaultColorSpace)
				fast := &adaptiveRasterizer{Rasterizer: fastBase, resolution: res, fastImages: true}
				fast.RenderImage(src, m)
				fastBase.Close()

				if !bytes.Equal(origImg.Pix, fastImg.Pix) {
					n := 0
					for i := range origImg.Pix {
						if origImg.Pix[i] != fastImg.Pix[i] {
							n++
						}
					}
					t.Fatalf("快路径与 canvas 原路径输出不一致：%d/%d 字节不同", n, len(origImg.Pix))
				}
			})
		}
	}
}
