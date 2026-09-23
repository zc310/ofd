// Package tinyskia 提供基于 tiny-skia 纯 Go 移植（Canvas 2D API）的页面
// 栅格化后端，通过空白导入注册到 internal/render 的注册表：
//
//	import _ "github.com/zc310/ofd/internal/render/backends/tinyskia"
//
// 之后即可用 drawing.BackendTinySkia="tinyskia" 作为栅格后端名，或经
// converter.RasterBackend("tinyskia")
// 的后端名。只用于渲染速度对照实验，不承诺与 canvas/gogpu 的逐像素输出
// 一致。渲染上下文状态机由 internal/render/backends/rastercore 共享，本包
// 只依赖 geom 类型，不依赖 tdewolff/canvas。
package tinyskia

import (
	"image"
	"image/color"

	"github.com/lumifloat/tinyskia"

	"github.com/zc310/ofd/internal/render/backends/rastercore"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
)

var _ drawing.Backend = (*rastercore.Core)(nil)

func init() {
	_ = drawing.RegisterBackend(drawing.BackendTinySkia, func(width, height float64, resolution geom.Resolution) (drawing.Backend, error) {
		return New(width, height, resolution)
	})
}

// New 创建 tinyskia 光栅后端。输出分辨率与像素尺寸规则和其它后端一致。
func New(width, height float64, resolution geom.Resolution) (drawing.Backend, error) {
	core, err := rastercore.New(width, height, resolution, func(wpx, hpx int, dpmm, hpix float64) rastercore.Hooks {
		c := tinyskia.NewCanvas(wpx, hpx)
		return &tinyskiaHooks{canvas: c, ctx: c.GetContext(), dpmm: dpmm, hpix: hpix}
	})
	if err != nil {
		return nil, err
	}
	return core, nil
}

// tinyskiaHooks 是 rastercore.Hooks 的 tinyskia 实现。
//
// tinyskia 的 FillPath/StrokePath 会把 context 变换矩阵作用到路径几何上，
// 为避免重复变换，共享状态机已把路径换算成设备像素且保持 context 变换为
// 恒等矩阵。渐变无法注入自定义采样器（Style 接口内部实现），改为把 geom
// 渐变的几何换算到设备空间后重建 tinyskia 的 Linear/RadialGradient。
type tinyskiaHooks struct {
	canvas   *tinyskia.Canvas
	ctx      *tinyskia.Context
	dpmm     float64
	hpix     float64
	fillRule geom.FillRule
}

func (h *tinyskiaHooks) Push() { h.ctx.Save() }
func (h *tinyskiaHooks) Pop()  { h.ctx.Restore() }

func (h *tinyskiaHooks) SetFillSolid(c color.Color)   { h.ctx.SetFillStyleSolidColor(c) }
func (h *tinyskiaHooks) SetStrokeSolid(c color.Color) { h.ctx.SetStrokeStyleSolidColor(c) }
func (h *tinyskiaHooks) ClearFill()                   { h.ctx.SetFillStyleSolidColor(color.RGBA{}) }
func (h *tinyskiaHooks) ClearStroke()                 { h.ctx.SetStrokeStyleSolidColor(color.RGBA{}) }

func (h *tinyskiaHooks) SetFillRule(rule geom.FillRule) { h.fillRule = rule }

func (h *tinyskiaHooks) DrawDevicePath(dp *rastercore.DevicePath, m geom.Matrix, fill, stroke bool, fillGrad, strokeGrad geom.Gradient, style rastercore.StrokeStyle) {
	h.ctx.BeginPath()
	replayPath(h.ctx, dp)
	if fill {
		if fillGrad != nil {
			h.setGradient(fillGrad, m, true)
		}
		// tinyskia 的 Fill/Stroke 都不会自动清空路径，因此同一条路径可以
		// 先 Fill 后 Stroke；这里显式使用填充规则版本区分 NonZero/EvenOdd。
		switch h.fillRule {
		case geom.EvenOdd:
			h.ctx.FillWithFillRule(tinyskia.CanvasFillRuleEvenodd)
		default:
			h.ctx.FillWithFillRule(tinyskia.CanvasFillRuleNonzero)
		}
	}
	if stroke {
		h.strokeStyle(style)
		if strokeGrad != nil {
			h.setGradient(strokeGrad, m, false)
		}
		h.ctx.Stroke()
	}
}

func (h *tinyskiaHooks) RenderImage(img image.Image, m geom.Matrix) {
	// 把图像像素→逻辑毫米的矩阵 m 换算成设备像素仿射，交给 tinyskia 的
	// DrawImageScaled：源图像直接经该仿射变换采样覆盖矩形。canvas 的
	// RenderImage 用 Catmull-Rom，此处是 tiny-skia 自身的 pattern 引擎。
	src := img.Bounds().Size()
	cw, ch := float64(src.X), float64(src.Y)
	h.ctx.SetTransformValues(
		h.dpmm*m[0][0], -h.dpmm*m[1][0], h.dpmm*m[0][1], -h.dpmm*m[1][1],
		h.dpmm*m[0][2], h.hpix-h.dpmm*m[1][2],
	)
	h.ctx.DrawImageScaled(img, 0, 0, cw, ch)
	h.ctx.ResetTransform()
}

