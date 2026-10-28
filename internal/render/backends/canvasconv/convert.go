// Package canvasconv 为 canvas 绘制后端和字体引擎共享 OFD 画笔与渐变适配。
// 几何类型直接共享，仅在绘制入口补齐空路径、端点和连接器的默认值。
package canvasconv

import (
	"image/color"

	"github.com/zc310/canvas"

	"github.com/zc310/ofd/internal/render/geom"
)

// ToCanvasPath 直接返回共享路径，不重建轮廓或缓存路径快照。
func ToCanvasPath(p *geom.Path) *canvas.Path {
	if p == nil {
		return &canvas.Path{}
	}
	return p
}

// FromCanvasGradient 保留原生轴向/径向渐变的几何类型，使文字轮廓裁剪后
// 仍可序列化为PDF/SVG渐变；其他采样器使用接口包装。
func FromCanvasGradient(g canvas.Gradient) geom.Gradient {
	if g == nil {
		return nil
	}
	copyStops := func(stops canvas.Grad) geom.Grad {
		out := make(geom.Grad, len(stops))
		for i, stop := range stops {
			out[i] = geom.Stop{Offset: stop.Offset, Color: stop.Color}
		}
		return out
	}
	switch gradient := g.(type) {
	case *canvas.LinearGradient:
		out := geom.NewLinearGradient(gradient.Start, gradient.End)
		out.Grad, out.Extend = copyStops(gradient.Grad), gradient.Extend
		return out
	case *canvas.RadialGradient:
		out := geom.NewRadialGradient(gradient.C0, gradient.R0, gradient.C1, gradient.R1)
		out.Grad, out.Extend = copyStops(gradient.Grad), gradient.Extend
		return out
	}
	return canvasGradientSource{g}
}

type canvasGradientSource struct{ g canvas.Gradient }

func (s canvasGradientSource) At(x, y float64) color.RGBA { return s.g.At(x, y) }

// ToCanvasGradient 把 geom.Gradient 包装为 canvas.Gradient。原生的
// *geom.LinearGradient/*geom.RadialGradient 会还原为 canvas 原生线性/径向
// 渐变，以保留 PDF/SVG 的矢量渐变输出；其余自定义采样渐变包装为
// canvas.Gradient（canvas 无法矢量序列化时按采样栅格化）。默认
// LinearColorSpace（canvas 不做 gamma 往返）下采样结果与 canvas 原生渐变一致。
func ToCanvasGradient(g geom.Gradient) canvas.Gradient {
	if g == nil {
		return nil
	}
	switch value := g.(type) {
	case *geom.LinearGradient:
		cg := canvas.NewLinearGradient(value.Start, value.End)
		cg.Grad = toCanvasGrad(value.Grad)
		cg.Extend = value.Extend
		return cg
	case *geom.RadialGradient:
		cg := canvas.NewRadialGradient(value.C0, value.R0, value.C1, value.R1)
		cg.Grad = toCanvasGrad(value.Grad)
		cg.Extend = value.Extend
		return cg
	default:
		return geomGradientSource{g}
	}
}

// toCanvasGrad 把 geom 渐变色标复制为 canvas 渐变色标。
func toCanvasGrad(g geom.Grad) canvas.Grad {
	if len(g) == 0 {
		return nil
	}
	out := make(canvas.Grad, len(g))
	for i, stop := range g {
		out[i] = canvas.Stop{Offset: stop.Offset, Color: stop.Color}
	}
	return out
}

type geomGradientSource struct{ g geom.Gradient }

func (s geomGradientSource) At(x, y float64) color.RGBA { return s.g.At(x, y) }

func (s geomGradientSource) SetColorSpace(canvas.ColorSpace) canvas.Gradient { return s }

// FromCanvasCapper 保留线帽实现，nil沿用平头默认值。
func FromCanvasCapper(c canvas.Capper) geom.Capper {
	if c == nil {
		return geom.ButtCapper{}
	}
	return c
}

// ToCanvasCapper 保留线帽实现，nil沿用平头默认值。
func ToCanvasCapper(c geom.Capper) canvas.Capper {
	if c == nil {
		return canvas.ButtCapper{}
	}
	return c
}

// FromCanvasJoiner 保留连接器实现，nil沿用斜接默认值。
func FromCanvasJoiner(j canvas.Joiner) geom.Joiner {
	if j == nil {
		return geom.MiterJoiner{GapJoiner: geom.BevelJoiner{}, Limit: 4.0}
	}
	return j
}

// ToCanvasJoiner 保留连接器实现，nil沿用斜接默认值。
func ToCanvasJoiner(j geom.Joiner) canvas.Joiner {
	if j == nil {
		return canvas.MiterJoiner{GapJoiner: canvas.BevelJoiner{}, Limit: 4.0}
	}
	return j
}

// FromCanvasPaint 把 canvas.Paint 转为 geom.Paint。
func FromCanvasPaint(p canvas.Paint) geom.Paint {
	if p.IsColor() {
		return geom.SolidPaint(p.Color)
	}
	if p.IsGradient() {
		return geom.GradientPaint(FromCanvasGradient(p.Gradient))
	}
	return geom.Paint{}
}

// ToCanvasPaint 把 geom.Paint 转为 canvas.Paint。
func ToCanvasPaint(p geom.Paint) canvas.Paint {
	if p.IsGradient() {
		return canvas.Paint{Gradient: ToCanvasGradient(p.Gradient)}
	}
	if p.Color != nil {
		return canvas.Paint{Color: color.RGBAModel.Convert(p.Color).(color.RGBA)}
	}
	return canvas.Paint{}
}
