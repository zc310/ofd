package canvas

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/canvas/renderers/svg"
	"github.com/zc310/ofd/internal/testutil"
)

func TestNativeClippedTextPreservesPixelsTextAndFollowingGraphicsState(t *testing.T) {
	family := pdfTextTestFamily(t)
	line := canvas.NewTextLine(family.Face(24, canvas.Black), "HELLO", canvas.Left)
	clip := canvas.Rectangle(5, 10).Translate(0, -2)
	matrix := canvas.Identity.Translate(4, 8)
	page := canvas.New(40, 25)
	page.RenderClippedText(line, matrix, clip)
	style := canvas.DefaultStyle
	style.Fill, style.Stroke = canvas.Paint{Color: canvas.Red}, canvas.Paint{}
	page.RenderPath(canvas.Rectangle(4, 4), style, canvas.Identity.Translate(30, 2))
	data := renderPDFText(t, 40, 25, func(r pdfTextRenderer) { page.RenderTo(r) })
	if !bytes.Contains(data, []byte(" W n ")) || !bytes.Contains(data, []byte(" BT")) {
		t.Fatal("PDF没有保留原生文字和裁剪")
	}
	t.Run("PDF像素", func(t *testing.T) {
		tool, err := exec.LookPath("pdftoppm")
		if err != nil {
			t.Skip("缺少pdftoppm")
		}
		pages, err := testutil.RasterizePDFPages(t.Context(), tool, data, 254)
		if err != nil {
			t.Fatal(err)
		}
		img := pages[0]
		ink := 0
		for y := 50; y < 190; y++ {
			for x := 20; x < 280; x++ {
				pixel := img.RGBAAt(x, y)
				if pixel.R < 100 && pixel.G < 100 && pixel.B < 100 {
					ink++
					if x > 90 {
						t.Fatalf("原生文字超出裁剪右侧: %d,%d", x, y)
					}
				}
			}
		}
		if ink < 100 {
			t.Fatal("裁剪区域内文字丢失")
		}
		red := img.RGBAAt(320, 210)
		if red.R < 200 || red.G > 30 || red.B > 30 {
			t.Fatalf("后续图元被裁剪或颜色状态污染: %v", red)
		}
	})
	t.Run("PDF文字", func(t *testing.T) {
		tool, err := exec.LookPath("pdftotext")
		if err != nil {
			t.Skip("缺少pdftotext")
		}
		command := exec.CommandContext(t.Context(), tool, "-", "-")
		command.Stdin = bytes.NewReader(data)
		text, err := command.CombinedOutput()
		if err != nil || strings.Count(string(text), "HELLO") != 1 {
			t.Fatalf("裁剪文字提取重复或丢失: %q %v", text, err)
		}
	})
	t.Run("SVG坐标", func(t *testing.T) {
		var output bytes.Buffer
		r := svg.New(&output, 40, 25, nil)
		page.RenderTo(svgTextRenderer{r})
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), `<text`) || !strings.Contains(output.String(), `clip-path="url(#clip0)"`) || !strings.Contains(output.String(), `M4 19H9V9H4z`) {
			t.Fatalf("SVG没有保留原生文字或裁剪翻转不正确: %s", output.String())
		}
	})
}
