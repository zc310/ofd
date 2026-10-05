package geom

import (
	"image/color"
	"math"
)

// 常用颜色（与 canvas 的 Transparent/Black/White 一致）。
var (
	Transparent = color.RGBA{0x00, 0x00, 0x00, 0x00}
	Black       = color.RGBA{0x00, 0x00, 0x00, 0xff}
	White       = color.RGBA{0xff, 0xff, 0xff, 0xff}
)

// Gradient 是与具体绘制库无关的渐变采样接口：返回 (x,y) 处的颜色。
// canvas 后端把它包装成 canvas.Gradient；gg 等后端直接用采样画笔消费。
type Gradient interface {
	At(x, y float64) color.RGBA
}

// GradientFunc 让普通函数实现 Gradient。
type GradientFunc func(x, y float64) color.RGBA

// At 实现 Gradient。
func (f GradientFunc) At(x, y float64) color.RGBA { return f(x, y) }

// Stop 是渐变色标。
type Stop struct {
	Offset float64
	Color  color.RGBA
}

// Grad 是按 offset 升序排列的渐变色标集合。
type Grad []Stop

// NewGradient 返回空的渐变色标集合。
func NewGradient() Grad { return Grad{} }

// Add 插入或替换一个色标，保持升序。
func (g *Grad) Add(t float64, c color.RGBA) {
	t = min(max(t, 0.0), 1.0)
	stop := Stop{Offset: t, Color: c}
	for i := range *g {
		if Equal((*g)[i].Offset, stop.Offset) {
			(*g)[i] = stop
			return
		} else if stop.Offset < (*g)[i].Offset {
			*g = append((*g)[:i], append(Grad{stop}, (*g)[i:]...)...)
			return
		}
	}
	*g = append(*g, stop)
}

