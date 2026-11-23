package imageops

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func TestMaskAlphaRegionKeepsFullCanvasAndSourceStride(t *testing.T) {
	parent := image.NewRGBA(image.Rect(-10, -5, 80, 70))
	for y := parent.Rect.Min.Y; y < parent.Rect.Max.Y; y++ {
		for x := parent.Rect.Min.X; x < parent.Rect.Max.X; x++ {
			a := [...]byte{0, 1, 127, 255}[(x+10+y+5)%4]
			parent.SetRGBA(x, y, color.RGBA{R: a / 2, G: a / 3, B: a / 4, A: a})
		}
	}
	src, _ := parent.SubImage(image.Rect(10, 15, 60, 55)).(*image.RGBA)
	before := bytes.Clone(src.Pix)
	for _, region := range []image.Rectangle{
		image.Rect(20, 25, 30, 45), image.Rect(5, 5, 30, 30), image.Rect(55, 45, 70, 70),
		image.Rect(100, 100, 110, 110),
	} {
		mask := image.NewAlpha(image.Rect(-3, -4, region.Dx()-3, region.Dy()-4))
		for i := range mask.Pix {
			mask.Pix[i] = byte(i)
		}
		savedMask := bytes.Clone(mask.Pix)
		got := ApplyMaskAlphaRegion(src, mask, region, 102)
		if got.Bounds() != src.Bounds() {
			t.Fatal("区域合成改变了源图尺寸或原点")
		}
		for y := src.Rect.Min.Y; y < src.Rect.Max.Y; y++ {
			for x := src.Rect.Min.X; x < src.Rect.Max.X; x++ {
				want := color.NRGBA{}
				if image.Pt(x, y).In(region) {
					want, _ = color.NRGBAModel.Convert(src.At(x, y)).(color.NRGBA)
					ma := mask.AlphaAt(x-region.Min.X+mask.Rect.Min.X, y-region.Min.Y+mask.Rect.Min.Y).A
					want.A = alphaMul(alphaMul(want.A, ma), 102)
				}
				if c := got.NRGBAAt(x, y); c != want {
					t.Fatalf("区域、步长、去预乘或 Alpha 舍入改变：(%d,%d) %v != %v", x, y, c, want)
				}
			}
		}
		if !bytes.Equal(src.Pix, before) || !bytes.Equal(mask.Pix, savedMask) {
			t.Fatal("区域合成修改了共享源图或掩码")
		}
	}
}

func TestMaskAlphaRegionParallelRowsKeepDestinationOffsets(t *testing.T) {
	parent := image.NewRGBA(image.Rect(-7, 9, 1100, 900))
	for i := 0; i < len(parent.Pix); i += 4 {
		parent.Pix[i], parent.Pix[i+3] = 64, 128
	}
	source, _ := parent.SubImage(image.Rect(13, 29, 1013, 829)).(*image.RGBA)
	region := image.Rect(101, 103, 701, 703)
	mask := image.NewAlpha(image.Rectangle{Max: region.Size()})
	for i := range mask.Pix {
		mask.Pix[i] = byte(i * 17)
	}
	fullMask := image.NewAlpha(image.Rectangle{Max: source.Rect.Size()})
	for y := range region.Dy() {
		offset := fullMask.PixOffset(region.Min.X-source.Rect.Min.X, region.Min.Y-source.Rect.Min.Y+y)
		copy(fullMask.Pix[offset:offset+region.Dx()], mask.Pix[y*mask.Stride:y*mask.Stride+region.Dx()])
	}
	want, _ := ApplyMaskAlpha(source, fullMask, 102).(*image.NRGBA)
	got := ApplyMaskAlphaRegion(source, mask, region, 102)
	for y := source.Rect.Min.Y; y < source.Rect.Max.Y; y++ {
		for x := source.Rect.Min.X; x < source.Rect.Max.X; x++ {
			if image.Pt(x, y).In(region) {
				if got.NRGBAAt(x, y) != want.NRGBAAt(x, y) {
					t.Fatalf("并行行分块写错源图或输出偏移：(%d,%d)", x, y)
				}
			} else if got.NRGBAAt(x, y) != (color.NRGBA{}) {
				t.Fatal("窗口外的输出没有保持透明")
			}
		}
	}
}
