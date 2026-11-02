package canvas

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/pdfcpuwriter"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/testutil"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

func TestPDFCPUPrototypeIntroPage26(t *testing.T) {
	_, data, reference := prototypeIntroPage(t, 26)
	content := prototypePDFContent(t, data, 1)
	start := bytes.Index(content, []byte("0.556862745098 0.337254901961 0.607843137255 RG"))
	if start < 0 || !bytes.Contains(content[:start], []byte(" W n\n")) {
		t.Fatal("紫色水印没有保留原生裁剪及描边")
	}
	end := bytes.Index(content[start:], []byte(" S\n"))
	if end < 0 || bytes.Count(content[start:start+end], []byte(" c")) != 224 {
		t.Fatal("紫色水印的原始曲线被展开或丢失")
	}
	images := pdfTextImages(t, data)
	masks := 0
	for _, image := range images {
		if image.Dict["SMask"] != nil {
			masks++
		}
	}
	if masks != 2 {
		t.Fatalf("第26页透明图片数=%d，期望2", masks)
	}
	prototypeComparePage(t, data, reference)
}

func prototypeIntroPage(t *testing.T, number int) (*pdfcpuwriter.Document, []byte, []byte) {
	t.Helper()
	document := prototypeIntroDocument(t)
	page := document.Pages[number-1]
	box, err := page.PhysicalBox()
	if err != nil {
		t.Fatal(err)
	}
	scene := canvas.New(box.Width, box.Height)
	if err := document.Draw(newCanvasBackend(canvas.NewContext(scene)), page); err != nil {
		t.Fatal(err)
	}
	r, err := pdfcpuwriter.New(t.Context(), box.Width, box.Height)
	if err != nil {
		t.Fatal(err)
	}
	// Render straight into pdfcpu: no canvas scene extensions are required.
	if err := document.Draw(newCanvasBackend(canvas.NewContext(r)), page); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := r.Write(&output); err != nil {
		t.Fatal(err)
	}
	data := output.Bytes()
	if err := api.Validate(t.Context(), bytes.NewReader(data), model.NewDefaultConfiguration(), nil); err != nil {
		t.Fatalf("pdfcpu生成的页面未通过结构校验：%v", err)
	}
	// Match compression and subsetting options rather than comparing defaults.
	var referenceOutput bytes.Buffer
	referenceDocument, err := newCanvasPDFDocument(&referenceOutput, drawing.PDFOptions{Compress: true, SubsetFonts: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := referenceDocument.AddPage(canvasVectorSurface{c: scene}, nil); err != nil {
		t.Fatal(err)
	}
	if err := referenceDocument.Close(); err != nil {
		t.Fatal(err)
	}
	reference := referenceOutput.Bytes()
	t.Logf("第%d页：pdfcpu=%d字节，canvas=%d字节；内容流=%d字节；字体=%d，渐变=%d，渐变函数=%d",
		number, len(data), len(reference), r.Stats().ContentBytes, r.Stats().Fonts, r.Stats().Patterns, r.Stats().Functions)
	prototypeSavePage(t, number, data, reference)
	return r, data, reference
}

func prototypeComparePage(t *testing.T, data, reference []byte) {
	t.Helper()
	prototypeComparePageAtMatchRate(t, data, reference, .99)
}

func prototypeComparePageAtMatchRate(t *testing.T, data, reference []byte, minimum float64) {
	t.Helper()
	t.Run("文字提取", func(t *testing.T) {
		first, second := prototypeExtractText(t, data), prototypeExtractText(t, reference)
		if !bytes.Equal(first, second) || len(bytes.TrimSpace(first)) < 10 {
			t.Fatalf("文字提取不同：\npdfcpu=%q\ncanvas=%q", first, second)
		}
	})
	t.Run("PDF像素", func(t *testing.T) {
		tool, err := exec.LookPath("pdftoppm")
		if err != nil {
			t.Skip("缺少pdftoppm")
		}
		first, err := testutil.RasterizePDFPages(t.Context(), tool, data, 144)
		if err != nil {
			t.Fatal(err)
		}
		second, err := testutil.RasterizePDFPages(t.Context(), tool, reference, 144)
		if err != nil {
			t.Fatal(err)
		}
		comparison, err := testutil.ComparePageImages(first, second, 20)
		if err != nil || comparison.WorstPageMatchRate < minimum {
			t.Fatalf("PDF像素差异过大：%+v，错误=%v", comparison, err)
		}
		t.Logf("像素比较（通道容差20）：%+v", comparison)
	})
}

func prototypeSavePage(t *testing.T, number int, data, reference []byte) {
	t.Helper()
	if dir := os.Getenv("OFD_PDFCPU_PROTOTYPE_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range map[string][]byte{fmt.Sprintf("page%d-pdfcpu.pdf", number): data, fmt.Sprintf("page%d-canvas.pdf", number): reference} {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func prototypeIntroDocument(t *testing.T) *render.Document {
	t.Helper()
	const input = "../../../../testdata/ofdrw/intro.ofd"
	ofd, err := parser.NewOFD(input)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skipf("缺少样例 %s", input)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ofd.Close() })
	document := render.NewDocument(color.Transparent, ofd.Documents[0])
	t.Cleanup(document.Close)
	return document
}

func TestPDFCPUPrototypeIntroPage7KeepsSharedGradientSpace(t *testing.T) {
	r, data, reference := prototypeIntroPage(t, 7)
	if r.Stats().Patterns != 16 {
		t.Fatalf("第7页渐变资源=%d，期望16，不能随逐字定位膨胀", r.Stats().Patterns)
	}
	prototypeComparePage(t, data, reference)
}

func prototypeExtractText(t *testing.T, data []byte) []byte {
	t.Helper()
	tool, err := exec.LookPath("pdftotext")
	if err != nil {
		t.Skip("缺少pdftotext")
	}
	command := exec.CommandContext(t.Context(), tool, "-layout", "-", "-")
	command.Stdin = bytes.NewReader(data)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("文字提取失败：%v，%s", err, stderr.Bytes())
	}
	return output
}

