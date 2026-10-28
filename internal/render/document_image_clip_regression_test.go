package render

import (
	"image"
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
	imagelayout "github.com/zc310/ofd/internal/render/layout/image"
	"github.com/zc310/ofd/pkg/creator"
)

// 面积几乎等于外框的裁剪仍可能含洞或圆角，不能按出血矩形跳过。
func TestImageClipKeepsSmallHolesAndRoundedCorners(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 200, 200))
	for i := 3; i < len(src.Pix); i += 4 {
		src.Pix[i] = 255
	}
	hole := geom.Rectangle(200, 200).Not(geom.Rectangle(8, 8).Translate(96, 96))
	for _, tc := range []struct {
		name string
		clip *geom.Path
		x, y int
	}{
		{"小洞", hole, 100, 100},
		{"圆角", imagelayout.Border(200, 200, 8, 8), 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := imageWithClip(src, tc.clip, geom.Identity)
			if _, _, _, alpha := got.At(tc.x, tc.y).RGBA(); alpha != 0 {
				t.Fatalf("裁剪外像素 (%d,%d) alpha=%d，期望透明", tc.x, tc.y, alpha)
			}
			if _, _, _, alpha := got.At(20, 20).RGBA(); alpha != 65535 {
				t.Fatal("裁剪内像素丢失")
			}
		})
	}
}

// 只转发基础绘制接口，使图片经过掩码回退路径。
type imageClipFallbackContext struct{ DrawContext }

func TestImageSmallClipFeaturesMatchRasterAndPDF(t *testing.T) {
	data, err := creator.Marshal(creator.Document{
		ID: "small-image-clip", Pages: []creator.Page{{}},
		Media: []creator.Media{{ID: 30, Type: "Image", Format: "PNG", Data: solidPNG(t, 200, 200, color.NRGBA{B: 255, A: 255})}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := NewDocument(nil, ofd.Documents[0])
	defer doc.Close()
	box := models.StBox{Width: 40, Height: 40}
	object := models.ImageObject{CtImage: models.CtImage{
		CTGraphicUnit: models.CTGraphicUnit{Boundary: box}, ResourceID: 30,
	}}
	for _, tc := range []struct {
		name string
		clip *geom.Path
		x, y int
	}{
		{"小洞", geom.Rectangle(40, 40).Not(geom.Rectangle(1.6, 1.6).Translate(19.2, 19.2)), 100, 100},
		{"圆角", imagelayout.Border(40, 40, 1.6, 1.6), 0, 0},
	} {
		for _, native := range []bool{true, false} {
			name := tc.name + map[bool]string{true: "/原生裁剪", false: "/掩码回退"}[native]
			t.Run(name, func(t *testing.T) {
				surface, err := NewVectorSurface(40, 40, func(ctx DrawContext) {
					if !native {
						ctx = imageClipFallbackContext{DrawContext: ctx}
					}
					doc.image(ctx, object, nil, box, nil, tc.clip)
				})
				if err != nil {
					t.Fatal(err)
				}
				check := func(t *testing.T, img image.Image) {
					t.Helper()
					outside := img.At(tc.x, tc.y)
					r, g, b, a := outside.RGBA()
					if a != 0 && (r < 245*257 || g < 245*257 || b < 245*257) {
						t.Fatalf("裁剪外像素仍有图片: %v", outside)
					}
					inside := img.At(20, 20)
					r, _, b, a = inside.RGBA()
					if a < 250*257 || r > 10*257 || b < 245*257 {
						t.Fatalf("裁剪内蓝色像素丢失: %v", inside)
					}
				}
				check(t, surface.Rasterize(geom.DPMM(5)))
				t.Run("PDF", func(t *testing.T) { check(t, clipPDFImage(t, surface, 127)) })
			})
		}
	}
}
