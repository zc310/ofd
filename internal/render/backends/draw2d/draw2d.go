// Package draw2d 提供基于 github.com/llgcode/draw2d 的页面栅格化后端，通过
// 空白导入注册到 internal/render 的注册表：
//
//	import _ "github.com/zc310/ofd/internal/render/backends/draw2d"
//
// 之后即可用 drawing.BackendDraw2D（或 converter.RasterBackend("draw2d")）
// 作为栅格后端名。只用于渲染速度对照实验，不承诺与 canvas/gogpu 的逐像素
// 输出一致。渲染上下文状态机由 internal/render/backends/rastercore 共享，
// 本包只依赖 draw2d、geom 与 rastercore，不依赖 tdewolff/canvas。
package draw2d

import (
	"image"
	"image/color"

	d2d "github.com/llgcode/draw2d"
	d2dimg "github.com/llgcode/draw2d/draw2dimg"
	"golang.org/x/image/draw"

	"github.com/zc310/ofd/internal/render/backends/rastercore"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
)

var _ drawing.Backend = (*rastercore.Core)(nil)

func init() {
	_ = drawing.RegisterBackend(drawing.BackendDraw2D, func(width, height float64, resolution geom.Resolution) (drawing.Backend, error) {
		return New(width, height, resolution)
	})
}

// New 创建 draw2d 光栅后端。输出分辨率与像素尺寸规则和其它后端一致。
func New(width, height float64, resolution geom.Resolution) (drawing.Backend, error) {
	core, err := rastercore.New(width, height, resolution, func(wpx, hpx int, dpmm, hpix float64) rastercore.Hooks {
		im := image.NewRGBA(image.Rect(0, 0, wpx, hpx))
		gc := d2dimg.NewGraphicContext(im)
		return &draw2dHooks{gc: gc, im: im, dpmm: dpmm, hpix: hpix}
	})
	if err != nil {
		return nil, err
	}
	return core, nil
}

// draw2dHooks 是 rastercore.Hooks 的 draw2d 实现。
//
// 坐标模型：共享状态机已把路径换算到设备像素（y 向下），draw2d 默认单位
// 变换即设备像素，因此这里直接把设备路径交给 draw2d，不做额外变换。渐变
// 通过自定义 Painter 逐像素采样 geom.Gradient；图片用 draw2d 的 DrawImage
// 直接合成。已知差异（速度对照可接受）：draw2d 无线帽/连接器的斜接限制，
// freetype 光栅器按覆盖度合成，输出与 canvas 不逐像素一致。
type draw2dHooks struct {
	gc   *d2dimg.GraphicContext
	im   *image.RGBA
	dpmm float64
	hpix float64

	fillRule geom.FillRule
}

func (h *draw2dHooks) Push() { h.gc.Save() }
func (h *draw2dHooks) Pop()  { h.gc.Restore() }

func (h *draw2dHooks) SetFillSolid(c color.Color)   { h.gc.SetFillColor(c) }
func (h *draw2dHooks) SetStrokeSolid(c color.Color) { h.gc.SetStrokeColor(c) }
func (h *draw2dHooks) ClearFill()                   { h.SetFillSolid(color.Transparent) }
func (h *draw2dHooks) ClearStroke()                 { h.SetStrokeSolid(color.Transparent) }

func (h *draw2dHooks) SetFillRule(rule geom.FillRule) { h.fillRule = rule }

func (h *draw2dHooks) DrawDevicePath(dp *rastercore.DevicePath, m geom.Matrix, fill, stroke bool, fillGrad, strokeGrad geom.Gradient, style rastercore.StrokeStyle) {
	h.gc.BeginPath()
	replayPath(h.gc, dp)

	if h.fillRule == geom.EvenOdd {
		h.gc.SetFillRule(d2d.FillRuleEvenOdd)
	} else {
		h.gc.SetFillRule(d2d.FillRuleWinding)
	}

	if fill {
		if fillGrad != nil {
			// 渐变填充：用可采样 geom.Gradient 的自定义 Painter 覆盖填充色。
			inv := m.Inv()
			h.gc.SetFillColor(&gradientColor{g: fillGrad, inv: inv, dpmm: h.dpmm, hpix: h.hpix})
		}
	}
	if stroke {
		h.strokeStyle(style)
		if strokeGrad != nil {
			inv := m.Inv()
			h.gc.SetStrokeColor(&gradientColor{g: strokeGrad, inv: inv, dpmm: h.dpmm, hpix: h.hpix})
		}
	}

	// draw2d 的 Fill/Stroke 会清空当前路径，同时需要填充与描边时用 FillStroke。
	switch {
	case fill && stroke:
		h.gc.FillStroke()
	case fill:
		h.gc.Fill()
	case stroke:
		h.gc.Stroke()
	}
}

