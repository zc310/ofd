package pdf

import (
	"bytes"
	"image"
	"image/color"
	"math/rand"
	"testing"
)

// The reference implementations are the original per-pixel loops that called
// img.At(x, y).RGBA(). The optimised helpers must agree with them byte for byte
// for every concrete image type they special-case and for arbitrary images too.

func refAlphaGray(img image.Image, size image.Point) (*image.Gray, bool) {
	hasMask := false
	sp := img.Bounds().Min
	mask := image.NewGray(image.Rect(0, 0, size.X, size.Y))
	for y := 0; y < size.Y; y++ {
		for x := 0; x < size.X; x++ {
			_, _, _, A := img.At(sp.X+x, sp.Y+y).RGBA()
			if A != 0 {
				mask.Pix[y*mask.Stride+x] = byte(A >> 8)
			}
			if A>>8 != 255 {
				hasMask = true
			}
		}
	}
	return mask, hasMask
}

func refRGBStream(img image.Image, size image.Point) (rgb, alpha []byte, hasAlpha bool) {
	rgb = make([]byte, size.X*size.Y*3)
	alpha = make([]byte, size.X*size.Y)
	sp := img.Bounds().Min
	for y := 0; y < size.Y; y++ {
		for x := 0; x < size.X; x++ {
			i := (y*size.X + x) * 3
			R, G, B, A := img.At(sp.X+x, sp.Y+y).RGBA()
			if A != 0 {
				rgb[i+0] = byte((R * 65535 / A) >> 8)
				rgb[i+1] = byte((G * 65535 / A) >> 8)
				rgb[i+2] = byte((B * 65535 / A) >> 8)
			}
			alpha[y*size.X+x] = byte(A >> 8)
			if alpha[y*size.X+x] != 0xff {
				hasAlpha = true
			}
		}
	}
	if !hasAlpha {
		alpha = nil
	}
	return rgb, alpha, hasAlpha
}

// wrapped hides the concrete type behind the image.Image interface so the
// fallback path is exercised as well.
type wrapped struct{ img image.Image }

func (w wrapped) ColorModel() color.Model { return w.img.ColorModel() }
func (w wrapped) Bounds() image.Rectangle { return w.img.Bounds() }
func (w wrapped) At(x, y int) color.Color { return w.img.At(x, y) }

func sampleImages(t *testing.T) map[string]image.Image {
	t.Helper()
	const w, h = 23, 17
	rnd := rand.New(rand.NewSource(7))

	imgs := map[string]image.Image{}

	nrgba := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range nrgba.Pix {
		nrgba.Pix[i] = byte(rnd.Intn(256))
	}
	imgs["NRGBA"] = nrgba

	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range rgba.Pix {
		rgba.Pix[i] = byte(rnd.Intn(256))
	}
	imgs["RGBA"] = rgba

	// An image with a sub-image origin, so the stride handling is covered.
	full := image.NewNRGBA(image.Rect(0, 0, w*2, h*2))
	for i := range full.Pix {
		full.Pix[i] = byte(rnd.Intn(256))
	}
	imgs["NRGBA sub-image"] = full.SubImage(image.Rect(3, 5, 3+w, 5+h))

	gray := image.NewGray(image.Rect(0, 0, w, h))
	for i := range gray.Pix {
		gray.Pix[i] = byte(rnd.Intn(256))
	}
	imgs["Gray"] = gray

	rgba64 := image.NewRGBA64(image.Rect(0, 0, w, h))
	for i := 0; i < len(rgba64.Pix); i += 2 {
		rgba64.Pix[i] = byte(rnd.Intn(256))
		rgba64.Pix[i+1] = byte(rnd.Intn(256))
	}
	imgs["RGBA64"] = rgba64

	// Fully opaque, so the alpha plane must be dropped.
	opaque := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range opaque.Pix {
		opaque.Pix[i] = 0x40
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			opaque.SetNRGBA(x, y, color.NRGBA{0x40, 0x80, 0xc0, 0xff})
		}
	}
	imgs["NRGBA opaque"] = opaque

	// Fully transparent, exercising the A == 0 branch.
	clear := image.NewNRGBA(image.Rect(0, 0, w, h))
	imgs["NRGBA transparent"] = clear

	imgs["wrapped NRGBA"] = wrapped{nrgba}
	imgs["wrapped Gray"] = wrapped{gray}
	return imgs
}

func TestAlphaGrayMatchesReference(t *testing.T) {
	for name, img := range sampleImages(t) {
		t.Run(name, func(t *testing.T) {
			size := img.Bounds().Size()
			gotMask, gotHas := alphaGray(img, size)
			wantMask, wantHas := refAlphaGray(img, size)
			if gotHas != wantHas {
				t.Fatalf("hasAlpha = %v, 期望 %v", gotHas, wantHas)
			}
			if !bytes.Equal(gotMask.Pix, wantMask.Pix) {
				t.Fatalf("alpha 平面不一致:\n got %v\nwant %v", gotMask.Pix, wantMask.Pix)
			}
		})
	}
}

func TestRGBStreamMatchesReference(t *testing.T) {
	for name, img := range sampleImages(t) {
		t.Run(name, func(t *testing.T) {
			size := img.Bounds().Size()
			gotRGB, gotAlpha, gotHas := rgbStream(img, size)
			wantRGB, wantAlpha, wantHas := refRGBStream(img, size)
			if gotHas != wantHas {
				t.Fatalf("hasAlpha = %v, 期望 %v", gotHas, wantHas)
			}
			if !bytes.Equal(gotRGB, wantRGB) {
				t.Fatalf("RGB 流不一致:\n got %v\nwant %v", gotRGB, wantRGB)
			}
			if (gotAlpha == nil) != (wantAlpha == nil) {
				t.Fatalf("alpha 平面为 nil = %v, 期望 %v", gotAlpha == nil, wantAlpha == nil)
			}
			if !bytes.Equal(gotAlpha, wantAlpha) {
				t.Fatalf("alpha 流不一致:\n got %v\nwant %v", gotAlpha, wantAlpha)
			}
		})
	}
}
