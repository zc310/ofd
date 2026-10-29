package render

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

func strokeStyleText() models.TextObject {
	object := regressionTextObject()
	object.Boundary = models.StBox{Width: 40, Height: 40}
	object.TextCode = []models.TextCode{{X: 10, Y: 20, Value: "A"}}
	object.Fill.Set(false)
	object.Stroke = true
	return object
}

func strokeStyleDocument() *Document {
	return &Document{fonts: metricsFontEngine{face: clipLayoutFace{paths: map[rune]*geom.Path{'A': geom.Rectangle(10, 10)}}}}
}

func TestTextStrokeObjectWidthOverridesDrawParam(t *testing.T) {
	doc := strokeStyleDocument()
	object := strokeStyleText()
	object.LineWidth = 2
	surface := clipTestSurface(t, func(ctx DrawContext) {
		doc.Text(ctx, object, &models.DrawParam{LineWidth: 0.2}, models.StBox{Width: 40, Height: 40})
	})
	check := func(t *testing.T, img image.Image) {
		t.Helper()
		r, _, _, a := img.At(94, 150).RGBA()
		if a < 60000 || r > 10000 {
			t.Fatal("图元 LineWidth=2 未覆盖引用参数 LineWidth=0.2")
		}
	}
	check(t, surface.Rasterize(geom.DPMM(10)))
	t.Run("PDF", func(t *testing.T) { check(t, clipPDFImage(t, surface, 254)) })
}

func TestTextStrokeKeepsObjectDashesWhenClipped(t *testing.T) {
	doc := strokeStyleDocument()
	object := strokeStyleText()
	object.LineWidth = 1
	object.DashPattern = &models.StArrayF{2, 2}
	object.Clips = rectangleClip(18, 40)
	surface := clipTestSurface(t, func(ctx DrawContext) {
		doc.Text(ctx, object, &models.DrawParam{LineWidth: 1}, models.StBox{Width: 40, Height: 40})
	})
	check := func(t *testing.T, img image.Image) {
		t.Helper()
		for _, sample := range []struct {
			x    int
			want bool
		}{{110, true}, {130, false}, {150, true}, {170, false}, {190, false}} {
			r, _, _, a := img.At(sample.x, 200).RGBA()
			if got := a > 32767 && r < 32767; got != sample.want {
				t.Fatalf("文字虚线 x=%d 墨迹=%v，期望%v", sample.x, got, sample.want)
			}
		}
	}
	check(t, surface.Rasterize(geom.DPMM(10)))
	t.Run("PDF", func(t *testing.T) { check(t, clipPDFImage(t, surface, 254)) })
}

func TestTextStrokeDefaultBlackRespectsObjectAlpha(t *testing.T) {
	doc := strokeStyleDocument()
	object := strokeStyleText()
	object.LineWidth, object.Alpha = 1, new(uint8(128))
	surface := clipTestSurface(t, func(ctx DrawContext) { doc.Text(ctx, object, nil, models.StBox{Width: 40, Height: 40}) })
	if got := surface.Rasterize(geom.DPMM(10)).RGBAAt(100, 150).A; got < 127 || got > 129 {
		t.Fatalf("缺省黑描边 Alpha=%d，期望128", got)
	}
	t.Run("PDF", func(t *testing.T) {
		r, _, _, _ := clipPDFImage(t, surface, 254).At(100, 150).RGBA()
		if r < 125*257 || r > 130*257 {
			t.Fatalf("PDF 半透明黑描边白底亮度=%d，期望127", r>>8)
		}
	})
}

func TestTextGradientStrokePreservesColorAlphaAndPDFText(t *testing.T) {
	doc := clipTestDocument(t)
	object := regressionTextObject()
	object.Fill.Set(false)
	object.Stroke, object.LineWidth, object.Alpha = true, 0.8, new(uint8(128))
	object.StrokeColor = rasterTextGradient()
	object.StrokeColor.Alpha = new(uint8(128))
	surface := clipTestSurface(t, func(ctx DrawContext) { doc.Text(ctx, object, nil, models.StBox{Width: 40, Height: 40}) })
	maxAlpha := uint8(0)
	for i, value := range surface.Rasterize(geom.DPMM(10)).Pix {
		if i%4 == 3 {
			maxAlpha = max(maxAlpha, value)
		}
	}
	if maxAlpha < 63 || maxAlpha > 65 {
		t.Fatalf("渐变描边 Alpha=%d，期望64", maxAlpha)
	}
	t.Run("PDF pixels", func(t *testing.T) {
		img := clipPDFImage(t, surface, 254)
		minGreen := uint8(255)
		for y := range 400 {
			for x := range 400 {
				_, g, _, _ := img.At(x, y).RGBA()
				minGreen = min(minGreen, uint8(g>>8))
			}
		}
		if minGreen < 188 || minGreen > 194 {
			t.Fatalf("PDF 渐变描边白底绿色=%d，期望191", minGreen)
		}
	})
	t.Run("PDF text", func(t *testing.T) { checkPDFExtractedText(t, surface, "AB") })
}

