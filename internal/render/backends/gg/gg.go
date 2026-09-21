// Package gg 提供基于 gogpu/gg 的页面栅格化后端，通过空白导入注册到
// internal/render 的注册表：
//
//	import _ "github.com/zc310/ofd/internal/render/backends/gg"
//
// 之后即可用 render.BackendGG="gg" 作为 Document.RasterizePage 的后端名。
// 渲染上下文状态机（Push/Pop、坐标换算、路径扫描、描边缩放）由
// internal/render/backends/rastercore 共享，本包只实现 gogpu/gg 的画笔、
// 路径提交与图像合成。
package gg

import (
	"image"
	"image/color"

	"github.com/gogpu/gg"
	"github.com/tdewolff/canvas"
	"golang.org/x/image/draw"
	"golang.org/x/image/math/f64"

	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/backends/rastercore"
)

var _ render.Backend = (*rastercore.Core)(nil)

func init() {
	_ = render.RegisterBackend(render.BackendGG, func(width, height float64, resolution canvas.Resolution) (render.Backend, error) {
		return New(width, height, resolution)
	})
}

// New 创建 gogpu/gg 光栅后端。输出分辨率由 resolution 决定，页面物理尺寸
// 为 width×height(mm)；Raster 的图片尺寸与 canvas 的 Rasterize 取整规则
// 逐像素一致（int(w*dpmm+0.5)）。
func New(width, height float64, resolution canvas.Resolution) (render.Backend, error) {
	core, err := rastercore.New(width, height, resolution, func(wpx, hpx int, dpmm, hpix float64) rastercore.Hooks {
		pm := gg.NewPixmap(wpx, hpx)
		return &ggHooks{dc: gg.NewContextForPixmap(pm), pm: pm, dpmm: dpmm, hpix: hpix, path: gg.NewPath()}
	})
	if err != nil {
		return nil, err
	}
	return core, nil
}

// ggHooks 是 rastercore.Hooks 的 gogpu/gg 实现。
//
// 坐标模型：rastercore 已把路径换算到设备像素（y 向下），gogpu/gg 的软件
// 栅格器只接受设备坐标，因此这里直接把设备路径交给 gg，不做额外变换。
// 渐变按设备像素中心经完整矩阵逆映射回逻辑毫米后采样 canvas.Gradient，
// 与 canvas 默认的 LinearColorSpace（不做 gamma 往返）一致。图片按 canvas
// rasterizer.RenderImage 的边距与仿射换算，用 x/image 的 Catmull-Rom 直接
// 合成到 gg 像素缓冲。
type ggHooks struct {
	dc   *gg.Context
	pm   *gg.Pixmap
	dpmm float64
	hpix float64
	path *gg.Path // 复用的设备路径缓冲（SetPath 会复制内容）
}

func (h *ggHooks) Push() { h.dc.Push() }
func (h *ggHooks) Pop()  { h.dc.Pop() }

func (h *ggHooks) SetFillSolid(c color.Color)   { h.dc.SetFillBrush(gg.Solid(ggRGBA(c))) }
func (h *ggHooks) SetStrokeSolid(c color.Color) { h.dc.SetStrokeBrush(gg.Solid(ggRGBA(c))) }
func (h *ggHooks) ClearFill()                   { h.dc.SetFillBrush(gg.Solid(gg.Transparent)) }
func (h *ggHooks) ClearStroke()                 { h.dc.SetStrokeBrush(gg.Solid(gg.Transparent)) }

func (h *ggHooks) SetFillRule(rule canvas.FillRule) {
	switch rule {
	case canvas.NonZero:
		h.dc.SetFillRule(gg.FillRuleNonZero)
	default:
		h.dc.SetFillRule(gg.FillRuleEvenOdd)
	}
}

func (h *ggHooks) DrawDevicePath(dp *rastercore.DevicePath, m canvas.Matrix, fill, stroke bool, fillGrad, strokeGrad canvas.Gradient, style rastercore.StrokeStyle) {
	fillGGPath(h.path, dp)
	h.dc.SetPath(h.path)
	if fill {
		if fillGrad != nil {
			h.dc.SetFillBrush(h.gradBrush(fillGrad, m.Inv()))
		}
		// gg 的 Fill/Stroke 会清空路径：同时需要描边时用 FillPreserve，
		// 否则后续 Stroke 拿到的是空路径，描边会被整体丢帧。
		if stroke {
			_ = h.dc.FillPreserve()
		} else {
			_ = h.dc.Fill()
		}
	}
	if stroke {
		h.strokeStyle(style)
		if strokeGrad != nil {
			h.dc.SetStrokeBrush(h.gradBrush(strokeGrad, m.Inv()))
		}
		_ = h.dc.Stroke()
	}
}

