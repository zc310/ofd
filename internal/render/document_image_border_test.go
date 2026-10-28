package render

import (
	"encoding/xml"
	"image"
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/creator"
)

// 使用透明图片隔离边框像素，分别核对直接光栅与 PDF 输出。
func TestImageBorderFollowsParentTransformAndClip(t *testing.T) {
	doc := imageBorderTestDocument(t)
	for _, tc := range []struct {
		name   string
		parent models.CTM
		clip   *geom.Path
		x, y   float64
		svg    bool
	}{
		{"平移", models.CTM{1, 0, 0, 1, 20, 5}, nil, 35, 15, false},
		{"缩放", models.CTM{2, 0, 0, 2, 20, 5}, nil, 40, 31, false},
		{"旋转", models.CTM{0, 1, -1, 0, 20, 0}, nil, 26, 10, false},
		{"错切", models.CTM{1, 0.5, 0.5, 1, 20, 5}, nil, 35, 17.5, false},
		{"继承裁剪", models.CTM{1, 0, 0, 1, 20, 5}, geom.Rectangle(4, 20).Translate(31, 20), 33, 23, false},
		{"SVG矢量平移", models.CTM{1, 0, 0, 1, 20, 5}, nil, 35, 15, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			object := imageBorderTestObject(t, `LineWidth="1"`)
			if tc.svg {
				object.ResourceID = 31
			}
			surface, err := NewVectorSurface(60, 60, func(ctx DrawContext) {
				doc.image(ctx, object, nil, models.StBox{Width: 60, Height: 60}, &tc.parent, tc.clip)
			})
			if err != nil {
				t.Fatal(err)
			}
			check := func(t *testing.T, img image.Image) {
				t.Helper()
				assertBorderPixel(t, img, tc.x, tc.y, true)
				assertBorderPixel(t, img, 15, 10, false)
				if tc.clip != nil {
					assertBorderPixel(t, img, 38, 23, false)
					assertBorderPixel(t, img, 33, 15, false)
				}
			}
			check(t, surface.Rasterize(geom.DPMM(10)))
			t.Run("PDF", func(t *testing.T) { check(t, clipPDFImage(t, surface, 254)) })
		})
	}
}

func TestImageBorderHonorsObjectClip(t *testing.T) {
	doc := imageBorderTestDocument(t)
	for _, resource := range []models.StRefID{30, 31} {
		object := imageBorderTestObject(t, `LineWidth="1"`)
		object.ResourceID, object.Clips = resource, rectangleClip(5, 8)
		surface, err := NewVectorSurface(60, 60, func(ctx DrawContext) {
			doc.Image(ctx, object, nil, models.StBox{Width: 60, Height: 60})
		})
		if err != nil {
			t.Fatal(err)
		}
		check := func(t *testing.T, img image.Image) {
			t.Helper()
			assertBorderPixel(t, img, 12.5, 10.2, true)
			assertBorderPixel(t, img, 17.5, 10, false)
			assertBorderPixel(t, img, 20, 14, false)
		}
		check(t, surface.Rasterize(geom.DPMM(10)))
		t.Run(map[models.StRefID]string{30: "PNG", 31: "SVG"}[resource], func(t *testing.T) { check(t, clipPDFImage(t, surface, 254)) })
	}
}

func TestImageBorderDoesNotInheritPreviousDashes(t *testing.T) {
	doc := imageBorderTestDocument(t)
	object := imageBorderTestObject(t, `LineWidth="1"`)
	surface, err := NewVectorSurface(60, 60, func(ctx DrawContext) {
		ctx.SetDashes(0, 1, 100)
		doc.Image(ctx, object, nil, models.StBox{Width: 60, Height: 60})
	})
	if err != nil {
		t.Fatal(err)
	}
	assertBorderPixel(t, surface.Rasterize(geom.DPMM(10)), 15, 10, true)
	t.Run("PDF", func(t *testing.T) { assertBorderPixel(t, clipPDFImage(t, surface, 254), 15, 10, true) })
}

func TestImageBorderUsesDefaultAndExplicitLineWidth(t *testing.T) {
	doc := imageBorderTestDocument(t)
	for _, tc := range []struct {
		name, attrs string
		visible     bool
	}{
		{"缺省线宽", "", true},
		{"正线宽", `LineWidth="1"`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			object := imageBorderTestObject(t, tc.attrs)
			surface, err := NewVectorSurface(60, 60, func(ctx DrawContext) {
				doc.Image(ctx, object, nil, models.StBox{Width: 60, Height: 60})
			})
			if err != nil {
				t.Fatal(err)
			}
			assertBorderPixel(t, surface.Rasterize(geom.DPMM(10)), 15, 10, tc.visible)
			t.Run("PDF", func(t *testing.T) { assertBorderPixel(t, clipPDFImage(t, surface, 254), 15, 10, tc.visible) })
		})
	}
}

func imageBorderTestDocument(t *testing.T) *Document {
	t.Helper()
	data, err := creator.Marshal(creator.Document{
		ID: "image-border", Pages: []creator.Page{{}},
		Media: []creator.Media{
			{ID: 30, Type: "Image", Format: "PNG", Data: solidPNG(t, 10, 8, color.NRGBA{})},
			{ID: 31, Type: "Image", Format: "SVG", Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="8"><rect width="10" height="8" fill="none"/></svg>`)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ofd.Close() })
	doc := NewDocument(nil, ofd.Documents[0])
	t.Cleanup(doc.Close)
	return doc
}

func imageBorderTestObject(t *testing.T, attrs string) models.ImageObject {
	t.Helper()
	var object models.ImageObject
	if err := xml.Unmarshal([]byte(`<ImageObject Boundary="10 10 10 8" ResourceID="30"><Border `+attrs+`/></ImageObject>`), &object); err != nil {
		t.Fatal(err)
	}
	return object
}

func assertBorderPixel(t *testing.T, img image.Image, x, y float64, want bool) {
	t.Helper()
	r, g, b, a := img.At(int(x*10), int(y*10)).RGBA()
	got := a > 32767 && r < 32767 && g < 32767 && b < 32767
	if got != want {
		t.Fatalf("(%g,%g)mm 边框=%v，期望 %v，RGBA=%v", x, y, got, want, []uint32{r, g, b, a})
	}
}
