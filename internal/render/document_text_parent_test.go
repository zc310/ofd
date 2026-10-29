package render

import (
	"image"
	"testing"

	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

// 父级矩阵必须作用于整个字形，不能只移动基线起点。
func TestTextParentTransformsWholeGlyphInRasterAndPDF(t *testing.T) {
	doc := &Document{fonts: metricsFontEngine{face: clipLayoutFace{paths: map[rune]*geom.Path{fontrepair.GlyphRune(21): geom.Rectangle(2, 4)}}}}
	for _, tc := range []struct {
		name   string
		parent models.CTM
		inside image.Point
	}{
		{"非等比缩放", models.CTM{2, 0, 0, 3, 2, 1}, image.Pt(130, 210)},
		{"旋转", models.CTM{0, 1, -1, 0, 20, 0}, image.Pt(130, 50)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			object := models.TextObject{CtText: models.CtText{
				CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{Width: 40, Height: 40}},
				Size:          4, TextCode: []models.TextCode{{X: 4, Y: 10, Value: "A"}},
				CGTransform: []models.CTCGTransform{{CodeCount: 1, GlyphCount: 1, Glyphs: models.StArrayI{21}}},
			}}
			surface := clipTestSurface(t, func(ctx DrawContext) {
				var budget renderBudget
				budget.reset()
				doc.textWithBudget(ctx, object, nil, models.StBox{Width: 40, Height: 40}, &tc.parent, nil, &budget)
			})
			check := func(t *testing.T, img image.Image) {
				t.Helper()
				r, _, _, a := img.At(tc.inside.X, tc.inside.Y).RGBA()
				if a < 60000 || r > 10000 {
					t.Fatalf("父级变换未作用于字形内部: point=%v RGBA=(%d,%d)", tc.inside, r, a)
				}
			}
			check(t, surface.Rasterize(geom.DPMM(10)))
			t.Run("PDF", func(t *testing.T) { check(t, clipPDFImage(t, surface, 254)) })
		})
	}
}

// 比较父级平移前后的实际颜色，渐变应随容器一起移动。
func TestTextGradientFollowsParentTranslation(t *testing.T) {
	doc := strokeStyleDocument()
	for _, stroke := range []bool{false, true} {
		t.Run(map[bool]string{false: "填充", true: "描边"}[stroke], func(t *testing.T) {
			object := strokeStyleText()
			object.Fill.Set(!stroke)
			object.Stroke, object.LineWidth = stroke, 1
			gradient := rasterTextGradient()
			object.FillColor, object.StrokeColor = gradient, gradient
			render := func(offset float64) VectorSurface {
				return clipTestSurface(t, func(ctx DrawContext) {
					var budget renderBudget
					budget.reset()
					doc.textWithBudget(ctx, object, nil, models.StBox{Width: 40, Height: 40}, &models.CTM{1, 0, 0, 1, offset, 0}, nil, &budget)
				})
			}
			first, shifted := render(0), render(5)
			check := func(t *testing.T, a, b image.Image) {
				t.Helper()
				x := 150
				if stroke {
					x = 100
				}
				r1, _, b1, _ := a.At(x, 150).RGBA()
				r2, _, b2, _ := b.At(x+50, 150).RGBA()
				if abs(float64(r1)-float64(r2)) > 1500 || abs(float64(b1)-float64(b2)) > 1500 || r1+b1 < 50000 {
					t.Fatalf("父级平移改变了渐变颜色: original=(%d,%d) shifted=(%d,%d)", r1>>8, b1>>8, r2>>8, b2>>8)
				}
			}
			check(t, first.Rasterize(geom.DPMM(10)), shifted.Rasterize(geom.DPMM(10)))
			t.Run("PDF", func(t *testing.T) { check(t, clipPDFImage(t, first, 254), clipPDFImage(t, shifted, 254)) })
		})
	}
}

