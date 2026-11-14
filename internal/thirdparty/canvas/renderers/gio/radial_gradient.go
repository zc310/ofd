package gio

import "github.com/zc310/ofd/internal/thirdparty/canvas"

// renderUniformRadial 只处理可化简为整平面纯色的特例，不近似其他径向渐变。
// GB/T 33190—2016 8.3.4.3 表 30：保留径向范围及延伸方向。
func (r *Gio) renderUniformRadial(path *canvas.Path, style canvas.Style, m canvas.Matrix) bool {
	fill := style.Fill
	if style.HasStroke() {
		fill = style.Stroke
	}
	g, ok := fill.Gradient.(*canvas.RadialGradient)
	if !ok || fill.IsPattern() || !finite(m.Det()) || m.Det() == 0 {
		return false
	}
	col, ok := g.UniformPlaneColor()
	if !ok {
		return false
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
	w, h := r.width*r.xScale, r.height*r.yScale
	page := r.polygonClip([]canvas.Point{{X: 0, Y: 0}, {X: w, Y: 0}, {X: w, Y: h}, {X: 0, Y: h}}).Push(r.ops)
	r.renderPath(outline, canvas.Paint{Color: col}, m)
	page.Pop()
	return true
}
