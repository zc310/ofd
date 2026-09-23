// Package ftgg 提供基于 FloatTech/gg 的页面栅格化后端（fogleman API +
// freetype/raster），通过空白导入注册到 internal/render 的注册表（Go
// image 包风格）：
//
//	import _ "github.com/zc310/ofd/internal/render/backends/ftgg"
//
// 之后即可用 drawing.BackendFTGG="ftgg" 作为栅格后端名，或经
// converter.RasterBackend("ftgg") 使用。
// 只用于渲染速度对照实验，不承诺与 canvas/gogpu 的逐像素输出一致。
// 渲染上下文状态机由 internal/render/backends/rastercore 共享，本包只依赖
// geom 类型，不依赖 tdewolff/canvas。
package ftgg

import (
	"image"
	"image/color"

	ftgg "github.com/FloatTech/gg"
	"golang.org/x/image/draw"
	"golang.org/x/image/math/f64"

	"github.com/zc310/ofd/internal/render/backends/rastercore"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
)

var _ drawing.Backend = (*rastercore.Core)(nil)

func init() {
	_ = drawing.RegisterBackend(drawing.BackendFTGG, func(width, height float64, resolution geom.Resolution) (drawing.Backend, error) {
		return New(width, height, resolution)
	})
}

// New 创建 FloatTech/gg 光栅后端。输出分辨率和像素尺寸规则与 gogpu/gg
// 后端一致。
func New(width, height float64, resolution geom.Resolution) (drawing.Backend, error) {
	core, err := rastercore.New(width, height, resolution, func(wpx, hpx int, dpmm, hpix float64) rastercore.Hooks {
		im := image.NewRGBA(image.Rect(0, 0, wpx, hpx))
		return &ftggHooks{dc: ftgg.NewContextForRGBA(im), im: im, dpmm: dpmm, hpix: hpix}
	})
	if err != nil {
		return nil, err
	}
	return core, nil
}

// ftggHooks 是 rastercore.Hooks 的 FloatTech/gg 实现。
//
// 已知差异（速度对照可接受）：
//   - freetype/raster 的 Stroke 对 nil joiner 回退为 RoundJoiner，因此 canvas
//     的 MiterJoin 在此降级为圆角连接（FloatTech 无 Miter 连接器可用）。
//   - 渐变通过自定义 Pattern 逐像素采样 geom.Gradient，与 gogpu 的
//     CustomBrush 语义一致。
type ftggHooks struct {
	dc   *ftgg.Context
	im   *image.RGBA
	dpmm float64
	hpix float64
}

func (h *ftggHooks) Push() { h.dc.Push() }
func (h *ftggHooks) Pop()  { h.dc.Pop() }

func (h *ftggHooks) SetFillSolid(c color.Color) {
	h.dc.SetFillStyle(ftgg.NewSolidPattern(ftggRGBA(c)))
}
func (h *ftggHooks) SetStrokeSolid(c color.Color) {
	h.dc.SetStrokeStyle(ftgg.NewSolidPattern(ftggRGBA(c)))
}
func (h *ftggHooks) ClearFill()   { h.dc.SetFillStyle(ftgg.NewSolidPattern(color.Transparent)) }
func (h *ftggHooks) ClearStroke() { h.dc.SetStrokeStyle(ftgg.NewSolidPattern(color.Transparent)) }

func (h *ftggHooks) SetFillRule(rule geom.FillRule) {
	switch rule {
	case geom.NonZero:
		h.dc.SetFillRule(ftgg.FillRuleWinding)
	default:
		h.dc.SetFillRule(ftgg.FillRuleEvenOdd)
	}
}

func (h *ftggHooks) DrawDevicePath(dp *rastercore.DevicePath, m geom.Matrix, fill, stroke bool, fillGrad, strokeGrad geom.Gradient, style rastercore.StrokeStyle) {
	replayPath(h.dc, dp)
	if fill {
		if fillGrad != nil {
			h.dc.SetFillStyle(&ftggPattern{inv: m.Inv(), dpmm: h.dpmm, hpix: h.hpix, g: fillGrad})
		}
		// FloatTech 的 Fill/Stroke 同样会清空路径：同时需要描边时用
		// FillPreserve，否则后续 Stroke 拿到的路径为空。
		if stroke {
			h.dc.FillPreserve()
		} else {
			h.dc.Fill()
		}
	}
	if stroke {
		h.strokeStyle(style)
		if strokeGrad != nil {
			h.dc.SetStrokeStyle(&ftggPattern{inv: m.Inv(), dpmm: h.dpmm, hpix: h.hpix, g: strokeGrad})
		}
		h.dc.Stroke()
	}
}

