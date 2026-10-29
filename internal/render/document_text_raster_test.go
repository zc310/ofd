package render

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/renderstate"
)

func rasterTextGradient() *models.CTColor {
	return &models.CTColor{AxialShd: &models.CTAxialShd{
		EndPoint: models.StPos{X: 4}, MapType: "Repeat", MapUnit: 4, Extend: 3,
		Segment: []models.Segment{
			{Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}},
			{Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}},
		},
	}}
}

// 固定轮廓区分源字符与 CGTransform 指定的实际字形。
func TestRasterGradientTextUsesMappedGlyphsAndParentTransform(t *testing.T) {
	face := clipLayoutFace{paths: map[rune]*geom.Path{
		'A': geom.Rectangle(1, 1), 'B': geom.Rectangle(1, 1), fontrepair.GlyphRune(21): geom.Rectangle(4, 5),
	}}
	doc := &Document{fonts: metricsFontEngine{face: face}}
	object := models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{Width: 40, Height: 40}},
		Size:          6, TextCode: []models.TextCode{{X: 4, Y: 10, Value: "AB"}}, FillColor: rasterTextGradient(),
		CGTransform: []models.CTCGTransform{{CodeCount: 2, GlyphCount: 1, Glyphs: models.StArrayI{21}}},
	}}
	parent := &models.CTM{1, 0, 0, 1, 8, 3}
	surface := clipTestSurface(t, func(ctx DrawContext) {
		var budget renderBudget
		budget.reset()
		doc.textWithBudget(ctx, object, nil, models.StBox{Width: 40, Height: 40}, parent, nil, &budget)
	})
	check := func(t *testing.T, img image.Image) {
		t.Helper()
		_, _, _, a := img.At(140, 100).RGBA()
		r, g, b, _ := img.At(140, 100).RGBA()
		if a < 60000 || g > 10000 || r+b < 40000 {
			t.Fatalf("映射字形在父级变换后丢失: RGBA=%v", []uint32{r, g, b, a})
		}
		r, g, b, a = img.At(45, 95).RGBA()
		if a > 32767 && (r < 64000 || g < 64000 || b < 64000) {
			t.Fatal("渐变文字仍绘制在原位置")
		}
	}
	check(t, surface.Rasterize(geom.DPMM(10)))
	t.Run("PDF", func(t *testing.T) { check(t, clipPDFImage(t, surface, 254)) })
}

func TestRasterGradientTextKeepsStrokeAndPDFText(t *testing.T) {
	doc := clipTestDocument(t)
	object := regressionTextObject()
	object.FillColor = rasterTextGradient()
	object.Stroke = true
	object.StrokeColor = &models.CTColor{Value: &models.Color{RGBA: color.RGBA{G: 255, A: 255}}}
	surface := clipTestSurface(t, func(ctx DrawContext) { doc.Text(ctx, object, nil, models.StBox{Width: 40, Height: 40}) })
	check := func(t *testing.T, img image.Image) {
		t.Helper()
		green, colored := 0, 0
		for y := range 400 {
			for x := range 400 {
				r, g, b, a := img.At(x, y).RGBA()
				if a > 60000 && g > 50000 && r < 10000 && b < 10000 {
					green++
				}
				if a > 60000 && g < 10000 && r+b > 50000 {
					colored++
				}
			}
		}
		if green < 10 || colored < 10 {
			t.Fatalf("渐变填充或描边丢失: stroke=%d fill=%d", green, colored)
		}
	}
	t.Run("Raster", func(t *testing.T) { check(t, surface.Rasterize(geom.DPMM(10))) })
	t.Run("PDF pixels", func(t *testing.T) { check(t, clipPDFImage(t, surface, 254)) })
	t.Run("PDF text", func(t *testing.T) { checkPDFExtractedText(t, surface, "AB") })
}

