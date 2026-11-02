package canvas

import (
	"bytes"
	"image/color"
	"os/exec"
	"testing"

	"github.com/zc310/ofd/internal/render/backends/canvasconv"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/paint"
	"github.com/zc310/ofd/internal/testutil"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

type sharedTextPaintRenderer interface {
	RenderTextWithPaint(text *canvas.Text, matrix canvas.Matrix, clip *canvas.Path, paint canvas.Paint, paintMatrix canvas.Matrix) bool
}

func TestSharedTextPaintPreservesAxialAndRadialFieldsUnderAffineTransforms(t *testing.T) {
	if _, ok := any(canvas.New(1, 1)).(sharedTextPaintRenderer); !ok {
		t.Skip("当前canvas依赖尚未支持独立文字画笔坐标")
	}
	family := pdfTextTestFamily(t)
	stops := geom.Grad{{Offset: 0, Color: color.RGBA{R: 255, A: 255}}, {Offset: 1, Color: color.RGBA{B: 255, A: 255}}}
	linear := geom.NewLinearGradient(geom.Point{X: -3}, geom.Point{X: 25, Y: 6})
	linear.Grad, linear.Extend = stops, [2]bool{false, true}
	radial := geom.NewRadialGradient(geom.Point{X: 8, Y: 1}, 0, geom.Point{X: 8, Y: 1}, 18)
	radial.Grad, radial.Extend = stops, [2]bool{true, false}
	for _, tc := range []struct {
		name     string
		gradient geom.Gradient
	}{{"轴向渐变", linear}, {"径向渐变", radial}} {
		t.Run(tc.name, func(t *testing.T) {
			anchor := canvas.Identity.Translate(5, 8).Scale(1.2, .8).Shear(.2, .1)
			shared := canvas.Paint{Gradient: canvasconv.ToCanvasGradient(tc.gradient)}
			draw := func(r pdfTextRenderer, optimized bool) {
				for _, x := range []float64{1, 9.25, 18.125} {
					local := paint.MapNativeGradient(tc.gradient, geom.Identity.Translate(x, 0))
					text := canvas.NewTextLine(family.Face(12, canvasconv.ToCanvasGradient(local)), "AB", canvas.Left)
					matrix := anchor.Translate(x, 0)
					clip := canvas.Rectangle(6, 5).Translate(0, -1)
					if !optimized {
						r.RenderClippedText(text, matrix, clip)
						continue
					}
					painter, ok := any(r).(sharedTextPaintRenderer)
					if !ok || !painter.RenderTextWithPaint(text, matrix, clip, shared, anchor) {
						t.Fatal("不透明原生渐变没有使用共享画笔坐标")
					}
				}
			}
			before := renderPDFText(t, 50, 25, func(r pdfTextRenderer) { draw(r, false) })
			after := renderPDFText(t, 50, 25, func(r pdfTextRenderer) { draw(r, true) })
			if bytes.Count(before, []byte("/PatternType 2")) != 3 || bytes.Count(after, []byte("/PatternType 2")) != 1 {
				t.Fatal("逐run画笔未归并为同一渐变资源")
			}
			t.Run("PDF像素", func(t *testing.T) {
				tool, err := exec.LookPath("pdftoppm")
				if err != nil {
					t.Skip("缺少pdftoppm，跳过PDF像素对照")
				}
				first, err := testutil.RasterizePDFPages(t.Context(), tool, before, 144)
				if err != nil {
					t.Fatal(err)
				}
				second, err := testutil.RasterizePDFPages(t.Context(), tool, after, 144)
				if err != nil {
					t.Fatal(err)
				}
				comparison, err := testutil.ComparePageImages(first, second, 1)
				if err != nil || comparison.PixelMatchRate != 1 {
					t.Fatalf("共享画笔改变了字形、裁剪或渐变取样：%+v，错误=%v", comparison, err)
				}
			})
			t.Run("PDF文字", func(t *testing.T) {
				tool, err := exec.LookPath("pdftotext")
				if err != nil {
					t.Skip("缺少pdftotext")
				}
				var texts [][]byte
				for _, data := range [][]byte{before, after} {
					command := exec.CommandContext(t.Context(), tool, "-", "-")
					command.Stdin = bytes.NewReader(data)
					text, err := command.CombinedOutput()
					if err != nil {
						t.Fatal(err)
					}
					texts = append(texts, text)
				}
				if !bytes.Equal(texts[0], texts[1]) || bytes.Count(texts[1], []byte("AB")) != 3 {
					t.Fatalf("共享画笔改变了文字提取：%q / %q", texts[0], texts[1])
				}
			})
		})
	}
}

func TestSharedTextPaintLeavesEmptyOutlineSemanticsOutsideClip(t *testing.T) {
	family := pdfTextTestFamily(t)
	face := family.Face(12, canvas.Black)
	// 仅修改本测试独立加载的字体，模拟只承载语义的空轮廓字体。
	face.Font.Head.XMin, face.Font.Head.XMax, face.Font.Head.YMin, face.Font.Head.YMax = 0, 0, 0, 0
	line := canvas.NewTextLine(face, "SECRET", canvas.Left)
	gradient := geom.NewLinearGradient(geom.Point{}, geom.Point{X: 20})
	gradient.Grad = geom.Grad{{Offset: 0, Color: color.RGBA{R: 255, A: 255}}, {Offset: 1, Color: color.RGBA{B: 255, A: 255}}}
	scene := canvas.New(40, 20)
	backend := &canvasBackend{ctx: canvas.NewContext(scene), vector: true}
	clip := geom.Rectangle(5, 5)
	if backend.DrawSharedPaintTextLine(line, clip, geom.GradientPaint(gradient), geom.Identity) {
		t.Fatal("空轮廓语义文字不应进入共享画笔裁剪层")
	}
	if !backend.DrawClippedTextLine(line, clip) {
		t.Fatal("空轮廓文字没有保留原有语义回退")
	}
	data := renderPDFText(t, 40, 20, func(r pdfTextRenderer) { scene.RenderTo(r) })
	tool, err := exec.LookPath("pdftotext")
	if err != nil {
		t.Skip("缺少pdftotext")
	}
	command := exec.CommandContext(t.Context(), tool, "-", "-")
	command.Stdin = bytes.NewReader(data)
	text, err := command.CombinedOutput()
	if err != nil || bytes.Count(text, []byte("SECRET")) != 1 {
		t.Fatalf("空轮廓语义提取重复或丢失：%q，错误=%v", text, err)
	}
}
