// Package testscene 提供后端一致性测试共用的“页面场景”与配套图元/度量：
// 同一段 DrawContext 绘制命令可依次作用到不同光栅后端，用于逐像素对照
// 与速度测试。它只依赖 canvas 数学结构与一个与 render.DrawContext 结构
// 等价的本地接口（Renderer），因此不引入 render 包，避免内部测试包与插件
// 包之间的 import cycle。
package testscene

import (
	"image"
	"image/color"
	"math"
	"os"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
)

// Renderer 是本包场景所需的绘制操作面，方法签名与 render.DrawContext 中
// 对应方法完全一致。canvas 后端与各栅格插件后端（gogpu/gg、FloatTech/gg、
// tinyskia）都满足该接口。
type Renderer interface {
	SetFillColor(c color.Color)
	SetFillGradient(g canvas.Gradient)
	SetFillRule(rule canvas.FillRule)
	ClearFill()
	SetStrokeColor(c color.Color)
	SetStrokeWidth(w float64)
	SetStrokeCapper(cap canvas.Capper)
	SetStrokeJoiner(join canvas.Joiner)
	ClearStroke()
	SetDashes(offset float64, dashes ...float64)
	DrawPath(x, y float64, p *canvas.Path)
	DrawImage(img image.Image, x, y float64, dpmm float64)
	TextPath(p *canvas.Path, x, y float64)
	RenderImage(img image.Image, m canvas.Matrix)
	CopyStrokeToFill()
	CurrentMatrix() canvas.Matrix
}

// PickTestFont 依次尝试已知可用的本地字体路径，找不到时返回空串。
func PickTestFont() string {
	for _, p := range []string{
		"~/.local/share/fonts/smiley-sans-v2.0.1/SmileySans-Oblique.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
	} {
		if st, err := os.Stat(p); err == nil && st.Size() > 0 {
			return p
		}
	}
	return ""
}

// LoadTestFace 从字体文件加载测试字体 14px。
func LoadTestFace(t testing.TB, fontPath string) *canvas.FontFace {
	t.Helper()
	family := canvas.NewFontFamily("backend-parity")
	if err := family.LoadFontFile(fontPath, canvas.FontRegular); err != nil {
		t.Skipf("字体加载失败 %s: %v", fontPath, err)
	}
	return family.Face(14)
}

// SceneDirect 用 canvas.Context 的既有调用风格绘制测试场景（含背景、图片、
// 开放路径描边、EvenOdd 双圆、渐变矩形、布尔裁剪、字形文字、虚线圆角矩形、
// 离屏图贴回与描边复制）。
func SceneDirect(t testing.TB, ctx *canvas.Context, fontPath string) {
	face := LoadTestFace(t, fontPath)

	ctx.SetFillColor(canvas.White)
	bleed := 0.0
	ctx.DrawPath(-bleed, -bleed, canvas.Rectangle(200+2*bleed, 140+2*bleed))

	ctx.DrawImage(14, 14, MaskedChecker(), canvas.DPMM(1))

	ctx.SetFill(nil)
	ctx.SetStrokeColor(cGreen)
	ctx.SetStrokeWidth(1.4)
	ctx.SetStrokeJoiner(canvas.MiterJoin)
	ctx.SetStrokeCapper(canvas.ButtCap)
	ctx.DrawPath(0, 0, WavePath())

	ctx.SetStroke(nil)
	ctx.SetFillRule(canvas.EvenOdd)
	ctx.SetFillColor(cRed)
	p := CirclePath(50, 105, 16)
	p = AppendCircle(p, 68, 105, 16)
	ctx.DrawPath(0, 0, p)
	ctx.SetFillRule(canvas.NonZero)

	g := canvas.Grad{}
	g.Add(0, cGradA)
	g.Add(1, cGradB)
	ctx.SetFillGradient(g.ToLinear(canvas.Point{X: 110, Y: 105}, canvas.Point{X: 188, Y: 128}))
	ctx.SetStrokeColor(canvas.Transparent)
	ctx.DrawPath(0, 0, canvas.Rectangle(78, 23).Translate(110, 105))

	clip := canvas.Circle(20).Translate(148, 37).And(canvas.Rectangle(24, 24).Translate(136, 25))
	fill := canvas.Circle(34).Translate(148, 37).And(clip)
	ctx.SetFillColor(cOrange)
	ctx.DrawPath(0, 0, fill)

	tp, _ := face.ToPath("OFD render")
	tp = tp.Transform(canvas.Identity.ReflectY()).Translate(42, 20)
	ctx.SetFillColor(cDark)
	ctx.SetStrokeColor(canvas.Transparent)
	ctx.DrawPath(0, 0, tp)

	ctx.SetFill(nil)
	ctx.SetStrokeColor(cNavy)
	ctx.SetStrokeWidth(1.2)
	ctx.SetDashes(0, 3, 1.2)
	ctx.SetStrokeCapper(canvas.RoundCap)
	ctx.SetStrokeJoiner(canvas.RoundJoin)
	ctx.DrawPath(0, 0, RoundedRectPath(8, 8, 192, 132, 12))

	m := ctx.CoordSystemView().Mul(ctx.View()).Mul(canvas.Identity.Translate(120, 55))
	ctx.RenderImage(OffscreenGradient(), m)

	ctx.SetStroke(canvas.Navy)
	ctx.SetStrokeWidth(0.8)
	ctx.SetFill(ctx.Style.Stroke)
	p2 := &canvas.Path{}
	p2.MoveTo(150, 115)
	p2.LineTo(185, 88)
	ctx.DrawPath(0, 0, p2)
}

