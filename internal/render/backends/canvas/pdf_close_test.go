package canvas

import (
	"bytes"
	"errors"
	"testing"

	"github.com/zc310/ofd/internal/render/drawing"
)

type closeFailWriter struct{ err error }

func (w closeFailWriter) Write([]byte) (int, error) { return 0, w.err }

func TestPDFCloseIsIdempotentAndRejectsFurtherWrites(t *testing.T) {
	for _, empty := range []bool{false, true} {
		var output bytes.Buffer
		doc, err := newCanvasPDFDocument(&output, drawing.PDFOptions{})
		if err != nil {
			t.Fatal(err)
		}
		page := newCanvasVectorSurface(10, 10, nil)
		if !empty {
			if err := doc.AddPage(page, nil); err != nil {
				t.Fatal(err)
			}
		}
		if err := doc.Close(); err != nil {
			t.Fatal(err)
		}
		size := output.Len()
		if err := doc.Close(); err != nil || output.Len() != size {
			t.Fatal("二次关闭再次写入 PDF")
		}
		if doc.AddPage(page, nil) == nil || doc.AddOutlines(nil) == nil || output.Len() != size {
			t.Fatal("关闭后仍接受写入")
		}
	}
}

func TestPDFClosePreservesFirstError(t *testing.T) {
	want := errors.New("输出失败")
	doc, err := newCanvasPDFDocument(closeFailWriter{want}, drawing.PDFOptions{})
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
