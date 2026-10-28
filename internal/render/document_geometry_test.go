package render

import (
	"image"
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

func TestClippedDashKeepsInheritedPatternAndPhase(t *testing.T) {
	doc := &Document{}
	pattern := models.StArrayF{2, 2}
	dp := &models.DrawParam{LineWidth: 1, DashPattern: &pattern, DashOffset: 1}
	object := clipTestPath(t, `<PathObject Boundary="1 1 30 4"><AbbreviatedData>M 0 2 L 28 2</AbbreviatedData></PathObject>`)
	// 部分裁剪才能触发描边区域求交，完整覆盖的矩形应由快路径跳过。
	object.Clips = rectangleClip(15, 4)
	surface := clipTestSurface(t, func(ctx DrawContext) {
		doc.Path(ctx, object, dp, models.StBox{Width: 40, Height: 40})
	})
	check := func(t *testing.T, img image.Image) {
		t.Helper()
		for _, point := range []struct {
			x   int
			ink bool
		}{{15, true}, {30, false}, {55, true}, {70, false}, {180, false}} {
			c := color.NRGBAModel.Convert(img.At(point.x, 30)).(color.NRGBA)
			ink := c.A > 100 && c.R < 100
			if ink != point.ink {
				t.Errorf("x=%d像素=%v，期望墨迹=%v", point.x, c, point.ink)
			}
		}
	}
	check(t, surface.Rasterize(geom.DPMM(10)))
	t.Run("PDF", func(t *testing.T) { check(t, clipPDFImage(t, surface, 254)) })
}

func TestTinyPositiveStrokeKeepsDashGapsInRasterAndPDF(t *testing.T) {
	doc := &Document{}
	object := clipTestPath(t, `<PathObject Boundary="1 1 30 4" LineWidth="1e-310" Join="Round" DashPattern="6 3" DashOffset="2"><AbbreviatedData>M 0 2 L 28 2</AbbreviatedData></PathObject>`)
	surface := clipTestSurface(t, func(ctx DrawContext) {
		doc.Path(ctx, object, nil, models.StBox{Width: 40, Height: 40})
	})
	check := func(t *testing.T, img image.Image) {
		t.Helper()
		for _, sample := range []struct {
			x   int
			ink bool
		}{{30, true}, {60, false}, {100, true}, {150, false}, {190, true}, {240, false}} {
			ink := false
			for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
				pixel := color.NRGBAModel.Convert(img.At(sample.x, y)).(color.NRGBA)
				ink = ink || pixel.A > 0 && pixel.R < 150
			}
			if ink != sample.ink {
				t.Errorf("极细虚线 x=%d 墨迹=%v，期望%v", sample.x, ink, sample.ink)
			}
		}
	}
	t.Run("Raster", func(t *testing.T) { check(t, surface.Rasterize(geom.DPMM(10))) })
	t.Run("PDF", func(t *testing.T) { check(t, clipPDFImage(t, surface, 254)) })
}

func TestRasterGradientStrokeKeepsDashGaps(t *testing.T) {
	doc := &Document{}
	object := clipTestPath(t, `<PathObject Boundary="1 1 30 4" LineWidth="1" DashPattern="2 2"><AbbreviatedData>M 0 2 L 28 2</AbbreviatedData></PathObject>`)
	alpha := uint8(128)
	object.Alpha = &alpha
	object.StrokeColor = &models.CTColor{AxialShd: &models.CTAxialShd{
		StartPoint: models.StPos{}, EndPoint: models.StPos{X: 30}, Extend: 3,
		Segment: []models.Segment{
			{Position: 0, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}},
			{Position: 1, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}},
		},
	}}
	surface := clipTestSurface(t, func(ctx DrawContext) { doc.Path(ctx, object, nil, models.StBox{Width: 40, Height: 40}) })
	img := surface.Rasterize(geom.DPMM(10))
	if img.RGBAAt(20, 30).A < 120 || img.RGBAAt(40, 30).A != 0 {
		t.Fatalf("渐变描边虚实错误：实线Alpha=%d，间隙Alpha=%d", img.RGBAAt(20, 30).A, img.RGBAAt(40, 30).A)
	}
}
