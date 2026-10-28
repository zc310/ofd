package creator

import (
	"bytes"
	"image"
	"image/png"
	"math"
	"testing"

	"github.com/zc310/ofd/internal/models"
)

func TestImageBorderLineWidthRoundTrips(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		width *float64
	}{
		{"缺省", nil}, {"关闭", new(0.0)}, {"正线宽", new(0.5)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := Marshal(Document{ID: "image-border-width", Pages: []Page{{Items: []Item{
				Image{Width: 10, Height: 10, Data: encoded.Bytes(), Format: "PNG", Border: &ImageBorder{LineWidth: tc.width}},
			}}}})
			if err != nil {
				t.Fatal(err)
			}
			ofd := newTestOFD(t, data)
			defer ofd.Close()
			object := firstItemOfKind(ofd.Documents[0].Pages[0].Content().Layer[0].Items, models.PageItemImage).Image
			if object == nil || object.Border == nil {
				t.Fatal("图片边框丢失")
			}
			width := object.Border.LineWidth
			if (width == nil) != (tc.width == nil) || (width != nil && *width != *tc.width) {
				t.Fatal("创建器没有保留缺省和显式零线宽")
			}
			checkGeneratedPackage(t, data)
		})
	}
}

func TestImageBorderRejectsInvalidLineWidths(t *testing.T) {
	for _, width := range []float64{-1, math.NaN(), math.Inf(1)} {
		if err := validateBorder(&ImageBorder{LineWidth: &width}); err == nil {
			t.Fatalf("接受了非法边框线宽: %g", width)
		}
	}
}
