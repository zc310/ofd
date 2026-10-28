package render

import (
	"image/color"
	"math"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

func TestPatternRejectsUnrepresentableTileIndexesBeforeChargingBudget(t *testing.T) {
	pattern := &models.CtPattern{Width: math.SmallestNonzeroFloat64, Height: math.SmallestNonzeroFloat64,
		CellContent: models.CellContent{CTPageBlock: models.CTPageBlock{Items: []models.PageItem{{Kind: models.PageItemPath}}}},
	}
	path := geom.Rectangle(1e290, 1e290).Translate(1e300, 1e300)
	var budget renderBudget
	budget.reset()
	doc := &Document{}
	if doc.drawPatternPath(nil, path, pattern, models.PathObject{}, models.StBox{Width: 100, Height: 100}, nil, &budget) || budget.patternTiles != 0 {
		t.Fatalf("浮点索引溢出被误当作有效图块：tiles=%d", budget.patternTiles)
	}
}

func TestZeroAlphaGradientEndpointsDoNotPaintColor(t *testing.T) {
	var stops geom.Grad
	stops.Add(0, color.RGBA{R: 255})
	stops.Add(1, color.RGBA{B: 255, A: 255})
	for _, name := range []string{"native", "sampled"} {
		t.Run(name, func(t *testing.T) {
			var gradient geom.Gradient = stops.ToLinear(geom.Point{X: 10}, geom.Point{X: 20})
			if name == "sampled" {
				gradient = &ofdLinearGradient{stops: stops, start: geom.Point{X: 10}, end: geom.Point{X: 20}, dx: 10, d2: 100, unit: 10, extend: 3}
			}
			ctx := newOffscreenSurface(30, 10, geom.DPMM(4))
			ctx.SetFillColor(geom.Black)
			ctx.ClearStroke()
			ctx.DrawPath(0, 0, geom.Rectangle(30, 10))
			ctx.SetFillGradient(gradient)
			ctx.DrawPath(0, 0, geom.Rectangle(30, 10))
			if got := ctx.Raster().RGBAAt(20, 20); got != geom.Black {
				t.Fatalf("透明红端点改变了黑色背景：%v", got)
			}
			t.Run("PDF", func(t *testing.T) {
				object := clipTestPath(t, `<PathObject Boundary="0 0 30 10" Fill="true" Stroke="false"><AbbreviatedData>M 0 0 L 30 0 L 30 10 L 0 10 C</AbbreviatedData></PathObject>`)
				alpha := uint8(0)
				extend := 3
				if name == "sampled" {
					extend = 1
				}
				object.FillColor = &models.CTColor{AxialShd: &models.CTAxialShd{StartPoint: models.StPos{X: 10}, EndPoint: models.StPos{X: 20}, Extend: extend, Segment: []models.Segment{
					{Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}, Alpha: &alpha}},
					{Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}},
				}}}
				surface, err := NewVectorSurface(30, 10, func(ctx DrawContext) {
					ctx.SetFillColor(geom.Black)
					ctx.ClearStroke()
					ctx.DrawPath(0, 0, geom.Rectangle(30, 10))
					doc := &Document{}
					doc.Path(ctx, object, nil, models.StBox{Width: 30, Height: 10})
				})
				if err != nil {
					t.Fatal(err)
				}
				img := clipPDFImage(t, surface, 254)
				if got := color.RGBAModel.Convert(img.At(50, 50)).(color.RGBA); got != geom.Black {
					t.Fatalf("PDF透明端点改变背景：%v", got)
				}
			})
		})
	}
}

func TestPatternTileRangeRejectsOverflowAndKeepsNormalMargins(t *testing.T) {
	if start, count, ok := patternTileRange(-2, 10, 2); !ok || start != -2 || count != 9 {
		t.Fatalf("普通图块范围=%d,%d,%v", start, count, ok)
	}
	maximum := float64(int(^uint(0) >> 1))
	for _, tc := range [][3]float64{
		{1e300, 1e300, math.SmallestNonzeroFloat64},
		{maximum, maximum, 1},
		{-maximum, maximum, 1},
		{math.NaN(), 1, 1},
		{0, 1, 0},
	} {
		if _, _, ok := patternTileRange(tc[0], tc[1], tc[2]); ok {
			t.Fatalf("越界索引未被拒绝：%v", tc)
		}
	}
}

func TestMeshZeroAlphaCarriersDoNotEscapeAsPixels(t *testing.T) {
	red := premultiplied(255, 0, 0, 0)
	blue := premultiplied(0, 0, 255, 255)
	if got := interpolateMeshColor(red, blue, blue, .5, .5, 0); got != (color.RGBA{R: 63, B: 63, A: 127}) {
		t.Fatalf("网格透明红蓝插值=%v", got)
	}
	gradient := &ofdMeshGradient{extend: true, backColor: red}
	if got := gradient.At(100, 100); got != (color.RGBA{}) {
		t.Fatalf("网格透明背景泄露RGB：%v", got)
	}
}
