package render

import (
	"image"
	"math"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	"golang.org/x/image/draw"
	"golang.org/x/image/math/f64"
)

const fastImageDownscaleThreshold = 1.25

// Rasterize 将画布栅格化为 RGBA 图片。图片明显缩小时使用较快的双线性
// 插值；其他图像仍使用 canvas 默认的 Catmull-Rom 插值。
func Rasterize(c *canvas.Canvas, resolution canvas.Resolution, colorSpace canvas.ColorSpace) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, int(c.W*resolution.DPMM()+0.5), int(c.H*resolution.DPMM()+0.5)))
	base := rasterizer.FromImage(img, resolution, colorSpace)
	renderer := &adaptiveRasterizer{
		Rasterizer: base,
		resolution: resolution,
		fastImages: colorSpace == nil || isLinearColorSpace(colorSpace),
	}
	c.RenderTo(renderer)
	base.Close()
	return img
}

type adaptiveRasterizer struct {
	*rasterizer.Rasterizer
	resolution canvas.Resolution
	fastImages bool
}

func (r *adaptiveRasterizer) RenderImage(img image.Image, m canvas.Matrix) {
	if !r.fastImages || !shouldUseFastImageTransform(img, m, r.resolution) {
		r.Rasterizer.RenderImage(img, m)
		return
	}

	// 保持 canvas 光栅器原有的四像素边距和坐标换算，仅在图片大比例缩小时
	// 使用更快的插值算法。
	margin := 0
	if (m[0][1] != 0.0 || m[1][0] != 0.0) && (m[0][0] != 0.0 || m[1][1] == 0.0) {
		margin = 4
		size := img.Bounds().Size()
		sp := img.Bounds().Min
		img2 := image.NewRGBA(image.Rect(0, 0, size.X+margin*2, size.Y+margin*2))
		draw.Draw(img2, image.Rect(margin, margin, size.X+margin, size.Y+margin), img, sp, draw.Over)
		img = img2
	}

	dpmm := r.resolution.DPMM()
	origin := m.Dot(canvas.Point{X: -float64(margin), Y: float64(img.Bounds().Size().Y - margin)}).Mul(dpmm)
	m = m.Scale(dpmm, dpmm)
	h := float64(r.Bounds().Size().Y)
	aff3 := f64.Aff3{m[0][0], -m[0][1], origin.X, -m[1][0], m[1][1], h - origin.Y}
	draw.ApproxBiLinear.Transform(r.Rasterizer, aff3, img, img.Bounds(), draw.Over, nil)
}

func isLinearColorSpace(colorSpace canvas.ColorSpace) bool {
	_, ok := colorSpace.(canvas.LinearColorSpace)
	return ok
}

func shouldUseFastImageTransform(img image.Image, m canvas.Matrix, resolution canvas.Resolution) bool {
	if img == nil || !finiteMatrix(m) || resolution <= 0 {
		return false
	}
	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return false
	}
	dpmm := resolution.DPMM()
	scaleX := math.Hypot(m[0][0], m[1][0]) * dpmm
	scaleY := math.Hypot(m[0][1], m[1][1]) * dpmm
	return scaleX < 1/fastImageDownscaleThreshold || scaleY < 1/fastImageDownscaleThreshold
}