func (h *ftggHooks) RenderImage(img image.Image, m geom.Matrix) {
	src := img
	margin := 0
	if (m[0][1] != 0.0 || m[1][0] != 0.0) && (m[0][0] != 0.0 || m[1][1] == 0.0) {
		margin = 4
		size := img.Bounds().Size()
		img2 := image.NewRGBA(image.Rect(0, 0, size.X+margin*2, size.Y+margin*2))
		draw.Draw(img2, image.Rect(margin, margin, size.X+margin, size.Y+margin), img, img.Bounds().Min, draw.Over)
		src = img2
	}
	hh := h.hpix
	srcH := float64(src.Bounds().Size().Y)
	origin := m.Dot(geom.Point{X: -float64(margin), Y: srcH - float64(margin)}).Mul(h.dpmm)
	m = m.Scale(h.dpmm, h.dpmm)
	aff3 := f64.Aff3{m[0][0], -m[0][1], origin.X, -m[1][0], m[1][1], hh - origin.Y}
	draw.CatmullRom.Transform(h.im, aff3, src, src.Bounds(), draw.Over, nil)
}

func (h *ftggHooks) Raster() *image.RGBA {
	dst := image.NewRGBA(h.im.Bounds())
	draw.Draw(dst, dst.Bounds(), h.im, h.im.Bounds().Min, draw.Src)
	return dst
}

// replayPath 把设备像素路径逐段追加到 FloatTech 上下文的当前路径。
func replayPath(dc *ftgg.Context, dp *rastercore.DevicePath) {
	for _, s := range dp.Segs {
		switch s.Op {
		case rastercore.MoveOp:
			dc.MoveTo(s.P[0].X, s.P[0].Y)
		case rastercore.LineOp:
			dc.LineTo(s.P[0].X, s.P[0].Y)
		case rastercore.QuadOp:
			dc.QuadraticTo(s.P[0].X, s.P[0].Y, s.P[1].X, s.P[1].Y)
		case rastercore.CubeOp:
			dc.CubicTo(s.P[0].X, s.P[0].Y, s.P[1].X, s.P[1].Y, s.P[2].X, s.P[2].Y)
		case rastercore.CloseOp:
			dc.ClosePath()
		}
	}
}

// strokeStyle 把共享状态机换算好的描边造型写入 FloatTech 上下文。虚线长度
// 与相位按线宽缩放（同 canvas 的 ScaleDash）；MiterJoin 无对应连接器，在
// freetype/raster 内回退为 RoundJoiner。
func (h *ftggHooks) strokeStyle(style rastercore.StrokeStyle) {
	h.dc.SetLineWidth(style.Width)
	switch style.Cap.(type) {
	case geom.RoundCapper:
		h.dc.SetLineCap(ftgg.LineCapRound)
	case geom.SquareCapper:
		h.dc.SetLineCap(ftgg.LineCapSquare)
	default:
		h.dc.SetLineCap(ftgg.LineCapButt)
	}
	switch style.Join.(type) {
	case geom.RoundJoiner:
		h.dc.SetLineJoin(ftgg.LineJoinRound)
	case geom.BevelJoiner:
		h.dc.SetLineJoin(ftgg.LineJoinBevel)
	default:
		h.dc.SetLineJoin(ftgg.LineJoinRound)
	}
	if len(style.Dashes) == 0 {
		h.dc.SetDash()
		h.dc.SetDashOffset(0)
		return
	}
	scaled := make([]float64, len(style.Dashes))
	for i, d := range style.Dashes {
		scaled[i] = d * style.Width
	}
	h.dc.SetDash(scaled...)
	h.dc.SetDashOffset(style.DashOffset * style.Width)
}

// ftggPattern 是 FloatTech 的逐像素 Pattern，复刻 gogpu gradBrush 的采样
// 语义：设备像素中心先经完整矩阵逆映射回逻辑毫米，再用 geom.Gradient.At
// 求色。
type ftggPattern struct {
	inv  geom.Matrix
	dpmm float64
	hpix float64
	g    geom.Gradient
}

// ColorAt 返回像素 (x,y) 处采样到的颜色。
func (p *ftggPattern) ColorAt(x, y int) color.Color {
	pt := p.inv.Dot(geom.Point{X: float64(x) / p.dpmm, Y: (p.hpix - float64(y)) / p.dpmm})
	return p.g.At(pt.X, pt.Y)
}

// ftggRGBA 把 color.Color 转换为直通 RGBA（与 canvas 默认 LinearColorSpace
// 一致，不做 gamma 往返）；FloatTech 内部按 sRGB Over 合成。
func ftggRGBA(c color.Color) color.RGBA {
	if c == nil {
		return color.RGBA{}
	}
	r, g, b, a := c.RGBA()
	if a == 0 {
		return color.RGBA{}
	}
	f := 255.0 / 65535.0
	return color.RGBA{
		R: uint8(float64(r) * f),
		G: uint8(float64(g) * f),
		B: uint8(float64(b) * f),
		A: uint8(float64(a) * f),
	}
}