// 填充应先画，较粗的描边覆盖它的内沿。
func TestTextGradientStrokeDrawsAboveSolidFill(t *testing.T) {
	doc := strokeStyleDocument()
	object := strokeStyleText()
	object.Fill.Set(true)
	object.LineWidth = 2
	object.FillColor = &models.CTColor{Value: &models.Color{RGBA: color.RGBA{G: 255, A: 255}}}
	object.StrokeColor = rasterTextGradient()
	surface := clipTestSurface(t, func(ctx DrawContext) { doc.Text(ctx, object, nil, models.StBox{Width: 40, Height: 40}) })
	check := func(t *testing.T, img image.Image) {
		t.Helper()
		r, g, b, a := img.At(104, 150).RGBA()
		if a < 60000 || g > 10000 || r+b < 50000 {
			t.Fatal("渐变描边未覆盖填充内沿")
		}
	}
	check(t, surface.Rasterize(geom.DPMM(10)))
	t.Run("PDF", func(t *testing.T) { check(t, clipPDFImage(t, surface, 254)) })
}

func TestTextStrokeCTMScaleIsSameWithIdentityParent(t *testing.T) {
	doc := strokeStyleDocument()
	object := strokeStyleText()
	object.LineWidth = 1
	object.CTM = &models.CTM{2, 0, 0, 2, 0, 0}
	object.TextCode = []models.TextCode{{X: 5, Y: 10, Value: "A"}}
	var first *image.RGBA
	for _, parent := range []*models.CTM{nil, &models.IdentityMatrix} {
		surface := clipTestSurface(t, func(ctx DrawContext) {
			var budget renderBudget
			budget.reset()
			doc.textWithBudget(ctx, object, nil, models.StBox{Width: 40, Height: 40}, parent, nil, &budget)
		})
		img := surface.Rasterize(geom.DPMM(10))
		if first != nil && !bytes.Equal(first.Pix, img.Pix) {
			t.Fatal("单位父级矩阵改变了文字描边宽度")
		}
		first = img
		check := func(t *testing.T, img image.Image) {
			t.Helper()
			r, _, _, a := img.At(94, 100).RGBA()
			if a < 60000 || r > 10000 {
				t.Fatal("CTM=2 时线宽没有同比扩大到 2mm")
			}
		}
		check(t, img)
		t.Run(map[bool]string{true: "有父级PDF", false: "无父级PDF"}[parent != nil], func(t *testing.T) { check(t, clipPDFImage(t, surface, 254)) })
	}
}

func TestTextStrokeKeepsOverlappingGlyphEdgesAndAppliesAlphaOnce(t *testing.T) {
	doc := strokeStyleDocument()
	for _, delta := range []float64{0, 5} {
		object := strokeStyleText()
		object.LineWidth, object.Alpha = 1, new(uint8(128))
		object.TextCode = []models.TextCode{{X: 10, Y: 20, Value: "AA", DeltaX: models.StArrayF{delta}}}
		surface := clipTestSurface(t, func(ctx DrawContext) { doc.Text(ctx, object, nil, models.StBox{Width: 40, Height: 40}) })
		x := 100 + int(delta*10)
		if a := surface.Rasterize(geom.DPMM(10)).RGBAAt(x, 150).A; a < 127 || a > 129 {
			t.Fatalf("重叠字形内沿丢失或重复应用透明度: delta=%g alpha=%d", delta, a)
		}
		t.Run(map[bool]string{true: "完全重叠PDF", false: "部分重叠PDF"}[delta == 0], func(t *testing.T) {
			r, _, _, _ := clipPDFImage(t, surface, 254).At(x, 150).RGBA()
			if r < 125*257 || r > 130*257 {
				t.Fatalf("PDF 重叠描边亮度=%d，期望127", r>>8)
			}
		})
	}
}

