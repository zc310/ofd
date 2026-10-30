package canvas

import (
	"image"
	"image/color"
	"math"

	"github.com/srwiley/rasterx"
	"github.com/srwiley/scanx"
	"github.com/zc310/canvas"
	"github.com/zc310/ofd/internal/render/geom"
	"golang.org/x/image/draw"
)

func (r *adaptiveRasterizer) RenderClippedText(text *canvas.Text, matrix canvas.Matrix, clip *canvas.Path) {
	if text == nil || text.Empty() || clip == nil || clip.Empty() {
		return
	}
	clip = clip.Copy().Transform(matrix)
	// 先按实际设备分辨率展开文字，保留普通文字的设备像素对齐。
	newRasterTextClip(r, clip).render(text, matrix)
}

type rasterTextClip struct {
	*adaptiveRasterizer

	clip       *canvas.Path
	bounds     canvas.Rect
	rectangle  bool
	mask       *textClipMask
	textBounds canvas.Rect
}

func newRasterTextClip(r *adaptiveRasterizer, clip *canvas.Path) *rasterTextClip {
	bounds := clip.Bounds()
	return &rasterTextClip{adaptiveRasterizer: r, clip: clip, bounds: bounds, rectangle: geom.ClipContainsRect(clip, bounds)}
}

func (r *rasterTextClip) render(text *canvas.Text, matrix canvas.Matrix) {
	spans := 0
	text.WalkSpans(func(_, _ float64, _ canvas.TextSpan) { spans++ })
	if _, ok := r.fastTarget(); !ok || spans < 2 {
		text.RenderTo(r, matrix, r.resolution)
		return
	}
	// 按设备分辨率只展开一次，收集整段实际轮廓范围后再回放，避免逐 span 扩容掩码。
	paths := canvas.New(0, 0)
	text.RenderTo(paths, matrix, r.resolution)
	r.textBounds = r.bounds.And(paths.Bounds())
	paths.RenderTo(r)
}

func (r *rasterTextClip) RenderPath(path *canvas.Path, style canvas.Style, matrix canvas.Matrix) {
	if path == nil || path.Empty() || canvas.Equal(matrix.Det(), 0) {
		return
	}
	bounds := path.Bounds().Transform(matrix)
	if !style.HasStroke() {
		if !r.bounds.Touches(bounds) {
			return
		}
		if r.rectangle && r.bounds.Contains(bounds) {
			r.adaptiveRasterizer.RenderPath(path, style, matrix)
			return
		}
		if target, ok := r.fastTarget(); ok && (style.Fill.IsColor() || style.Fill.IsGradient()) {
			visible := r.bounds.And(bounds)
			if r.mask == nil || !r.mask.bounds.Contains(visible) {
				if !r.textBounds.Empty() {
					visible = visible.Add(r.textBounds)
				}
				if r.mask != nil {
					visible = r.mask.bounds.Add(visible)
				}
				r.mask = newTextClipMask(target, r.clip, visible, r.rectangle, r.resolution)
			}
			r.mask.render(path, style, matrix, r.resolution)
			return
		}
	}
	// 不适用掩码的画笔保留几何回退。复制后闭合每个子路径，避免改写共享字形。
	var closed *canvas.Path
	for _, subpath := range path.Split() {
		closed = closed.Append(subpath.Copy())
		closed.Close()
	}
	if style.FillRule == canvas.EvenOdd {
		closed = closed.Settle(canvas.EvenOdd)
	}
	visible := closed.And(r.clip.Copy().Transform(matrix.Inv()))
	style.FillRule = canvas.NonZero
	if !visible.Empty() {
		r.adaptiveRasterizer.RenderPath(visible, style, matrix)
	}
}

func (r *rasterTextClip) RenderText(text *canvas.Text, matrix canvas.Matrix) {
	text.RenderTo(r, matrix, r.resolution)
}

func (r *rasterTextClip) RenderClippedText(text *canvas.Text, matrix canvas.Matrix, clip *canvas.Path) {
	if text == nil || text.Empty() || clip == nil || clip.Empty() {
		return
	}
	clip = geom.IntersectClips(r.clip, clip.Copy().Transform(matrix))
	newRasterTextClip(r.adaptiveRasterizer, clip).render(text, matrix)
}

func (r *rasterTextClip) RenderImage(img image.Image, matrix canvas.Matrix) {
	if canvas.Equal(matrix.Det(), 0) {
		return
	}
	canvas.NewContext(r.adaptiveRasterizer).RenderClippedImage(img, matrix, r.clip.Copy().Transform(matrix.Inv()))
}

