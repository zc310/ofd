package render

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

func TestArcCTMTransformsWholeEllipse(t *testing.T) {
	doc := &Document{}
	object := clipTestPath(t, `<PathObject Boundary="0 0 100 100"><AbbreviatedData>M 10 0 A 10 10 0 0 1 0 10</AbbreviatedData></PathObject>`)
	for _, tc := range []struct {
		name string
		ctm  models.CTM
	}{
		{"scale", models.CTM{2, 0, 0, 2, 0, 0}},
		{"unequal scale", models.CTM{3, 0, 0, 0.5, 2, 4}},
		{"rotation", models.CTM{0, 2, -2, 0, 30, 0}},
		{"shear", models.CTM{1, 0.2, 0.7, 1, 4, 5}},
		{"reflection", models.CTM{-2, 0, 0, 1, 30, 5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			object.CTM = &tc.ctm
			path := doc.buildObjectPathWithTransform(object, 100, nil)
			// 取原圆上的解析点，变换后必须满足输出椭圆的方程。
			// 直接核对弧参数，避免把Flatten自身的近似误差混入断言。
			scanner := path.Scanner()
			found := false
			for scanner.Scan() {
				if scanner.Cmd() != geom.ArcToCmd {
					continue
				}
				found = true
				rx, ry, degrees, large, sweep := scanner.Arc()
				angle := degrees * math.Pi / 180
				for i := 0; i <= 8; i++ {
					theta := float64(i) * math.Pi / 16
					x, y := tc.ctm.Transform(10*math.Cos(theta), 10*math.Sin(theta))
					x, y = x-tc.ctm[4], -(y - tc.ctm[5])
					u, v := (x*math.Cos(angle)+y*math.Sin(angle))/rx, (-x*math.Sin(angle)+y*math.Cos(angle))/ry
					if math.Abs(u*u+v*v-1) > 1e-8 {
						t.Fatalf("变换后的点不在输出椭圆上：u²+v²=%g", u*u+v*v)
					}
				}
				wantSweep := tc.ctm[0]*tc.ctm[3]-tc.ctm[1]*tc.ctm[2] < 0
				if large || sweep != wantSweep {
					t.Fatal("变换丢失扫掠方向")
				}
			}
			if !found {
				t.Fatal("椭圆弧丢失")
			}
			if tc.name == "scale" && !path.Bounds().Equals(geom.Rect{X0: 0, Y0: 80, X1: 20, Y1: 100}) {
				t.Fatalf("放大圆弧边界=%v，期望(0,80)-(20,100)", path.Bounds())
			}
		})
	}
}

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