func TestRasterGradientTextAppliesAlphaOnceToOverlappingGlyphs(t *testing.T) {
	face := clipLayoutFace{paths: map[rune]*geom.Path{'A': geom.Rectangle(4, 5)}}
	doc := &Document{fonts: metricsFontEngine{face: face}}
	alpha := uint8(128)
	source := rasterTextGradient()
	source.Alpha = &alpha
	object := models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{Width: 40, Height: 40}, Alpha: &alpha},
		Size:          6, TextCode: []models.TextCode{{X: 4, Y: 10, Value: "AA", DeltaX: models.StArrayF{0}}}, FillColor: source,
	}}
	surface := clipTestSurface(t, func(ctx DrawContext) { doc.Text(ctx, object, nil, models.StBox{Width: 40, Height: 40}) })
	img := surface.Rasterize(geom.DPMM(10))
	if a := img.RGBAAt(60, 80).A; a < 63 || a > 65 {
		t.Fatalf("重叠字形 Alpha=%d，期望约64", a)
	}
	t.Run("PDF", func(t *testing.T) {
		_, g, _, _ := clipPDFImage(t, surface, 254).At(60, 80).RGBA()
		if g < 188*257 || g > 194*257 {
			t.Fatalf("PDF 重叠字形白底绿色=%d，期望约191", g>>8)
		}
	})
}

func TestRasterGradientTextHollowDoesNotPaintFill(t *testing.T) {
	doc := clipTestDocument(t)
	object := regressionTextObject()
	object.FillColor, object.Stroke = rasterTextGradient(), true
	object.Fill.Set(false)
	object.StrokeColor = &models.CTColor{Value: &models.Color{RGBA: color.RGBA{G: 255, A: 255}}}
	surface := clipTestSurface(t, func(ctx DrawContext) { doc.Text(ctx, object, nil, models.StBox{Width: 40, Height: 40}) })
	check := func(t *testing.T, img image.Image) {
		t.Helper()
		green := 0
		for y := range 400 {
			for x := range 400 {
				r, g, b, a := img.At(x, y).RGBA()
				if a > 60000 && g > 50000 && r < 10000 && b < 10000 {
					green++
				}
				if a > 60000 && g < 10000 && r+b > 50000 {
					t.Fatal("空心字仍绘制了渐变填充")
				}
			}
		}
		if green < 10 {
			t.Fatal("空心字的描边丢失")
		}
	}
	check(t, surface.Rasterize(geom.DPMM(10)))
	t.Run("PDF", func(t *testing.T) { check(t, clipPDFImage(t, surface, 254)) })
}

func TestRasterGradientTextChargesInkBoundsBeforeAllocation(t *testing.T) {
	face := clipLayoutFace{paths: map[rune]*geom.Path{'A': geom.Rectangle(4, 5)}}
	doc := &Document{}
	object := models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{Width: 600, Height: 600}},
		Size:          6, TextCode: []models.TextCode{{X: 4, Y: 10, Value: "A"}},
	}}
	for _, limited := range []bool{false, true} {
		t.Run(map[bool]string{false: "墨迹范围", true: "预算耗尽"}[limited], func(t *testing.T) {
			var budget renderBudget
			budget.reset()
			if limited && !budget.AllowOffscreenPixels(renderstate.MaxOffscreenPixels, 1, 25.4) {
				t.Fatal("预先消费预算失败")
			}
			calls := 0
			setOffscreenSurfaceFactory(t, func(w, h float64, resolution geom.Resolution) OffscreenSurface {
				calls++
				if w >= 5 || h >= 6 || budget.Usage().OffscreenPixels < w*h*resolution.DPMM()*resolution.DPMM() {
					t.Fatalf("未按墨迹范围预先扣费: width=%g height=%g usage=%g", w, h, budget.Usage().OffscreenPixels)
				}
				return nil
			})
			clipTestSurface(t, func(ctx DrawContext) {
				doc.drawMeshText(ctx, face, rasterTextGradient(), object, models.StBox{Width: 600, Height: 600}, nil, nil, &budget)
			})
			want := 1
			if limited {
				want = 0
			}
			if calls != want {
				t.Fatalf("离屏分配次数=%d，期望%d", calls, want)
			}
		})
	}
}

