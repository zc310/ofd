package media

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func TestEncodePNGProducesDecodableImage(t *testing.T) {
	source := image.NewGray(image.Rect(0, 0, 2, 1))
	source.SetGray(0, 0, color.Gray{Y: 32})
	source.SetGray(1, 0, color.Gray{Y: 224})

	var encoded bytes.Buffer
	if err := EncodePNG(&encoded, source); err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeBytes(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds() != source.Bounds() {
		t.Fatalf("decoded bounds = %v, want %v", decoded.Bounds(), source.Bounds())
	}
	for x, want := range []uint8{32, 224} {
		if got := color.GrayModel.Convert(decoded.At(x, 0)).(color.Gray).Y; got != want {
			t.Fatalf("decoded pixel %d = %d, want %d", x, got, want)
		}
	}
}

func TestEncodePNGLevelProducesDecodableImage(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 1, 1))
	source.SetRGBA(0, 0, color.RGBA{R: 12, G: 34, B: 56, A: 255})

	var encoded bytes.Buffer
	if err := EncodePNGLevel(&encoded, source, 7); err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeBytes(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, a := decoded.At(0, 0).RGBA()
	if r>>8 != 12 || g>>8 != 34 || b>>8 != 56 || a>>8 != 255 {
		t.Fatalf("decoded pixel = (%d,%d,%d,%d), want (12,34,56,255)", r>>8, g>>8, b>>8, a>>8)
	}
}
