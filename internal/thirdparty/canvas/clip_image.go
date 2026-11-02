package canvas

import (
	"image"
	"image/color"

	"github.com/srwiley/scanx"
)

type imageCoverageSpanner struct {
	mask *image.Alpha
}

func (s imageCoverageSpanner) SetColor(any) {}

func (s imageCoverageSpanner) GetSpanFunc() scanx.SpanFunc {
	return func(y, x0, x1 int, coverage uint32) {
		source := uint8(uint64(coverage) * 255 / 65535)
		if source == 0 {
			return
		}
		offset := y*s.mask.Stride + x0
		for x := x0; x < x1; x++ {
			destination := uint32(s.mask.Pix[offset])
			s.mask.Pix[offset] = uint8(uint32(source) + destination*(255-uint32(source))/255)
			offset++
		}
	}
}

func clipImage(img image.Image, clip *Path) image.Image {
	if img == nil || clip == nil || clip.Empty() {
		return image.NewNRGBA(image.Rectangle{})
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return img
	}
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	spanner := imageCoverageSpanner{mask: mask}
	scanner := scanx.NewScanner(spanner, w, h)
	clip.ToScanxScanner(scanner, float64(h), DPMM(1))
	scanner.Draw()

	out := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		source := img.Bounds().Min.X
		destination := out.PixOffset(bounds.Min.X, y)
		maskOffset := (y - bounds.Min.Y) * mask.Stride
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(source, y)).(color.NRGBA)
			c.A = uint8(uint16(c.A) * uint16(mask.Pix[maskOffset+x-bounds.Min.X]) / 255)
			out.Pix[destination+0] = c.R
			out.Pix[destination+1] = c.G
			out.Pix[destination+2] = c.B
			out.Pix[destination+3] = c.A
			source++
			destination += 4
		}
	}
	return out
}
