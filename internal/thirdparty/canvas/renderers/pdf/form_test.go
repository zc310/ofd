package pdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"testing"

	fastzlib "github.com/klauspost/compress/zlib"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

func formScene() *canvas.Canvas {
	scene := canvas.New(10, 10)
	scene.RenderPath(canvas.Rectangle(4, 4), canvas.DefaultStyle, canvas.Identity.Translate(1, 2))
	return scene
}

func TestPDFFormReusesObjectAcrossPagesAndIsolatesState(t *testing.T) {
	var output bytes.Buffer
	r := New(&output, 20, 20, &Options{})
	scene := formScene()
	page := r.w
	page.SetAlpha(0.25)
	page.SetFill(canvas.Paint{Color: canvas.Red}, canvas.Identity)
	beforeAlpha, beforeFill := page.alpha, page.fill
	builds := 0
	draw := func() { builds++; scene.RenderTo(r) }
	r.RenderCanvasWith(scene, canvas.Identity, draw)
	ref := page.pdf.forms[scene]
	r.RenderCanvasWith(scene, canvas.Identity.Translate(5, 6), draw)
	if builds != 1 || len(page.pdf.forms) != 1 {
		t.Fatalf("builds=%d forms=%d, want 1 each", builds, len(page.pdf.forms))
	}
	if page.alpha != beforeAlpha || page.fill != beforeFill {
		t.Fatal("Form changed the enclosing page's tracked graphics state")
	}
	if bytes.Count(page.Bytes(), []byte(fmt.Sprintf("/Fm%d Do Q", ref))) != 2 {
		t.Fatalf("page did not reuse Form: %s", page.Bytes())
	}
	r.NewPage(30, 40)
	if width, height := r.Size(); width != 30 || height != 40 {
		t.Fatalf("new page size = %gx%g", width, height)
	}
	r.RenderCanvasWith(scene, canvas.Identity, draw)
	if builds != 1 || r.w.resources["XObject"].(pdfDict)[pdfName(fmt.Sprintf("Fm%d", ref))] != ref {
		t.Fatal("second page did not reference the same Form")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if r.w.pdf.forms != nil || r.w.pdf.flateWriter != nil {
		t.Fatal("Close retained Forms or compression buffers")
	}
	if bytes.Count(output.Bytes(), []byte("/Subtype/Form")) != 1 {
		t.Fatal("expected one serialized Form")
	}
	// References cannot be shared with another PDF writer.
	other := New(io.Discard, 20, 20, &Options{})
	other.RenderCanvasWith(scene, canvas.Identity, func() { builds++; scene.RenderTo(other) })
	if builds != 2 {
		t.Fatal("Form cache leaked across PDF documents")
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPDFFormSupportsNestedCanvasesAndBoundsCache(t *testing.T) {
	var output bytes.Buffer
	r := New(&output, 20, 20, &Options{Compress: true})
	child := formScene()
	parent := canvas.New(20, 20)
	parent.RenderCanvas(child, canvas.Identity.Translate(3, 4))
	r.RenderCanvas(parent, canvas.Identity)
	if len(r.w.pdf.forms) != 2 {
		t.Fatalf("nested forms = %d, want 2", len(r.w.pdf.forms))
	}
	for range 100 {
		r.RenderCanvas(formScene(), canvas.Identity)
	}
	if len(r.w.pdf.forms) > 64 || r.w.pdf.formBytes > 32<<20 {
		t.Fatal("Form cache exceeded its retention bound")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPDFCompressionReusesWriterAndProducesIndependentStreams(t *testing.T) {
	var output bytes.Buffer
	w := newPDFWriter(&output)
	var first io.WriteCloser
	for i := range 3 {
		contents := []byte(fmt.Sprintf("page %d content", i))
		start := output.Len()
		w.writeObject(pdfStream{dict: pdfDict{"Filter": pdfFilterFlate}, stream: contents})
		if i == 0 {
			first = w.flateWriter
		} else if first != w.flateWriter {
			t.Fatal("writer was allocated again for the next content stream")
		}
		data := output.Bytes()[start:]
		stream := bytes.Index(data, []byte("stream\n")) + len("stream\n")
		end := bytes.LastIndex(data, []byte("\nendstream"))
		var fresh bytes.Buffer
		compressor := fastzlib.NewWriter(&fresh)
		_, _ = compressor.Write(contents)
		_ = compressor.Close()
		if !bytes.Equal(data[stream:end], fresh.Bytes()) {
			t.Fatal("reset writer changed compressed bytes compared with a fresh writer")
		}
		reader, err := zlib.NewReader(bytes.NewReader(data[stream:end]))
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(reader)
		reader.Close()
		if err != nil || !bytes.Equal(got, contents) {
			t.Fatalf("stream %d = %q, %v", i, got, err)
		}
	}
}