func TestTextGradientStrokeUsesBoundaryCoordinatesAcrossRuns(t *testing.T) {
	doc := strokeStyleDocument()
	object := strokeStyleText()
	object.LineWidth = 1
	object.TextCode = []models.TextCode{{X: 10, Y: 20, Value: "AA", DeltaX: models.StArrayF{20}}}
	object.StrokeColor = &models.CTColor{AxialShd: &models.CTAxialShd{EndPoint: models.StPos{X: 40}, Extend: 3,
		Segment: []models.Segment{
			{Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}},
			{Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}},
		},
	}}
	surface := clipTestSurface(t, func(ctx DrawContext) { doc.Text(ctx, object, nil, models.StBox{Width: 40, Height: 40}) })
	check := func(t *testing.T, img image.Image) {
		t.Helper()
		r1, _, b1, _ := img.At(100, 150).RGBA()
		r2, _, b2, _ := img.At(300, 150).RGBA()
		if r1 < 150*257 || b1 > 100*257 || r2 > 100*257 || b2 < 150*257 {
			t.Fatalf("描边渐变在各 run 重置: first=(%d,%d) second=(%d,%d)", r1>>8, b1>>8, r2>>8, b2>>8)
		}
	}
	check(t, surface.Rasterize(geom.DPMM(10)))
	t.Run("PDF", func(t *testing.T) { check(t, clipPDFImage(t, surface, 254)) })
}

func TestTextPatternStrokeKeepsDashesClipOpacityAndPDFText(t *testing.T) {
	doc := strokeStyleDocument()
	object := strokeStyleText()
	object.LineWidth, object.Alpha = 1, new(uint8(128))
	object.DashPattern = &models.StArrayF{2, 2}
	object.Clips = rectangleClip(18, 40)
	cell := clipTestPath(t, `<PathObject Boundary="0 0 2 2" Fill="true" Stroke="false"><FillColor Value="255 0 0"/><AbbreviatedData>M 0 0 L 2 0 L 2 2 L 0 2 C</AbbreviatedData></PathObject>`)
	source := &models.CTColor{Alpha: new(uint8(128)), Pattern: &models.CtPattern{
		Width: 2, Height: 2, CellContent: models.CellContent{CTPageBlock: models.CTPageBlock{
			Items: []models.PageItem{{Kind: models.PageItemPath, Path: &cell}},
		}},
	}}
	// 引用的描边底纹也必须生效。
	surface := clipTestSurface(t, func(ctx DrawContext) {
		doc.Text(ctx, object, &models.DrawParam{StrokeColor: source}, models.StBox{Width: 40, Height: 40})
	})
	check := func(t *testing.T, img image.Image, pdf bool) {
		t.Helper()
		r, g, b, a := img.At(111, 201).RGBA()
		if pdf {
			if r < 64000 || g < 188*257 || g > 200*257 || b != g {
				t.Fatalf("PDF 底纹描边颜色或透明度错误: %v", []uint32{r, g, b, a})
			}
		} else if r < 62*257 || r > 65*257 || g != 0 || b != 0 || a < 62*257 || a > 65*257 {
			t.Fatalf("底纹描边颜色或透明度错误: %v", []uint32{r, g, b, a})
		}
		for _, point := range []image.Point{{130, 200}, {190, 200}, {150, 150}} {
			r, g, b, a := img.At(point.X, point.Y).RGBA()
			if a > 32767 && (r < 64000 || g < 64000 || b < 64000) {
				t.Fatalf("虚线间隙、裁剪外或空心区域出现墨迹: %v", point)
			}
			if !pdf && a != 0 {
				t.Fatalf("虚线间隙、裁剪外或空心区域不透明: %v", point)
			}
		}
	}
	check(t, surface.Rasterize(geom.DPMM(10)), false)
	t.Run("PDF", func(t *testing.T) { check(t, clipPDFImage(t, surface, 254), true) })

	// 真实字体核对空心底纹字的语义，不依赖测试轮廓支持文字整形。
	realDoc := clipTestDocument(t)
	object = regressionTextObject()
	object.Fill.Set(false)
	object.Stroke, object.StrokeColor, object.LineWidth = true, source, 0.8
	realSurface := clipTestSurface(t, func(ctx DrawContext) {
		realDoc.Text(ctx, object, nil, models.StBox{Width: 40, Height: 40})
	})
	t.Run("PDF text", func(t *testing.T) { checkPDFExtractedText(t, realSurface, "AB") })
}

