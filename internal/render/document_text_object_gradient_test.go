package render

import (
	"image"
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/paint"
)

func objectCTMGradient() *models.CTColor {
	return &models.CTColor{AxialShd: &models.CTAxialShd{
		EndPoint: models.StPos{X: 40}, Extend: 3,
		Segment: []models.Segment{
			{Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}},
			{Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}},
		},
	}}
}

// 原生文字按字形坐标取样，描边/栅格文字按页面坐标取样；两者在同一物理点必须同色。
func TestTextNativeAndPageGradientsAgreeWithObjectCTM(t *testing.T) {
	source := objectCTMGradient()
	doc := &Document{}
	boundaryGradient := doc.textShadingGradient(source, nil)
	for _, ctm := range []models.CTM{
		{2, 0, 0, 0.8, 1, 2}, {0, 2, -2, 0, 30, 2}, {1, 0.25, 0.5, 1, 1, 2},
	} {
		for _, parent := range []*models.CTM{nil, {1.2, 0.2, 0.3, 0.8, 1, 2}} {
			object := models.TextObject{CtText: models.CtText{
				CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{X: 2, Y: 3, Width: 35, Height: 32}, CTM: &ctm},
				HScale:        0.8,
			}}
			const runX, runY, height = 4.0, 12.0, 40.0
			native := paint.MapNativeGradient(boundaryGradient, textRunBoundaryMatrix(object, runX, runY))
			page := doc.textPageGradient(source, object.Boundary, height, parent)
			if native == nil || page == nil {
				t.Fatal("渐变映射失败")
			}
			matrix := textPageTransform(object, height, parent)(runX, runY)
			for _, point := range []geom.Point{{}, {X: 2, Y: 5}, {X: 8, Y: 3}} {
				position := matrix.Dot(point)
				fromNative := native.At(point.X, point.Y)
				fromPage := page.At(position.X, position.Y)
				if colorDistance(fromNative, fromPage) > 1 {
					t.Fatalf("对象 CTM 导致两条渐变路径取色不一致: ctm=%v parent=%v point=%v native=%v page=%v", ctm, parent, point, fromNative, fromPage)
				}
			}
		}
	}
}

// 对象 CTM 移动字形后，填充与描边仍按其实际 Boundary 位置取色；不能再变换一次渐变轴。
func TestTextGradientFillAndStrokeSampleBoundaryAfterObjectCTM(t *testing.T) {
	doc := &Document{fonts: metricsFontEngine{face: clipLayoutFace{paths: map[rune]*geom.Path{
		fontrepair.GlyphRune(21): geom.Rectangle(10, 10),
	}}}}
	for _, tc := range []struct {
		name string
		ctm  models.CTM
	}{
		{"缩放", models.CTM{2, 0, 0, 1.5, 1, 1}},
		{"旋转", models.CTM{0, 1, -1, 0, 28, 2}},
		{"倾斜", models.CTM{1, 0.25, 0.5, 1, 1, 2}},
	} {
		for _, stroke := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/栅格填充", true: "/描边"}[stroke], func(t *testing.T) {
				source := objectCTMGradient()
				if !stroke {
					// Repeat 强制走采样分支；本用例的采样点都位于首个渐变区间。
					source.AxialShd.MapType, source.AxialShd.MapUnit = "Repeat", 40
				}
				object := models.TextObject{CtText: models.CtText{
					CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{X: 2, Y: 3, Width: 35, Height: 32}, CTM: &tc.ctm, LineWidth: 1.5},
					Size:          6, Stroke: stroke, FillColor: source, StrokeColor: source,
					TextCode:    []models.TextCode{{X: 4, Y: 12, Value: "A"}},
					CGTransform: []models.CTCGTransform{{CodeCount: 1, GlyphCount: 1, Glyphs: models.StArrayI{21}}},
				}}
				object.Fill.Set(!stroke)
				surface := clipTestSurface(t, func(ctx DrawContext) {
					doc.Text(ctx, object, nil, models.StBox{Width: 40, Height: 40})
				})
				x, y := 9.0, 7.0
				if stroke {
					x = 4
				}
				bx, by := tc.ctm.Transform(x, y)
				want := color.RGBA{R: uint8(255 * (1 - bx/40)), B: uint8(255 * bx / 40), A: 255}
				check := func(t *testing.T, img image.Image) {
					t.Helper()
					r, g, b, a := img.At(int((object.Boundary.X+bx)*10), int((object.Boundary.Y+by)*10)).RGBA()
					got := color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
					if colorDistance(got, want) > 5 {
						t.Fatalf("渐变未按变换后的 Boundary 位置取色: point=(%g,%g) got=%v want=%v", bx, by, got, want)
					}
				}
				check(t, surface.Rasterize(geom.DPMM(10), nil))
				t.Run("PDF", func(t *testing.T) { check(t, clipPDFImage(t, surface, 254)) })
			})
		}
	}
}
