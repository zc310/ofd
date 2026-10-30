package converter

import (
	"bytes"
	"image"
	"image/draw"
	"io"
	"testing"

	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
)

func TestImageWriterRetainedPagesStayIndependent(t *testing.T) {
	input := conversionOutputFixture(t, 2)
	for _, thumbnail := range []int{0, 8} {
		for _, withFile := range []bool{false, true} {
			var images []image.Image
			var snapshots []*image.RGBA
			opts := []Option{PNG(), DPI(25.4), Thumbnail(thumbnail), ImageWriter(func(_ int, img image.Image) error {
				images = append(images, img)
				copyImage := image.NewRGBA(img.Bounds())
				draw.Draw(copyImage, copyImage.Bounds(), img, img.Bounds().Min, draw.Src)
				snapshots = append(snapshots, copyImage)
				return nil
			})}
			if withFile {
				opts = append(opts, Writer(func(int) (io.WriteCloser, error) {
					return testWriteCloser{Writer: io.Discard}, nil
				}))
			}
			if err := Image(t.Context(), input, opts...); err != nil {
				t.Fatal(err)
			}
			if len(images) != 2 || images[0] == images[1] {
				t.Fatalf("thumbnail=%d file=%v：回调未得到两张独立图像", thumbnail, withFile)
			}
			for i, img := range images {
				got := image.NewRGBA(img.Bounds())
				draw.Draw(got, got.Bounds(), img, img.Bounds().Min, draw.Src)
				if !bytes.Equal(got.Pix, snapshots[i].Pix) {
					t.Fatalf("thumbnail=%d file=%v：第 %d 页图像被后续渲染改写", thumbnail, withFile, i+1)
				}
			}
		}
	}
}

type countedRasterSurface struct {
	render.VectorSurface

	calls int
}

func (s *countedRasterSurface) Rasterize(resolution geom.Resolution, dst *image.RGBA) *image.RGBA {
	s.calls++
	return s.VectorSurface.Rasterize(resolution, dst)
}

func TestPageRasterizedOnceForFileAndImageWriter(t *testing.T) {
	surface, err := render.NewVectorSurface(10, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"png", "jpeg"} {
		for _, thumbnail := range []int{0, 8} {
			page := &countedRasterSurface{VectorSurface: surface}
			conv := newConverter(t.Context(), WithFormat(format), DPI(25.4), Thumbnail(thumbnail),
				Writer(func(int) (io.WriteCloser, error) { return testWriteCloser{Writer: io.Discard}, nil }),
				ImageWriter(func(int, image.Image) error { return nil }))
			if err := conv.renderPage(1, page, nil); err != nil {
				t.Fatal(err)
			}
			if page.calls != 1 {
				t.Fatalf("format=%s thumbnail=%d：同页栅格化 %d 次", format, thumbnail, page.calls)
			}
		}
	}
}

// 比较一次串行转换 52 张 A4 页面时，独立分配与复用页面缓冲的开销。
func BenchmarkRenderPNGPageBuffer(b *testing.B) {
	surface, err := render.NewVectorSurface(210, 297, nil)
	if err != nil {
		b.Fatal(err)
	}
	for _, name := range []string{"fresh", "reuse"} {
		b.Run(name, func(b *testing.B) {
			conv := newConverter(b.Context(), PNG(), DPI(150), Writer(func(int) (io.WriteCloser, error) {
				return testWriteCloser{Writer: io.Discard}, nil
			}))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				var buffer *image.RGBA
				if name == "reuse" {
					buffer = &image.RGBA{}
				}
				for page := 1; page <= 52; page++ {
					if err := conv.renderPage(page, surface, buffer); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