// SceneBackend 用 Renderer（即 render.DrawContext 的实现）绘制与
// SceneDirect 完全一致、仅方法调度方式不同的测试场景。
func SceneBackend(t testing.TB, b Renderer, fontPath string) {
	t.Helper()
	face := LoadTestFace(t, fontPath)

	b.SetFillColor(color.White)
	b.DrawPath(0, 0, canvas.Rectangle(200, 140))

	b.DrawImage(MaskedChecker(), 14, 14, 25.4/25.4)

	b.ClearFill()
	b.SetStrokeColor(cGreen)
	b.SetStrokeWidth(1.4)
	b.SetStrokeJoiner(canvas.MiterJoin)
	b.SetStrokeCapper(canvas.ButtCap)
	b.DrawPath(0, 0, WavePath())

	b.ClearStroke()
	b.SetFillRule(canvas.EvenOdd)
	b.SetFillColor(cRed)
	p := CirclePath(50, 105, 16)
	p = AppendCircle(p, 68, 105, 16)
	b.DrawPath(0, 0, p)
	b.SetFillRule(canvas.NonZero)

	g := canvas.Grad{}
	g.Add(0, cGradA)
	g.Add(1, cGradB)
	b.SetFillGradient(g.ToLinear(canvas.Point{X: 110, Y: 105}, canvas.Point{X: 188, Y: 128}))
	b.SetStrokeColor(color.Transparent)
	b.DrawPath(0, 0, canvas.Rectangle(78, 23).Translate(110, 105))

	clip := canvas.Circle(20).Translate(148, 37).And(canvas.Rectangle(24, 24).Translate(136, 25))
	fill := canvas.Circle(34).Translate(148, 37).And(clip)
	b.SetFillColor(cOrange)
	b.DrawPath(0, 0, fill)

	tp, _ := face.ToPath("OFD render")
	tp = tp.Transform(canvas.Identity.ReflectY()).Translate(42, 20)
	b.SetFillColor(cDark)
	b.SetStrokeColor(color.Transparent)
	b.TextPath(tp, 0, 0)

	b.ClearFill()
	b.SetStrokeColor(cNavy)
	b.SetStrokeWidth(1.2)
	b.SetDashes(0, 3, 1.2)
	b.SetStrokeCapper(canvas.RoundCap)
	b.SetStrokeJoiner(canvas.RoundJoin)
	b.DrawPath(0, 0, RoundedRectPath(8, 8, 192, 132, 12))

	m := b.CurrentMatrix().Mul(canvas.Identity.Translate(120, 55))
	b.RenderImage(OffscreenGradient(), m)

	b.SetStrokeColor(canvas.Navy)
	b.SetStrokeWidth(0.8)
	b.CopyStrokeToFill()
	p2 := &canvas.Path{}
	p2.MoveTo(150, 115)
	p2.LineTo(185, 88)
	b.DrawPath(0, 0, p2)
}

var (
	// 供各路场景/图元复用的颜色。
	cGreen  = color.RGBA{0x2e, 0x7d, 0x32, 0xff}
	cRed    = color.RGBA{0xd3, 0x2f, 0x2f, 0xff}
	cGradA  = color.RGBA{0xe5, 0x39, 0x35, 0xff}
	cGradB  = color.RGBA{0x1e, 0x88, 0xe5, 0xff}
	cOrange = color.RGBA{0xfb, 0x8c, 0x00, 0xff}
	cDark   = color.RGBA{0x1a, 0x1a, 0x1a, 0xff}
	cNavy   = color.RGBA{0x15, 0x65, 0xc0, 0xff}
)

// MaskedChecker 返回带椭圆 alpha 掩码的棋盘图。
func MaskedChecker() image.Image {
	const n = 24
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			dx := (float64(x) + 0.5 - 12) / 10
			dy := (float64(y) + 0.5 - 12) / 8
			c := cGreen
			if ((x+y)/3)%2 == 0 {
				c = color.RGBA{0xf4, 0xe4, 0xbc, 0xff}
			}
			if dx*dx+dy*dy > 1 {
				c.A = 0
			}
			img.Set(x, y, c)
		}
	}
	return img
}

