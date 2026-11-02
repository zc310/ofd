package canvas_test

import (
	"bytes"
	"fmt"
	"image"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/internal/thirdparty/canvas/renderers/rasterizer"
)

func TestClippedTextFallbackUsesRasterResolution(t *testing.T) {
	t.Parallel()

	for _, dpi := range []float64{72, 96, 150, 300} {
		t.Run(fmt.Sprintf("DPI=%g", dpi), func(t *testing.T) {
			t.Parallel()

			family := canvas.NewFontFamily("clip-resolution")

			err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular)
			if err != nil {
				t.Fatal(err)
			}

			resolution := canvas.DPI(dpi)
			dpmm := resolution.DPMM()
			line := canvas.NewTextLine(family.Face(18*72/dpi, canvas.Black), "OOOO", canvas.Left)
			matrix := canvas.Identity.Translate(5.37/dpmm, 35.13/dpmm)
			clip := canvas.Rectangle(200/dpmm, 100/dpmm).Transform(matrix.Inv())
			plain := canvas.New(200/dpmm, 100/dpmm)
			plain.RenderText(line, matrix)
			want := rasterizer.Draw(plain, resolution, canvas.DefaultColorSpace)

			for _, context := range []bool{false, true} {
				got := image.NewRGBA(want.Bounds())
				raster := rasterizer.FromImage(got, resolution, canvas.DefaultColorSpace)

				if context {
					canvas.NewContext(canvas.NewContext(raster)).RenderClippedText(line, matrix, clip)
				} else {
					page := canvas.New(200/dpmm, 100/dpmm)
					page.RenderClippedText(line, matrix, clip)
					page.RenderTo(raster)
				}

				raster.Close()

				if !bytes.Equal(got.Pix, want.Pix) {
					t.Fatalf("context=%v: clipping changed device-aligned text pixels", context)
				}
			}
		})
	}
}

func TestClippedTextFallbackPixelsPreserveGlyphHoles(t *testing.T) {
	t.Parallel()

	family := canvas.NewFontFamily("clip-pixels")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	face := family.Face(48, canvas.Black)
	line := canvas.NewTextLine(face, "O", canvas.Left)
	outline, _ := face.ToPath("O")
	matrix := canvas.Identity.Translate(4, 6)
	bounds := outline.Bounds().Transform(matrix)
	cut := bounds.X0 + bounds.W()*.75
	clip := canvas.Rect{X0: 0, Y0: 0, X1: cut, Y1: 30}.ToPath().Transform(matrix.Inv())
	full, clipped := canvas.New(30, 30), canvas.New(30, 30)
	full.RenderText(line, matrix)
	clipped.RenderClippedText(line, matrix, clip)
	const dpmm = 16
	reference := rasterizer.Draw(full, canvas.DPMM(dpmm), canvas.LinearColorSpace{})
	actual := rasterizer.Draw(clipped, canvas.DPMM(dpmm), canvas.LinearColorSpace{})
	ink, removed, hole := 0, 0, 0
	for y := range actual.Bounds().Dy() {
		for x := range actual.Bounds().Dx() {
			px, py := (float64(x)+.5)/dpmm, 30-(float64(y)+.5)/dpmm
			a, b := actual.RGBAAt(x, y).A, reference.RGBAAt(x, y).A
			if px > cut+.2 {
				if a != 0 {
					t.Fatalf("ink outside clip at (%d, %d): alpha %d, cut=%g glyph=%v", x, y, a, cut, bounds)
				}
				if b > 200 {
					removed++
				}
			} else if px < cut-.2 {
				if b == 255 && opaqueInterior(reference, x, y) {
					if a < 240 {
						t.Fatalf("glyph fill disappeared inside clip at (%d, %d)", x, y)
					}
					ink++
				}
				center := bounds.Center()
				if center.X-1 < px && px < center.X+1 && center.Y-1 < py && py < center.Y+1 && b == 0 {
					if a != 0 {
						t.Fatalf("glyph counter was filled at (%d, %d)", x, y)
					}
					hole++
				}
			}
		}
	}
	if ink == 0 || removed == 0 || hole == 0 {
		t.Fatalf("incomplete pixel coverage: retained=%d removed=%d hole=%d", ink, removed, hole)
	}
}

func TestClippedTextFallbackPreservesNormalizedEvenOddClipHole(t *testing.T) {
	t.Parallel()

	family := canvas.NewFontFamily("clip-hole")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	face := family.Face(48, canvas.Black)
	line := canvas.NewTextLine(face, "O", canvas.Left)
	outline, _ := face.ToPath("O")
	bounds := outline.Bounds()
	end := bounds.X0 + bounds.W()*.4
	hole := canvas.Rect{X0: bounds.X0 - 1, Y0: -2, X1: end, Y1: 20}
	raw := canvas.Rectangle(30, 30).Translate(-5, -5).Append(hole.ToPath())
	clip := raw.Settle(canvas.EvenOdd)
	matrix := canvas.Identity.Translate(4, 6)
	full, clipped := canvas.New(30, 30), canvas.New(30, 30)
	full.RenderText(line, matrix)
	clipped.RenderClippedText(line, matrix, clip)
	const dpmm = 16
	reference := rasterizer.Draw(full, canvas.DPMM(dpmm), canvas.LinearColorSpace{})
	actual := rasterizer.Draw(clipped, canvas.DPMM(dpmm), canvas.LinearColorSpace{})
	removed, retained := 0, 0
	for y := range actual.Bounds().Dy() {
		for x := range actual.Bounds().Dx() {
			if reference.RGBAAt(x, y).A != 255 || !opaqueInterior(reference, x, y) {
				continue
			}
			px := (float64(x)+.5)/dpmm - 4
			py := 30 - (float64(y)+.5)/dpmm - 6
			alpha := actual.RGBAAt(x, y).A
			if hole.Expand(-.2).ContainsPoint(canvas.Point{X: px, Y: py}) {
				if alpha != 0 {
					t.Fatal("normalized EvenOdd clip hole was filled")
				}
				removed++
			} else if !hole.Expand(.2).ContainsPoint(canvas.Point{X: px, Y: py}) {
				if alpha < 240 {
					t.Fatal("ink outside the clip hole was lost")
				}
				retained++
			}
		}
	}
	if removed == 0 || retained == 0 {
		t.Fatalf("normalized clip did not exercise both regions: removed=%d retained=%d", removed, retained)
	}
}

// Ignore the one-pixel glyph edge affected by outline flattening, while still
// requiring all opaque interior pixels to survive clipping with the Go font.
func opaqueInterior(img *image.RGBA, x, y int) bool {
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if img.RGBAAt(x+dx, y+dy).A != 255 {
				return false
			}
		}
	}
	return true
}