func TestTextParentMatchesWholeObjectTransformAndKeepsPDFText(t *testing.T) {
	doc := clipTestDocument(t)
	axial := rasterTextGradient()
	axial.AxialShd.MapType, axial.AxialShd.MapUnit = "", 0
	axial.AxialShd.EndPoint.X = 20
	for _, tc := range []struct {
		name   string
		parent models.CTM
		canvas geom.Matrix
	}{
		{"缩放", models.CTM{2, 0, 0, 1.5, 2, 3}, geom.Matrix{{2, 0, 2}, {0, 1.5, -23}}},
		{"旋转", models.CTM{0, 1, -1, 0, 30, 2}, geom.Matrix{{0, 1, -10}, {-1, 0, 38}}},
		{"倾斜", models.CTM{1, 0, 0.5, 1, 1, 2}, geom.Matrix{{1, -0.5, 21}, {0, 1, -2}}},
	} {
		for _, fill := range []struct {
			name   string
			source *models.CTColor
		}{{"实色", nil}, {"轴向", axial}} {
			t.Run(tc.name+"/"+fill.name, func(t *testing.T) {
				object := regressionTextObject()
				object.Boundary = models.StBox{Width: 40, Height: 40}
				object.CTM = &models.CTM{1, 0, 0.25, 1, 0, 0}
				object.FillColor = fill.source
				object.TextCode = []models.TextCode{{X: 4, Y: 12, Value: "AB"}}
				expected := clipTestSurface(t, func(ctx DrawContext) {
					ctx.Transform(tc.canvas)
					doc.Text(ctx, object, nil, models.StBox{Width: 40, Height: 40})
				})
				actual := clipTestSurface(t, func(ctx DrawContext) {
					var budget renderBudget
					budget.reset()
					doc.textWithBudget(ctx, object, nil, models.StBox{Width: 40, Height: 40}, &tc.parent, nil, &budget)
				})
				compare := func(t *testing.T, a, b image.Image) {
					t.Helper()
					ink, mismatch, colorMismatch := 0, 0, 0
					for y := range 400 {
						for x := range 400 {
							r1, g1, b1, a1 := a.At(x, y).RGBA()
							r2, g2, b2, a2 := b.At(x, y).RGBA()
							p1, p2 := a1 > 32767 && g1 < 32767, a2 > 32767 && g2 < 32767
							if p1 || p2 {
								ink++
							}
							if p1 != p2 {
								mismatch++
							}
							if p1 && p2 && g1 < 3000 && g2 < 3000 && a1 > 64000 && a2 > 64000 && max(abs(float64(r1)-float64(r2)), abs(float64(b1)-float64(b2))) > 12*257 {
								colorMismatch++
							}
						}
					}
					if ink == 0 || float64(mismatch)/float64(ink) > 0.03 || float64(colorMismatch)/float64(ink) > 0.03 {
						t.Fatalf("父级变换与整体变换不一致: ink=%d mismatch=%d colorMismatch=%d", ink, mismatch, colorMismatch)
					}
				}
				compare(t, expected.Rasterize(geom.DPMM(10)), actual.Rasterize(geom.DPMM(10)))
				t.Run("PDF pixels", func(t *testing.T) { compare(t, clipPDFImage(t, expected, 254), clipPDFImage(t, actual, 254)) })
				t.Run("PDF text", func(t *testing.T) { checkPDFExtractedText(t, actual, "AB") })
			})
		}
	}
}

// 固定轮廓的内部采样避开重采样边缘，核对仿射变换下的渐变颜色。
func TestTextGradientColorsFollowAffineParent(t *testing.T) {
	doc := &Document{fonts: metricsFontEngine{face: clipLayoutFace{paths: map[rune]*geom.Path{fontrepair.GlyphRune(21): geom.Rectangle(10, 10)}}}}
	axial := rasterTextGradient()
	axial.AxialShd.MapType, axial.AxialShd.MapUnit = "", 0
	axial.AxialShd.EndPoint.X = 30
	radial := &models.CTColor{RadialShd: &models.CTRadialShd{
		StartPoint: models.StPos{X: 10, Y: 10}, EndPoint: models.StPos{X: 10, Y: 10}, EndRadius: 12,
		Extend: 3, Segment: axial.AxialShd.Segment,
	}}
	for _, source := range []struct {
		name  string
		color *models.CTColor
	}{{"轴向", axial}, {"径向", radial}, {"Repeat", rasterTextGradient()}} {
		for _, stroke := range []bool{false, true} {
			for _, parent := range []models.CTM{
				{1.5, 0, 0, 0.8, 1, 2}, {0, 1, -1, 0, 30, 2}, {1, 0, 0.5, 1, 1, 2},
			} {
				t.Run(source.name+"/"+map[bool]string{false: "填充", true: "描边"}[stroke]+"/"+parent.String(), func(t *testing.T) {
					object := strokeStyleText()
					object.Fill.Set(!stroke)
					object.Stroke, object.LineWidth = stroke, 1
					object.CGTransform = []models.CTCGTransform{{CodeCount: 1, GlyphCount: 1, Glyphs: models.StArrayI{21}}}
					object.FillColor, object.StrokeColor = source.color, source.color
					render := func(parent *models.CTM) VectorSurface {
						return clipTestSurface(t, func(ctx DrawContext) {
							var budget renderBudget
							budget.reset()
							doc.textWithBudget(ctx, object, nil, models.StBox{Width: 40, Height: 40}, parent, nil, &budget)
						})
					}
					first, transformed := render(nil), render(&parent)
					x, y := 15.0, 15.0
					if stroke {
						x = 10
					}
					tx, ty := parent.Transform(x, y)
					check := func(t *testing.T, a, b image.Image) {
						t.Helper()
						r1, g1, b1, a1 := a.At(int(x*10), int(y*10)).RGBA()
						r2, g2, b2, a2 := b.At(int(tx*10), int(ty*10)).RGBA()
						if a1 < 60000 || a2 < 60000 || g1 > 5000 || g2 > 5000 || max(abs(float64(r1)-float64(r2)), abs(float64(b1)-float64(b2))) > 8*257 {
							t.Fatalf("父级仿射变换改变了渐变颜色: original=%v transformed=%v", []uint32{r1, g1, b1, a1}, []uint32{r2, g2, b2, a2})
						}
					}
					check(t, first.Rasterize(geom.DPMM(10)), transformed.Rasterize(geom.DPMM(10)))
					t.Run("PDF", func(t *testing.T) { check(t, clipPDFImage(t, first, 254), clipPDFImage(t, transformed, 254)) })
				})
			}
		}
	}
}
