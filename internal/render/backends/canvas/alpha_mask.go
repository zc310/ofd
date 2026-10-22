package canvas

import (
	"image"
	"image/color"
	"math"

	"github.com/srwiley/scanx"
	tcanvas "github.com/zc310/canvas"

	"github.com/zc310/ofd/internal/render/geom"
)

// alphaMaskSurface 将实色路径覆盖率直接栅格化到单字节像素中。
// 裁剪掩码不包含 RGB 信息，使用 RGBA 目标会让临时掩码内存增加到四倍。
type alphaMaskSurface struct {
	ctx      *tcanvas.Context
	renderer *alphaMaskRenderer
}

type alphaMaskRenderer struct {
	width, height float64
	resolution    tcanvas.Resolution
	image         *image.Alpha
}

func newAlphaMaskSurface(width, height float64, resolution geom.Resolution) *alphaMaskSurface {
	res := resolution
	w := max(0, int(math.Round(width*res.DPMM())))
	h := max(0, int(math.Round(height*res.DPMM())))
	renderer := &alphaMaskRenderer{
		width: width, height: height, resolution: res,
		image: image.NewAlpha(image.Rect(0, 0, w, h)),
	}
	return &alphaMaskSurface{ctx: tcanvas.NewContext(renderer), renderer: renderer}
}

func (s *alphaMaskSurface) SetFillColor(c color.Color) { s.ctx.SetFillColor(c) }

func (s *alphaMaskSurface) DrawPath(x, y float64, path *geom.Path) {
	s.ctx.DrawPath(x, y, path)
}

func (s *alphaMaskSurface) RasterAlpha() *image.Alpha { return s.renderer.image }

func (r *alphaMaskRenderer) Size() (float64, float64) { return r.width, r.height }

func (r *alphaMaskRenderer) RenderPath(path *tcanvas.Path, style tcanvas.Style, matrix tcanvas.Matrix) {
	if path == nil || !style.HasFill() || r.image.Bounds().Empty() {
		return
	}
	fillAlpha := uint32(0xffff)
	if style.Fill.IsColor() {
		_, _, _, a := style.Fill.Color.RGBA()
		fillAlpha = a
	}
	if fillAlpha == 0 {
		return
	}
	spanner := alphaCoverageSpanner{image: r.image, alpha: fillAlpha}
	scanner := scanx.NewScanner(spanner, r.image.Rect.Dx(), r.image.Rect.Dy())
	scanner.SetWinding(style.FillRule == tcanvas.NonZero)
	path.Copy().Transform(matrix).ToScanxScanner(scanner, float64(r.image.Rect.Dy()), r.resolution)
	scanner.Draw()
}

func (*alphaMaskRenderer) RenderText(*tcanvas.Text, tcanvas.Matrix) {}
func (*alphaMaskRenderer) RenderImage(image.Image, tcanvas.Matrix)  {}

type alphaCoverageSpanner struct {
	image *image.Alpha
	alpha uint32
}

func (s alphaCoverageSpanner) SetColor(any) {}

func (s alphaCoverageSpanner) GetSpanFunc() scanx.SpanFunc {
	return func(y, x0, x1 int, coverage uint32) {
		source := uint32(uint64(s.alpha) * uint64(coverage) / 65535 >> 8)
		if source == 0 {
			return
		}
		offset := y*s.image.Stride + x0
		for x := x0; x < x1; x++ {
			destination := uint32(s.image.Pix[offset])
			s.image.Pix[offset] = uint8(source + destination*(255-source)/255)
			offset++
		}
	}
}

var _ scanx.Spanner = alphaCoverageSpanner{}
var _ tcanvas.Renderer = (*alphaMaskRenderer)(nil)
