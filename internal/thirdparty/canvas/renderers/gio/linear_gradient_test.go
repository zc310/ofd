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

func gradientTransforms() []canvas.Matrix {
	return []canvas.Matrix{
		canvas.Identity.Translate(5, 5),
		canvas.Identity.Translate(15, 5).Rotate(20).Scale(1.2, 0.6),
		{{1, 0.5, 5}, {0, 1, 5}},
		canvas.Identity.Translate(45, 5).Scale(-1, 1),
	}
}

func TestGradientAxisPreservesParameterThroughAffineTransforms(t *testing.T) {
	g := canvas.NewLinearGradient(canvas.Point{X: 3, Y: 7}, canvas.Point{X: 20, Y: 12})
	var ops op.Ops
	r := NewStretch(layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(300, 600))}, 60, 60)
	for _, m := range gradientTransforms() {
		axis, ok := r.gradientAxis(g, m)
		if !ok {
			t.Fatalf("有效变换未创建梯度轴: %v", m)
		}
		for _, point := range []canvas.Point{{X: 3, Y: 7}, {X: 20, Y: 12}, {X: 5, Y: 30}} {
			p := m.Dot(point)
			screen := canvas.Point{X: r.xScale * p.X, Y: r.yScale * (r.height - p.Y)}
			d := g.End.Sub(g.Start)
			want := point.Sub(g.Start).Dot(d) / d.Dot(d)
			if got := axis.parameter(screen); math.Abs(got-want) > 1e-10 {
				t.Fatalf("变换 %v 后参数 = %v，期望 %v", m, got, want)
			}
		}
	}
}

func TestGradientAxisUsesSamplerCachedDirection(t *testing.T) {
	var ops op.Ops
	r := New(layout.Context{Ops: &ops}, 60, 60)
	for _, tc := range []struct {
		name  string
		g     *canvas.LinearGradient
		point canvas.Point
		want  float64
	}{
		{"修改公开端点", func() *canvas.LinearGradient {
			g := canvas.NewLinearGradient(canvas.Point{}, canvas.Point{X: 20})
			g.End = canvas.Point{Y: 20}
			return g
		}(), canvas.Point{X: 10, Y: 20}, 0.5},
		{"近水平轴遵循采样器容差", canvas.NewLinearGradient(canvas.Point{}, canvas.Point{X: 20, Y: canvas.Epsilon / 2}), canvas.Point{X: 10, Y: 1e9}, 0.5},
		{"近垂直轴遵循采样器容差", canvas.NewLinearGradient(canvas.Point{}, canvas.Point{X: canvas.Epsilon / 2, Y: 20}), canvas.Point{X: 1e9, Y: 10}, 0.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			axis, ok := r.gradientAxis(tc.g, canvas.Identity)
			if !ok {
				t.Fatal("有效色轴被拒绝")
			}
			p := canvas.Point{X: tc.point.X, Y: 60 - tc.point.Y}
			if got := axis.parameter(p); math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("色轴参数 = %v，期望 %v", got, tc.want)
			}
		})
	}
	// 直接构造未初始化内部轴，采样器使用常量参数，不能按公开端点创建渐变。
	g := &canvas.LinearGradient{Start: canvas.Point{}, End: canvas.Point{X: 20}}
	if _, ok := r.gradientAxis(g, canvas.Identity); ok {
		t.Fatal("未初始化的缓存轴应回退")
	}
}