func TestTextPatternStrokeDrawsAboveSolidFill(t *testing.T) {
	doc := &Document{fonts: metricsFontEngine{face: strokeFillFace{
		clipLayoutFace: clipLayoutFace{paths: map[rune]*geom.Path{'A': geom.Rectangle(10, 10)}},
		fill:           geom.SolidPaint(color.RGBA{G: 255, A: 255}),
	}}}
	object := strokeStyleText()
	object.Fill.Set(true)
	object.LineWidth = 2
	object.FillColor = &models.CTColor{Value: &models.Color{RGBA: color.RGBA{G: 255, A: 255}}}
	cell := clipTestPath(t, `<PathObject Boundary="0 0 40 40" Fill="true" Stroke="false"><FillColor Value="255 0 0"/><AbbreviatedData>M 0 0 L 40 0 L 40 40 L 0 40 C</AbbreviatedData></PathObject>`)
	object.StrokeColor = &models.CTColor{Pattern: &models.CtPattern{
		Width: 40, Height: 40, CellContent: models.CellContent{CTPageBlock: models.CTPageBlock{
			Items: []models.PageItem{{Kind: models.PageItemPath, Path: &cell}},
		}},
	}}
	surface := clipTestSurface(t, func(ctx DrawContext) { doc.Text(ctx, object, nil, models.StBox{Width: 40, Height: 40}) })
	check := func(t *testing.T, img image.Image) {
		t.Helper()
		r, g, _, a := img.At(104, 150).RGBA()
		if a < 60000 || g > 10000 || r < 50000 {
			t.Fatal("底纹描边没有覆盖填充内沿")
		}
		r, g, _, a = img.At(150, 150).RGBA()
		if a < 60000 || g < 50000 || r > 10000 {
			t.Fatal("底纹描边覆盖了字形内部的填充")
		}
	}
	check(t, surface.Rasterize(geom.DPMM(10)))
	t.Run("PDF", func(t *testing.T) { check(t, clipPDFImage(t, surface, 254)) })
}

type strokeFillFace struct {
	clipLayoutFace

	fill geom.Paint
}

func (f strokeFillFace) Fill() geom.Paint { return f.fill }

func (f strokeFillFace) DirectPath(value string) *geom.Path { return f.ToPath(value) }

func TestTextPatternStrokeEmitsMappedSourceTextOnce(t *testing.T) {
	face := rasterSemanticFace{clipLayoutFace{paths: map[rune]*geom.Path{fontrepair.GlyphRune(21): geom.Rectangle(4, 5)}}}
	doc := &Document{fonts: metricsFontEngine{face: face}}
	cell := clipTestPath(t, `<PathObject Boundary="0 0 40 40" Fill="true" Stroke="false"><FillColor Value="255 0 0"/><AbbreviatedData>M 0 0 L 40 0 L 40 40 L 0 40 C</AbbreviatedData></PathObject>`)
	source := &models.CTColor{Pattern: &models.CtPattern{
		Width: 40, Height: 40, CellContent: models.CellContent{CTPageBlock: models.CTPageBlock{
			Items: []models.PageItem{{Kind: models.PageItemPath, Path: &cell}},
		}},
	}}
	for _, fill := range []bool{false, true} {
		object := models.TextObject{CtText: models.CtText{
			CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{Width: 40, Height: 40}},
			Size:          6, Stroke: true, StrokeColor: source, FillColor: source,
			TextCode:    []models.TextCode{{X: 4, Y: 10, Value: "AB"}},
			CGTransform: []models.CTCGTransform{{CodeCount: 2, GlyphCount: 1, Glyphs: models.StArrayI{21}}},
		}}
		object.LineWidth = 1
		object.Fill.Set(fill)
		clipTestSurface(t, func(ctx DrawContext) {
			capture := &rasterSemanticContext{DrawContext: ctx}
			doc.Text(capture, object, nil, models.StBox{Width: 40, Height: 40})
			if len(capture.values) != 1 || capture.values[0] != "AB" {
				t.Fatalf("底纹填充与描边的源文字应仅输出一次: fill=%v values=%v", fill, capture.values)
			}
		})
	}
}
