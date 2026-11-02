package pdf

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/zc310/ofd/internal/thirdparty/canvas"
	cimage "github.com/zc310/ofd/internal/thirdparty/canvas/image"
)

func TestPDFImageRestoresOpacityForFollowingText(t *testing.T) {
	for _, kind := range []string{"image", "clipped_image", "image_inside_clipped_text"} {
		t.Run(kind, func(t *testing.T) {
			family := canvas.NewFontFamily("image-opacity")
			if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
				t.Fatal(err)
			}
			translucent := canvas.NewTextLine(family.Face(24, color.RGBA{A: 128}), "Hello", canvas.Left)
			opaque := canvas.NewTextLine(family.Face(24, canvas.Black), "World", canvas.Left)
			img := image.NewRGBA(image.Rect(0, 0, 2, 2))
			for i := 3; i < len(img.Pix); i += 4 {
				img.Pix[i] = 255
			}
			var output bytes.Buffer
			r := New(&output, 60, 30, &Options{Compress: false, SubsetFonts: true, ImageEncoding: cimage.Lossless})
			r.RenderText(translucent, canvas.Identity.Translate(2, 22))
			draw := func() {
				alpha := r.w.alpha
				if kind == "clipped_image" {
					r.RenderClippedImage(img, canvas.Identity.Translate(50, 2), canvas.Rectangle(1, 2))
				} else {
					r.RenderImage(img, canvas.Identity.Translate(50, 2))
				}
				if r.w.alpha != alpha {
					t.Errorf("image changed cached alpha from %v to %v after Q", alpha, r.w.alpha)
				}
			}
			if kind == "image_inside_clipped_text" {
				r.RenderClippedTextWith(opaque, canvas.Identity, canvas.Rectangle(60, 30), draw)
			} else {
				draw()
			}
			start := r.w.Len()
			r.RenderText(opaque, canvas.Identity.Translate(2, 10))
			reset := fmt.Sprintf(" /%v gs", r.w.graphicsStates[1])
			if !bytes.Contains(r.w.Bytes()[start:], []byte(reset)) {
				t.Error("opaque text did not restore opacity after the image's Q")
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			checkImageTextPixelsAndExtraction(t, output.Bytes())
		})
	}
}

