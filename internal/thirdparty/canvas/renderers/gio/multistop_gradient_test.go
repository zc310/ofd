package gio

import (
	"image"
	"image/color"
	"math"
	"testing"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

// 遍历图形内部像素而非只采样色标中心，同时保护分段接缝、Extend、
// 统一透明度、非等比缩放、切变和镜像。透明度在整个梯度合成一次。
func TestNativeMultistopGradientHasNoInteriorSeamsOrRepeatedAlpha(t *testing.T) {
	window, err := headless.NewWindow(240, 240)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	background := color.NRGBA{R: 30, G: 60, B: 90, A: 255}
	for _, m := range gradientTransforms() {
		for _, extend := range [][2]bool{{true, true}, {false, false}, {true, false}, {false, true}} {
			for _, alpha := range []uint8{255, 128, 0} {
				for _, stroke := range []bool{false, true} {
					g := (canvas.Grad{
						{Offset: 0.1, Color: color.RGBA{R: alpha, A: alpha}},
						{Offset: 0.4, Color: color.RGBA{G: alpha, A: alpha}},
						{Offset: 0.65, Color: color.RGBA{B: alpha, A: alpha}},
						{Offset: 0.9, Color: color.RGBA{R: alpha, G: alpha, A: alpha}},
					}).ToLinear(canvas.Point{X: 5, Y: 8}, canvas.Point{X: 25, Y: 15})
					g.Extend = extend
					var ops op.Ops
					outer := clip.Rect(image.Rect(0, 0, 240, 240)).Push(&ops)
					paint.Fill(&ops, background)
					transform := op.Affine(f32.NewAffine2D(4, 0, 0, 0, 4, 0)).Push(&ops)
					r := New(layout.Context{Ops: &ops}, 60, 60)
					r.SetGradientScale(4)
					style := canvas.DefaultStyle
					style.Fill = canvas.Paint{Gradient: g}
					if stroke {
						style.Fill, style.Stroke, style.StrokeWidth = canvas.Paint{}, style.Fill, 4
					}
					r.RenderPath(canvas.Rectangle(30, 30), style, m)
					if !r.HasNativeGradients() {
						t.Fatal("统一透明度的多色标未使用原生绘制")
					}
					transform.Pop()
					outer.Pop()
					if err := window.Frame(&ops); err != nil {
						t.Fatal(err)
					}
					pixels := image.NewRGBA(image.Rect(0, 0, 240, 240))
					if err := window.Screenshot(pixels); err != nil {
						t.Fatal(err)
					}
					inverse := m.Inv()
					d := g.End.Sub(g.Start)
					checked, seams := 0, 0
					for y := range 240 {
						for x := range 240 {
							local := inverse.Dot(canvas.Point{X: (float64(x) + 0.5) / 4, Y: 60 - (float64(y)+0.5)/4})
							if !interiorTestPoint(local, stroke) {
								continue
							}
							tValue := local.Sub(g.Start).Dot(d) / d.Dot(d)
							// Extend 的裁剪边缘本身会抗锯齿，内部接缝则不能漏色。
							if (!extend[0] && math.Abs(tValue) < 0.015) || (!extend[1] && math.Abs(tValue-1) < 0.015) {
								continue
							}
							got := pixels.RGBAAt(x, y)
							want := nativeMultistopColor(g, tValue, background)
							tolerance := 4
							if math.Abs(tValue-0.4) < 0.01 || math.Abs(tValue-0.65) < 0.01 {
								// 接缝附近会混合两段的像素覆盖率，与像素中心的解析颜色略有差异。
								tolerance = 8
							}
							for _, pair := range [][2]uint8{{got.R, want.R}, {got.G, want.G}, {got.B, want.B}, {got.A, want.A}} {
								if diff := int(pair[0]) - int(pair[1]); diff < -tolerance || diff > tolerance {
									t.Fatalf("渐变接缝或透明度错误: m=%v Extend=%v alpha=%d stroke=%v pixel=(%d,%d) t=%v got=%v want=%v", m, extend, alpha, stroke, x, y, tValue, got, want)
								}
							}
							checked++
							if math.Abs(tValue-0.4) < 0.01 || math.Abs(tValue-0.65) < 0.01 {
								seams++
							}
						}
					}
					if checked < 100 || seams == 0 {
						t.Fatalf("未覆盖足够像素或分段接缝: %d/%d", checked, seams)
					}
				}
			}
		}
	}
}

func TestNativeMultistopGradientRejectsVaryingAlphaAndInvalidStops(t *testing.T) {
	for _, stops := range []canvas.Grad{
		{{Offset: 0, Color: canvas.Red}, {Offset: 0.5, Color: color.RGBA{G: 128, A: 128}}, {Offset: 1, Color: canvas.Blue}},
		{{Offset: 0, Color: canvas.Red}, {Offset: 1, Color: color.RGBA{B: 255}}},
		{{Offset: 0, Color: color.RGBA{R: 255}}, {Offset: 0.5, Color: color.RGBA{}}, {Offset: 1, Color: color.RGBA{B: 255}}},
		{{Offset: 0, Color: canvas.Red}, {Offset: 0.5, Color: canvas.Green}, {Offset: 0.4, Color: canvas.Blue}},
		{{Offset: 0, Color: canvas.Red}, {Offset: 0.5, Color: canvas.Green}, {Offset: 0.5, Color: canvas.Blue}},
		{{Offset: 0, Color: canvas.Red}, {Offset: math.NaN(), Color: canvas.Green}, {Offset: 1, Color: canvas.Blue}},
		{{Offset: 0, Color: canvas.Red}, {Offset: 0.5, Color: canvas.Green}, {Offset: 1.1, Color: canvas.Blue}},
		{{Offset: 0, Color: canvas.Red}, {Offset: 0.5, Color: canvas.Green}, {Offset: math.Nextafter(0.5, 1), Color: canvas.Blue}},
	} {
		var ops op.Ops
		r := New(layout.Context{Ops: &ops}, 40, 40)
		g := canvas.NewLinearGradient(canvas.Point{}, canvas.Point{X: 30})
		g.Grad = stops
		style := canvas.DefaultStyle
		style.Fill = canvas.Paint{Gradient: g}
		if r.renderLinearGradient(canvas.Rectangle(30, 30), style, canvas.Identity) {
			t.Fatalf("不支持的色标进入了原生分段: %v", stops)
		}
		if r.HasNativeGradients() {
			t.Fatal("原生绘制失败不应标记为已记录")
		}
	}
}

// 同色线性透明度的中间色标是冗余的，逐像素核对合并前后的 GPU 输出。
func TestLinearAlphaMultistopMatchesEndpoints(t *testing.T) {
	window, err := headless.NewWindow(240, 240)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	stops := canvas.Grad{
		{Offset: 0.1, Color: color.RGBA{}},
		{Offset: 0.3, Color: color.RGBA{R: 60, A: 60}},
		{Offset: 0.5, Color: color.RGBA{R: 120, A: 120}},
		{Offset: 0.9, Color: color.RGBA{R: 240, A: 240}},
	}
	for _, m := range gradientTransforms() {
		for _, extend := range [][2]bool{{true, true}, {false, false}, {true, false}, {false, true}} {
			for _, stroke := range []bool{false, true} {
				frame := func(grad canvas.Grad) *image.RGBA {
					var ops op.Ops
					outer := clip.Rect(image.Rect(0, 0, 240, 240)).Push(&ops)
					paint.Fill(&ops, color.NRGBA{R: 30, G: 60, B: 90, A: 255})
					transform := op.Affine(f32.NewAffine2D(4, 0, 0, 0, 4, 0)).Push(&ops)
					r := New(layout.Context{Ops: &ops}, 60, 60)
					r.SetGradientScale(4)
					g := grad.ToLinear(canvas.Point{X: 5, Y: 8}, canvas.Point{X: 25, Y: 15})
					g.Extend = extend
					style := canvas.DefaultStyle
					style.Fill = canvas.Paint{Gradient: g}
					if stroke {
						style.Fill, style.Stroke, style.StrokeWidth = canvas.Paint{}, style.Fill, 4
					}
					r.RenderPath(canvas.Rectangle(30, 30), style, m)
					if !r.HasNativeGradients() {
						t.Fatal("线性透明度的同色渐变没有使用原生绘制")
					}
					transform.Pop()
					outer.Pop()
					if err := window.Frame(&ops); err != nil {
						t.Fatal(err)
					}
					pixels := image.NewRGBA(image.Rect(0, 0, 240, 240))
					if err := window.Screenshot(pixels); err != nil {
						t.Fatal(err)
					}
					return pixels
				}
				got := frame(stops)
				want := frame(canvas.Grad{stops[0], stops[len(stops)-1]})
				for i := range got.Pix {
					if got.Pix[i] != want.Pix[i] {
						t.Fatalf("合并色标后输出不同: m=%v Extend=%v stroke=%v byte=%d got=%d want=%d", m, extend, stroke, i, got.Pix[i], want.Pix[i])
					}
				}
			}
		}
	}
}

func TestLinearAlphaMultistopRejectsNonlinearOrDifferentColors(t *testing.T) {
	for _, stops := range []canvas.Grad{
		{{Offset: 0, Color: color.RGBA{}}, {Offset: 0.5, Color: color.RGBA{R: 100, A: 100}}, {Offset: 1, Color: color.RGBA{R: 240, A: 240}}},
		{{Offset: 0, Color: color.RGBA{}}, {Offset: 0.5, Color: color.RGBA{G: 120, A: 120}}, {Offset: 1, Color: color.RGBA{R: 240, A: 240}}},
		{{Offset: 0, Color: color.RGBA{R: 240, A: 240}}, {Offset: 0.5, Color: color.RGBA{R: 120, A: 120}}, {Offset: 1, Color: color.RGBA{R: 240, A: 240}}},
	} {
		var ops op.Ops
		r := New(layout.Context{Ops: &ops}, 40, 40)
		g := canvas.NewLinearGradient(canvas.Point{}, canvas.Point{X: 30})
		g.Grad = stops
		style := canvas.DefaultStyle
		style.Fill = canvas.Paint{Gradient: g}
		if r.renderLinearGradient(canvas.Rectangle(30, 30), style, canvas.Identity) {
			t.Fatalf("非线性透明度或不同颜色不能合并: %v", stops)
		}
	}
}

func TestLinearSameColorAlphaAcceptsAscendingAndDescending(t *testing.T) {
	for _, stops := range []canvas.Grad{
		{{Offset: 0.2, Color: color.RGBA{R: 20, A: 20}}, {Offset: 0.5, Color: color.RGBA{R: 80, A: 80}}, {Offset: 0.8, Color: color.RGBA{R: 140, A: 140}}},
		{{Offset: 0, Color: color.RGBA{B: 240, A: 240}}, {Offset: 0.5, Color: color.RGBA{B: 120, A: 120}}, {Offset: 1, Color: color.RGBA{}}},
	} {
		if !linearSameColorAlpha(stops) {
			t.Fatalf("线性透明度未被识别: %v", stops)
		}
	}
}

func interiorTestPoint(p canvas.Point, stroke bool) bool {
	if stroke {
		return (p.X > 3 && p.X < 27 && (math.Abs(p.Y) < 0.5 || math.Abs(p.Y-30) < 0.5)) ||
			(p.Y > 3 && p.Y < 27 && (math.Abs(p.X) < 0.5 || math.Abs(p.X-30) < 0.5))
	}
	return p.X > 1 && p.X < 29 && p.Y > 1 && p.Y < 29
}

// 解析计算 Gio 线性颜色空间中的分段插值，再与已知背景合成。
func nativeMultistopColor(g *canvas.LinearGradient, t float64, background color.NRGBA) color.RGBA {
	if (t < 0 && !g.Extend[0]) || (t > 1 && !g.Extend[1]) {
		return color.RGBA{R: background.R, G: background.G, B: background.B, A: 255}
	}
	a, b := g.Grad[0], g.Grad[len(g.Grad)-1]
	for i := 1; i < len(g.Grad); i++ {
		if t < g.Grad[i].Offset {
			a, b = g.Grad[i-1], g.Grad[i]
			break
		}
	}
	u := max(0, min(1, (t-a.Offset)/(b.Offset-a.Offset)))
	alpha := float64(a.Color.A) / 255
	c1, c2 := toNRGBA(a.Color), toNRGBA(b.Color)
	channel := func(x, y, bg uint8) uint8 {
		decode := func(v uint8) float64 {
			f := float64(v) / 255
			if f <= 0.04045 {
				return f / 12.92
			}
			return math.Pow((f+0.055)/1.055, 2.4)
		}
		linear := ((1-u)*decode(x)+u*decode(y))*alpha + decode(bg)*(1-alpha)
		if linear <= 0.0031308 {
			return uint8(math.Round(255 * 12.92 * linear))
		}
		return uint8(math.Round(255 * (1.055*math.Pow(linear, 1/2.4) - 0.055)))
	}
	return color.RGBA{R: channel(c1.R, c2.R, background.R), G: channel(c1.G, c2.G, background.G), B: channel(c1.B, c2.B, background.B), A: 255}
}
