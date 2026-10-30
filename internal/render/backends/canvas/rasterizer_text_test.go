package canvas

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/canvas/renderers/rasterizer"
	"github.com/zc310/ofd/internal/render/geom"
)

// 裁剪不能改变文字的设备像素对齐；同时核对跨字形裁剪、裁剪孔洞和共享轮廓。
func TestClippedTextRasterKeepsPixelAlignmentAndExactClip(t *testing.T) {
	family := pdfTextTestFamily(t)
	for _, dpi := range []float64{72, 96, 150, 300} {
		t.Run(fmt.Sprintf("DPI=%g", dpi), func(t *testing.T) {
			resolution := canvas.DPI(dpi)
			dpmm := resolution.DPMM()
			const width, height = 200, 100
			line := canvas.NewTextLine(family.Face(18*72/dpi, canvas.Black), "OOOO", canvas.Left)
			matrix := canvas.Identity.Translate(5.37/dpmm, 35.13/dpmm)
			plain := canvas.New(width/dpmm, height/dpmm)
			plain.RenderText(line, matrix)
			want := rasterize(plain, resolution, canvas.DefaultColorSpace, nil)
			for _, kind := range []string{"完整覆盖", "部分裁剪", "裁剪孔洞"} {
				t.Run(kind, func(t *testing.T) {
					clip := canvas.Rectangle(width/dpmm, height/dpmm)
					switch kind {
					case "部分裁剪":
						clip = canvas.Rectangle(40/dpmm, height/dpmm)
					case "裁剪孔洞":
						clip = clip.Append(canvas.Rectangle(10/dpmm, 25/dpmm).Translate(10/dpmm, 35/dpmm)).Settle(canvas.EvenOdd)
					}
					before := clip.ToSVG()
					page := canvas.New(width/dpmm, height/dpmm)
					page.RenderClippedText(line, matrix, clip.Copy().Transform(matrix.Inv()))
					got := rasterize(page, resolution, canvas.DefaultColorSpace, nil)
					ink, removed := 0, 0
					for y := range height {
						for x := range width {
							expected := want.RGBAAt(x, y)
							outside := kind == "部分裁剪" && x >= 40 || kind == "裁剪孔洞" && x >= 10 && x < 20 && height-1-y >= 35 && height-1-y < 60
							if outside {
								removed += int(expected.A)
								expected = color.RGBA{}
							}
							ink += int(expected.A)
							pixel := got.RGBAAt(x, y)
							if (kind == "完整覆盖" || outside) && pixel != expected {
								t.Fatalf("裁剪改变了像素对齐或边界：(%d,%d)=%v，期望%v", x, y, pixel, expected)
							}
							// 曲线求交会重新细分抗锯齿边缘，只对一致的 3×3 实心/空白区域
							// 核对覆盖率，仍可发现基线移动、丢字和字形孔洞被填实。
							if !outside && (expected.A == 0 || expected.A == 255) && x > 0 && x+1 < width && y > 0 && y+1 < height {
								solid := true
								for yy := y - 1; yy <= y+1; yy++ {
									for xx := x - 1; xx <= x+1; xx++ {
										solid = solid && want.RGBAAt(xx, yy).A == expected.A
									}
								}
								if solid && pixel != expected {
									t.Fatalf("裁剪改变了字形实心区域：(%d,%d)=%v，期望%v", x, y, pixel, expected)
								}
							}
						}
					}
					if ink < 255*50 || kind != "完整覆盖" && removed < 255*10 {
						t.Fatalf("样例未覆盖可见文字与裁剪：ink=%d removed=%d", ink, removed)
					}
					if clip.ToSVG() != before {
						t.Fatal("栅格化改写了共享裁剪路径")
					}
				})
			}
			if after := rasterize(plain, resolution, canvas.DefaultColorSpace, nil); !bytes.Equal(after.Pix, want.Pix) {
				t.Fatal("裁剪改写了共享字形轮廓")
			}
		})
	}
}