func checkImageTextPixelsAndExtraction(t *testing.T, data []byte) {
	t.Helper()
	t.Run("pixels", func(t *testing.T) {
		tool, err := exec.LookPath("pdftoppm")
		if err != nil {
			t.Skip("pdftoppm unavailable")
		}
		root := t.TempDir()
		input, output := filepath.Join(root, "opacity.pdf"), filepath.Join(root, "page")
		if err := os.WriteFile(input, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if log, err := exec.CommandContext(t.Context(), tool, "-r", "144", "-png", "-singlefile", input, output).CombinedOutput(); err != nil {
			t.Fatalf("pdftoppm: %v: %s", err, log)
		}
		imageData, err := os.ReadFile(output + ".png")
		if err != nil {
			t.Fatal(err)
		}
		page, err := png.Decode(bytes.NewReader(imageData))
		if err != nil {
			t.Fatal(err)
		}
		minGray := [2]uint8{255, 255}
		for y := range page.Bounds().Dy() {
			if page.Bounds().Dy()/3 <= y && y < page.Bounds().Dy()/2 {
				continue
			}
			for x := 0; x < page.Bounds().Dx()/2; x++ {
				red, _, _, _ := page.At(x, y).RGBA()
				gray := uint8(red >> 8)
				half := 0
				if y >= page.Bounds().Dy()/2 {
					half = 1
				}
				if gray < minGray[half] {
					minGray[half] = gray
				}
			}
		}
		if minGray[0] < 110 || minGray[0] > 140 || minGray[1] > 15 {
			t.Fatalf("translucent/opaque text minimum gray = %v, want about [127 0]", minGray)
		}
	})
	t.Run("text", func(t *testing.T) {
		tool, err := exec.LookPath("pdftotext")
		if err != nil {
			t.Skip("pdftotext unavailable")
		}
		command := exec.CommandContext(t.Context(), tool, "-", "-")
		command.Stdin = bytes.NewReader(data)
		text, err := command.CombinedOutput()
		if err != nil || strings.Join(strings.Fields(string(text)), " ") != "Hello World" {
			t.Fatalf("extracted text = %q, error = %v", text, err)
		}
	})
}

func TestPDFClippedTextRestoresTextPositionCache(t *testing.T) {
	family := canvas.NewFontFamily("clip-position")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	line := canvas.NewTextLine(family.Face(12, canvas.Black), "Hello", canvas.Left)
	var output bytes.Buffer
	r := New(&output, 30, 20, nil)
	r.RenderText(line, canvas.Identity.Translate(2, 8))
	position := r.w.textPosition
	r.RenderClippedText(line, canvas.Identity.Translate(12, 15), canvas.Rectangle(5, 5))
	if r.w.textPosition != position {
		t.Error("clipped text did not restore the text position cache")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPDFNestedClipsRestoreDrawingStateAndKeepResources(t *testing.T) {
	family := canvas.NewFontFamily("nested-state")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	line := canvas.NewTextLine(family.Face(12, canvas.Black), "Hello", canvas.Left)
	var output bytes.Buffer
	r := New(&output, 30, 20, nil)
	r.RenderText(line, canvas.Identity.Translate(2, 8))
	r.w.SetDashes(1, []float64{2, 3})
	outer := r.w.pdfGraphicsState
	outer.dashes = append([]float64(nil), outer.dashes...)
	clip := canvas.Rectangle(20, 20)
	r.RenderClippedTextWith(line, canvas.Identity, clip, func() {
		r.w.SetAlpha(.25)
		r.w.SetFill(canvas.Paint{Color: canvas.Red}, canvas.Identity)
		r.w.SetStroke(canvas.Paint{Color: canvas.Blue}, canvas.Identity)
		r.w.SetLineWidth(5)
		r.w.SetLineCap(canvas.RoundCap)
		r.w.SetLineJoin(canvas.RoundJoin)
		r.w.SetDashes(2, []float64{5, 6})
		inner := r.w.pdfGraphicsState
		inner.dashes = append([]float64(nil), inner.dashes...)
		r.RenderClippedTextWith(line, canvas.Identity, clip, func() {
			r.w.SetDashes(0, []float64{7, 8})
			r.RenderImage(image.NewRGBA(image.Rect(0, 0, 2, 2)), canvas.Identity)
			r.w.StartTextObject()
			r.w.SetFont(line.Fonts()[0], 20, r.w.fontDirection)
			r.w.SetTextPosition(canvas.Identity.Translate(12, 15))
			r.w.SetTextCharSpace(3)
			r.w.SetTextRenderMode(2)
			// popState must close this text object before restoring the clip.
		})
		if !reflect.DeepEqual(r.w.pdfGraphicsState, inner) {
			t.Fatal("inner clip changed its enclosing cached drawing state")
		}
	})
	if !reflect.DeepEqual(r.w.pdfGraphicsState, outer) {
		t.Fatal("outer clip did not restore all cached drawing state")
	}
	for name, minimum := range map[pdfName]int{"Font": 1, "XObject": 1, "ExtGState": 2} {
		resources, ok := r.w.resources[name].(pdfDict)
		if !ok || len(resources) < minimum {
			t.Fatalf("popping drawing state lost newly registered %s resources", name)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPDFClippedTextAcceptsNormalizedEvenOddClip(t *testing.T) {
	t.Parallel()

	family := canvas.NewFontFamily("clip-hole")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	line := canvas.NewTextLine(family.Face(12, canvas.Black), "Hello", canvas.Left)
	clip := canvas.Rectangle(20, 20).Append(canvas.Rectangle(5, 5).Translate(5, 5)).Settle(canvas.EvenOdd)
	if clip.ContainsPoint(7, 7, canvas.NonZero) || !clip.ContainsPoint(2, 2, canvas.NonZero) {
		t.Fatal("normalization did not preserve the EvenOdd hole")
	}
	var output bytes.Buffer
	r := New(&output, 30, 20, nil)
	r.RenderClippedText(line, canvas.Identity, clip)
	if !strings.Contains(r.w.String(), clip.ToPDF()+" W n") || !strings.Contains(r.w.String(), " BT") {
		t.Fatal("PDF did not preserve the normalized clipping contours and native text")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}
