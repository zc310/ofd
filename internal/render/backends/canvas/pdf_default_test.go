package canvas

import (
	"io"
	"testing"

	"github.com/zc310/ofd/internal/render/drawing"
)

func TestDefaultPDFRegistrationUsesCanvas(t *testing.T) {
	doc, err := drawing.NewPDFDocument(io.Discard, drawing.PDFOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.(*canvasPDFDocument); !ok {
		t.Fatalf("默认构建注册了 %T", doc)
	}
}