func (h *ggHooks) RenderImage(img image.Image, m canvas.Matrix) {
	// 复刻 canvas rasterizer.RenderImage 的边距与仿射换算，直接合成到 gg
	// 像素缓冲：非轴对齐变换时四周加 4px 透明边，避免旋转/斜切采样到缓冲
	// 外像素。LinearColorSpace 下 canvas 不做 gamma 往返，这里同样直接以
	// sRGB 采样。
	src := img
	margin := 0
	if (m[0][1] != 0.0 || m[1][0] != 0.0) && (m[0][0] != 0.0 || m[1][1] == 0.0) {
		margin = 4
		size := img.Bounds().Size()
		img2 := image.NewRGBA(image.Rect(0, 0, size.X+margin*2, size.Y+margin*2))
		draw.Draw(img2, image.Rect(margin, margin, size.X+margin, size.Y+margin), img, img.Bounds().Min, draw.Over)
		src = img2
	}
	hh := float64(h.pm.Height())
	srcH := float64(src.Bounds().Size().Y)
	origin := m.Dot(canvas.Point{X: -float64(margin), Y: srcH - float64(margin)}).Mul(h.dpmm)
	m = m.Scale(h.dpmm, h.dpmm)
	aff3 := f64.Aff3{m[0][0], -m[0][1], origin.X, -m[1][0], m[1][1], hh - origin.Y}
	// 直接把 Pixmap 的预乘 RGBA 缓冲别名为 *image.RGBA 作为目标：x/image/draw
	// 只有在目标和源都是具体标准类型时才走直接采样快路径，否则会逐像素经
	// color.Color 接口盒化（.At/Set），产生大量分配。Pixmap 的 Data 本身即
	// 预乘 RGBA，与 image.RGBA 语义一致。
	dst := &image.RGBA{
		Pix:    h.pm.Data(),
		Stride: h.pm.Width() * 4,
		Rect:   image.Rect(0, 0, h.pm.Width(), h.pm.Height()),
	}
	draw.CatmullRom.Transform(dst, aff3, src, src.Bounds(), draw.Over, nil)
}

func (h *ggHooks) Raster() *image.RGBA { return h.pm.ToImage() }

// gradBrush 返回按绘制完整矩阵逆采样渐变的画笔，复刻 canvas rasterizer
// 的 mInv.Dot(point) 语义；纯色填充的画笔已在 SetFillSolid 设置。
func (h *ggHooks) gradBrush(g canvas.Gradient, invFull canvas.Matrix) gg.Brush {
	dpmm, hpix := h.dpmm, h.hpix
	return gg.NewCustomBrush(func(x, y float64) gg.RGBA {
		p := invFull.Dot(canvas.Point{X: x / dpmm, Y: (hpix - y) / dpmm})
		return ggRGBA(g.At(p.X, p.Y))
	})
}

// strokeStyle 把共享状态机换算好的描边造型写入 gg 画笔层。
func (h *ggHooks) strokeStyle(style rastercore.StrokeStyle) {
	h.dc.SetLineWidth(style.Width)
	switch style.Cap.(type) {
	case canvas.RoundCapper:
		h.dc.SetLineCap(gg.LineCapRound)
	case canvas.SquareCapper:
		h.dc.SetLineCap(gg.LineCapSquare)
	default:
		h.dc.SetLineCap(gg.LineCapButt)
	}
	switch style.Join.(type) {
	case canvas.RoundJoiner:
		h.dc.SetLineJoin(gg.LineJoinRound)
	case canvas.BevelJoiner:
		h.dc.SetLineJoin(gg.LineJoinBevel)
	default:
		h.dc.SetLineJoin(gg.LineJoinMiter)
		h.dc.SetMiterLimit(style.MiterLimit)
	}
	if len(style.Dashes) == 0 {
		h.dc.ClearDash()
		return
	}
	// canvas 在渲染时把虚线长度与相位按线宽缩放（ScaleDash），gg 的原生
	// 虚线是设备像素绝对单位，因此这里做同样的缩放。
	scaled := make([]float64, len(style.Dashes))
	for i, d := range style.Dashes {
		scaled[i] = d * style.Width
	}
	h.dc.SetDash(scaled...)
	h.dc.SetDashOffset(style.DashOffset * style.Width)
}

// fillGGPath 把 rastercore 的设备像素路径写入复用的 gg 路径缓冲，避免每次
// 绘制都分配 gg.Path。调用方随后通过 SetPath 复制内容，因此可安全复用。
func fillGGPath(gp *gg.Path, dp *rastercore.DevicePath) {
	gp.Reset()
	for _, s := range dp.Segs {
		switch s.Op {
		case rastercore.MoveOp:
			gp.MoveTo(s.P[0].X, s.P[0].Y)
		case rastercore.LineOp:
			gp.LineTo(s.P[0].X, s.P[0].Y)
		case rastercore.QuadOp:
			gp.QuadraticTo(s.P[0].X, s.P[0].Y, s.P[1].X, s.P[1].Y)
		case rastercore.CubeOp:
			gp.CubicTo(s.P[0].X, s.P[0].Y, s.P[1].X, s.P[1].Y, s.P[2].X, s.P[2].Y)
		case rastercore.CloseOp:
			gp.Close()
		}
	}
}

// ggRGBA 把 color.Color 转换为 gg 的直通 RGBA（与 canvas 默认的
// LinearColorSpace 一致，不做 gamma 转换）。
func ggRGBA(c color.Color) gg.RGBA {
	if c == nil {
		return gg.Transparent
	}
	r, g, b, a := c.RGBA()
	if a == 0 {
		return gg.Transparent
	}
	f := 1.0 / 65535.0
	return gg.RGBA{R: float64(r) * f, G: float64(g) * f, B: float64(b) * f, A: float64(a) * f}
}
