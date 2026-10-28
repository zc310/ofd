package render

import (
	"image"
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
)

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
