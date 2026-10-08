package render

import (
	"math"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/drawing"
)

// uriActions 构造一组只含 CLICK 外部链接的动作。
func uriActions(uri string) *models.Actions {
	return &models.Actions{
		Action: []models.CtAction{{
			Event: models.ActionEventClick,
			URI:   &models.ActionURI{URI: uri},
		}},
	}
}

func textItem(boundary models.StBox, actions *models.Actions) models.PageItem {
	return models.PageItem{
		Kind: models.PageItemText,
		Text: &models.TextObject{
			CTGraphicUnit: models.CTGraphicUnit{Boundary: boundary, Actions: actions},
		},
	}
}

// TestCollectItemExternalLinksOnlyTakesClickURI 守住热区提取只认 CLICK 外部链接：
// 页面跳转、附件与媒体动作都不是 PDF 注解能表达的，不应混进外链列表。
func TestCollectItemExternalLinksOnlyTakesClickURI(t *testing.T) {
	gotoAction := &models.Actions{Action: []models.CtAction{{
		Event: models.ActionEventClick,
		Goto:  &models.ActionGoto{Dest: &models.CtDest{Type: models.DestTypeXYZ, PageID: 2}},
	}}}
	uriOnPointerUp := &models.Actions{Action: []models.CtAction{{
		Event: models.ActionEventPO,
		URI:   &models.ActionURI{URI: "https://pointerup.example"},
	}}}
	emptyURI := &models.Actions{Action: []models.CtAction{{
		Event: models.ActionEventClick,
		URI:   &models.ActionURI{URI: ""},
	}}}

	box := models.StBox{X: 10, Y: 20, Width: 30, Height: 4}
	links := make([]drawing.PageLink, 0)
	collectItemExternalLinks([]models.PageItem{
		textItem(box, gotoAction),
		textItem(box, uriOnPointerUp),
		textItem(box, emptyURI),
		textItem(box, nil),
		textItem(box, uriActions("https://example.com")),
	}, &links)

	if len(links) != 1 {
		t.Fatalf("只应产出 1 个外部链接，实际 %d: %+v", len(links), links)
	}
	if links[0].URI != "https://example.com" {
		t.Fatalf("URI = %q", links[0].URI)
	}
}

// TestCollectItemExternalLinksRecursesIntoBlocks 守住嵌套 PageBlock 中的链接
// 也会被收集：复合图元里的文字同样可点。
func TestCollectItemExternalLinksRecursesIntoBlocks(t *testing.T) {
	box := models.StBox{X: 1, Y: 2, Width: 3, Height: 4}
	links := make([]drawing.PageLink, 0)
	collectItemExternalLinks([]models.PageItem{{
		Kind: models.PageItemBlock,
		Block: &models.PageBlock{
			Items: []models.PageItem{
				textItem(box, nil),
				textItem(box, uriActions("https://nested.example")),
			},
		},
	}}, &links)

	if len(links) != 1 || links[0].URI != "https://nested.example" {
		t.Fatalf("嵌套块中的链接未被收集: %+v", links)
	}
}

// TestCollectItemExternalLinksKeepsInvisibleItems 守住不可见图元上的链接仍然导出。
//
// OFD 里链接热区常写成 Visible="false" 的 PathObject——热区只是点击范围，不该
// 画出来。若因不可见而跳过，用户会得到一个点不动的 PDF。
func TestCollectItemExternalLinksKeepsInvisibleItems(t *testing.T) {
	invisible := textItem(models.StBox{X: 0, Y: 0, Width: 10, Height: 5}, uriActions("https://invisible.example"))
	invisible.Text.Visible = models.OptionalBool{}

	links := make([]drawing.PageLink, 0)
	collectItemExternalLinks([]models.PageItem{invisible}, &links)
	if len(links) != 1 {
		t.Fatalf("不可见图元的链接应保留，实际 %d", len(links))
	}
}

// TestCollectItemExternalLinksRejectsUnusableBoundary 守住宽高为零的热区被丢弃：
// 零面积矩形点不中，写进 PDF 只会留下一个永远触发不了的注解。
func TestCollectItemExternalLinksRejectsUnusableBoundary(t *testing.T) {
	links := make([]drawing.PageLink, 0)
	collectItemExternalLinks([]models.PageItem{
		textItem(models.StBox{X: 0, Y: 0, Width: 0, Height: 5}, uriActions("https://zero-width.example")),
		textItem(models.StBox{X: 0, Y: 0, Width: 5, Height: 0}, uriActions("https://zero-height.example")),
		textItem(models.StBox{X: math.NaN(), Y: 0, Width: 5, Height: 5}, uriActions("https://nan.example")),
		textItem(models.StBox{X: 0, Y: 0, Width: 5, Height: 5}, uriActions("https://ok.example")),
	}, &links)

	if len(links) != 1 || links[0].URI != "https://ok.example" {
		t.Fatalf("应只剩 1 个可用热区，实际 %+v", links)
	}
}

