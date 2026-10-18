package render

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
)

func TestRenderBudgetLimitsCompositeExpansion(t *testing.T) {
	var budget renderBudget
	budget.reset()

	for i := 0; i < maxCompositeExpansions; i++ {
		if !budget.allowComposite(models.StID(i + 1)) {
			t.Fatalf("composite expansion %d was rejected", i)
		}
	}
	if budget.allowComposite(models.StID(maxCompositeExpansions + 1)) {
		t.Fatal("composite expansion beyond the global limit was accepted")
	}
}

func TestRenderBudgetLimitsRepeatedCompositeResource(t *testing.T) {
	var budget renderBudget
	budget.reset()
	resourceID := models.StID(1)

	for i := 0; i < maxCompositeResourceExpansions; i++ {
		if !budget.allowComposite(resourceID) {
			t.Fatalf("composite resource expansion %d was rejected", i)
		}
	}
	if budget.allowComposite(resourceID) {
		t.Fatal("repeated composite resource expansion beyond the limit was accepted")
	}
}

func TestRenderBudgetLimitsPatternTilesAndOffscreenPixels(t *testing.T) {
	var budget renderBudget
	budget.reset()

	if !budget.allowPatternTiles(maxPatternTilesPerRender - 1) {
		t.Fatal("pattern tiles within the budget were rejected")
	}
	if budget.allowPatternTiles(2) {
		t.Fatal("pattern tiles beyond the budget were accepted")
	}

	budget.reset()
	if !budget.allowOffscreenPixels(10, 10, 10) {
		t.Fatal("offscreen allocation within the budget was rejected")
	}
	if budget.allowOffscreenPixels(1e9, 1e9, 1200) {
		t.Fatal("offscreen allocation beyond the budget was accepted")
	}
}

func TestRenderBudgetResetClearsUsage(t *testing.T) {
	var budget renderBudget
	budget.reset()
	if !budget.allowComposite(models.StID(1)) || !budget.allowPatternTiles(1) || !budget.allowOffscreenPixels(10, 10, 10) {
		t.Fatal("failed to consume render budget")
	}

	budget.reset()
	if !budget.allowComposite(models.StID(1)) || !budget.allowPatternTiles(1) || !budget.allowOffscreenPixels(10, 10, 10) {
		t.Fatal("render budget was not reset")
	}
}

func budgetComposite(t *testing.T) (*Document, *models.PathObject, models.CompositeObject) {
	t.Helper()
	ofd, err := parser.NewOFD(writeCompositeOFD(t, 600, 600, compositeRect20))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ofd.Close() })
	doc := NewDocument(nil, ofd.Documents[0])
	t.Cleanup(doc.Close)
	path := ofd.Documents[0].GetCompositeUnit(5).Content.Items[0].Path
	object := models.CompositeObject{CtComposite: models.CtComposite{
		CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{Width: 100, Height: 100}, CTM: &models.CTM{0.5, 0, 0, 0.5, 0, 0}},
		ResourceID:    5,
	}}
	return doc, path, object
}

func TestCompositeVectorSharesPatternAndOffscreenBudget(t *testing.T) {
	doc, path, object := budgetComposite(t)
	ctx := newCanvasBackend(canvas.NewContext(canvas.New(100, 100)))
	pb := models.StBox{Width: 100, Height: 100}
	var budget renderBudget
	budget.reset()
	path.FillColor = &models.CTColor{Pattern: &models.CtPattern{
		Width: 10, Height: 10,
		CellContent: models.CellContent{CTPageBlock: models.CTPageBlock{Items: []models.PageItem{{Kind: models.PageItemPath, Path: &models.PathObject{CtPath: models.CtPath{
			CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{Width: 10, Height: 10}},
			Fill:          true, Stroke: models.NewOptionalBool(false),
			FillColor:       &models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}},
			AbbreviatedData: models.SVGPath{{Type: models.MoveTo, Points: []models.StPos{{}}}, {Type: models.LineTo, Points: []models.StPos{{X: 10, Y: 10}}}},
		}}}}}},
	}}
	doc.compositeWithBudget(ctx, object, nil, pb, nil, nil, 0, &budget)
	if budget.patternTiles == 0 {
		t.Fatal("复合图元内的图案未计入外层预算")
	}
	previous := newOffscreenSurfaceFactory
	t.Cleanup(func() { newOffscreenSurfaceFactory = previous })
	allocations := 0
	newOffscreenSurfaceFactory = func(float64, float64, geom.Resolution) OffscreenSurface {
		allocations++
		return nil
	}
	path.FillColor = &models.CTColor{GouraudShd: &models.CTGouraudShd{Point: []models.GouraudPoint{
		{X: 50, Y: 50, Color: basicMeshColor(color.RGBA{R: 255, A: 255})},
		{X: 70, Y: 50, Color: basicMeshColor(color.RGBA{G: 255, A: 255})},
		{X: 50, Y: 70, Color: basicMeshColor(color.RGBA{B: 255, A: 255})},
	}}}
	budget.offscreenPixels = maxOffscreenPixels
	doc.compositeWithBudget(ctx, object, nil, pb, nil, nil, 0, &budget)
	if allocations != 0 {
		t.Fatal("复合图元重新获得离屏额度，绕过外层已耗尽的预算")
	}
}

type blankCompositeSurface struct{ DrawContext }

func (blankCompositeSurface) Raster() *image.RGBA { return image.NewRGBA(image.Rect(0, 0, 1, 1)) }

func TestCompositeBlankRetryChargesFullCanvasBeforeAllocation(t *testing.T) {
	for _, limited := range []bool{false, true} {
		t.Run(map[bool]string{false: "enough budget", true: "retry exceeds budget"}[limited], func(t *testing.T) {
			doc, _, object := budgetComposite(t)
			ctx := newCanvasBackend(canvas.NewContext(canvas.New(100, 100)))
			previous := newOffscreenSurfaceFactory
			t.Cleanup(func() { newOffscreenSurfaceFactory = previous })
			var budget renderBudget
			budget.reset()
			if limited {
				budget.offscreenPixels = maxOffscreenPixels - 10000
			}
			initial, allocated := budget.offscreenPixels, 0.0
			calls := 0
			newOffscreenSurfaceFactory = func(w, h float64, r geom.Resolution) OffscreenSurface {
				calls++
				allocated += w * h * r.DPMM() * r.DPMM()
				if math.Abs(budget.offscreenPixels-initial-allocated) > 1e-6 {
					t.Fatal("离屏画布创建前未累计扣除预算")
				}
				return blankCompositeSurface{DrawContext: ctx}
			}
			// 父级矩阵使矢量分支退回离屏绘制；透明结果强制触发整幅画布重试。
			parent := models.IdentityMatrix
			doc.compositeWithBudget(ctx, object, nil, models.StBox{Width: 100, Height: 100}, &parent, nil, 0, &budget)
			want := 2
			if limited {
				want = 1
			}
			if calls != want {
				t.Fatalf("创建离屏画布 %d 次，期望 %d 次", calls, want)
			}
		})
	}
}
