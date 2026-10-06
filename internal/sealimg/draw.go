package sealimg

import (
	"image"
	"image/color"
	"math"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
)

// sealInk 是朱红印章色，alpha 200：全不透明会像印刷图，全透明又盖不住底字，
// 200 是既能看清笔画、又能透出底下内容的常用取值。
var sealInk = color.NRGBA{R: 0xC8, G: 0x16, B: 0x1D, A: 200}

// pen 持有画布上下文与图片。
//
// canvas 的坐标单位约定是毫米，这里把分辨率设为 1 dpi 之外的等价关系：
// 用 canvas.Resolution(1) 让「1 个画布单位 = 1 个输出像素」，于是所有尺寸参数
// 可以直接按像素写，读代码时不用在毫米和像素之间换算。
type pen struct {
	ctx *canvas.Context
	img *image.RGBA
	ras *rasterizer.Rasterizer
}

func newPen(img *image.RGBA) *pen {
	ras := rasterizer.FromImage(img, canvas.Resolution(1), canvas.DefaultColorSpace)
	ctx := canvas.NewContext(ras)
	ctx.SetFillColor(sealInk)
	ctx.SetStrokeColor(sealInk)
	return &pen{ctx: ctx, img: img, ras: ras}
}

func (p *pen) close() { p.ras.Close() }

// drawBorders 画外框与内框。公章是双线，内线明显细于外线。
func (p *pen) drawBorders(layout sealLayout) {
	p.ctx.SetStrokeColor(sealInk)
	p.ctx.SetStrokeWidth(layout.shortSide * borderWidthRatio)
	p.strokeEllipse(layout.cx, layout.cy, layout.borderRX, layout.borderRY, false)

	inner := innerBorderRatio
	p.ctx.SetStrokeWidth(layout.shortSide * innerWidthRatio)
	p.strokeEllipse(layout.cx, layout.cy, layout.borderRX*inner, layout.borderRY*inner, false)
}

// strokeEllipse 描一个整椭圆。闭合参数为 true 时额外画一条到起点的连线，
// 描边路径并不需要。
func (p *pen) strokeEllipse(cx, cy, rx, ry float64, _ bool) {
	// canvas 的 Arc 收角度而不是弧度：传 2π 会被当成 6.28°，画出一个退化到
	// 看不见的小弧段，外框就整个消失了。
	p.ctx.MoveTo(cx+rx, cy)
	p.ctx.Arc(rx, ry, 0, 0, 360)
	p.ctx.Close()
	p.ctx.Stroke()
}

// drawStar 画正五角星。
//
// 内接半径取外接半径的 0.382（正五角星的边角比），不是随手定的：五角星的轮廓
// 是两条对角线的交点，这个比例才让五个角等长。
func (p *pen) drawStar(layout sealLayout) {
	outer := math.Min(layout.borderRX, layout.borderRY) * starRadiusRatio
	inner := outer * 0.382
	p.ctx.SetFillColor(sealInk)
	for i := range 5 {
		angle := math.Pi/2 + float64(i)*2*math.Pi/5
		point := func(radius, offset float64) (x, y float64) {
			theta := angle + offset
			return layout.cx + radius*math.Cos(theta), layout.cy + radius*math.Sin(theta)
		}
		ox, oy := point(outer, 0)
		ix, iy := point(inner, math.Pi/5)
		if i == 0 {
			p.ctx.MoveTo(ox, oy)
		} else {
			p.ctx.LineTo(ox, oy)
		}
		p.ctx.LineTo(ix, iy)
	}
	p.ctx.Close()
	p.ctx.Fill()
}

func (p *pen) drawCenterText(face *canvas.FontFace, layout sealLayout, text string) {
	width := face.TextWidth(text)
	outer := math.Min(layout.borderRX, layout.borderRY) * starRadiusRatio
	x := layout.cx - width/2
	y := layout.cy - outer - layout.shortSide*centerTextGapRatio
	p.ctx.SetFillColor(sealInk)
	p.ctx.DrawText(x, y, canvas.NewTextLine(face, text, canvas.Left))
}

// drawArcText 把 text 沿椭圆弧排布。
//
// 逐字推进而不是按角度均分：均分会让同一个字占据固定的角度跨度，而汉字在
// 接近竖直的弧段上投影更宽，均分必然在弧顶挤、在两端松。正确做法是把每个字的
// 宽度按字体度量累加到弧长上。
//
// dir 是排布方向：+1 沿参数增大方向，-1 相反。顶部必须用 -1，否则字基朝左，
// 整行字会左右镜像。
//
// flip 为 true 时整行字再旋转 180°。底字不需要额外旋转，否则会造成左右镜像的观感。
func (p *pen) drawArcText(face *canvas.FontFace, layout sealLayout, text string, baseAngle, dir float64, flip bool, spacingRatio float64) {
	runes := []rune(text)
	advances := make([]float64, len(runes))
	for i, r := range runes {
		advances[i] = face.TextWidth(string(r))
		if r == ' ' {
			// 文案中的空格只用于分隔来源标识和声明，不应在弧线上制造大断档。
			advances[i] *= 0.25
		}
	}
	spacing := layout.shortSide * spacingRatio
	span := arcTextSpan(advances, spacing)
	if span == 0 {
		return
	}
	// 从整行宽度的中间起步，字才在弧上居中。
	s := -span / 2
	flipDeg := 0.0
	if flip {
		flipDeg = 180
	}
	for i, r := range runes {
		t := paramAtArcLength(layout.textRX, layout.textRY, baseAngle, s*dir)
		x, y := arcPoint(layout.cx, layout.cy, layout.textRX, layout.textRY, t)
		tangent := arcTangent(layout.textRX, layout.textRY, t, dir)
		saved := p.ctx.View()
		p.ctx.Translate(x, y)
		p.ctx.Rotate(tangent + flipDeg)
		p.ctx.SetFillColor(sealInk)
		p.ctx.DrawText(0, 0, canvas.NewTextLine(face, string(r), canvas.Left))
		p.ctx.SetView(saved)
		s += advances[i] + spacing
	}
}

// arcTextSpan 返回一行弧形文字包含字间距后的总弧长。
func arcTextSpan(advances []float64, spacing float64) float64 {
	span := 0.0
	for _, advance := range advances {
		span += advance
	}
	if len(advances) > 1 {
		span += spacing * float64(len(advances)-1)
	}
	return span
}
