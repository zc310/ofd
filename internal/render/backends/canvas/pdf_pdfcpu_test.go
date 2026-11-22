//go:build pdfcpu

package canvas

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

func TestPDFCPURegistrationWritesMultipagePDF(t *testing.T) {
	var output, reference bytes.Buffer
	const producer = "zc310/ofd vtest"
	doc, err := drawing.NewPDFDocument(&output, drawing.PDFOptions{Producer: producer})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.(*pdfcpuPDFDocument); !ok {
		t.Fatalf("pdfcpu 标签注册了 %T", doc)
	}
	ref, err := newCanvasPDFDocument(&reference, drawing.PDFOptions{Compress: true, SubsetFonts: true})
	if err != nil {
		t.Fatal(err)
	}
	family := pdfTextTestFamily(t)
	for _, width := range []float64{40, 50} {
		scene := canvas.New(width, 30)
		ctx := canvas.NewContext(scene)
		ctx.DrawPath(2, 2, canvas.Rectangle(10, 5))
		ctx.DrawText(2, 20, canvas.NewTextLine(family.Face(12, canvas.Black), "PDFCPU", canvas.Left))
		for _, target := range []drawing.PDFDocument{doc, ref} {
			if err := target.AddPage(canvasVectorSurface{c: scene}, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := doc.AddOutlines(nil); err != nil {
		t.Fatal(err)
	}
	if err := doc.Close(); err != nil {
		t.Fatal(err)
	}
	size := output.Len()
	if err := doc.Close(); err != nil || output.Len() != size {
		t.Fatal("重复关闭改变输出或错误")
	}
	if doc.AddPage(newCanvasVectorSurface(10, 10, nil), nil) == nil || doc.AddOutlines(nil) == nil {
		t.Fatal("关闭后仍接受写入")
	}
	if err := ref.Close(); err != nil {
		t.Fatal(err)
	}
	if err := api.Validate(t.Context(), bytes.NewReader(output.Bytes()), model.NewStatelessConfiguration(), nil); err != nil {
		t.Fatal(err)
	}
	parsed, err := api.ReadContext(t.Context(), bytes.NewReader(output.Bytes()), model.NewStatelessConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	if err := api.ValidateContext(t.Context(), parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Producer != producer {
		t.Fatalf("Producer = %q, want %q", parsed.Producer, producer)
	}
	prototypeComparePage(t, output.Bytes(), reference.Bytes())
}

func TestPDFCPURegistrationSupportsLinksOutlinesAndLossyOption(t *testing.T) {
	if _, err := drawing.NewPDFDocument(nil, drawing.PDFOptions{}); err == nil {
		t.Fatal("接受了空输出")
	}
	if _, err := drawing.NewPDFDocument(io.Discard, drawing.PDFOptions{LossyImages: true}); err != nil {
		t.Fatalf("有损图片选项：%v", err)
	}
	var output bytes.Buffer
	doc, err := drawing.NewPDFDocument(&output, drawing.PDFOptions{LossyImages: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.AddOutlines(nil); err == nil {
		t.Fatal("未写入页面时接受了大纲")
	}
	if err := doc.AddPage(newCanvasVectorSurface(10, 10, nil), []drawing.PageLink{{URI: "https://example.com", Width: 2, Height: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := doc.AddOutlines([]drawing.PageOutline{{Title: "大纲", Target: &drawing.LinkTarget{Page: 0}}}); err != nil {
		t.Fatal(err)
	}
	if err := doc.Close(); err != nil {
		t.Fatal(err)
	}
	if err := api.Validate(t.Context(), bytes.NewReader(output.Bytes()), model.NewStatelessConfiguration(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestPDFCPURegistrationClosePreservesWriteError(t *testing.T) {
	want := errors.New("输出失败")
	doc, err := drawing.NewPDFDocument(closeFailWriter{want}, drawing.PDFOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.AddPage(newCanvasVectorSurface(10, 10, nil), nil); err != nil {
		t.Fatal(err)
	}
	first, second := doc.Close(), doc.Close()
	if !errors.Is(first, want) || !errors.Is(second, want) {
		t.Fatal("重复关闭没有保留首次输出错误")
	}
}
