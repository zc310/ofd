package render

import (
	"bytes"
	"image"
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
)

func TestImageMaskAlphaMatchesSeparatePasses(t *testing.T) {
	for name, img := range alphaTestImages(t) {
		t.Run(name, func(t *testing.T) {
			bounds := img.Bounds()
			mask := image.NewRGBA(image.Rect(7, 9, 7+bounds.Dx(), 9+bounds.Dy()))
			for i := 3; i < len(mask.Pix); i += 4 {
				mask.Pix[i] = uint8(i * 37)
			}
			before := referenceImageAlpha(img, 255)
			for _, alpha := range []uint8{0, 1, 51, 128, 254, 255} {
				want := referenceImageAlpha(applyImageMaskReference(img, mask), alpha)
				got := applyImageMaskAlpha(img, mask, alpha).(*image.NRGBA)
				if got.Rect != want.Rect || !bytes.Equal(got.Pix, want.Pix) {
					t.Fatalf("合并掩码与Alpha=%d后与独立计算不一致", alpha)
				}
			}
			if !bytes.Equal(before.Pix, referenceImageAlpha(img, 255).Pix) {
				t.Fatal("修改了共享源图")
			}
		})
	}
}

var composedImageBenchmark image.Image

func BenchmarkImageClipsAndAlpha(b *testing.B) {
	img := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	objectClip, parentClip := geom.Rectangle(800, 1024), geom.Rectangle(1024, 800)
	for _, combined := range []bool{false, true} {
		b.Run(map[bool]string{false: "separate", true: "combined"}[combined], func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if combined {
					composedImageBenchmark = imageWithClipAlpha(img, intersectClips(objectClip, parentClip), geom.Identity, 128)
				} else {
					out := imageWithClip(img, objectClip, geom.Identity)
					out = imageWithClip(out, parentClip, geom.Identity)
					composedImageBenchmark = applyImageAlpha(out, 128)
				}
			}
		})
	}
}
