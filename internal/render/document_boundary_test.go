package render

import (
	"image"
	"image/color"
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/imageops"
)

func TestContentImageBoundsSupportsOriginsAndSubimages(t *testing.T) {
	for _, bounds := range []image.Rectangle{
		image.Rect(0, 0, 12, 10), image.Rect(10, 20, 22, 30), image.Rect(-8, -6, 4, 4),
	} {
		for _, nrgba := range []bool{false, true} {
			var img image.Image
			if nrgba {
				img = image.NewNRGBA(bounds)
			} else {
				img = image.NewRGBA(bounds)
			}
			pixels := img.(interface{ Set(int, int, color.Color) })
			pixels.Set(bounds.Min.X+2, bounds.Min.Y+3, color.NRGBA{A: 9})
			pixels.Set(bounds.Min.X+7, bounds.Min.Y+6, color.NRGBA{A: 255})
			pixels.Set(bounds.Min.X, bounds.Min.Y, color.NRGBA{A: 8})
			sub := img.(interface {
				SubImage(image.Rectangle) image.Image
			}).SubImage(
				image.Rect(bounds.Min.X+1, bounds.Min.Y+2, bounds.Max.X-1, bounds.Max.Y-1))
			for _, source := range []image.Image{img, sub} {
				got := [4]float64{}
				got[0], got[1], got[2], got[3] = contentImageBounds(source)
				want := [4]float64{float64(bounds.Min.X + 2), float64(bounds.Min.Y + 3), float64(bounds.Min.X + 8), float64(bounds.Min.Y + 7)}
				if got != want {
					t.Fatalf("NRGBA=%v bounds=%v: got %v, want %v", nrgba, source.Bounds(), got, want)
				}
				var reference [4]float64
				reference[0], reference[1], reference[2], reference[3] = imageops.ContentBounds(genericBoundsImage{source})
				if got != reference {
					t.Fatalf("fast scan %v differs from generic %v", got, reference)
				}
			}
		}
	}
	for _, img := range []image.Image{
		image.NewRGBA(image.Rect(10, 20, 14, 24)), image.NewNRGBA(image.Rect(-4, -3, 0, 0)),
		image.NewRGBA(image.Rect(2, 3, 2, 3)),
	} {
		x0, y0, x1, y1 := contentImageBounds(img)
		if x0 != 0 || y0 != 0 || x1 != 0 || y1 != 0 {
			t.Fatal("transparent or empty image has content bounds")
		}
	}
}

type genericBoundsImage struct{ image.Image }

func TestOffscreenUnavailableSkipsGradientAndClippedImage(t *testing.T) {
	for _, factory := range []func(float64, float64, geom.Resolution) OffscreenSurface{
		nil, func(float64, float64, geom.Resolution) OffscreenSurface { return nil },
	} {
		setOffscreenSurfaceFactory(t, factory)
		setAlphaMaskSurfaceFactory(t, nil)
		var doc Document
		var budget renderBudget
		budget.reset()
		ctx := newCanvasBackend(canvas.NewContext(canvas.New(20, 20)))
		gradient := geom.NewLinearGradient(geom.Point{}, geom.Point{X: 10, Y: 10})
		if doc.drawMeshPaintGradient(ctx, geom.Rectangle(10, 10), gradient, models.StBox{Width: 20, Height: 20}, nil, &budget) {
			t.Fatal("gradient reported success without an offscreen surface")
		}
		img := image.NewNRGBA(image.Rect(0, 0, 20, 20))
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 255
		}
		clip := geom.Rectangle(5, 5)
		if mask := imageClipMask(clip, 20, 20); mask != nil {
			t.Fatal("unavailable surface produced a mask")
		}
		got := imageWithClip(img, clip, geom.Identity)
		if got.Bounds() != img.Bounds() {
			t.Fatal("clipped fallback changed bounds")
		}
		for y := 0; y < 20; y++ {
			for x := 0; x < 20; x++ {
				if _, _, _, a := got.At(x, y).RGBA(); a != 0 {
					t.Fatal("unclipped image leaked through")
				}
			}
		}
		if img.Pix[3] != 255 {
			t.Fatal("fallback mutated source")
		}
	}
}