func TestNativeGradientUsesCachedAxisAfterEndpointChange(t *testing.T) {
	window, err := headless.NewWindow(160, 160)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	var ops op.Ops
	outer := clip.Rect(image.Rect(0, 0, 160, 160)).Push(&ops)
	paint.Fill(&ops, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	transform := op.Affine(f32.NewAffine2D(4, 0, 0, 0, 4, 0)).Push(&ops)
	r := New(layout.Context{Ops: &ops}, 40, 40)
	r.SetGradientScale(4)
	g := (canvas.Grad{{Offset: 0, Color: canvas.Red}, {Offset: 1, Color: canvas.Blue}}).ToLinear(canvas.Point{}, canvas.Point{X: 20})
	g.End = canvas.Point{Y: 20}
	style := canvas.DefaultStyle
	style.Fill = canvas.Paint{Gradient: g}
	r.RenderPath(canvas.Rectangle(30, 30), style, canvas.Identity.Translate(5, 5))
	if !r.HasNativeGradients() {
		t.Fatal("有效缓存轴应使用原生渐变")
	}
	transform.Pop()
	outer.Pop()
	if err := window.Frame(&ops); err != nil {
		t.Fatal(err)
	}
	pixels := image.NewRGBA(image.Rect(0, 0, 160, 160))
	if err := window.Screenshot(pixels); err != nil {
		t.Fatal(err)
	}
	// 本地点约 (10, 25)，缓存轴为水平中点；错误使用公开端点会得到蓝色。
	got := pixels.RGBAAt(60, 40)
	if got.R < 170 || got.B < 170 || got.G > 3 {
		t.Fatalf("原生渐变未使用缓存水平轴: %v", got)
	}
}

// 两色标渐变明确使用 Gio 线性颜色空间；以解析计算核对中间色，
// 不通过放宽 CPU 数值通道插值的比较阈值掩盖两种颜色模型的差异。
func TestNativeLinearGradientTransformsStopsAndExtend(t *testing.T) {
	window, err := headless.NewWindow(240, 240)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	for _, m := range gradientTransforms() {
		for _, extend := range [][2]bool{{true, true}, {false, false}, {true, false}, {false, true}} {
			for _, alpha := range []uint8{255, 128} {
				var ops op.Ops
				gtx := layout.Context{Ops: &ops}
				g := (canvas.Grad{{Offset: 0.2, Color: color.RGBA{R: alpha, A: alpha}}, {Offset: 0.8, Color: color.RGBA{B: alpha, A: alpha}}}).ToLinear(canvas.Point{X: 8, Y: 8}, canvas.Point{X: 22, Y: 15})
				g.Extend = extend
				style := canvas.DefaultStyle
				style.Fill = canvas.Paint{Gradient: g}
				outer := clip.Rect(image.Rect(0, 0, 240, 240)).Push(&ops)
				paint.Fill(&ops, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
				transform := op.Affine(f32.NewAffine2D(4, 0, 0, 0, 4, 0)).Push(&ops)
				r := New(gtx, 60, 60)
				r.SetGradientScale(4)
				if !r.renderLinearGradient(canvas.Rectangle(30, 30), style, m) {
					t.Fatalf("两色标渐变未走原生路径: m=%v", m)
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
				for _, local := range []canvas.Point{{X: 3, Y: 10}, {X: 12, Y: 12}, {X: 17, Y: 14}, {X: 27, Y: 20}} {
					p := m.Dot(local)
					x, y := int(p.X*4), int((60-p.Y)*4)
					// 使用被采样像素的真实中心坐标反算渐变参数。
					object := m.Inv().Dot(canvas.Point{X: (float64(x) + 0.5) / 4, Y: 60 - (float64(y)+0.5)/4})
					d := g.End.Sub(g.Start)
					tValue := object.Sub(g.Start).Dot(d) / d.Dot(d)
					want := nativeRedBlue(tValue, extend, float64(alpha)/255)
					got := pixels.RGBAAt(x, y)
					for _, pair := range [][2]uint8{{got.R, want.R}, {got.G, want.G}, {got.B, want.B}, {got.A, want.A}} {
						if diff := int(pair[0]) - int(pair[1]); diff < -3 || diff > 3 {
							t.Fatalf("原生渐变像素错误: m=%v Extend=%v alpha=%d t=%v got=%v want=%v", m, extend, alpha, tValue, got, want)
						}
					}
				}
			}
		}
	}
}

func nativeRedBlue(t float64, extend [2]bool, alpha float64) color.RGBA {
	if (t < 0 && !extend[0]) || (t > 1 && !extend[1]) {
		return color.RGBA{R: 255, G: 255, B: 255, A: 255}
	}
	u := max(0, min(1, (t-0.2)/0.6))
	encode := func(linear float64) uint8 {
		if linear <= 0.0031308 {
			return uint8(math.Round(255 * 12.92 * linear))
		}
		return uint8(math.Round(255 * (1.055*math.Pow(linear, 1/2.4) - 0.055)))
	}
	return color.RGBA{R: encode((1-u)*alpha + 1 - alpha), G: encode(1 - alpha), B: encode(u*alpha + 1 - alpha), A: 255}
}

func TestNativeLinearGradientRejectsFallbackCasesAndDoesNotMutateInput(t *testing.T) {
	var ops op.Ops
	r := New(layout.Context{Ops: &ops}, 60, 60)
	path := canvas.Rectangle(30, 30)
	before := path.ToSVG()
	g := (canvas.Grad{{Offset: 0, Color: canvas.Red}, {Offset: 1, Color: canvas.Blue}}).ToLinear(canvas.Point{}, canvas.Point{X: 30})
	style := canvas.DefaultStyle
	style.Fill = canvas.Paint{Gradient: g}
	if !r.renderLinearGradient(path, style, canvas.Identity) {
		t.Fatal("未选择原生线性渐变")
	}
	if path.ToSVG() != before || len(g.Grad) != 2 || g.Grad[0].Color != canvas.Red {
		t.Fatal("绘制修改了共享路径或渐变")
	}
	g.Grad = append(g.Grad[:1], canvas.Stop{Offset: 0.5, Color: canvas.Green}, g.Grad[1])
	if !r.renderLinearGradient(path, style, canvas.Identity) {
		t.Fatal("统一透明度多色标应走原生路径")
	}
	g.Grad[1].Color.A = 128
	if r.renderLinearGradient(path, style, canvas.Identity) {
		t.Fatal("透明度变化的多色标应回退，不能重复叠色")
	}
	g.Grad = canvas.Grad{{Offset: 0, Color: canvas.Red}, {Offset: 1, Color: canvas.Blue}}
	if r.renderLinearGradient(path, style, canvas.Identity.Scale(0, 1)) {
		t.Fatal("奇异变换不应进入原生路径")
	}
}

func TestNativeLinearGradientPreservesEvenOddHolesAndStrokeOutline(t *testing.T) {
	window, err := headless.NewWindow(160, 160)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	for _, stroke := range []bool{false, true} {
		var ops op.Ops
		outer := clip.Rect(image.Rect(0, 0, 160, 160)).Push(&ops)
		paint.Fill(&ops, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		transform := op.Affine(f32.NewAffine2D(4, 0, 0, 0, 4, 0)).Push(&ops)
		r := New(layout.Context{Ops: &ops}, 40, 40)
		r.SetGradientScale(4)
		fill := canvas.Paint{Gradient: (canvas.Grad{{Offset: 0, Color: canvas.Red}, {Offset: 1, Color: canvas.Blue}}).ToLinear(canvas.Point{}, canvas.Point{X: 30})}
		style := canvas.DefaultStyle
		style.Fill, style.FillRule = fill, canvas.EvenOdd
		path := canvas.Rectangle(30, 30).Append(canvas.Rectangle(10, 10).Translate(10, 10))
		if stroke {
			style.Fill, style.Stroke, style.StrokeWidth = canvas.Paint{}, fill, 4
			path = canvas.Rectangle(30, 30)
		}
		r.RenderPath(path, style, canvas.Identity.Translate(5, 5))
		if !r.HasNativeGradients() {
			t.Fatal("渐变未走原生路径")
		}
		transform.Pop()
		outer.Pop()
		if err := window.Frame(&ops); err != nil {
			t.Fatal(err)
		}
		pixels := image.NewRGBA(image.Rect(0, 0, 160, 160))
		if err := window.Screenshot(pixels); err != nil {
			t.Fatal(err)
		}
		if got := pixels.RGBAAt(80, 80); got != (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
			t.Fatalf("孔洞或描边内部被填充: %v", got)
		}
		point := image.Pt(80, 30)
		if stroke {
			point = image.Pt(80, 20)
		}
		if got := pixels.RGBAAt(point.X, point.Y); got.R < 100 || got.B < 100 || got.G > 3 {
			t.Fatalf("渐变填充或描边没有绘制: %v", got)
		}
	}
}

func BenchmarkLinearGradientPaint(b *testing.B) {
	for _, tc := range []struct {
		name         string
		count        int
		varyingAlpha bool
	}{
		{"原生两色标", 2, false}, {"原生三色标", 3, false}, {"透明度变化三色标回退", 3, true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			var ops op.Ops
			r := New(layout.Context{Ops: &ops}, 40, 40)
			stops := canvas.Grad{{Offset: 0, Color: canvas.Red}, {Offset: 1, Color: canvas.Blue}}
			if tc.count == 3 {
				stops = canvas.Grad{{Offset: 0, Color: canvas.Red}, {Offset: 0.5, Color: canvas.Green}, {Offset: 1, Color: canvas.Blue}}
			}
			if tc.varyingAlpha {
				stops[1].Color = color.RGBA{G: 128, A: 128}
			}
			style := canvas.DefaultStyle
			style.Fill = canvas.Paint{Gradient: stops.ToLinear(canvas.Point{}, canvas.Point{X: 30})}
			path := canvas.Rectangle(30, 30)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ops.Reset()
				r.RenderPath(path, style, canvas.Identity.Translate(5, 5))
			}
		})
	}
}
