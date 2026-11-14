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

// 恒色径向画笔与同色矢量路径逐像素对比，包括填充孔洞、描边和仿射变换。
func TestUniformRadialMatchesSolidVector(t *testing.T) {
	window, err := headless.NewWindow(240, 240)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	for _, m := range gradientTransforms() {
		for _, alpha := range []uint8{0, 128, 255} {
			for _, stroke := range []bool{false, true} {
				col := color.RGBA{R: alpha, A: alpha}
				g := (canvas.Grad{{Offset: 0.1, Color: col}, {Offset: 0.5, Color: col}, {Offset: 0.9, Color: col}}).ToRadial(canvas.Point{X: 10, Y: 12}, 0, canvas.Point{X: 10, Y: 12}, 20)
				path := canvas.Rectangle(30, 30).Append(canvas.Rectangle(10, 10).Translate(10, 10))
				before := path.ToSVG()
				frame := func(radial bool) *image.RGBA {
					var ops op.Ops
					outer := clip.Rect(image.Rect(0, 0, 240, 240)).Push(&ops)
					paint.Fill(&ops, color.NRGBA{R: 30, G: 60, B: 90, A: 255})
					transform := op.Affine(f32.NewAffine2D(4, 0, 0, 0, 4, 0)).Push(&ops)
					r := New(layout.Context{Ops: &ops}, 60, 60)
					style := canvas.DefaultStyle
					style.FillRule = canvas.EvenOdd
					if radial {
						style.Fill = canvas.Paint{Gradient: g}
					} else {
						style.Fill = canvas.Paint{Color: col}
					}
					if stroke {
						style.Fill, style.Stroke, style.StrokeWidth = canvas.Paint{}, style.Fill, 4
					}
					if radial {
						if !r.renderUniformRadial(path, style, m) {
							t.Fatal("恒色径向画笔未化简")
						}
					} else {
						r.RenderPath(path, style, m)
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
				got, want := frame(true), frame(false)
				for i := range got.Pix {
					// 多一层页面裁剪可能导致 GPU 抗锯齿量化相差一档。
					if diff := int(got.Pix[i]) - int(want.Pix[i]); diff < -2 || diff > 2 {
						t.Fatalf("恒色径向与纯色不同: m=%v alpha=%d stroke=%v byte=%d got=%d want=%d", m, alpha, stroke, i, got.Pix[i], want.Pix[i])
					}
				}
				if path.ToSVG() != before {
					t.Fatal("绘制修改了共享路径")
				}
			}
		}
	}
}

func TestUniformRadialColorMatchesCanvasSampler(t *testing.T) {
	for _, col := range []color.RGBA{{}, {R: 128, A: 128}, {R: 255, G: 64, A: 255}} {
		g := (canvas.Grad{{Offset: 0.2, Color: col}, {Offset: 0.8, Color: col}}).ToRadial(canvas.Point{X: 10, Y: 15}, 0, canvas.Point{X: 10, Y: 15}, 20)
		got, ok := g.UniformPlaneColor()
		if !ok || got != col {
			t.Fatalf("恒色识别错误: got=%v ok=%v want=%v", got, ok, col)
		}
		for _, p := range []canvas.Point{{X: 10, Y: 15}, {X: 11, Y: 15}, {X: 20, Y: 15}, {X: 30, Y: 15}, {X: -100, Y: 80}} {
			if sampled := g.At(p.X, p.Y); sampled != got {
				t.Fatalf("位置 %v 的采样颜色 %v 与化简颜色 %v 不同", p, sampled, got)
			}
		}
	}
}

func TestUniformRadialRejectsNonEquivalentCases(t *testing.T) {
	newGradient := func() *canvas.RadialGradient {
		return (canvas.Grad{{Offset: 0, Color: canvas.Red}, {Offset: 1, Color: canvas.Red}}).ToRadial(canvas.Point{}, 0, canvas.Point{}, 10)
	}
	for _, tc := range []struct {
		name string
		edit func(*canvas.RadialGradient)
	}{
		{"有限延伸", func(g *canvas.RadialGradient) { g.Extend[1] = false }},
		{"不同颜色", func(g *canvas.RadialGradient) { g.Grad[1].Color = canvas.Blue }},
		{"透明色载体", func(g *canvas.RadialGradient) {
			g.Grad[0].Color, g.Grad[1].Color = color.RGBA{R: 255}, color.RGBA{R: 255}
		}},
		{"色标顺序无效", func(g *canvas.RadialGradient) { g.Grad[1].Offset = 0 }},
		{"色标无穷", func(g *canvas.RadialGradient) { g.Grad[1].Offset = math.Inf(1) }},
		{"修改半径但未更新缓存", func(g *canvas.RadialGradient) { g.R1 = 20 }},
		{"修改圆心但未更新缓存", func(g *canvas.RadialGradient) { g.C1.X = 1 }},
		{"修改起始半径", func(g *canvas.RadialGradient) { g.R0 = 1 }},
		{"零半径", func(g *canvas.RadialGradient) { g.R1 = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newGradient()
			tc.edit(g)
			var ops op.Ops
			r := New(layout.Context{Ops: &ops}, 40, 40)
			style := canvas.DefaultStyle
			style.Fill = canvas.Paint{Gradient: g}
			if r.renderUniformRadial(canvas.Rectangle(30, 30), style, canvas.Identity) {
				t.Fatal("不等价的径向画笔不应化简")
			}
		})
	}
	var ops op.Ops
	r := New(layout.Context{Ops: &ops}, 40, 40)
	style := canvas.DefaultStyle
	style.Fill = canvas.Paint{Gradient: newGradient()}
	if r.renderUniformRadial(canvas.Rectangle(30, 30), style, canvas.Identity.Scale(0, 1)) {
		t.Fatal("奇异变换应保留原有回退")
	}
	// 原本偏心的画笔即使修改公开字段变成同心，内部缓存仍不应被忽略。
	eccentric := (canvas.Grad{{Offset: 0, Color: canvas.Red}, {Offset: 1, Color: canvas.Red}}).ToRadial(canvas.Point{}, 0, canvas.Point{X: 5}, 10)
	eccentric.C1 = eccentric.C0
	if _, ok := eccentric.UniformPlaneColor(); ok {
		t.Fatal("内部缓存仍为偏心的画笔不能化简")
	}
}
