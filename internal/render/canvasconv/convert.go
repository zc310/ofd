// Package canvasconv 提供 internal/render/geom 的中性几何类型与
// tdewolff/canvas 类型之间的转换，供 canvas 后端（把 geom 绘制指令转成
// canvas.Canvas 用于 Page()/PDF/SVG 矢量输出）以及迁移期的适配层使用。
//
// 它把对 canvas 的依赖集中在这一个小包里：geom 本身保持零绘制库依赖，
// 其余后端（gg/ftgg/tinyskia）只依赖 geom，不依赖本包。
package canvasconv

import (
	"github.com/tdewolff/canvas"
	"image/color"

	"github.com/zc310/ofd/internal/render/geom"
)

// FromCanvasMatrix 把 canvas.Matrix 转为 geom.Matrix（两者布局一致）。
func FromCanvasMatrix(m canvas.Matrix) geom.Matrix { return geom.Matrix(m) }

// ToCanvasMatrix 把 geom.Matrix 转为 canvas.Matrix。
func ToCanvasMatrix(m geom.Matrix) canvas.Matrix { return canvas.Matrix(m) }

// FromCanvasPath 把 canvas.Path 转为 geom.Path。椭圆弧保留为弧段，由 geom
// 在需要时展开。
func FromCanvasPath(p *canvas.Path) *geom.Path {
	out := &geom.Path{}
	if p == nil || p.Empty() {
		return out
	}
	sc := p.Scanner()
	for sc.Scan() {
		switch sc.Cmd() {
		case canvas.MoveToCmd:
			e := sc.End()
			out.MoveTo(e.X, e.Y)
		case canvas.LineToCmd:
			e := sc.End()
			out.LineTo(e.X, e.Y)
		case canvas.QuadToCmd:
			cp, e := sc.CP1(), sc.End()
			out.QuadTo(cp.X, cp.Y, e.X, e.Y)
		case canvas.CubeToCmd:
			c1, c2, e := sc.CP1(), sc.CP2(), sc.End()
			out.CubeTo(c1.X, c1.Y, c2.X, c2.Y, e.X, e.Y)
		case canvas.ArcToCmd:
			rx, ry, rot, large, sweep := sc.Arc()
			e := sc.End()
			out.ArcTo(rx, ry, rot, large, sweep, e.X, e.Y)
		case canvas.CloseCmd:
			out.Close()
		}
	}
	return out
}

// ToCanvasPath 把 geom.Path 转为 canvas.Path。
func ToCanvasPath(p *geom.Path) *canvas.Path {
	out := &canvas.Path{}
	if p == nil {
		return out
	}
	sc := p.Scanner()
	for sc.Scan() {
		switch sc.Cmd() {
		case geom.MoveToCmd:
			e := sc.End()
			out.MoveTo(e.X, e.Y)
		case geom.LineToCmd:
			e := sc.End()
			out.LineTo(e.X, e.Y)
		case geom.QuadToCmd:
			cp, e := sc.CP1(), sc.End()
			out.QuadTo(cp.X, cp.Y, e.X, e.Y)
		case geom.CubeToCmd:
			c1, c2, e := sc.CP1(), sc.CP2(), sc.End()
			out.CubeTo(c1.X, c1.Y, c2.X, c2.Y, e.X, e.Y)
		case geom.ArcToCmd:
			rx, ry, rot, large, sweep := sc.Arc()
			e := sc.End()
			out.ArcTo(rx, ry, rot, large, sweep, e.X, e.Y)
		case geom.CloseCmd:
			out.Close()
		}
	}
	return out
}

// FromCanvasGradient 把 canvas.Gradient 包装为 geom.Gradient。
func FromCanvasGradient(g canvas.Gradient) geom.Gradient {
	if g == nil {
		return nil
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
		cg := canvas.NewLinearGradient(canvas.Point(value.Start), canvas.Point(value.End))
		cg.Grad = toCanvasGrad(value.Grad)
		return cg
	case *geom.RadialGradient:
		cg := canvas.NewRadialGradient(canvas.Point(value.C0), value.R0, canvas.Point(value.C1), value.R1)
		cg.Grad = toCanvasGrad(value.Grad)
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

// ToCanvasFillRule 把 geom.FillRule 转为 canvas.FillRule。
// 两者的常量顺序一致（NonZero/EvenOdd/Positive/Negative），直接按底层值转换。
func ToCanvasFillRule(rule geom.FillRule) canvas.FillRule { return canvas.FillRule(rule) }

// FromCanvasFillRule 把 canvas.FillRule 转为 geom.FillRule。
func FromCanvasFillRule(rule canvas.FillRule) geom.FillRule { return geom.FillRule(rule) }

// FromCanvasResolution 把 canvas.Resolution 转为 geom.Resolution（两者底层
// 均以点/毫米存储）。
func FromCanvasResolution(r canvas.Resolution) geom.Resolution { return geom.Resolution(r) }

// ToCanvasResolution 把 geom.Resolution 转为 canvas.Resolution。
func ToCanvasResolution(r geom.Resolution) canvas.Resolution { return canvas.Resolution(r) }

// FromCanvasCapper 把 canvas 线帽转为 geom 线帽。
func FromCanvasCapper(c canvas.Capper) geom.Capper {
	switch c.(type) {
	case canvas.RoundCapper:
		return geom.RoundCapper{}
	case canvas.SquareCapper:
		return geom.SquareCapper{}
	default:
		return geom.ButtCapper{}
	}
}

// ToCanvasCapper 把 geom 线帽转为 canvas 线帽。
func ToCanvasCapper(c geom.Capper) canvas.Capper {
	switch c.(type) {
	case geom.RoundCapper:
		return canvas.RoundCapper{}
	case geom.SquareCapper:
		return canvas.SquareCapper{}
	default:
		return canvas.ButtCapper{}
	}
}

// FromCanvasJoiner 把 canvas 连接器转为 geom 连接器。
func FromCanvasJoiner(j canvas.Joiner) geom.Joiner {
	switch v := j.(type) {
	case canvas.RoundJoiner:
		return geom.RoundJoiner{}
	case canvas.BevelJoiner:
		return geom.BevelJoiner{}
	case canvas.MiterJoiner:
		return geom.MiterJoiner{GapJoiner: FromCanvasJoiner(v.GapJoiner), Limit: v.Limit}
	default:
		return geom.MiterJoiner{GapJoiner: geom.BevelJoiner{}, Limit: 4.0}
	}
}

// ToCanvasJoiner 把 geom 连接器转为 canvas 连接器。
func ToCanvasJoiner(j geom.Joiner) canvas.Joiner {
	switch v := j.(type) {
	case geom.RoundJoiner:
		return canvas.RoundJoiner{}
	case geom.BevelJoiner:
		return canvas.BevelJoiner{}
	case geom.MiterJoiner:
		return canvas.MiterJoiner{GapJoiner: ToCanvasJoiner(v.GapJoiner), Limit: v.Limit}
	default:
		return canvas.MiterJoiner{GapJoiner: canvas.BevelJoiner{}, Limit: 4.0}
	}
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
