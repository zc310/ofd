package canvas

import (
	"bytes"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/canvas/renderers/svg"
	"github.com/zc310/ofd/internal/testutil"
	"golang.org/x/image/font/gofont/goregular"
)

type clippedSpanCall struct {
	text   *canvas.Text
	matrix canvas.Matrix
	clip   *canvas.Path
}

type clippedSpanCapture struct {
	*canvas.Canvas

	calls []clippedSpanCall
}

func (r *clippedSpanCapture) RenderClippedText(text *canvas.Text, matrix canvas.Matrix, clip *canvas.Path) {
	r.calls = append(r.calls, clippedSpanCall{text: text, matrix: matrix, clip: clip})
	r.Canvas.RenderClippedText(text, matrix, clip)
}

func (r *clippedSpanCapture) RenderText(text *canvas.Text, matrix canvas.Matrix) {
	r.calls = append(r.calls, clippedSpanCall{text: text, matrix: matrix})
	r.Canvas.RenderText(text, matrix)
}

func TestClippedTextFallsBackOnlyForEmptyOutlineSpan(t *testing.T) {
	family := pdfTextTestFamily(t)
	semanticFamily := canvas.NewFontFamily(t.Name() + "-semantic")
	if err := semanticFamily.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	semantic := semanticFamily.Face(20, canvas.Black)
	// 独立修改字体元数据以模拟空轮廓语义字体，不影响共享字体或整形字形。
	semantic.Font.Head.XMin, semantic.Font.Head.XMax, semantic.Font.Head.YMin, semantic.Font.Head.YMax = 0, 0, 0, 0
	rich := canvas.NewRichText(family.Face(20, canvas.Black))
	rich.WriteString("LEFT")
	rich.WriteFace(semantic, "SECRET")
	rich.WriteFace(family.Face(20, canvas.Black), "RIGHT")
	line := rich.ToText(100, 0, canvas.Left, canvas.Top, nil)
	clip := canvas.Rectangle(50, 30).Translate(-1, -15)
	matrix := canvas.Identity.Translate(3.37, 16.13)
	for _, mode := range []canvas.WritingMode{canvas.HorizontalTB, canvas.VerticalRL} {
		line.WritingMode = mode
		capture := &clippedSpanCapture{Canvas: canvas.New(60, 40)}
		backend := &canvasBackend{ctx: canvas.NewContext(capture), vector: true}
		backend.Transform(matrix)
		if !backend.DrawClippedTextLine(line, clip) {
			t.Fatal("混合空轮廓字体不应导致整行回退")
		}
		if len(capture.calls) != 3 || capture.calls[0].clip == nil || capture.calls[1].clip != nil || capture.calls[2].clip == nil {
			t.Fatalf("只有空轮廓 span 应脱离裁剪: %+v", capture.calls)
		}
		index := 0
		line.WalkLines(func(y float64, spans []canvas.TextSpan) {
			x := 0.0
			if mode != canvas.HorizontalTB {
				x, y = -y, 0
			}
			for _, original := range spans {
				call := capture.calls[index]
				if call.matrix != matrix.Translate(x, y) || call.clip != nil && !call.clip.Copy().Translate(x, y).Equals(clip) {
					t.Fatalf("第 %d 个 span 的位置或裁剪被改写", index)
				}
				call.text.WalkLines(func(_ float64, copied []canvas.TextSpan) {
					got := copied[0]
					if got.X != original.X || got.Text != original.Text || got.Rotation != original.Rotation || !reflect.DeepEqual(got.Glyphs, original.Glyphs) {
						t.Fatal("拆分 span 改变了原始整形或位置")
					}
					if (index == 1) == got.Face.Fill.Has() {
						t.Fatal("只有空轮廓 span 应使用不可见画笔")
					}
				})
				if !original.Face.Fill.Has() {
					t.Fatal("回退改写了共享字体画笔")
				}
				index++
			}
		})
		if mode != canvas.HorizontalTB {
			continue
		}
		data := renderPDFText(t, 60, 40, func(r pdfTextRenderer) { capture.RenderTo(r) })
		if tool, err := exec.LookPath("pdftotext"); err == nil {
			command := exec.CommandContext(t.Context(), tool, "-", "-")
			command.Stdin = bytes.NewReader(data)
			output, err := command.CombinedOutput()
			if err != nil || strings.Count(string(output), "SECRET") != 1 {
				t.Fatalf("语义 span 提取重复或丢失: %q %v", output, err)
			}
		}
		// 空轮廓回退不能留下可见像素，且后续普通 span 仍应绘制。
		img := rasterize(capture.Canvas, canvas.DPMM(5), canvas.DefaultColorSpace, nil)
		ink := 0
		for _, pixel := range img.Pix {
			ink += int(pixel)
		}
		if ink == 0 {
			t.Fatal("普通 span 的可见内容丢失")
		}
		semanticOnly := canvas.New(60, 40)
		semanticOnly.RenderText(capture.calls[1].text, capture.calls[1].matrix)
		for _, value := range rasterize(semanticOnly, canvas.DPMM(5), canvas.DefaultColorSpace, nil).Pix {
			if value != 0 {
				t.Fatal("空轮廓语义 span 留下了可见像素")
			}
		}
	}
}

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