// 不把各 span 合成一个不透明度组：重叠的半透明实色与渐变仍分别按原顺序叠加。
func TestClippedTextRasterPreservesOverlappingSpansAndGradientCoordinates(t *testing.T) {
	family := pdfTextTestFamily(t)
	gradient := canvas.NewLinearGradient(canvas.Point{}, canvas.Point{X: 18})
	gradient.Grad = canvas.Grad{{Offset: 0, Color: color.RGBA{R: 128, A: 128}}, {Offset: 1, Color: color.RGBA{B: 192, A: 192}}}
	rich := canvas.NewRichText(family.Face(22, color.RGBA{G: 128, A: 128}))
	rich.WriteString("OOOO")
	rich.WriteFace(family.Face(22, gradient), "OOOO")
	text := rich.ToText(50, 0, canvas.Left, canvas.Top, nil)
	text.WalkLines(func(_ float64, spans []canvas.TextSpan) {
		if len(spans) != 2 {
			t.Fatalf("样例应有两个文字 span，实际 %d 个", len(spans))
		}
		spans[1].X = spans[0].X + 2
	})
	const width, height = 200, 120
	resolution := canvas.DPMM(5)
	clip := canvas.Rectangle(22, 14).Translate(4, 5)
	for _, matrix := range []canvas.Matrix{
		canvas.Identity.Translate(2.37, 16.13),
		canvas.Identity.Translate(3.37, 13.13).Rotate(15).Scale(.9, 1.15),
	} {
		render := func(clipped bool) *image.RGBA {
			page := canvas.New(width/5, height/5)
			style := canvas.DefaultStyle
			style.Fill, style.Stroke = canvas.Paint{Color: canvas.White}, canvas.Paint{}
			page.RenderPath(canvas.Rectangle(width/5, height/5), style, canvas.Identity)
			if clipped {
				page.RenderClippedText(text, matrix, clip.Copy().Transform(matrix.Inv()))
			} else {
				page.RenderText(text, matrix)
			}
			return rasterize(page, resolution, canvas.DefaultColorSpace, nil)
		}
		want, got := render(false), render(true)
		ink := 0
		for y := range height {
			for x := range width {
				expected := color.RGBA{255, 255, 255, 255}
				// 矩形四边均落在设备像素边界上，裁剪应与逐像素 scissor 一致。
				if x >= 20 && x < 130 && height-1-y >= 25 && height-1-y < 95 {
					expected = want.RGBAAt(x, y)
					if expected != (color.RGBA{255, 255, 255, 255}) {
						ink++
					}
				}
				if pixel := got.RGBAAt(x, y); pixel != expected {
					t.Fatalf("矩阵%v，像素(%d,%d)=%v，期望%v", matrix, x, y, pixel, expected)
				}
			}
		}
		if ink < 100 {
			t.Fatal("样例没有覆盖足够的可见文字")
		}
	}
}

func TestTextClipMaskKeepsFractionalCoverageAndPageBounds(t *testing.T) {
	resolution := canvas.DPMM(5)
	for _, clip := range []*canvas.Path{
		canvas.Rectangle(13.27, 8.71).Translate(2.13, 3.17),
		canvas.Rectangle(15, 8).Rotate(23).Translate(-2, 5),
		canvas.Circle(7.13).Translate(17.17, 7.23),
		canvas.Rectangle(10, 10).Translate(40, 40),
	} {
		want, got := image.NewRGBA(image.Rect(0, 0, 100, 100)), image.NewRGBA(image.Rect(0, 0, 100, 100))
		style := canvas.DefaultStyle
		style.Fill, style.Stroke = canvas.Paint{Color: color.RGBA{R: 128, A: 128}}, canvas.Paint{}
		base := rasterizer.FromImage(want, resolution, canvas.DefaultColorSpace)
		base.RenderPath(clip, style, canvas.Identity)
		base.Close()
		backend := &adaptiveRasterizer{Rasterizer: rasterizer.FromImage(got, resolution, canvas.DefaultColorSpace), resolution: resolution, fastImages: true}
		newRasterTextClip(backend, clip).RenderPath(canvas.Rectangle(30, 30), style, canvas.Identity)
		backend.Close()
		for i, expected := range want.Pix {
			// 单通道掩码保存为 8 位覆盖率，允许一次量化产生的一级误差。
			if delta := int(got.Pix[i]) - int(expected); delta < -1 || delta > 1 {
				t.Fatalf("裁剪%v，通道%d=%d，期望%d", clip.Bounds(), i, got.Pix[i], expected)
			}
		}
	}
}

