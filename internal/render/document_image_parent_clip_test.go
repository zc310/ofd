package render

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/creator"
)

type clippedImageCapture struct {
	DrawContext
	image image.Image
}

func (c *clippedImageCapture) RenderImage(img image.Image, _ geom.Matrix) { c.image = img }

func TestImageClipKeepsParentTransformWithoutExplicitCTM(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := 3; i < len(src.Pix); i += 4 {
		src.Pix[i] = 255
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, src); err != nil {
		t.Fatal(err)
	}
	data, err := creator.Marshal(creator.Document{
		ID: "parent-image-clip", Pages: []creator.Page{{}},
		Media: []creator.Media{{ID: 30, Type: "Image", Format: "PNG", Data: encoded.Bytes()}},
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
	for _, tc := range []struct {
		name   string
		parent models.CTM
	}{
		{"translation", models.CTM{1, 0, 0, 1, 20, 5}},
		{"scale and shear", models.CTM{2, 0.3, 0.4, 1.5, 20, 5}},
		{"rotation", models.CTM{0, 1, -1, 0, 20, 5}},
	} {
		for _, explicit := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/implicit", true: "/explicit"}[explicit], func(t *testing.T) {
				object := models.ImageObject{CtImage: models.CtImage{
					CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{Width: 10, Height: 8}},
					ResourceID:    30,
				}}
				clipW, clipH := 5.0, 8.0
				if explicit {
					object.CTM = &models.CTM{10, 0, 0, 8, 0, 0}
					clipW, clipH = 0.5, 1
				}
				object.Clips = &models.Clips{Clip: []models.CtClip{{Area: []models.ClipArea{{Path: &models.CtPath{
					AbbreviatedData: models.SVGPath{
						{Type: models.MoveTo, Points: []models.StPos{{}}},
						{Type: models.LineTo, Points: []models.StPos{{X: clipW}}},
						{Type: models.LineTo, Points: []models.StPos{{X: clipW, Y: clipH}}},
						{Type: models.LineTo, Points: []models.StPos{{Y: clipH}}},
					},
				}}}}}}
				var capture *clippedImageCapture
				_, err := NewVectorSurface(60, 60, func(ctx DrawContext) {
					capture = &clippedImageCapture{DrawContext: ctx}
					doc.image(capture, object, nil, models.StBox{Width: 60, Height: 60}, &tc.parent, nil)
				})
				if err != nil {
					t.Fatal(err)
				}
				if capture.image == nil {
					t.Fatal("图片未绘制")
				}
				for x, want := range map[int]uint32{1: 65535, 6: 0} {
					if _, _, _, alpha := capture.image.At(x, 4).RGBA(); alpha != want {
						t.Fatalf("源像素 x=%d alpha=%d，期望 %d；父级变换下应仅保留左半图", x, alpha, want)
					}
				}
			})
		}
	}
}
