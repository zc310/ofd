package gio

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/internal/thirdparty/canvas/renderers/rasterizer"
	"golang.org/x/image/font/gofont/goregular"
)

func TestContainAndStretchUseMaximumConstraints(t *testing.T) {
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(800, 600))}
	if got := NewContain(gtx, 200, 100).Dimensions().Size; got != image.Pt(800, 400) {
		t.Fatalf("固定约束下 contain = %v", got)
	}
	if got := NewStretch(gtx, 200, 100).Dimensions().Size; got != image.Pt(800, 600) {
		t.Fatalf("固定约束下 stretch = %v", got)
	}
}

// 对照同一页面的 CPU 光栅结果，保护 GB/T 33190—2016 9.2 填充规则、
// 8.3.4 渐变和 8.4 裁剪语义。所有样本在内存中构造，不依赖外部文档。
func TestGioMatchesCPUForFillsGradientsAndClippedImages(t *testing.T) {
	window, err := headless.NewWindow(160, 160)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	family := canvas.NewFontFamily("gio-test")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	grad := canvas.Grad{
		{Offset: 0.2, Color: color.RGBA{R: 255, A: 255}},
		{Offset: 0.5, Color: color.RGBA{G: 255, A: 255}},
		{Offset: 0.8, Color: color.RGBA{B: 255, A: 255}},
	}
	for _, tc := range []struct {
		name   string
		draw   func(*canvas.Canvas)
		points []image.Point
	}{
		{"奇偶规则保留孔洞", func(c *canvas.Canvas) {
			p := canvas.Rectangle(30, 30).Append(canvas.Rectangle(10, 10).Translate(10, 10))
			style := canvas.DefaultStyle
			style.FillRule = canvas.EvenOdd
			c.RenderPath(p, style, canvas.Identity.Translate(5, 5))
		}, []image.Point{image.Pt(30, 30), image.Pt(80, 80)}},
		{"自定义线性画笔回退含非端点色标", func(c *canvas.Canvas) {
			style := canvas.DefaultStyle
			style.Fill = canvas.Paint{Gradient: &countedGradient{Gradient: grad.ToLinear(canvas.Point{X: 0, Y: 0}, canvas.Point{X: 30, Y: 0})}}
			c.RenderPath(canvas.Rectangle(30, 30), style, canvas.Identity.Translate(5, 5))
		}, []image.Point{image.Pt(30, 80), image.Pt(65, 80), image.Pt(90, 80), image.Pt(130, 80)}},
		{"径向渐变与透明扩展", func(c *canvas.Canvas) {
			g := grad.ToRadial(canvas.Point{X: 15, Y: 15}, 4, canvas.Point{X: 15, Y: 15}, 12)
			g.Extend = [2]bool{false, false}
			style := canvas.DefaultStyle
			style.Fill = canvas.Paint{Gradient: g}
			c.RenderPath(canvas.Rectangle(30, 30), style, canvas.Identity.Translate(5, 5))
		}, []image.Point{image.Pt(80, 80), image.Pt(106, 80), image.Pt(118, 80), image.Pt(138, 80)}},
		{"自定义渐变描边回退", func(c *canvas.Canvas) {
			style := canvas.DefaultStyle
			style.Fill = canvas.Paint{}
			style.Stroke = canvas.Paint{Gradient: &countedGradient{Gradient: grad.ToLinear(canvas.Point{}, canvas.Point{X: 30})}}
			style.StrokeWidth = 4
			c.RenderPath(canvas.Rectangle(30, 30), style, canvas.Identity.Translate(5, 5))
		}, []image.Point{image.Pt(40, 20), image.Pt(80, 20), image.Pt(120, 20)}},
		{"图片非对称裁剪及有限边界", func(c *canvas.Canvas) {
			img := image.NewRGBA(image.Rect(0, 0, 10, 10))
			for y := range 10 {
				for x := range 10 {
					img.SetRGBA(x, y, color.RGBA{R: 255, A: 255})
				}
			}
			m := canvas.Identity.Translate(5, 5).Scale(3, 3)
			c.RenderClippedImage(img, m, canvas.Rectangle(10, 4))
		}, []image.Point{image.Pt(80, 120), image.Pt(80, 40), image.Pt(10, 150), image.Pt(145, 120)}},
		{"图片有限边界", func(c *canvas.Canvas) {
			img := image.NewNRGBA(image.Rect(0, 0, 10, 10))
			for y := range 10 {
				for x := range 10 {
					img.SetNRGBA(x, y, color.NRGBA{B: 255, A: 255})
				}
			}
			c.RenderImage(img, canvas.Identity.Translate(5, 5).Scale(2, 2))
		}, []image.Point{image.Pt(40, 120), image.Pt(40, 40), image.Pt(120, 120)}},
		{"文字局部裁剪", func(c *canvas.Canvas) {
			line := canvas.NewTextLine(family.Face(12, canvas.Black), "Hello", canvas.Left)
			c.RenderClippedText(line, canvas.Identity.Translate(5, 15), canvas.Rectangle(8, 20))
		}, []image.Point{image.Pt(100, 80), image.Pt(100, 100), image.Pt(20, 20)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := canvas.New(40, 40)
			ctx := canvas.NewContext(c)
			ctx.SetFillColor(color.White)
			ctx.DrawPath(0, 0, canvas.Rectangle(40, 40))
			tc.draw(c)
			want := rasterizer.Draw(c, canvas.DPMM(4), canvas.LinearColorSpace{})
			var ops op.Ops
			gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(160, 160))}
			defer clip.Rect(image.Rect(0, 0, 160, 160)).Push(&ops).Pop()
			paint.Fill(&ops, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
			transform := op.Affine(f32.NewAffine2D(4, 0, 0, 0, 4, 0)).Push(&ops)
			c.RenderTo(New(gtx, 40, 40))
			transform.Pop()
			if err := window.Frame(&ops); err != nil {
				t.Fatal(err)
			}
			got := image.NewRGBA(image.Rect(0, 0, 160, 160))
			if err := window.Screenshot(got); err != nil {
				t.Fatal(err)
			}
			for _, p := range tc.points {
				a, b := got.RGBAAt(p.X, p.Y), want.RGBAAt(p.X, p.Y)
				for _, pair := range [][2]uint8{{a.R, b.R}, {a.G, b.G}, {a.B, b.B}, {a.A, b.A}} {
					// 局部回退为 150 DPI，允许与 4 px/mm CPU 采样网格间的插值误差。
					if diff := int(pair[0]) - int(pair[1]); diff < -12 || 12 < diff {
						t.Fatalf("像素 %v 与 CPU 不一致: GPU=%v CPU=%v", p, a, b)
					}
				}
			}
		})
	}
}