type rasterSemanticFace struct{ clipLayoutFace }

func (rasterSemanticFace) InvisibleShapedRun(value string) TextRun { return value }

type rasterSemanticContext struct {
	DrawContext

	values []TextRun
}

func (c *rasterSemanticContext) DrawSemanticTextLine(run TextRun) {
	c.values = append(c.values, run)
}

func TestRasterGradientTextEmitsMappedSourceTextOnce(t *testing.T) {
	face := rasterSemanticFace{clipLayoutFace{paths: map[rune]*geom.Path{fontrepair.GlyphRune(21): geom.Rectangle(4, 5)}}}
	doc := &Document{fonts: metricsFontEngine{face: face}}
	for _, stroke := range []bool{false, true} {
		object := models.TextObject{CtText: models.CtText{
			CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{Width: 40, Height: 40}},
			Size:          6, Stroke: stroke, FillColor: rasterTextGradient(), TextCode: []models.TextCode{{X: 4, Y: 10, Value: "AB"}},
			CGTransform: []models.CTCGTransform{{CodeCount: 2, GlyphCount: 1, Glyphs: models.StArrayI{21}}},
		}}
		clipTestSurface(t, func(ctx DrawContext) {
			capture := &rasterSemanticContext{DrawContext: ctx}
			doc.Text(capture, object, nil, models.StBox{Width: 40, Height: 40})
			if len(capture.values) != 1 || capture.values[0] != "AB" {
				t.Fatalf("源文字应仅输出一次: stroke=%v values=%v", stroke, capture.values)
			}
		})
	}
}

func TestRasterGradientTextMatchesSolidPositionsAndClipping(t *testing.T) {
	doc := clipTestDocument(t)
	for _, direction := range []int{0, 90} {
		object := regressionTextObject()
		object.CharDirection, object.HScale = direction, 0.8
		object.CTM = &models.CTM{1, 0, 0.25, 1, 0, 0}
		object.TextCode[0].DeltaX = models.StArrayF{8}
		parent := &models.CTM{1, 0, 0, 1, 5, 2}
		render := func(source *models.CTColor) VectorSurface {
			object.FillColor = source
			return clipTestSurface(t, func(ctx DrawContext) {
				ctx.Translate(1, 2)
				var budget renderBudget
				budget.reset()
				doc.textWithBudget(ctx, object, nil, models.StBox{Width: 40, Height: 40}, parent, geom.Rectangle(25, 40), &budget)
			})
		}
		solid, gradient := render(nil), render(rasterTextGradient())
		check := func(t *testing.T, a, b image.Image) {
			t.Helper()
			ink, mismatch := 0, 0
			for y := range 400 {
				for x := range 400 {
					_, g1, _, a1 := a.At(x, y).RGBA()
					_, g2, _, a2 := b.At(x, y).RGBA()
					p1, p2 := a1 > 32767 && g1 < 32767, a2 > 32767 && g2 < 32767
					if p1 || p2 {
						ink++
					}
					if p1 != p2 {
						mismatch++
					}
				}
			}
			if ink == 0 || float64(mismatch)/float64(ink) > 0.12 {
				t.Fatalf("渐变与实色字形位置不一致: direction=%d ink=%d mismatch=%d", direction, ink, mismatch)
			}
		}
		before := gradient.Rasterize(geom.DPMM(10))
		check(t, solid.Rasterize(geom.DPMM(10)), before)
		t.Run(map[int]string{0: "水平PDF", 90: "旋转PDF"}[direction], func(t *testing.T) {
			check(t, clipPDFImage(t, solid, 254), clipPDFImage(t, gradient, 254))
		})
		if !bytes.Equal(before.Pix, gradient.Rasterize(geom.DPMM(10)).Pix) {
			t.Fatal("PDF 输出改写了共享字形或渐变")
		}
	}
}
