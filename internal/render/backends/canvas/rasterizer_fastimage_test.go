package canvas

import (
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

// 快路径与 canvas 原路径（draw.CatmullRom）之间允许的差异上限。
// 快路径改用 draw.ApproxBiLinear 后两者不再是同一采样核，实测在下面 12 个
// 用例上的差异上界为：平均 <= 3.52、最大 <= 112、>64 的字节占比 <= 2.03%。
// 下面的阈值在此基础上留了余量：平均留 2.8 倍、最大留 1.4 倍、占比留 3 倍。
// 最大值阈值同时兼顾错位检出：故意注入 1px origin 错误时 rgba 用例的最大
// 差异会升到 234，超过该阈值。
const (
	fastImageMaxMeanDiff   = 10
	fastImageMaxByteDiff   = 160
	fastImageMaxHeavyRatio = 0.06
)

// TestAdaptiveRasterizerRenderImageMatchesCanvas 回归图片合成快路径：
// adaptiveRasterizer 把绘制目标换成底层 *image.RGBA 以命中 x/image/draw
// 的直接采样路径后，输出必须与 canvas 原始 Rasterizer.RenderImage 在上述
// 容差内一致。覆盖 RGBA/NRGBA/YCbCr 三种源、平移/缩放/旋转/错切四种矩阵。
//
// 注意：快路径与原路径使用不同采样核（ApproxBiLinear vs CatmullRom），本测试
// 不再保证逐字节一致，因此在平滑渐变图上对 1px 级错位不再敏感；它仍能拦截
// 矩阵换算、margin 计算、坐标原点等结构性错误。渐变图的平滑性使 1px 错位
// 产生的差异与换核本身的差异同量级，这是本测试放弃逐字节一致性的已知代价。
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

				sum, maxDiff, heavy := 0, 0, 0
				for i := range origImg.Pix {
					d := int(origImg.Pix[i]) - int(fastImg.Pix[i])
					if d < 0 {
						d = -d
					}
					sum += d
					if d > maxDiff {
						maxDiff = d
					}
					if d > 64 {
						heavy++
					}
				}
				mean := float64(sum) / float64(len(origImg.Pix))
				heavyRatio := float64(heavy) / float64(len(origImg.Pix))
				if mean > fastImageMaxMeanDiff || maxDiff > fastImageMaxByteDiff || heavyRatio > fastImageMaxHeavyRatio {
					t.Fatalf("快路径与 canvas 原路径输出差异过大：平均 %.3f（上限 %d）、最大 %d（上限 %d）、>64 占比 %.2f%%（上限 %.2f%%）",
						mean, fastImageMaxMeanDiff, maxDiff, fastImageMaxByteDiff, heavyRatio*100, fastImageMaxHeavyRatio*100)
				}
			})
		}
	}
}