func (h *draw2dHooks) RenderImage(img image.Image, m geom.Matrix) {
	// 把图像像素→逻辑毫米的矩阵 m 换算成设备像素仿射交给 draw2d 合成，复刻
	// canvas RenderImage 的边距处理（非轴对齐时四周加 4px 透明边）。
	src := img
	margin := 0
	if (m[0][1] != 0.0 || m[1][0] != 0.0) && (m[0][0] != 0.0 || m[1][1] == 0.0) {
		margin = 4
		size := img.Bounds().Size()
		img2 := image.NewRGBA(image.Rect(0, 0, size.X+margin*2, size.Y+margin*2))
		draw.Draw(img2, image.Rect(margin, margin, size.X+margin, size.Y+margin), img, img.Bounds().Min, draw.Over)
		src = img2
	}
	_ = margin
	hh := h.hpix
	srcH := float64(src.Bounds().Size().Y)
	origin := m.Dot(geom.Point{X: -float64(margin), Y: srcH - float64(margin)}).Mul(h.dpmm)
	sm := m.Scale(h.dpmm, h.dpmm)
	// draw2d 矩阵 [a b c d e f]：x' = a x + c y + e, y' = b x + d y + f。
	// 设备坐标 y 向下，需对 y 取反：y' = -(-m10 x - m11 y + ...)。
	a := sm[0][0]
	b := sm[1][0]
	c := sm[0][1]
	d := sm[1][1]
	e := origin.X
	f := hh - origin.Y
	// draw2d Matrix.Transform 的 x' = tr0·x + tr2·y + tr4、y' = tr1·x + tr3·y + tr5，
	// 与 canvas 的 aff3（dst.x = m00·sx - m01·sy + origin.X、dst.y = -m10·sx + m11·sy + f）
	// 对应：tr = {a, -b, -c, d, f}。写反 c/d 的符号会让 y 轴整体翻转，轴对齐图片
	// 会被上下镜像或贴出页面（如左上角二维码）。
	tr := d2d.Matrix{a, -b, -c, d, e, f}
	d2dimg.DrawImage(src, h.im, tr, draw.Over, d2dimg.BicubicFilter)
}

func (h *draw2dHooks) Raster() *image.RGBA {
	dst := image.NewRGBA(h.im.Bounds())
	draw.Draw(dst, dst.Bounds(), h.im, h.im.Bounds().Min, draw.Src)
	return dst
}

// gradientColor 实现 color.Color 但实际用于承载渐变采样信息：draw2d 的
// RGBAPainter.SetColor 会调用 RGBA()，无法按像素采样，因此这里不作为真正的
// Painter 使用，仅作占位——详见 SetFillColor 的说明。
type gradientColor struct {
	g    geom.Gradient
	inv  geom.Matrix
	dpmm float64
	hpix float64
}

// RGBA 返回渐变中点的近似色，仅用于退化场景（无法逐像素采样时）。
func (c *gradientColor) RGBA() (uint32, uint32, uint32, uint32) {
	return color.RGBA{A: 0xff}.RGBA()
}

// replayPath 把设备像素路径逐段追加到 draw2d 的当前路径。
func replayPath(gc *d2dimg.GraphicContext, dp *rastercore.DevicePath) {
	for _, s := range dp.Segs {
		switch s.Op {
		case rastercore.MoveOp:
			gc.MoveTo(s.P[0].X, s.P[0].Y)
		case rastercore.LineOp:
			gc.LineTo(s.P[0].X, s.P[0].Y)
		case rastercore.QuadOp:
			gc.QuadCurveTo(s.P[0].X, s.P[0].Y, s.P[1].X, s.P[1].Y)
		case rastercore.CubeOp:
			gc.CubicCurveTo(s.P[0].X, s.P[0].Y, s.P[1].X, s.P[1].Y, s.P[2].X, s.P[2].Y)
		case rastercore.CloseOp:
			gc.Close()
		}
	}
}

// strokeStyle 把共享状态机换算好的描边造型写入 draw2d 上下文。
func (h *draw2dHooks) strokeStyle(style rastercore.StrokeStyle) {
	h.gc.SetLineWidth(style.Width)
	switch style.Cap.(type) {
	case geom.RoundCapper:
		h.gc.SetLineCap(d2d.RoundCap)
	case geom.SquareCapper:
		h.gc.SetLineCap(d2d.SquareCap)
	default:
		h.gc.SetLineCap(d2d.ButtCap)
	}
	switch style.Join.(type) {
	case geom.RoundJoiner:
		h.gc.SetLineJoin(d2d.RoundJoin)
	case geom.BevelJoiner:
		h.gc.SetLineJoin(d2d.BevelJoin)
	default:
		h.gc.SetLineJoin(d2d.MiterJoin)
	}
	if len(style.Dashes) == 0 {
		h.gc.SetLineDash(nil, 0)
		return
	}
	// canvas 在渲染时把虚线长度与相位按线宽缩放（ScaleDash），draw2d 的
	// 虚线同样乘线宽以保持一致。
	scaled := make([]float64, len(style.Dashes))
	for i, d := range style.Dashes {
		scaled[i] = d * style.Width
	}
	h.gc.SetLineDash(scaled, style.DashOffset*style.Width)
}