// textClipMask 在同一次文字回放中复用扫描器与裁剪覆盖率，不再逐 span 求轮廓交集。
// 掩码只覆盖文字、裁剪与页面相交的像素范围；像素对齐的矩形直接使用 scanner scissor。
// GB/T 33190—2016 §8.4：裁剪区外不受绘制影响；掩码逐 span 应用，保留叠加顺序。
type textClipMask struct {
	height  int
	bounds  canvas.Rect
	spanner *scanx.ImgSpanner
	scanner *scanx.Scanner
}

func newTextClipMask(target *image.RGBA, clip *canvas.Path, bounds canvas.Rect, rectangle bool, resolution canvas.Resolution) *textClipMask {
	w, h := target.Bounds().Dx(), target.Bounds().Dy()
	dpmm := resolution.DPMM()
	x0, y0 := bounds.X0*dpmm, float64(h)-bounds.Y1*dpmm
	x1, y1 := bounds.X1*dpmm, float64(h)-bounds.Y0*dpmm
	rect := image.Rect(int(math.Floor(max(0, min(float64(w), x0)))), int(math.Floor(max(0, min(float64(h), y0)))),
		int(math.Ceil(max(0, min(float64(w), x1)))), int(math.Ceil(max(0, min(float64(h), y1)))))
	result := &textClipMask{height: h, bounds: bounds}
	if rect.Empty() {
		return result
	}
	result.spanner = scanx.NewImgSpanner(target)
	result.spanner.Op = draw.Over
	var spanner scanx.Spanner = result.spanner
	// 字形范围的小数边界由字形扫描器保留覆盖率；只有裁剪边界需要 Alpha 掩码。
	// GB/T 33190—2016 §8.4：快速路径仍须精确限制裁剪区外的绘制。
	aligned := false
	if rectangle {
		clipBounds := clip.Bounds()
		cx0, cy0 := clipBounds.X0*dpmm, float64(h)-clipBounds.Y1*dpmm
		cx1, cy1 := clipBounds.X1*dpmm, float64(h)-clipBounds.Y0*dpmm
		aligned = cx0 == math.Round(cx0) && cy0 == math.Round(cy0) && cx1 == math.Round(cx1) && cy1 == math.Round(cy1)
	}
	if !aligned {
		surface := newAlphaMaskSurface(float64(rect.Dx())/dpmm, float64(rect.Dy())/dpmm, geom.Resolution(resolution))
		surface.SetFillColor(color.White)
		surface.DrawPath(-float64(rect.Min.X)/dpmm, -float64(h-rect.Max.Y)/dpmm, clip)
		spanner = textMaskSpanner{ImgSpanner: result.spanner, mask: surface.RasterAlpha(), bounds: rect}
	}
	result.scanner = scanx.NewScanner(spanner, w, h)
	result.scanner.SetClip(rect)
	return result
}

func (m *textClipMask) render(path *canvas.Path, style canvas.Style, matrix canvas.Matrix, resolution canvas.Resolution) {
	if m.scanner == nil {
		return
	}
	m.scanner.Clear()
	m.scanner.SetWinding(style.FillRule == canvas.NonZero)
	if style.Fill.IsGradient() {
		inverse := matrix.Inv()
		gradient := style.Fill.Gradient.SetColorSpace(canvas.LinearColorSpace{})
		m.spanner.SetColor(rasterx.ColorFunc(func(x, y int) color.Color {
			point := inverse.Dot(canvas.Point{X: (float64(x) + .5) / resolution.DPMM(), Y: (float64(m.height-y) - .5) / resolution.DPMM()})
			return gradient.At(point.X, point.Y)
		}))
	} else {
		m.spanner.SetColor(style.Fill.Color)
	}
	path.Copy().Transform(matrix).ToScanxScanner(m.scanner, float64(m.height), resolution)
	m.scanner.Draw()
}

type textMaskSpanner struct {
	*scanx.ImgSpanner

	mask   *image.Alpha
	bounds image.Rectangle
}

func (s textMaskSpanner) GetSpanFunc() scanx.SpanFunc {
	span := s.ImgSpanner.GetSpanFunc()
	return func(y, x0, x1 int, coverage uint32) {
		offset := (y-s.bounds.Min.Y)*s.mask.Stride - s.bounds.Min.X
		for x := x0; x < x1; {
			alpha := s.mask.Pix[offset+x]
			end := x + 1
			for end < x1 && s.mask.Pix[offset+end] == alpha {
				end++
			}
			if alpha != 0 {
				span(y, x, end, coverage*uint32(alpha)/255)
			}
			x = end
		}
	}
}
