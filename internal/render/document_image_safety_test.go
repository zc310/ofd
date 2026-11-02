package render

import (
	"image"
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/renderstate"
	"github.com/zc310/ofd/pkg/creator"
)

type imageAllocationCapture struct {
	DrawContext

	images []image.Image
}

func (c *imageAllocationCapture) RenderImage(img image.Image, matrix geom.Matrix) {
	c.images = append(c.images, img)
	c.DrawContext.RenderImage(img, matrix)
}

func TestCachedImageEffectsCannotBypassOffscreenBudget(t *testing.T) {
	for _, effect := range []string{"对象透明度", "资源遮罩", "软件裁剪"} {
		t.Run(effect, func(t *testing.T) {
			data, err := creator.Marshal(creator.Document{ID: "image-budget", Media: []creator.Media{
				{ID: 1, Type: "Image", Format: "PNG", Data: solidPNG(t, 8, 6, color.NRGBA{R: 255, A: 255})},
			}, Pages: []creator.Page{{}}})
			if err != nil {
				t.Fatal(err)
			}
			ofd, err := parser.NewOFD(data)
			if err != nil {
				t.Fatal(err)
			}
			defer ofd.Close()
			doc := NewDocument(nil, ofd.Documents[0], 0)
			defer doc.Close()
			media := doc.GetMedia(1)
			if _, err := doc.decodeImage(media.MediaFile, media.Format); err != nil {
				t.Fatal(err)
			}
			object := models.ImageObject{CtImage: models.CtImage{
				CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{Width: 20, Height: 20}}, ResourceID: 1,
			}}
			switch effect {
			case "对象透明度":
				object.Alpha = new(uint8(128))
			case "资源遮罩":
				object.ImageMask = 1
			case "软件裁剪":
				object.Clips = rectangleClip(10, 20)
			}
			var budget renderBudget
			budget.reset()
			budget.Report = &renderstate.ReportBuilder{}
			if !budget.AllowOffscreenPixels(renderstate.MaxOffscreenPixels, 1, 25.4) {
				t.Fatal("预消费预算失败")
			}
			setAlphaMaskSurfaceFactory(t, func(float64, float64, geom.Resolution) AlphaMaskSurface {
				t.Fatal("预算拒绝后仍然创建裁剪掩码")
				return nil
			})
			clipTestSurface(t, func(ctx DrawContext) {
				capture := &imageAllocationCapture{DrawContext: ctx}
				doc.imageWithBudget(capture, object, nil, models.StBox{Width: 40, Height: 40}, nil, nil, &budget)
				if len(capture.images) != 0 {
					t.Fatal("预算耗尽后仍然绘制派生图片")
				}
			})
			if len(budget.Report.Diagnostics) == 0 {
				t.Fatal("图片预算拒绝没有诊断")
			}
		})
	}
}

func TestSVGImageRasterFallbackUsesPlacedSizeAndTargetDPI(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "svg-target", Media: []creator.Media{{
		ID: 1, Type: "Image", Format: "SVG",
		Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10mm" height="10mm"><rect width="100%" height="100%" fill="red"/></svg>`),
	}}, Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := NewDocument(nil, ofd.Documents[0], geom.DPI(254))
	defer doc.Close()
	clipTestSurface(t, func(ctx DrawContext) {
		capture := &imageAllocationCapture{DrawContext: ctx}
		doc.Image(capture, models.ImageObject{CtImage: models.CtImage{
			CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{Width: 20, Height: 20}, Alpha: new(uint8(128))}, ResourceID: 1,
		}}, nil, models.StBox{Width: 40, Height: 40})
		if len(capture.images) != 1 || capture.images[0].Bounds().Dx() != 200 || capture.images[0].Bounds().Dy() != 200 {
			t.Fatalf("放大后的 SVG 未按目标 254 DPI 输出 200x200 像素：%v", capture.images)
		}
	})
}

func TestImageMaskFailureNeverDrawsUnmaskedImage(t *testing.T) {
	valid := solidPNG(t, 4, 4, color.NRGBA{R: 255, A: 255})
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"缺失资源", nil},
		{"未知格式", []byte("broken")},
		{"空图片头", valid[:8]},
		{"头有效但像素损坏", valid[:33]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			media := []creator.Media{{ID: 1, Type: "Image", Format: "PNG", Data: valid}}
			if tc.data != nil {
				media = append(media, creator.Media{ID: 2, Type: "Image", Format: "PNG", Data: tc.data})
			}
			data, err := creator.Marshal(creator.Document{ID: "mask-failure", Media: media, Pages: []creator.Page{{}}})
			if err != nil {
				t.Fatal(err)
			}
			ofd, err := parser.NewOFD(data)
			if err != nil {
				t.Fatal(err)
			}
			defer ofd.Close()
			doc := NewDocument(nil, ofd.Documents[0], 0)
			defer doc.Close()
			var budget renderBudget
			budget.reset()
			budget.Report = &renderstate.ReportBuilder{}
			surface := clipTestSurface(t, func(ctx DrawContext) {
				doc.imageWithBudget(ctx, models.ImageObject{CtImage: models.CtImage{
					CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{Width: 20, Height: 20}},
					ResourceID:    1, ImageMask: 2,
				}}, nil, models.StBox{Width: 40, Height: 40}, nil, nil, &budget)
			})
			if got := budget.Report.Diagnostics; len(got) != 1 || got[0].Code != "image_mask_failed" || got[0].ResourceID != 2 {
				t.Fatalf("遮罩失败诊断=%+v", got)
			}
			for _, pdf := range []bool{false, true} {
				var img image.Image = surface.Rasterize(geom.DPI(254), nil)
				if pdf {
					img = clipPDFImage(t, surface, 254)
				}
				r, _, _, a := img.At(100, 100).RGBA()
				if (!pdf && a != 0) || (pdf && r != 65535) {
					t.Fatalf("遮罩失败仍绘制了原图：pdf=%v r=%d a=%d", pdf, r, a)
				}
			}
		})
	}
}