func TestPDFCPUPrototypePreservesGlyphAliasesAndSearchableText(t *testing.T) {
	family := pdfTextTestFamily(t)
	text := canvas.NewTextLine(family.Face(18, canvas.Black), "AA", canvas.Left)
	text.WalkSpans(func(_, _ float64, span canvas.TextSpan) { span.Glyphs[0].Text = "😀" })
	r, err := pdfcpuwriter.New(t.Context(), 40, 20)
	if err != nil {
		t.Fatal(err)
	}
	r.RenderText(text, canvas.Identity.Translate(2, 10))
	var output bytes.Buffer
	if err := r.Write(&output); err != nil {
		t.Fatal(err)
	}
	if text := prototypeExtractText(t, output.Bytes()); !bytes.Contains(text, []byte("😀A")) {
		t.Fatalf("同一字形对应不同语义时丢失ToUnicode：%q", text)
	}
}

func TestPDFCPUPrototypeDoesNotHideUnsupportedPaintOrCancellation(t *testing.T) {
	r, err := pdfcpuwriter.New(t.Context(), 40, 20)
	if err != nil {
		t.Fatal(err)
	}
	gradient := canvas.NewLinearGradient(canvas.Point{}, canvas.Point{X: 20})
	gradient.Add(0, canvas.Red)
	gradient.Add(1, canvas.Blue)
	gradient.Grad[0].Color = color.RGBA{R: 128, A: 128}
	style := canvas.DefaultStyle
	style.Fill.Gradient = gradient
	r.RenderPath(canvas.Rectangle(10, 10), style, canvas.Identity)
	var output bytes.Buffer
	if err := r.Write(&output); !errors.Is(err, pdfcpuwriter.ErrUnsupported) || output.Len() != 0 {
		t.Fatalf("不支持的透明渐变未在写出前失败：%v，输出%d字节", err, output.Len())
	}
	ctx, cancel := context.WithCancel(t.Context())
	r, err = pdfcpuwriter.New(ctx, 40, 20)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := r.Write(&output); err != context.Canceled || output.Len() != 0 { //nolint:errorlint // The cancellation contract requires the original ctx.Err().
		t.Fatalf("取消没有原样返回或产生部分输出：%v", err)
	}
}

func TestPDFCPUPrototypeNativeClipPreservesHoleOpacityAndFollowingDrawing(t *testing.T) {
	r, err := pdfcpuwriter.New(t.Context(), 40, 30)
	if err != nil {
		t.Fatal(err)
	}
	backend := &canvasBackend{ctx: canvas.NewContext(r), vector: true}
	backend.ClearFill()
	backend.SetStrokeColor(color.RGBA{R: 128, A: 128})
	backend.SetStrokeWidth(2)
	clip := geom.Rectangle(20, 10).Translate(5, 5).
		Append(geom.Rectangle(4, 4).Translate(13, 8)).Settle(geom.EvenOdd)
	if !backend.DrawClippedPath(geom.MustParseSVGPath("M0 10 L30 10"), clip) {
		t.Fatal("pdfcpu原型未接收原生裁剪")
	}
	backend.ClearStroke()
	backend.SetFillColor(canvas.Blue)
	backend.DrawPath(0, 0, geom.Rectangle(4, 4).Translate(30, 20))
	var output bytes.Buffer
	if err := r.Write(&output); err != nil {
		t.Fatal(err)
	}
	tool, err := exec.LookPath("pdftoppm")
	if err != nil {
		t.Skip("缺少pdftoppm")
	}
	pages, err := testutil.RasterizePDFPages(t.Context(), tool, output.Bytes(), 254)
	if err != nil {
		t.Fatal(err)
	}
	checkNativeClippedPathPixels(t, pages[0])
}

func prototypePDFContext(t *testing.T, data []byte) *model.Context {
	t.Helper()
	ctx, err := api.ReadAndValidate(t.Context(), bytes.NewReader(data), model.NewDefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func prototypePDFContent(t *testing.T, data []byte, number int) []byte {
	t.Helper()
	ctx := prototypePDFContext(t, data)
	page, _, _, err := ctx.PageDict(t.Context(), number, false)
	if err != nil {
		t.Fatal(err)
	}
	content, err := ctx.PageContent(page, number)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