// WavePath 返回开放波浪样条路径。
func WavePath() *canvas.Path {
	p := &canvas.Path{}
	p.MoveTo(20, 78)
	p.CubeTo(30, 72, 40, 88, 50, 78)
	p.CubeTo(57, 70, 64, 86, 71, 78)
	p.CubeTo(78, 74, 84, 82, 90, 78)
	return p
}

// CirclePath 返回近似圆的多边形路径。
func CirclePath(cx, cy, r float64) *canvas.Path {
	p := &canvas.Path{}
	p.MoveTo(cx+r, cy)
	seg := int(r * 12)
	for i := 1; i <= seg; i++ {
		th := float64(i) / float64(seg) * 2 * math.Pi
		p.LineTo(cx+r*math.Cos(th), cy+r*math.Sin(th))
	}
	p.Close()
	return p
}

// AppendCircle 在路径后追加一个圆。
func AppendCircle(p *canvas.Path, cx, cy, r float64) *canvas.Path {
	return p.Append(CirclePath(cx, cy, r))
}

// RoundedRectPath 返回圆角矩形路径。
func RoundedRectPath(x0, y0, x1, y1, r float64) *canvas.Path {
	p := &canvas.Path{}
	p.MoveTo(x0+r, y0)
	p.LineTo(x1-r, y0)
	p.ArcTo(r, r, 0, false, true, x1, y0+r)
	p.LineTo(x1, y1-r)
	p.ArcTo(r, r, 0, false, true, x1-r, y1)
	p.LineTo(x0+r, y1)
	p.ArcTo(r, r, 0, false, true, x0, y1-r)
	p.LineTo(x0, y0+r)
	p.ArcTo(r, r, 0, false, true, x0+r, y0)
	p.Close()
	return p
}

// OffscreenGradient 返回离屏预渲染的线性渐变图，用作按完整矩阵贴回的素材。
func OffscreenGradient() image.Image {
	oc := canvas.New(30, 20)
	octx := canvas.NewContext(oc)
	octx.SetCoordSystem(canvas.CartesianIV)
	g := canvas.Grad{}
	g.Add(0, cNavy)
	g.Add(1, cOrange)
	octx.SetFillGradient(g.ToLinear(canvas.Point{X: 0, Y: 0}, canvas.Point{X: 30, Y: 20}))
	octx.SetStrokeColor(canvas.Transparent)
	octx.DrawPath(0, 0, canvas.Rectangle(30, 20))
	return rasterizePNG(oc, canvas.DPI(96))
}

func rasterizePNG(c *canvas.Canvas, resolution canvas.Resolution) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, int(c.W*resolution.DPMM()+0.5), int(c.H*resolution.DPMM()+0.5)))
	base := rasterizer.FromImage(img, resolution, canvas.DefaultColorSpace)
	c.RenderTo(base)
	base.Close()
	return img
}

// DiffStat 统计两图逐通道（RRGGBBAA，premultiplied）差异。
type DiffStat struct {
	Changed  int     // 至少一个通道差异的像素数
	Hot      int     // 任一个通道差异 > 32 的像素数
	MaxDelta int     // 最大单通道差异（0..255）
	AvgDelta float64 // 平均单通道差异（全部字节）
}

// CalcDiff 计算两图的逐像素差异统计，尺寸或像素长度不一致时测试失败。
func CalcDiff(t testing.TB, a, b *image.RGBA) DiffStat {
	t.Helper()
	if a.Bounds() != b.Bounds() {
		t.Fatalf("尺寸不一致: %v vs %v", a.Bounds(), b.Bounds())
	}
	if len(a.Pix) != len(b.Pix) {
		t.Fatalf("像素长度不一致: %d vs %d", len(a.Pix), len(b.Pix))
	}
	s := DiffStat{}
	total := 0
	sum := 0
	for i := 0; i < len(a.Pix); i++ {
		d := int(a.Pix[i]) - int(b.Pix[i])
		if d < 0 {
			d = -d
		}
		if d > s.MaxDelta {
			s.MaxDelta = d
		}
		sum += d
		total++
	}
	for i := 0; i+3 < len(a.Pix); i += 4 {
		pxHot := false
		pxDiff := false
		for j := 0; j < 4; j++ {
			d := int(a.Pix[i+j]) - int(b.Pix[i+j])
			if d < 0 {
				d = -d
			}
			if d > 0 {
				pxDiff = true
			}
			if d > 32 {
				pxHot = true
			}
		}
		if pxDiff {
			s.Changed++
		}
		if pxHot {
			s.Hot++
		}
	}
	if total > 0 {
		s.AvgDelta = float64(sum) / float64(total)
	}
	return s
}