// Gio 在 GPU 线性空间进行透明合成，与 CPU 默认的数值通道混色有差异；
// 此处将回退图片与同一 GPU 的原生半透明颜色比较，避免错误要求两种混色模型逐像素相同。
func TestGradientFallbackPreservesAlphaLikeNativeGioPaint(t *testing.T) {
	window, err := headless.NewWindow(160, 160)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	var ops op.Ops
	defer clip.Rect(image.Rect(0, 0, 160, 160)).Push(&ops).Pop()
	paint.Fill(&ops, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	transform := op.Affine(f32.NewAffine2D(4, 0, 0, 0, 4, 0)).Push(&ops)
	r := New(layout.Context{Ops: &ops}, 40, 40)
	style := canvas.DefaultStyle
	style.Fill = canvas.Paint{Color: color.RGBA{R: 128, A: 128}}
	r.RenderPath(canvas.Rectangle(10, 20), style, canvas.Identity.Translate(5, 10))
	style.Fill = canvas.Paint{Gradient: &countedGradient{Gradient: (canvas.Grad{
		{Offset: 0, Color: color.RGBA{R: 128, A: 128}},
		{Offset: 1, Color: color.RGBA{R: 128, A: 128}},
	}).ToRadial(canvas.Point{X: 5, Y: 10}, 0, canvas.Point{X: 5, Y: 10}, 20)}}
	r.RenderPath(canvas.Rectangle(10, 20), style, canvas.Identity.Translate(25, 10))
	transform.Pop()
	if err := window.Frame(&ops); err != nil {
		t.Fatal(err)
	}
	got := image.NewRGBA(image.Rect(0, 0, 160, 160))
	if err := window.Screenshot(got); err != nil {
		t.Fatal(err)
	}
	if a, b := got.RGBAAt(40, 80), got.RGBAAt(120, 80); a != b {
		t.Fatalf("渐变透明度与原生颜色不一致: native=%v fallback=%v", a, b)
	}
}

// 全透明色标的 RGB 是 canvas 的直通插值载体，不是普通预乘透明黑色。
// 比较显式回退画笔，避免将这类色标错误交给 Gio 的预乘渐变。
func TestTransparentColorCarrierUsesGradientFallback(t *testing.T) {
	window, err := headless.NewWindow(160, 160)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	for _, stops := range []canvas.Grad{
		{{Offset: 0, Color: color.RGBA{B: 255}}, {Offset: 1, Color: canvas.Red}},
		{{Offset: 0, Color: color.RGBA{B: 255}}, {Offset: 0.5, Color: color.RGBA{R: 120, A: 120}}, {Offset: 1, Color: color.RGBA{R: 240, A: 240}}},
	} {
		frame := func(wrapped bool) *image.RGBA {
			var ops op.Ops
			outer := clip.Rect(image.Rect(0, 0, 160, 160)).Push(&ops)
			paint.Fill(&ops, color.NRGBA{A: 255})
			transform := op.Affine(f32.NewAffine2D(4, 0, 0, 0, 4, 0)).Push(&ops)
			r := New(layout.Context{Ops: &ops}, 40, 40)
			g := stops.ToLinear(canvas.Point{}, canvas.Point{X: 30})
			var gradient canvas.Gradient = g
			if wrapped {
				gradient = &countedGradient{Gradient: g}
			}
			style := canvas.DefaultStyle
			style.Fill = canvas.Paint{Gradient: gradient}
			r.RenderPath(canvas.Rectangle(30, 30), style, canvas.Identity.Translate(5, 5))
			if r.HasNativeGradients() {
				t.Fatal("携带 RGB 的透明色标不能使用 Gio 原生渐变")
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
			return pixels
		}
		got, want := frame(false), frame(true)
		for i := range got.Pix {
			if got.Pix[i] != want.Pix[i] {
				t.Fatalf("透明色载体回退结果不一致: byte=%d", i)
			}
		}
		if sample := got.RGBAAt(50, 80); sample.B < 30 {
			t.Fatalf("透明色标携带的蓝色没有参与插值: %v", sample)
		}
	}
}

func TestGradientFallbackDoesNotPaintOutsidePage(t *testing.T) {
	var ops op.Ops
	r := New(layout.Context{Ops: &ops}, 40, 40)
	style := canvas.DefaultStyle
	g := &countedGradient{Gradient: (canvas.Grad{{Offset: 0, Color: color.RGBA{R: 255, A: 255}}}).ToLinear(canvas.Point{}, canvas.Point{X: 10})}
	style.Fill = canvas.Paint{Gradient: g}
	// 极大的页面外图形应先裁掉，不能按其整个外接框分配图片。
	r.RenderPath(canvas.Rectangle(1e9, 1e9), style, canvas.Identity.Translate(100, 100))
	if g.samples != 0 {
		t.Fatal("页面外图形不应分配或采样渐变图片")
	}
}

func TestGradientFallbackRoundedImageDoesNotSpillOutsidePage(t *testing.T) {
	window, err := headless.NewWindow(200, 200)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	var ops op.Ops
	outer := clip.Rect(image.Rect(0, 0, 200, 200)).Push(&ops)
	paint.Fill(&ops, color.NRGBA{A: 255})
	transform := op.Affine(f32.NewAffine2D(4, 0, 10, 0, 4, 10)).Push(&ops)
	r := New(layout.Context{Ops: &ops}, 40, 40)
	style := canvas.DefaultStyle
	style.Fill = canvas.Paint{Gradient: &countedGradient{Gradient: (canvas.Grad{{Offset: 0, Color: canvas.Red}}).ToLinear(canvas.Point{}, canvas.Point{X: 10})}}
	r.RenderPath(canvas.Rectangle(60, 60), style, canvas.Identity.Translate(-10, -10))
	transform.Pop()
	outer.Pop()
	if err := window.Frame(&ops); err != nil {
		t.Fatal(err)
	}
	pixels := image.NewRGBA(image.Rect(0, 0, 200, 200))
	if err := window.Screenshot(pixels); err != nil {
		t.Fatal(err)
	}
	if got := pixels.RGBAAt(100, 100); got.R < 250 {
		t.Fatalf("页面内部未绘制: %v", got)
	}
	for _, p := range []image.Point{image.Pt(9, 100), image.Pt(170, 100), image.Pt(100, 9), image.Pt(100, 170)} {
		if got := pixels.RGBAAt(p.X, p.Y); got != (color.RGBA{A: 255}) {
			t.Fatalf("取整后的回退图片画到页面外 %v: %v", p, got)
		}
	}
}

type countedGradient struct {
	canvas.Gradient

	samples int
}

func (g *countedGradient) At(x, y float64) color.RGBA { g.samples++; return g.Gradient.At(x, y) }
func (g *countedGradient) SetColorSpace(_ canvas.ColorSpace) canvas.Gradient { //nolint:ireturn // 实现 canvas.Gradient 契约，测试统计采样次数。
	return g
}

func TestImagePreparationPreservesSharedPixelsAndNormalizesOrigin(t *testing.T) {
	src := image.NewNRGBA(image.Rect(10, 20, 12, 22))
	src.SetNRGBA(10, 20, color.NRGBA{R: 255, A: 128})
	before := src.NRGBAAt(10, 20)
	got := imageForGio(src)
	if src.NRGBAAt(10, 20) != before {
		t.Fatal("转换修改了共享图片")
	}
	if got.Bounds() != image.Rect(0, 0, 2, 2) {
		t.Fatalf("图片起点未归零: %v", got.Bounds())
	}
	r, _, _, a := got.At(0, 0).RGBA()
	if r <= 128*257 || a != 128*257 {
		t.Fatal("未正确编码线性预乘透明像素")
	}
}

func BenchmarkImageForGio(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 512, 512))
	for y := range 512 {
		for x := range 512 {
			a := uint8(1 + (x+y)%254)
			img.SetRGBA(x, y, color.RGBA{R: a, G: a / 2, B: a / 4, A: a})
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = imageForGio(img)
	}
}

func TestGioDoesNotMutateSharedPathsOrPanicOnNilImage(t *testing.T) {
	var ops op.Ops
	r := New(layout.Context{Ops: &ops}, 40, 40)
	p := canvas.Rectangle(20, 20)
	before := p.ToSVG()
	style := canvas.DefaultStyle
	style.FillRule = canvas.EvenOdd
	style.Stroke = canvas.Paint{Color: color.RGBA{R: 255, A: 255}}
	style.StrokeWidth = 1
	for range 2 {
		r.RenderPath(p, style, canvas.Identity.Translate(2, 2))
	}
	if p.ToSVG() != before {
		t.Fatal("重放修改了共享路径")
	}
	r.RenderImage(nil, canvas.Identity)
}
