package canvas

import (
	"image"
	"math"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	"github.com/zc310/ofd/internal/render/geom"
	"golang.org/x/image/draw"
	"golang.org/x/image/math/f64"
)

const fastImageDownscaleThreshold = 1.25

// rasterize 将画布栅格化为 RGBA 图片。图片明显缩小时使用较快的双线性
// 插值；其他图像仍使用 canvas 默认的 Catmull-Rom 插值。
func rasterize(c *canvas.Canvas, resolution canvas.Resolution, colorSpace canvas.ColorSpace) *image.RGBA {
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
	if r.fastImages {
		if decoded, err := decodedImageOf(img); err == nil {
			img = decoded
		}
	}
	// 源与目标都是具体标准类型时，x/image/draw 才走直接采样路径；canvas 的
	// *Rasterizer 因内嵌 draw.Image 接口永远匹配不到具体目标类型，导致每像素
	// 经 color.Color 接口盒化（高 DPI 下数百万次分配）。这里把目标换成其底层的
	// *image.RGBA 复刻 canvas.RenderImage 的换算，仅对 LinearColorSpace（默认，
	// 不做 gamma 往返）生效，其余颜色空间回退到原实现以保证逐像素一致。
	if rgba, ok := r.fastTarget(); ok {
		r.renderImageFast(rgba, img, m)
		return
	}
	r.Rasterizer.RenderImage(img, m)
}

// fastTarget 在 LinearColorSpace（fastImages，不做 gamma 往返）且底层缓冲是
// *image.RGBA 时返回该缓冲，用于命中 x/image/draw 的直接采样路径。
func (r *adaptiveRasterizer) fastTarget() (*image.RGBA, bool) {
	if !r.fastImages {
		return nil, false
	}
	rgba, ok := r.Rasterizer.Image.(*image.RGBA)
	return rgba, ok
}

// renderImageFast 复刻 canvas Rasterizer.RenderImage 的边距/坐标换算与插值，
// 但直接以底层 *image.RGBA 为绘制目标，使 x/image/draw 命中具体类型快路径。
// 插值算法选择与原 adaptiveRasterizer 保持一致（大幅缩小时用 ApproxBiLinear）。
func (r *adaptiveRasterizer) renderImageFast(dst *image.RGBA, img image.Image, m canvas.Matrix) {
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
	h := float64(dst.Bounds().Size().Y)
	aff3 := f64.Aff3{m[0][0], -m[0][1], origin.X, -m[1][0], m[1][1], h - origin.Y}
	interp := draw.Interpolator(draw.CatmullRom)
	if shouldUseFastImageTransform(img, m, r.resolution) {
		interp = draw.ApproxBiLinear
	}
	interp.Transform(dst, aff3, img, img.Bounds(), draw.Over, nil)
}

// decodedImageWrapper 是延迟解码图片包装器（如 canvas/image.Image）暴露
// 原生 image.Image 的最小接口。
type decodedImageWrapper interface {
	Image() (image.Image, error)
}

// decodedImageOf 取出延迟解码包装器内部的已解码原生图像。canvas 的
// Rasterizer.RenderImage 用 x/image/draw 采样，若源是包装器则每像素经
// color.Color 接口盒化（.At），产生大量分配；换成具体类型（*image.RGBA、
// *image.NRGBA、*image.YCbCr 等）可命中 draw 的直接采样路径。
func decodedImageOf(img image.Image) (image.Image, error) {
	if wrapper, ok := img.(decodedImageWrapper); ok {
		decoded, err := wrapper.Image()
		if err != nil {
			return img, err
		}
		if decoded != nil && !decoded.Bounds().Empty() {
			return decoded, nil
		}
	}
	return img, nil
}

func isLinearColorSpace(colorSpace canvas.ColorSpace) bool {
	_, ok := colorSpace.(canvas.LinearColorSpace)
	return ok
}

func shouldUseFastImageTransform(img image.Image, m canvas.Matrix, resolution canvas.Resolution) bool {
	if img == nil || !finiteMatrix(geom.Matrix(m)) || resolution <= 0 {
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
