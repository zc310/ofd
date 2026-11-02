package canvas

import (
	"image"
	"image/color"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

type clippedImageCapture struct{ image image.Image }

func (c *clippedImageCapture) Size() (float64, float64)              { return 20, 20 }
func (c *clippedImageCapture) RenderPath(*Path, Style, Matrix)       {}
func (c *clippedImageCapture) RenderText(*Text, Matrix)              {}
func (c *clippedImageCapture) RenderImage(img image.Image, _ Matrix) { c.image = img }

func TestCanvasClippedImageFallbackAppliesCoverage(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i-3], img.Pix[i-2], img.Pix[i-1], img.Pix[i] = 200, 100, 50, 255
	}
	scene := New(20, 20)
	scene.RenderClippedImage(img, Identity, Rectangle(5, 10))
	renderer := &clippedImageCapture{}
	scene.RenderTo(renderer)
	if renderer.image == nil {
		t.Fatal("clipped image fallback was not rendered")
	}
	inside := color.NRGBAModel.Convert(renderer.image.At(2, 5)).(color.NRGBA)
	outside := color.NRGBAModel.Convert(renderer.image.At(8, 5)).(color.NRGBA)
	if inside.A != 255 || outside.A != 0 {
		t.Fatalf("inside=%v outside=%v, want opaque inside and transparent outside", inside, outside)
	}
}

type clippedTextCapture struct {
	*Canvas

	text *Text
	clip *Path
	m    Matrix
}

func (r *clippedTextCapture) RenderClippedText(text *Text, matrix Matrix, clip *Path) {
	r.text, r.m, r.clip = text, matrix, clip
}

func TestClippedTextKeepsTextAndCopiesClipAcrossReplay(t *testing.T) {
	t.Parallel()

	family := NewFontFamily("clip-test")

	err := family.LoadFont(goregular.TTF, 0, FontRegular)
	if err != nil {
		t.Fatal(err)
	}

	text := NewTextLine(family.Face(12, Black), "Hello", Left)
	clip := Rectangle(5, 4)
	scene := New(30, 20)
	matrix := Identity.Translate(2, 3).Shear(.2, .1)
	scene.RenderClippedText(text, matrix, clip)
	clip.Translate(100, 100)
	renderer := &clippedTextCapture{Canvas: New(30, 20), m: Identity}
	view := Identity.Scale(2, 3)
	scene.RenderViewTo(renderer, view)

	if renderer.text != text || renderer.m != view.Mul(matrix) || !renderer.clip.Equals(Rectangle(5, 4)) {
		t.Fatal("native replay changed text, transform or the recorded clipping path")
	}

	if scene.MemorySize() <= New(30, 20).MemorySize() {
		t.Fatal("clipped text was not charged to the canvas memory budget")
	}
	// The outline fallback must preserve the recorded text and clipping path.
	fallback := New(30, 20)
	renderClippedText(fallback, text, matrix, renderer.clip)

	if fallback.Empty() || !renderer.clip.Equals(Rectangle(5, 4)) {
		t.Fatal("outline fallback produced no paths or modified the shared clip")
	}
}
