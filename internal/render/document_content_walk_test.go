package render

import (
	"image/color"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/renderstate"
	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/image/font/gofont/goregular"
)

func textSourceDocument(t *testing.T, cropped bool) *Document {
	t.Helper()
	unitWidth := 100.0
	if cropped {
		unitWidth = 40
	}
	f := false
	data, err := creator.Marshal(creator.Document{
		ID: "text-sources", PageSize: creator.PageSize{Width: 100, Height: 100}, Fonts: []creator.Font{{Name: "Go", Data: goregular.TTF}},
		Composites: []creator.CompositeGraphicUnit{{ID: 100, Width: unitWidth, Height: unitWidth, Items: []creator.Item{
			creator.Text{X: 10, Y: 10, Width: 25, Height: 10, Font: "Go", Size: 4, TextCodes: []creator.TextCode{{Y: float64Ptr(5), Value: "INNER"}}},
			creator.Path{X: 10, Y: 10, Width: 25, Height: 10, Data: "M 0 0 L 25 0 L 25 10 L 0 10 C", Visible: &f, Actions: []creator.Action{{Event: creator.ActionEventClick, URI: &creator.URIAction{URI: "https://composite.example"}}}},
		}}},
		Pages: []creator.Page{{Items: []creator.Item{
			creator.Text{X: 2, Y: 2, Width: 25, Height: 8, Font: "Go", Size: 4, Value: "BODY"},
			creator.Composite{X: 20, Y: 20, Width: 40, Height: 40, ResourceID: 100},
		}}},
		Annotations: []creator.AnnotationPage{{Page: 0, Items: []creator.Annotation{
			{ID: 201, Type: "Watermark", Creator: "test", LastModDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Boundary: &creator.Box{X: 5, Y: 60, Width: 40, Height: 10}, Items: []creator.Item{creator.Text{Width: 30, Height: 8, Font: "Go", Size: 4, Value: "WATER"}}},
			{ID: 202, Type: "Stamp", Creator: "test", LastModDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Boundary: &creator.Box{X: 5, Y: 75, Width: 40, Height: 10}, Items: []creator.Item{creator.Text{Width: 30, Height: 8, Font: "Go", Size: 4, Value: "NOTE"}}},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	doc := NewDocument(color.White, ofd.Documents[0])
	t.Cleanup(func() { doc.Close(); _ = ofd.Close() })
	return doc
}

func TestTextSourcesCompositeCoordinatesAndLinksUseSameMapping(t *testing.T) {
	for _, cropped := range []bool{false, true} {
		doc := textSourceDocument(t, cropped)
		page := doc.Pages[0]
		values, err := doc.TextValuesWithOptionsE(page, PageVisibility{}, TextOptions{IncludeAnnotations: true, IncludeWatermarks: true})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(values, []string{"WATER", "BODY", "INNER", "NOTE"}) {
			t.Fatalf("来源顺序=%v", values)
		}
		layouts, err := doc.TextLayoutsWithOptionsE(page, PageVisibility{}, TextOptions{IncludeAnnotations: true, IncludeWatermarks: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(layouts) != len(values) || layouts[0].Source != TextSourceWatermark || layouts[3].Source != TextSourceAnnotation {
			t.Fatalf("来源布局=%+v", layouts)
		}
		for i, layout := range layouts {
			if layout.Text != values[i] {
				t.Fatal("索引文字和布局不一致")
			}
		}
		inner := layouts[2]
		if len(inner.Glyphs) != 5 || inner.Glyphs[0].Width <= 0 || inner.Glyphs[0].X < 20 || inner.Glyphs[4].X > 60 {
			t.Fatalf("复合文字未映射到页面：%+v", inner)
		}
		if !cropped && math.Abs(inner.Glyphs[0].X-30) > 1e-8 {
			t.Fatalf("单元内容偏移=%g，期望30", inner.Glyphs[0].X)
		}
		links, err := doc.PageLinksWithVisibilityE(page, nil, PageVisibility{})
		if err != nil {
			t.Fatal(err)
		}
		if len(links) != 1 || !links[0].InComposite || links[0].URI != "https://composite.example" || links[0].X < 19.9 || links[0].X+links[0].Width > 60.1 {
			t.Fatalf("复合链接映射错误：%+v", links)
		}
		selected, err := doc.TextLayoutsForRunsWithOptionsE(page, []int{2}, PageVisibility{}, TextOptions{IncludeAnnotations: true, IncludeWatermarks: true})
		if err != nil || !reflect.DeepEqual(selected, layouts[2:3]) {
			t.Fatalf("来源选择导致run索引错配：%v %+v", err, selected)
		}
	}
}

func TestRenderReportCollectsMissingResourcesAndIsBounded(t *testing.T) {
	doc := NewDocument(color.White, &parser.Document{})
	defer doc.Close()
	items := make([]models.PageItem, 0, 200)
	for i := range 200 {
		items = append(items, models.PageItem{Kind: models.PageItemImage, Image: &models.ImageObject{ID: models.StID(i + 1), CtImage: models.CtImage{
			CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{Width: 10, Height: 10}}, ResourceID: 999,
		}}})
	}
	page := parser.NewPage(models.PageContent{Area: &models.CtPageArea{PhysicalBox: models.StBox{Width: 20, Height: 20}}, Content: &models.Content{Layer: []*models.Layer{{CTPageBlock: models.CTPageBlock{Items: items}}}}})
	_, report, err := doc.RasterizePageWithReport(page, BackendCanvas, geom.DPI(25.4))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Diagnostics) != renderstate.MaxDiagnostics || report.Dropped != 200-renderstate.MaxDiagnostics || report.Stats.ObjectsVisited != 200 {
		t.Fatalf("报告未限量：%+v", report)
	}
	if report.Diagnostics[0].Code != "missing_image" || report.Diagnostics[0].ObjectID != 1 || report.Diagnostics[0].ResourceID != 999 {
		t.Fatalf("诊断缺少对象资源定位：%+v", report.Diagnostics[0])
	}
	report.Diagnostics[0].Code = "changed"
	_, second, err := doc.PageWithReport(page, PageVisibility{})
	if err != nil || second.Diagnostics[0].Code != "missing_image" {
		t.Fatal("外部修改污染后续报告")
	}
}

func TestRenderReportAggregatesBudgetRefusals(t *testing.T) {
	report := &renderReportBuilder{}
	var budget renderBudget
	budget.reset()
	budget.Report = report
	budget.ObjectID = 42
	if !budget.AllowPatternTiles(renderstate.MaxPatternTiles) {
		t.Fatal("预先消费图块预算失败")
	}
	for range 10 {
		budget.AllowPatternTiles(1)
	}
	got := report.Snapshot()
	if len(got.Diagnostics) != 1 || got.Diagnostics[0].Count != 10 || got.Diagnostics[0].ObjectID != 42 {
		t.Fatalf("重复诊断未合并：%+v", got)
	}
}

func TestCompositeTextTraversalBoundsCycles(t *testing.T) {
	doc := textSourceDocument(t, false)
	unit := doc.GetCompositeUnit(100)
	unit.Content.Items = append(unit.Content.Items, models.PageItem{Kind: models.PageItemComposite, Composite: &models.CompositeObject{CtComposite: models.CtComposite{
		CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{Width: 40, Height: 40}}, ResourceID: 100,
	}}})
	values, err := doc.TextValuesWithVisibilityE(doc.Pages[0], PageVisibility{})
	if err != nil || len(values) < 2 || len(values) > maxCompositeDepth+1 {
		t.Fatalf("循环展开未受限：%v %d", err, len(values))
	}
}

func TestInvisibleCompositeHotspotSurvivesEmptyRaster(t *testing.T) {
	doc := textSourceDocument(t, true)
	unit := doc.GetCompositeUnit(100)
	unit.Content.Items[0].Text.Visible.Set(false)
	links, err := doc.PageLinksWithVisibilityE(doc.Pages[0], nil, PageVisibility{})
	if err != nil || len(links) != 1 || math.Abs(links[0].X-30) > 1e-8 || math.Abs(links[0].Width-25) > 1e-8 {
		t.Fatalf("无墨迹复合热区丢失：%v %+v", err, links)
	}
}

func TestOFDSealTextUsesChildFontAndPagePlacement(t *testing.T) {
	doc := &Document{}
	defer doc.Close()
	info := &parser.SealInfo{SealData: &parser.SealData{FileType: "ofd", Data: concurrentSealData(t)}, StampAnnot: &models.StampAnnot{Boundary: models.StBox{X: 30, Y: 40, Width: 20, Height: 20}}}
	var layouts []TextLayout
	err := doc.walkSealText(info, func(child contentFrame, item models.PageItem) {
		if item.Kind != models.PageItemText {
			return
		}
		var local []TextLayout
		index := 0
		collectObjectTextLayouts(*item.Text, &local, child.Scope.Document, nil, &index)
		for _, layout := range local {
			layouts = append(layouts, child.MapLayout(layout))
		}
	})
	if err != nil || len(layouts) != 1 || layouts[0].Text != "Seal" || layouts[0].Source != TextSourceSeal || layouts[0].Font != 0 {
		t.Fatalf("子文档文字来源错误：%v %+v", err, layouts)
	}
	if layouts[0].Glyphs[0].X < 30 || layouts[0].Glyphs[0].Y < 40 {
		t.Fatalf("印章文字没有映射到父页：%+v", layouts[0])
	}
}