// TestPageLinkRectFlipsToPDFCoordinates 守住 OFD 距页顶的坐标换算成 PDF 左下角
// 原点、y 向上的矩形，且宽高为正。
func TestPageLinkRectFlipsToPDFCoordinates(t *testing.T) {
	// OFD 热区 X=10 Y=20 宽 30 高 4，页面高 297mm。
	x0, y0, x1, y1 := drawing.PageLink{X: 10, Y: 20, Width: 30, Height: 4}.Rect(297)
	if x0 != 10 || x1 != 40 {
		t.Fatalf("横向未保持: x0=%v x1=%v", x0, x1)
	}
	if y0 != 297-24 || y1 != 297-20 {
		t.Fatalf("纵向未翻转: y0=%v y1=%v", y0, y1)
	}
	if y0 >= y1 {
		t.Fatalf("y0 应小于 y1: %v %v", y0, y1)
	}
}

// TestPageLinkRectNormalizesNegativeExtent 守住负宽高被归一化。真实 OFD 存在负宽高
// 的 Boundary，若不取 min/max 会得到上下颠倒的矩形，热区整体移位。
func TestPageLinkRectNormalizesNegativeExtent(t *testing.T) {
	px0, py0, px1, py1 := drawing.PageLink{X: 10, Y: 20, Width: 30, Height: 4}.Rect(297)
	// 同一矩形，起点与尺寸反向给出。
	nx0, ny0, nx1, ny1 := drawing.PageLink{X: 40, Y: 24, Width: -30, Height: -4}.Rect(297)
	if nx0 != px0 || ny0 != py0 || nx1 != px1 || ny1 != py1 {
		t.Fatalf("负宽高未归一化: %v %v %v %v vs %v %v %v %v",
			nx0, ny0, nx1, ny1, px0, py0, px1, py1)
	}
}

// TestCollectAnnotationExternalLinksUsesAppearanceBoundary 守住注解链接的热区取
// Appearance 自身的 Boundary（页面绝对位置），而不是外观图元相对注解的 Boundary。
func TestCollectAnnotationExternalLinksUsesAppearanceBoundary(t *testing.T) {
	annot := &models.PageAnnot{
		Annots: []*models.Annot{{
			Type: models.AnnotTypeLink,
			Appearance: &models.Appearance{
				Boundary: &models.StBox{X: 30, Y: 160, Width: 150, Height: 10},
				CTPageBlock: models.CTPageBlock{
					Items: []models.PageItem{{
						Kind: models.PageItemPath,
						Path: &models.PathObject{
							CTGraphicUnit: models.CTGraphicUnit{
								// 外观图元的 Boundary 相对注解定位，不是页面坐标。
								Boundary: models.StBox{X: 0, Y: 0, Width: 150, Height: 10},
								Actions:  uriActions("https://github.com/zc310/ofd"),
							},
						},
					}},
				},
			},
		}},
	}
	links := make([]drawing.PageLink, 0)
	collectAnnotationExternalLinks(annot, &links)

	if len(links) != 1 {
		t.Fatalf("注解链接数 = %d", len(links))
	}
	got := links[0]
	if got.URI != "https://github.com/zc310/ofd" {
		t.Fatalf("URI = %q", got.URI)
	}
	if got.X != 30 || got.Y != 160 || got.Width != 150 || got.Height != 10 {
		t.Fatalf("热区应取注解 Appearance 边界，实际 %+v", got)
	}
}

// TestCollectAnnotationExternalLinksSkipsUnusableBoundary 守住外观边界缺失或零面积
// 的注解被丢弃，而不是产出无效热区。
func TestCollectAnnotationExternalLinksSkipsUnusableBoundary(t *testing.T) {
	path := func(uri string) models.PageItem {
		return models.PageItem{
			Kind: models.PageItemPath,
			Path: &models.PathObject{
				CTGraphicUnit: models.CTGraphicUnit{Actions: uriActions(uri)},
			},
		}
	}
	annot := &models.PageAnnot{Annots: []*models.Annot{
		{Type: models.AnnotTypeLink, Appearance: &models.Appearance{
			CTPageBlock: models.CTPageBlock{Items: []models.PageItem{path("https://no-boundary.example")}},
		}},
		{Type: models.AnnotTypeLink, Appearance: &models.Appearance{
			Boundary:    &models.StBox{X: 0, Y: 0, Width: 0, Height: 10},
			CTPageBlock: models.CTPageBlock{Items: []models.PageItem{path("https://zero-area.example")}},
		}},
		{Type: models.AnnotTypeLink, Appearance: &models.Appearance{
			Boundary:    &models.StBox{X: 1, Y: 2, Width: 3, Height: 4},
			CTPageBlock: models.CTPageBlock{Items: []models.PageItem{path("https://kept.example")}},
		}},
		nil,
		{Type: models.AnnotTypeLink},
	}}
	links := make([]drawing.PageLink, 0)
	collectAnnotationExternalLinks(annot, &links)

	if len(links) != 1 || links[0].URI != "https://kept.example" {
		t.Fatalf("应只剩 1 个可用注解热区，实际 %+v", links)
	}
}

// TestPageExternalLinksEmptyWithoutDocument 守住文档或页面缺失时返回空链接而不是
// panic：多文档体里可能有未加载的页，提取链接不该让整篇转换失败。
func TestPageExternalLinksEmptyWithoutDocument(t *testing.T) {
	empty := &Document{}
	if links := empty.PageExternalLinks(nil); links != nil {
		t.Fatalf("nil 页面应返回 nil，实际 %+v", links)
	}
	var nilDocument *Document
	if links := nilDocument.PageExternalLinks(nil); links != nil {
		t.Fatalf("nil 文档应返回 nil，实际 %+v", links)
	}
}
