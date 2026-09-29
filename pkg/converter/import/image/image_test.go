package image_test

import (
	"bytes"
	"context"
	stdimage "image"
	"image/color"
	"image/png"
	"testing"

	"github.com/hhrutter/tiff"
	"github.com/zc310/ofd/internal/ocr"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/converter"
	"github.com/zc310/ofd/pkg/converter/import/image"
)

type fakeEngine struct{}

func (fakeEngine) Recognize(context.Context, stdimage.Image) ([]ocr.TextBlock, error) {
	return []ocr.TextBlock{{Text: "识别文字", Bounds: stdimage.Rect(10, 10, 100, 30), Confidence: 99}}, nil
}

func testPNG(t *testing.T, c color.Color) []byte {
	t.Helper()
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, 120, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 120; x++ {
			img.Set(x, y, c)
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestConvertPNGToOFDAddsTextLayer(t *testing.T) {
	data := testPNG(t, color.RGBA{R: 255, A: 255})
	var output bytes.Buffer
	if err := image.Convert(context.Background(), data, &output, image.WithOCREngine(fakeEngine{}), image.WithTextMode(image.TextVisible)); err != nil {
		t.Fatal(err)
	}
	of, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer of.Close()
	if len(of.Documents) != 1 || len(of.Documents[0].Pages) != 1 {
		t.Fatalf("page count = %d", len(of.Documents[0].Pages))
	}
	if got := len(of.Documents[0].Pages[0].Content().Layer[0].Items); got != 2 {
		t.Fatalf("item count = %d, want image + text", got)
	}
}

func TestConvertMultiPageTIFFToOFD(t *testing.T) {
	images := []stdimage.Image{
		stdimage.NewRGBA(stdimage.Rect(0, 0, 40, 30)),
		stdimage.NewRGBA(stdimage.Rect(0, 0, 60, 50)),
	}
	var input bytes.Buffer
	if err := tiff.EncodeAll(&input, images, &tiff.Options{}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := image.Convert(context.Background(), input.Bytes(), &output, image.WithOCREngine(fakeEngine{}), image.WithTextMode(image.TextOff), image.WithMaxPages(2)); err != nil {
		t.Fatal(err)
	}
	of, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer of.Close()
	if len(of.Documents) != 1 || len(of.Documents[0].Pages) != 2 {
		t.Fatalf("page count = %d, want 2", len(of.Documents[0].Pages))
	}
}

func TestImageImporterRegistered(t *testing.T) {
	for _, name := range []string{"png", "jpeg", "tiff"} {
		if _, ok := converter.ImporterByName(name); !ok {
			t.Fatalf("image importer %q 未注册", name)
		}
	}
}
