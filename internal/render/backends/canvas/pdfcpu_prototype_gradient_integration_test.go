package canvas

import (
	"bytes"
	"image/color"
	"os/exec"
	"testing"

	"github.com/zc310/ofd/internal/pdfcpuwriter"
	"github.com/zc310/ofd/internal/render/backends/canvasconv"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/paint"
	"github.com/zc310/ofd/internal/testutil"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

func TestPDFCPUPrototypeGradientTextPreservesAffineSamplingAndSharedResources(t *testing.T) {
	stops := canvas.Grad{
		{Offset: .2, Color: color.RGBA{R: 240, A: 255}},
		{Offset: .65, Color: color.RGBA{G: 180, A: 255}},
		{Offset: .9, Color: color.RGBA{B: 240, A: 255}},
	}
	linear := canvas.NewLinearGradient(canvas.Point{X: -3}, canvas.Point{X: 30, Y: 6})
	linear.Grad, linear.Extend = stops, [2]bool{false, true}
	radial := canvas.NewRadialGradient(canvas.Point{X: 8, Y: 1}, 0, canvas.Point{X: 8, Y: 1}, 22)
	radial.Grad, radial.Extend = stops, [2]bool{false, false}
	for _, tc := range []struct {
		name     string
		gradient canvas.Gradient
	}{{"轴向", linear}, {"径向", radial}} {
		t.Run(tc.name, func(t *testing.T) {
			family := pdfTextTestFamily(t)
			anchor := canvas.Identity.Translate(5, 10).Scale(1.2, .8).Shear(.2, .1)
			shared := canvas.Paint{Gradient: tc.gradient}
			r, err := pdfcpuwriter.New(t.Context(), 70, 30)
			if err != nil {
				t.Fatal(err)
			}
			reference := renderPDFText(t, 70, 30, func(pdf pdfTextRenderer) {
				for _, x := range []float64{1, 10.25, 20.125} {
					local := paint.MapNativeGradient(canvasconv.FromCanvasGradient(tc.gradient), geom.Identity.Translate(x, 0))
					text := canvas.NewTextLine(family.Face(14, canvasconv.ToCanvasGradient(local)), "AB", canvas.Left)
					matrix := anchor.Translate(x, 0)
					clip := canvas.Rectangle(7, 6).Translate(0, -1)
					pdf.RenderClippedText(text, matrix, clip)
					if !r.RenderTextWithPaint(text, matrix, clip, shared, anchor) {
						t.Fatal("原型拒绝原生共享渐变文字")
					}
				}
			})
			var output bytes.Buffer
			if err := r.Write(&output); err != nil {
				t.Fatal(err)
			}
			if r.Stats().Patterns != 1 {
				t.Fatalf("逐run定位膨胀为%d个渐变Pattern", r.Stats().Patterns)
			}
			if first, second := prototypeExtractText(t, output.Bytes()), prototypeExtractText(t, reference); !bytes.Equal(first, second) || bytes.Count(first, []byte("AB")) != 3 {
				t.Fatalf("渐变文字提取不同：%q / %q", first, second)
			}
			tool, err := exec.LookPath("pdftoppm")
			if err != nil {
				t.Skip("缺少pdftoppm")
			}
			first, err := testutil.RasterizePDFPages(t.Context(), tool, output.Bytes(), 144)
			if err != nil {
				t.Fatal(err)
			}
			second, err := testutil.RasterizePDFPages(t.Context(), tool, reference, 144)
			if err != nil {
				t.Fatal(err)
			}
			comparison, err := testutil.ComparePageImages(first, second, 20)
			if err != nil || comparison.PixelMatchRate < .999 {
				t.Fatalf("渐变取样、Extend或字形定位改变：%+v，%v", comparison, err)
			}
		})
	}
}