// 像素对齐的是裁剪矩形，字形边缘仍保留原本的分数覆盖率。
func TestTextClipMaskAlignedRectangleKeepsFractionalGlyphEdges(t *testing.T) {
	resolution := canvas.DPMM(5)
	clip := canvas.Rectangle(12, 10).Translate(4, 3)
	path := canvas.Circle(5.13).Translate(8.17, 8.23)
	style := canvas.DefaultStyle
	style.Fill = canvas.Paint{Color: color.RGBA{R: 128, A: 128}}
	want, got := image.NewRGBA(image.Rect(0, 0, 120, 100)), image.NewRGBA(image.Rect(0, 0, 120, 100))
	plain := rasterizer.FromImage(want, resolution, canvas.DefaultColorSpace)
	plain.RenderPath(path, style, canvas.Identity)
	plain.Close()
	backend := &adaptiveRasterizer{Rasterizer: rasterizer.FromImage(got, resolution, canvas.DefaultColorSpace), resolution: resolution, fastImages: true}
	newRasterTextClip(backend, clip).RenderPath(path, style, canvas.Identity)
	backend.Close()
	ink, fractional, removed := 0, 0, 0
	for y := range 100 {
		for x := range 120 {
			expected := want.RGBAAt(x, y)
			if x < 20 || x >= 80 || y < 35 || y >= 85 {
				removed += int(expected.A)
				expected = color.RGBA{}
			} else {
				ink += int(expected.A)
				if expected.A > 0 && expected.A < 128 {
					fractional++
				}
			}
			if pixel := got.RGBAAt(x, y); pixel != expected {
				t.Fatalf("像素(%d,%d)=%v，期望%v", x, y, pixel, expected)
			}
		}
	}
	if ink == 0 || fractional == 0 || removed == 0 {
		t.Fatalf("样例未覆盖可见、抗锯齿和被裁剪部分：%d/%d/%d", ink, fractional, removed)
	}
}

func TestRasterBackendRecordsNativeTextClip(t *testing.T) {
	family := pdfTextTestFamily(t)
	line := canvas.NewTextLine(family.Face(24, canvas.Black), "OOOO", canvas.Left)
	clip := canvas.Rectangle(8.17, 12).Translate(0, -3)
	resolution := geom.DPI(150)
	backend := newCanvasRasterBackend(40, 25, resolution)
	backend.Translate(4, 8)
	if !backend.DrawClippedTextLine(line, clip) {
		t.Fatal("canvas 栅格后端应推迟裁剪，避免核心对字形逐段求交")
	}
	page := canvas.New(40, 25)
	page.RenderClippedText(line, canvas.Identity.Translate(4, 8), clip)
	want := rasterize(page, canvas.Resolution(resolution), canvas.DefaultColorSpace, nil)
	if got := backend.Raster(); !bytes.Equal(got.Pix, want.Pix) {
		t.Fatal("直接栅格与矢量回放的文字裁剪像素不一致")
	}
}

func TestTextClipMaskLimitedToSpanBounds(t *testing.T) {
	resolution := canvas.DPMM(5)
	target := image.NewRGBA(image.Rect(0, 0, 1200, 1600))
	backend := &adaptiveRasterizer{Rasterizer: rasterizer.FromImage(target, resolution, canvas.DefaultColorSpace), resolution: resolution, fastImages: true}
	defer backend.Close()
	clip := canvas.Rectangle(240, 320).Append(canvas.Circle(10).Translate(120, 160)).Settle(canvas.EvenOdd)
	renderer := newRasterTextClip(backend, clip)
	path := canvas.Rectangle(12, 4).Translate(2, 20)
	renderer.RenderPath(path, canvas.DefaultStyle, canvas.Identity)
	if renderer.mask == nil || renderer.mask.bounds != path.Bounds() {
		t.Fatalf("掩码没有限制在当前文字范围内: %v", renderer.mask)
	}
}

