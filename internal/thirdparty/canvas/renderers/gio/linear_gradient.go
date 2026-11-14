package gio

import (
	"math"

	"gioui.org/f32"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

// linearAxis 表示屏幕坐标上的参数场 t(p)=dot(p-origin, normal)。
// 以逆转置变换梯度方向，才能在非等比缩放、切变下维持原有等色线；
// 直接变换轴线的两个端点，仅在相似变换下正确。
type linearAxis struct {
	origin canvas.Point
	normal canvas.Point
}

func (r *Gio) gradientAxis(g *canvas.LinearGradient, m canvas.Matrix) (linearAxis, bool) {
	d, ok := g.ParameterDirection()
	if !ok || !finite(d.X) || !finite(d.Y) || m.Det() == 0 || r.xScale == 0 || r.yScale == 0 {
		return linearAxis{}, false
	}
	inverse := m.Inv()
	start := m.Dot(g.Start)
	axis := linearAxis{
		origin: canvas.Point{X: r.xScale * start.X, Y: r.yScale * (r.height - start.Y)},
		normal: canvas.Point{
			X: (inverse[0][0]*d.X + inverse[1][0]*d.Y) / r.xScale,
			Y: -(inverse[0][1]*d.X + inverse[1][1]*d.Y) / r.yScale,
		},
	}
	if !finite(axis.origin.X) || !finite(axis.origin.Y) || !finite(axis.normal.X) || !finite(axis.normal.Y) || axis.normal.Dot(axis.normal) == 0 {
		return linearAxis{}, false
	}
	return axis, true
}

func (a linearAxis) parameter(p canvas.Point) float64 { return p.Sub(a.origin).Dot(a.normal) }

func (a linearAxis) point(t float64) f32.Point {
	p := a.origin.Add(a.normal.Mul(t / a.normal.Dot(a.normal)))
	return f32.Pt(float32(p.X), float32(p.Y))
}

// clipParameter 将有限页面矩形裁到指定渐变参数半平面。没有引入像素尺寸，
// 页面缓存缩放时仍是矢量裁剪；Extend 的边界在 t=0/1，而非首末色标位置。
func clipParameter(polygon []canvas.Point, axis linearAxis, limit float64, greater bool) []canvas.Point {
	if len(polygon) == 0 {
		return nil
	}
	inside := func(t float64) bool {
		if greater {
			return t >= limit
		}
		return t <= limit
	}
	var result []canvas.Point
	previous := polygon[len(polygon)-1]
	tPrevious := axis.parameter(previous)
	for _, current := range polygon {
		tCurrent := axis.parameter(current)
		if inside(tPrevious) != inside(tCurrent) {
			u := (limit - tPrevious) / (tCurrent - tPrevious)
			result = append(result, previous.Add(current.Sub(previous).Mul(u)))
		}
		if inside(tCurrent) {
			result = append(result, current)
		}
		previous, tPrevious = current, tCurrent
	}
	return result
}

func (r *Gio) linearExtendClip(g *canvas.LinearGradient, axis linearAxis) clip.Op {
	w, h := r.width*r.xScale, r.height*r.yScale
	polygon := []canvas.Point{{X: 0, Y: 0}, {X: w, Y: 0}, {X: w, Y: h}, {X: 0, Y: h}}
	if !g.Extend[0] {
		polygon = clipParameter(polygon, axis, 0, true)
	}
	if !g.Extend[1] {
		polygon = clipParameter(polygon, axis, 1, false)
	}
	return r.polygonClip(polygon)
}

func (r *Gio) polygonClip(polygon []canvas.Point) clip.Op {
	var path clip.Path
	path.Begin(r.ops)
	for i, p := range polygon {
		point := f32.Pt(float32(p.X), float32(p.Y))
		if i == 0 {
			path.MoveTo(point)
		} else {
			path.LineTo(point)
		}
	}
	if len(polygon) > 0 {
		path.Close()
	}
	return (clip.Outline{Path: path.End()}).Op()
}

// renderLinearGradient 使用 Gio 原生 LinearGradientOp，不生成固定分辨率图片。
// 多色标通过从前往后覆盖的半平面分段，前段在后方保持端点颜色，避免并排
// 裁剪的抗锯齿覆盖不足产生白缝。多色标仅接受统一 Alpha，先以不透明色绘制，
// 再用一个 GPU 透明层合成，避免半透明分段重复叠色；同色且透明度线性变化
// 的多色标可安全合并为首尾色标，其余透明度变化的情况回退。
// GB/T 33190—2016 8.3.4.2：保留色标位置、Extend 与对象变换。
// 颜色插值及透明合成遵循 Gio 的线性颜色空间。
func (r *Gio) renderLinearGradient(path *canvas.Path, style canvas.Style, m canvas.Matrix) bool {
	fill := style.Fill
	if style.HasStroke() {
		fill = style.Stroke
	}
	g, ok := fill.Gradient.(*canvas.LinearGradient)
	if !ok || fill.IsPattern() || len(g.Grad) < 2 {
		return false
	}
	varyingAlpha := false
	for i, stop := range g.Grad {
		if !finite(stop.Offset) || stop.Offset < 0 || stop.Offset > 1 || (i > 0 && stop.Offset <= g.Grad[i-1].Offset) {
			return false
		}
		// canvas 允许全透明色标携带直通 RGB，参与非预乘插值；Gio 的
		// 预乘插值无法保留这种颜色信息，不能将它当成普通透明黑色。
		if stop.Color.A == 0 && (stop.Color.R != 0 || stop.Color.G != 0 || stop.Color.B != 0) {
			return false
		}
		varyingAlpha = varyingAlpha || stop.Color.A != g.Grad[0].Color.A
	}
	mergeStops := len(g.Grad) > 2 && varyingAlpha
	if mergeStops && !linearSameColorAlpha(g.Grad) {
		return false
	}
	axis, ok := r.gradientAxis(g, m)
	if !ok {
		return false
	}
	// 先验证全部色标，保证失败回退前没有向操作列表写入一部分渐变。
	previous := r.gradientPoint(axis, g.Grad[0].Offset)
	for _, stop := range g.Grad[1:] {
		point := r.gradientPoint(axis, stop.Offset)
		if point == previous || !validGradientPoint(previous) || !validGradientPoint(point) {
			return false
		}
		previous = point
	}
	outline := path.Copy()
	if style.HasStroke() {
		if style.IsDashed() {
			offset, dashes := canvas.ScaleDash(style.StrokeWidth, style.DashOffset, style.Dashes)
			outline = outline.Dash(offset, dashes...)
		}
		outline = outline.Stroke(style.StrokeWidth, style.StrokeCapper, style.StrokeJoiner, canvas.Tolerance)
	} else if style.FillRule != canvas.NonZero {
		outline = outline.Settle(style.FillRule)
	}
	defer r.pathClip(outline, m).Push(r.ops).Pop()
	if !g.Extend[0] || !g.Extend[1] {
		defer r.linearExtendClip(g, axis).Push(r.ops).Pop()
	}
	if len(g.Grad) == 2 || mergeStops {
		r.paintLinearSegment(axis, g.Grad[0], g.Grad[len(g.Grad)-1], false)
	} else {
		alpha := g.Grad[0].Color.A
		if alpha < 255 {
			defer paint.PushOpacity(r.ops, float32(alpha)/255).Pop()
		}
		r.paintLinearSegment(axis, g.Grad[0], g.Grad[1], true)
		for i := 1; i < len(g.Grad)-1; i++ {
			w, h := r.width*r.xScale, r.height*r.yScale
			polygon := clipParameter([]canvas.Point{{X: 0, Y: 0}, {X: w, Y: 0}, {X: w, Y: h}, {X: 0, Y: h}}, axis, g.Grad[i].Offset, true)
			stack := r.polygonClip(polygon).Push(r.ops)
			r.paintLinearSegment(axis, g.Grad[i], g.Grad[i+1], true)
			stack.Pop()
		}
	}
	r.nativeLinear = true
	return true
}

// 同色且 Alpha 沿色轴线性变化时，Gio 预乘颜色的插值也在同一直线上。
// 全透明色标的 RGB 不参与预乘插值；其余色标必须具有完全相同的非预乘颜色。
func linearSameColorAlpha(stops canvas.Grad) bool {
	first, last := stops[0], stops[len(stops)-1]
	var red, green, blue uint8
	haveColor := false
	for _, stop := range stops {
		c := toNRGBA(stop.Color)
		if c.A != 0 {
			if haveColor && (red != c.R || green != c.G || blue != c.B) {
				return false
			}
			red, green, blue, haveColor = c.R, c.G, c.B, true
		}
		want := float64(first.Color.A) + (float64(last.Color.A)-float64(first.Color.A))*(stop.Offset-first.Offset)/(last.Offset-first.Offset)
		if math.Abs(float64(c.A)-want) > 1e-9 {
			return false
		}
	}
	return true
}

func validGradientPoint(p f32.Point) bool { return finite(float64(p.X)) && finite(float64(p.Y)) }

func (r *Gio) gradientPoint(axis linearAxis, t float64) f32.Point {
	p := axis.point(t)
	if r.gradientScale > 0 {
		return p.Mul(r.gradientScale)
	}
	return p
}

func (r *Gio) paintLinearSegment(axis linearAxis, a, b canvas.Stop, opaque bool) {
	c1, c2 := toNRGBA(a.Color), toNRGBA(b.Color)
	if opaque {
		c1.A, c2.A = 255, 255
	}
	paint.LinearGradientOp{Stop1: r.gradientPoint(axis, a.Offset), Stop2: r.gradientPoint(axis, b.Offset), Color1: c1, Color2: c2}.Add(r.ops)
	paint.PaintOp{}.Add(r.ops)
}