// At 返回位置 t ∈ [0,1] 处的颜色（线性插值，端点外取端点色）。
func (g Grad) At(t float64) color.RGBA {
	if len(g) == 0 {
		return Transparent
	} else if len(g) == 1 || t <= g[0].Offset {
		return g[0].Color
	} else if g[len(g)-1].Offset <= t {
		return g[len(g)-1].Color
	}
	lo, hi := 0, len(g)-1
	for lo < hi {
		mid := (lo + hi) / 2
		if g[mid].Offset <= t {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	before := g[lo-1]
	after := g[lo]
	u := (t - before.Offset) / (after.Offset - before.Offset)
	return colorLerp(before.Color, after.Color, u)
}

// ToLinear 把色标集合绑定到线性渐变。
func (g Grad) ToLinear(start, end Point) *LinearGradient {
	grad := NewLinearGradient(start, end)
	grad.Grad = g
	return grad
}

// ToRadial 把色标集合绑定到径向渐变。
func (g Grad) ToRadial(c0 Point, r0 float64, c1 Point, r1 float64) *RadialGradient {
	grad := NewRadialGradient(c0, r0, c1, r1)
	grad.Grad = g
	return grad
}

// LinearGradient 是 start→end 的线性渐变。
type LinearGradient struct {
	Grad
	Start, End Point
	// Extend[0] 控制起点之前是否着色、Extend[1] 控制终点之后是否着色；
	// 为 false 时该区域不着色（透明）。
	Extend [2]bool
	d      Point
	d2     float64
}

// NewLinearGradient 返回线性渐变，默认两端都延伸。
func NewLinearGradient(start, end Point) *LinearGradient {
	d := end.Sub(start)
	return &LinearGradient{Start: start, End: end, Extend: [2]bool{true, true}, d: d, d2: d.Dot(d)}
}

// at 返回渐变参数 t 处的颜色，按 Extend 决定轴外是否着色。
func (g *LinearGradient) at(t float64) color.RGBA {
	if math.IsNaN(t) {
		return Transparent
	}
	if t < 0 && !g.Extend[0] {
		return Transparent
	}
	if t > 1 && !g.Extend[1] {
		return Transparent
	}
	return g.Grad.At(t)
}

// At 返回 (x,y) 处的颜色。
func (g *LinearGradient) At(x, y float64) color.RGBA {
	if len(g.Grad) == 0 {
		return Transparent
	}
	p := Point{x, y}.Sub(g.Start)
	if Equal(g.d.Y, 0.0) && !Equal(g.d.X, 0.0) {
		return g.at(p.X / g.d.X)
	} else if !Equal(g.d.Y, 0.0) && Equal(g.d.X, 0.0) {
		return g.at(p.Y / g.d.Y)
	}
	if Equal(g.d2, 0.0) {
		return g.at(0)
	}
	return g.at(p.Dot(g.d) / g.d2)
}

// RadialGradient 是两个圆之间的径向渐变。
type RadialGradient struct {
	Grad
	C0, C1 Point
	R0, R1 float64
	// Extend[0] 控制起始圆之前是否着色、Extend[1] 控制终止圆之后是否着色；
	// 为 false 时该区域不着色（透明）。
	Extend [2]bool
	cd     Point
	dr, a  float64
}

// NewRadialGradient 返回径向渐变，默认两端都延伸。
func NewRadialGradient(c0 Point, r0 float64, c1 Point, r1 float64) *RadialGradient {
	cd := c1.Sub(c0)
	dr := r1 - r0
	return &RadialGradient{C0: c0, R0: r0, C1: c1, R1: r1, Extend: [2]bool{true, true}, cd: cd, dr: dr, a: cd.Dot(cd) - dr*dr}
}

// RadialParameter 返回点 (x,y) 在两圆插值族 ((1-t)·C0+t·C1, R0+t·dr) 中的参数 t，
// 以及该点是否存在可用的解。参数 t 是 PDF/pixman 的同一参数：圆的半径为 R0+t·dr。
//
// 一个点可能同时落在两个圆上，两类根分别取较大的 t：轴内根属于扫描本身，轴外根
// 只有在该方向允许延伸时才算可用（extend 第 0 位允许 t<0 起点侧，第 1 位允许
// t>1 终点侧）。半径 R0+t·dr 为负的圆被排除。没有任何可用解时 ok 为 false，
// 表示该点不在着色范围内（未延伸区域保持透明）。
//
// 轴内根优先于轴外根：延伸只负责扫描范围之外的点，不该覆盖扫描已经给出的颜色。
// 若把两类根放进同一个「取最大 t」的池子，由于任何 t>1 的根都大于任何 t<=1 的根，
// 终点侧延伸会把终止圆附近整片区域压成终点色——Extend 含终点位时那片区域是纯蓝，
// 而同一批点在 Extend=0 与 Extend=3 下仍保留渐变，三者自相矛盾。
// 不同 Extend 组合的差异只出现在扫描覆盖不到的点：例如点只落在 t=1.8 上（轴内无根）时，
// Extend 含终点位取 1.8（终点色），否则透明。
func (g *RadialGradient) RadialParameter(x, y float64, extend int) (float64, bool) {
	pd := Point{x, y}.Sub(g.C0)
	b := pd.Dot(g.cd) + g.R0*g.dr
	c := pd.Dot(pd) - g.R0*g.R0
	a := g.a

	// usable 判断 t 处的族成员是否存在，即半径不为负。
	usable := func(t float64) bool {
		return !math.IsNaN(t) && g.R0+g.dr*t >= 0
	}

	bestCore, haveCore := 0.0, false
	bestExt, haveExt := 0.0, false
	consider := func(t float64) {
		if !usable(t) {
			return
		}
		if t >= 0 && t <= 1 {
			if !haveCore || t > bestCore {
				bestCore, haveCore = t, true
			}
			return
		}
		if t < 0 {
			if extend&1 == 0 {
				return
			}
		} else if extend&2 == 0 {
			return
		}
		if !haveExt || t > bestExt {
			bestExt, haveExt = t, true
		}
	}

	if a == 0 {
		if b == 0 {
			return 0, false
		}
		consider(c / (2.0 * b))
	} else {
		discr := b*b - a*c
		if discr < 0 {
			return 0, false
		}
		sqrtDiscr := math.Sqrt(discr)
		inva := 1.0 / a
		consider((b - sqrtDiscr) * inva)
		consider((b + sqrtDiscr) * inva)
	}

	if haveCore {
		return bestCore, true
	}
	if haveExt {
		return bestExt, true
	}
	return 0, false
}

// At 返回 (x,y) 处的颜色（参考 pixman-radial-gradient 实现）。
func (g *RadialGradient) At(x, y float64) color.RGBA {
	if len(g.Grad) == 0 {
		return Transparent
	}
	// 部分延伸时必须按 Extend 位选择轴外的根；两端都延伸时沿用原来的钳制实现，
	// 保持既有渲染结果不变。
	if !g.Extend[0] || !g.Extend[1] {
		extend := 0
		if g.Extend[0] {
			extend |= 1
		}
		if g.Extend[1] {
			extend |= 2
		}
		t, ok := g.RadialParameter(x, y, extend)
		if !ok {
			return Transparent
		}
		return g.Grad.At(t)
	}
	// 见 https://github.com/servo/pixman/blob/master/pixman/pixman-radial-gradient.c
	pd := Point{x, y}.Sub(g.C0)
	b := pd.Dot(g.cd) + g.R0*g.dr
	c := pd.Dot(pd) - g.R0*g.R0

	// 判别式为负说明该点不在插值族的任何一个圆上：偏心且两圆外离或相交时，
	// 起始圆与终止圆之间存在这样的空隙。此时必须返回透明，不能落到下面的
	// 端点色兜底，否则 Extend=3 会在这些空隙里涂上起始色，而只向起始侧延伸的
	// Extend=1（走 RadialParameter）返回透明，两者对同一点给出不同结果。
	discr := b*b - g.a*c
	if discr < 0 {
		return Transparent
	}
	t0, t1 := solveQuadraticFormula(g.a, -2.0*b, c)

	valid := func(t float64) bool {
		return !math.IsNaN(t) && t >= 0 && t <= 1 && g.R0+g.dr*t >= 0
	}
	hasPositive := func(t float64) bool {
		return !math.IsNaN(t) && t > 0 && g.R0+g.dr*t >= 0
	}
	// 取较大的有效 t（solveQuadraticFormula 返回 t0 <= t1）。依据 PDF 32000-1
	// 8.7.4.5.4 与 pixman：一个点同时落在两个圆上时，只有较大的 t 参与着色。
	if valid(t1) {
		return g.Grad.At(t1)
	}
	if valid(t0) {
		return g.Grad.At(t0)
	}
	// [0,1] 内无有效 t，延展端点颜色。
	if hasPositive(t0) || hasPositive(t1) {
		return g.Grad.At(1)
	}
	return g.Grad.At(0)
}

// colorLerp 在非预乘（straight）空间插值颜色与不透明度。OFD/SVG/PDF 的渐变都
// 独立插值 RGB 与 Alpha；若直接在预乘空间插值，透明红（255,0,0,alpha=0）会被
// 压成 (0,0,0,0)，与蓝色混合后红色分量完全消失，整段渐变只剩蓝色。
func colorLerp(c0, c1 color.RGBA, t float64) color.RGBA {
	n0 := straightColor(c0)
	n1 := straightColor(c1)
	T := uint32(t*65535.0 + 0.5)
	n := color.NRGBA{
		R: lerp(uint32(n0.R)*0x101, uint32(n1.R)*0x101, T),
		G: lerp(uint32(n0.G)*0x101, uint32(n1.G)*0x101, T),
		B: lerp(uint32(n0.B)*0x101, uint32(n1.B)*0x101, T),
		A: lerp(uint32(n0.A)*0x101, uint32(n1.A)*0x101, T),
	}
	return color.RGBAModel.Convert(n).(color.RGBA)
}

// straightColor 把色标转为非预乘（straight）分量。A > 0 时按预乘值还原；
// A == 0 时 NRGBAModel 会因除零保护把 RGB 归零（RGBA{255,0,0,0} 也变成
// {0,0,0,0}），而规范的预乘值在 A == 0 时必为全 0，非 0 字段只可能是构造方
// （render.premultiplied）保留的 straight 载体，此处按字段直读。
func straightColor(c color.RGBA) color.NRGBA {
	if c.A == 0 {
		return color.NRGBA{R: c.R, G: c.G, B: c.B}
	}
	return color.NRGBAModel.Convert(c).(color.NRGBA)
}

func lerp(a, b, t uint32) uint8 {
	return uint8(((0xffff-t)*a + t*b) >> 24)
}