func TestTextClipMaskGrowsAndReusesEarlierSpanBounds(t *testing.T) {
	resolution := canvas.DPMM(5)
	target := image.NewRGBA(image.Rect(0, 0, 1200, 1600))
	backend := &adaptiveRasterizer{Rasterizer: rasterizer.FromImage(target, resolution, canvas.DefaultColorSpace), resolution: resolution, fastImages: true}
	defer backend.Close()
	clip := canvas.Rectangle(240, 320).Append(canvas.Circle(10).Translate(120, 160)).Settle(canvas.EvenOdd)
	renderer := newRasterTextClip(backend, clip)
	var expanded *textClipMask
	for i, x := range []float64{2, 40, 2, 20, 40} {
		path := canvas.Rectangle(12, 4).Translate(x, 20)
		renderer.RenderPath(path, canvas.DefaultStyle, canvas.Identity)
		if renderer.mask == nil || !renderer.mask.bounds.Contains(path.Bounds()) {
			t.Fatal("掩码未包含当前 span")
		}
		if i == 1 {
			expanded = renderer.mask
			if expanded.bounds != (canvas.Rect{X0: 2, Y0: 20, X1: 52, Y1: 24}) {
				t.Fatalf("掩码没有累积已有文字范围: %v", expanded.bounds)
			}
		} else if i > 1 && renderer.mask != expanded {
			t.Fatal("返回已覆盖的 span 时不应重建掩码")
		}
		if target.RGBAAt(int((x+1)*5), 1600-21*5).A != 255 {
			t.Fatal("后续 span 超出首个掩码范围时丢失")
		}
	}
}

func TestTextClipMaskReusesWholeTextOutlineBounds(t *testing.T) {
	family := pdfTextTestFamily(t)
	rich := canvas.NewRichText(family.Face(20, canvas.Black))
	for i := range 8 {
		fill := canvas.Black
		if i%2 == 0 {
			fill = canvas.Red
		}
		rich.WriteFace(family.Face(20, fill), "OO ")
	}
	text := rich.ToText(80, 0, canvas.Left, canvas.Top, nil)
	resolution := canvas.DPMM(5)
	for _, matrix := range []canvas.Matrix{
		canvas.Identity.Translate(5.37, 45.13),
		canvas.Identity.Translate(2.37, 30.13).Rotate(15),
	} {
		target := image.NewRGBA(image.Rect(0, 0, 1200, 1600))
		backend := &adaptiveRasterizer{Rasterizer: rasterizer.FromImage(target, resolution, canvas.DefaultColorSpace), resolution: resolution, fastImages: true}
		clip := canvas.Rectangle(240, 320).Append(canvas.Circle(4).Translate(15, 40)).Settle(canvas.EvenOdd)
		renderer := newRasterTextClip(backend, clip)
		renderer.render(text, matrix)
		mask := renderer.mask
		if mask == nil || mask.bounds != renderer.textBounds || mask.bounds.W() >= 100 || mask.bounds.H() >= 100 {
			t.Fatalf("掩码应只覆盖整段文字轮廓与裁剪的交集: %+v", mask)
		}
		before := append([]byte(nil), target.Pix...)
		clear(target.Pix)
		// 再次从首个 span 回放，每个 span 都应复用已有掩码，且绘制结果不变。
		text.RenderTo(renderer, matrix, resolution)
		if renderer.mask != mask {
			t.Fatal("整段文字的掩码仍被逐 span 重建")
		}
		if !bytes.Equal(before, target.Pix) {
			t.Fatal("收集整段轮廓改变了文字的像素对齐或绘制顺序")
		}
		backend.Close()
	}
}