func (h *tinyskiaHooks) Raster() *image.RGBA {
	return h.canvas.Image().(*image.RGBA)
}

// setGradient 把 geom 渐变几何换算到设备空间后重建为 tinyskia 渐变样式。
// isFill 选择 fill/stroke 样式。
func (h *tinyskiaHooks) setGradient(g geom.Gradient, m geom.Matrix, isFill bool) {
	set := h.ctx.SetFillStyleGradient
	if !isFill {
		set = h.ctx.SetStrokeStyleGradient
	}
	if lg, ok := g.(*geom.LinearGradient); ok {
		sx, sy := h.toDev(m, lg.Start.X, lg.Start.Y)
		ex, ey := h.toDev(m, lg.End.X, lg.End.Y)
		if gr, err := h.ctx.CreateLinearGradient(sx, sy, ex, ey); err == nil {
			addStops(gr, lg.Grad)
			set(gr)
			return
		}
	} else if rg, ok := g.(*geom.RadialGradient); ok {
		scale := h.dpmm * rastercore.MatrixScale(m)
		c0x, c0y := h.toDev(m, rg.C0.X, rg.C0.Y)
		c1x, c1y := h.toDev(m, rg.C1.X, rg.C1.Y)
		if gr, err := h.ctx.CreateRadialGradient(c0x, c0y, rg.R0*scale, c1x, c1y, rg.R1*scale); err == nil {
			addStops(gr, rg.Grad)
			set(gr)
			return
		}
	}
	// 不支持的渐变类型：用透明底色兜底，避免沿用上一个样式。
	if isFill {
		h.ctx.SetFillStyleSolidColor(color.RGBA{})
	} else {
		h.ctx.SetStrokeStyleSolidColor(color.RGBA{})
	}
}

// replayPath 把设备像素路径逐段追加到 tinyskia 的当前路径。
func replayPath(ctx *tinyskia.Context, dp *rastercore.DevicePath) {
	for _, s := range dp.Segs {
		switch s.Op {
		case rastercore.MoveOp:
			ctx.MoveTo(s.P[0].X, s.P[0].Y)
		case rastercore.LineOp:
			ctx.LineTo(s.P[0].X, s.P[0].Y)
		case rastercore.QuadOp:
			ctx.QuadraticCurveTo(s.P[0].X, s.P[0].Y, s.P[1].X, s.P[1].Y)
		case rastercore.CubeOp:
			ctx.BezierCurveTo(s.P[0].X, s.P[0].Y, s.P[1].X, s.P[1].Y, s.P[2].X, s.P[2].Y)
		case rastercore.CloseOp:
			ctx.ClosePath()
		}
	}
}

// toDev 把逻辑毫米坐标按矩阵 m 换算成设备像素坐标
// device = {dpmm·X, Hpix − dpmm·Y}。
func (h *tinyskiaHooks) toDev(m geom.Matrix, x, y float64) (float64, float64) {
	lx := m[0][0]*x + m[0][1]*y + m[0][2]
	ly := m[1][0]*x + m[1][1]*y + m[1][2]
	return h.dpmm * lx, h.hpix - h.dpmm*ly
}

// strokeStyle 把共享状态机换算好的描边造型写入 tinyskia 上下文。线宽、虚线
// 长度与相位均为设备像素，虚线同样按线宽缩放（同 canvas 的 ScaleDash）。
func (h *tinyskiaHooks) strokeStyle(style rastercore.StrokeStyle) {
	h.ctx.SetLineWidth(style.Width)
	switch style.Cap.(type) {
	case geom.RoundCapper:
		h.ctx.SetLineCap(tinyskia.CanvasLineCapRound)
	case geom.SquareCapper:
		h.ctx.SetLineCap(tinyskia.CanvasLineCapSquare)
	default:
		h.ctx.SetLineCap(tinyskia.CanvasLineCapButt)
	}
	switch style.Join.(type) {
	case geom.RoundJoiner:
		h.ctx.SetLineJoin(tinyskia.CanvasLineJoinRound)
	case geom.BevelJoiner:
		h.ctx.SetLineJoin(tinyskia.CanvasLineJoinBevel)
	default:
		h.ctx.SetLineJoin(tinyskia.CanvasLineJoinMiter)
		h.ctx.SetMiterLimit(style.MiterLimit)
	}
	if len(style.Dashes) == 0 {
		h.ctx.SetLineDash(nil)
		h.ctx.SetLineDashOffset(0)
		return
	}
	scaled := make([]float64, len(style.Dashes))
	for i, d := range style.Dashes {
		scaled[i] = d * style.Width
	}
	h.ctx.SetLineDash(scaled)
	h.ctx.SetLineDashOffset(style.DashOffset * style.Width)
}

// addStops 把 geom 的渐变停靠点复制进 tinyskia 渐变对象。
func addStops(gr tinyskia.Gradient, grad geom.Grad) {
	for _, s := range grad {
		_ = gr.AddColorStop(s.Offset, s.Color)
	}
}
