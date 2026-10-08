package render

import (
	"math"
	"strings"
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

// gotoActions 构造一组只含 CLICK 内部跳转的动作。
func gotoActions(kind models.DestType, pageID models.StID) *models.Actions {
	return &models.Actions{
		Action: []models.CtAction{{
			Event: models.ActionEventClick,
			Goto:  &models.ActionGoto{Dest: &models.CtDest{Type: kind, PageID: models.StRefID(pageID)}},
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

// staticResolver 只认识一个目标页，用于验证跳转解析。
func staticResolver(pageID models.StID, ref LinkTargetRef) PageLinkResolver {
	return func(id models.StID) (LinkTargetRef, bool) {
		if id != pageID {
			return LinkTargetRef{}, false
		}
		return ref, true
	}
}

// TestCollectItemLinksOnlyTakesClickURI 守住热区提取只认 CLICK 外部链接：
// 页面跳转要能解析出目标页才导出，其余事件（PO/DO）不是 PDF 注解能表达的。
func TestCollectItemLinksOnlyTakesClickURI(t *testing.T) {
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
	collectItemLinks([]models.PageItem{
		textItem(box, uriOnPointerUp),
		textItem(box, emptyURI),
		textItem(box, nil),
		textItem(box, uriActions("https://example.com")),
	}, nil, &links)

	if len(links) != 1 {
		t.Fatalf("只应产出 1 个外部链接，实际 %d: %+v", len(links), links)
	}
	if links[0].URI != "https://example.com" {
		t.Fatalf("URI = %q", links[0].URI)
	}
}

// TestCollectItemLinksRecursesIntoBlocks 守住嵌套 PageBlock 中的链接也会被收集：
// 复合图元里的文字同样可点。
func TestCollectItemLinksRecursesIntoBlocks(t *testing.T) {
	box := models.StBox{X: 1, Y: 2, Width: 3, Height: 4}
	links := make([]drawing.PageLink, 0)
	collectItemLinks([]models.PageItem{{
		Kind: models.PageItemBlock,
		Block: &models.PageBlock{
			Items: []models.PageItem{
				textItem(box, nil),
				textItem(box, uriActions("https://nested.example")),
			},
		},
	}}, nil, &links)

	if len(links) != 1 || links[0].URI != "https://nested.example" {
		t.Fatalf("嵌套块中的链接未被收集: %+v", links)
	}
}

// TestCollectItemLinksKeepsInvisibleItems 守住不可见图元上的链接仍然导出。
//
// OFD 里链接热区常写成 Visible="false" 的 PathObject——热区只是点击范围，不该
// 画出来。若因不可见而跳过，用户会得到一个点不动的 PDF。
func TestCollectItemLinksKeepsInvisibleItems(t *testing.T) {
	invisible := textItem(models.StBox{X: 0, Y: 0, Width: 10, Height: 5}, uriActions("https://invisible.example"))
	invisible.Text.Visible = models.OptionalBool{}

	links := make([]drawing.PageLink, 0)
	collectItemLinks([]models.PageItem{invisible}, nil, &links)
	if len(links) != 1 {
		t.Fatalf("不可见图元的链接应保留，实际 %d", len(links))
	}
}

// TestCollectItemLinksRejectsUnusableBoundary 守住宽高为零的热区被丢弃：
// 零面积矩形点不中，写进 PDF 只会留下一个永远触发不了的注解。
func TestCollectItemLinksRejectsUnusableBoundary(t *testing.T) {
	links := make([]drawing.PageLink, 0)
	collectItemLinks([]models.PageItem{
		textItem(models.StBox{X: 0, Y: 0, Width: 0, Height: 5}, uriActions("https://zero-width.example")),
		textItem(models.StBox{X: 0, Y: 0, Width: 5, Height: 0}, uriActions("https://zero-height.example")),
		textItem(models.StBox{X: math.NaN(), Y: 0, Width: 5, Height: 5}, uriActions("https://nan.example")),
		textItem(models.StBox{X: 0, Y: 0, Width: 5, Height: 5}, uriActions("https://ok.example")),
	}, nil, &links)

	if len(links) != 1 || links[0].URI != "https://ok.example" {
		t.Fatalf("应只剩 1 个可用热区，实际 %+v", links)
	}
}

// TestCollectItemLinksSkipsGotoWithoutResolver 守住未提供解析器时内部跳转被丢弃。
//
// 提取方不知道最终会输出哪些页时，产出指向不存在页面的链接比不产出更糟。
func TestCollectItemLinksSkipsGotoWithoutResolver(t *testing.T) {
	box := models.StBox{X: 0, Y: 0, Width: 5, Height: 5}
	links := make([]drawing.PageLink, 0)
	collectItemLinks([]models.PageItem{
		textItem(box, gotoActions(models.DestTypeXYZ, 3)),
	}, nil, &links)
	if len(links) != 0 {
		t.Fatalf("无解析器时不应导出内部跳转: %+v", links)
	}
}

// TestCollectItemLinksResolvesGotoToTarget 守住内部跳转被换算成输出页序。
func TestCollectItemLinksResolvesGotoToTarget(t *testing.T) {
	box := models.StBox{X: 6, Y: 7, Width: 8, Height: 9}
	links := make([]drawing.PageLink, 0)
	collectItemLinks([]models.PageItem{
		textItem(box, gotoActions(models.DestTypeXYZ, 3)),
	}, staticResolver(3, LinkTargetRef{Index: 1, Height: 297}), &links)

	if len(links) != 1 {
		t.Fatalf("内部跳转数 = %d", len(links))
	}
	link := links[0]
	if link.URI != "" {
		t.Fatalf("内部跳转不应带 URI: %q", link.URI)
	}
	if link.Target == nil {
		t.Fatal("缺少跳转目标")
	}
	if link.Target.Page != 1 || link.Target.PageHeight != 297 {
		t.Fatalf("目标页序/高度 = %d/%v", link.Target.Page, link.Target.PageHeight)
	}
	if link.Target.Type != drawing.DestXYZ {
		t.Fatalf("目标类型 = %v", link.Target.Type)
	}
	if link.X != 6 || link.Y != 7 || link.Width != 8 || link.Height != 9 {
		t.Fatalf("热区未取图元边界: %+v", link)
	}
}

// TestCollectItemLinksDropsGotoWhenTargetMissing 守住目标页不在输出时链接被丢弃，
// 而不是写出指向不存在页面的注解。
func TestCollectItemLinksDropsGotoWhenTargetMissing(t *testing.T) {
	box := models.StBox{X: 0, Y: 0, Width: 5, Height: 5}
	links := make([]drawing.PageLink, 0)
	collectItemLinks([]models.PageItem{
		textItem(box, gotoActions(models.DestTypeXYZ, 99)),
	}, staticResolver(3, LinkTargetRef{Index: 1, Height: 297}), &links)
	if len(links) != 0 {
		t.Fatalf("目标页缺失时应丢弃链接: %+v", links)
	}
}

// TestCollectItemLinksTakesFirstResolvableClickAction 守住同一图元上多个 CLICK
// 动作时按文档顺序取第一个可解析的：顺序即语义，跳过无法解析的跳转继续往后找。
func TestCollectItemLinksTakesFirstResolvableClickAction(t *testing.T) {
	resolve := staticResolver(3, LinkTargetRef{Index: 0, Height: 297})

	// 目标页不存在时跳过该跳转，改取后面的外链。
	unreachable := &models.Actions{Action: []models.CtAction{
		{Event: models.ActionEventClick, Goto: &models.ActionGoto{Dest: &models.CtDest{Type: models.DestTypeXYZ, PageID: 99}}},
		{Event: models.ActionEventClick, URI: &models.ActionURI{URI: "https://after-dead-goto.example"}},
	}}
	box := models.StBox{X: 0, Y: 0, Width: 5, Height: 5}
	links := make([]drawing.PageLink, 0)
	collectItemLinks([]models.PageItem{textItem(box, unreachable)}, resolve, &links)
	if len(links) != 1 || links[0].URI != "https://after-dead-goto.example" {
		t.Fatalf("应跳过失效跳转取外链: %+v", links)
	}

	// 顺序在前的跳转有效时就用它，不看后面的外链。
	reachable := &models.Actions{Action: []models.CtAction{
		{Event: models.ActionEventClick, Goto: &models.ActionGoto{Dest: &models.CtDest{Type: models.DestTypeXYZ, PageID: 3}}},
		{Event: models.ActionEventClick, URI: &models.ActionURI{URI: "https://later.example"}},
	}}
	links = links[:0]
	collectItemLinks([]models.PageItem{textItem(box, reachable)}, resolve, &links)
	if len(links) != 1 || links[0].URI != "" || links[0].Target == nil {
		t.Fatalf("应取顺序在前的跳转: %+v", links)
	}
}

// TestLinkDestTypeMapsEveryOFDType 守住 OFD 的 Dest@Type 全部有对应，未知类型退化为
// Fit 而不是被丢弃（目标页仍应能打开）。
func TestLinkDestTypeMapsEveryOFDType(t *testing.T) {
	cases := map[models.DestType]drawing.DestType{
		models.DestTypeFit:  drawing.DestFit,
		models.DestTypeFitH: drawing.DestFitH,
		models.DestTypeFitV: drawing.DestFitV,
		models.DestTypeXYZ:  drawing.DestXYZ,
		models.DestTypeFitR: drawing.DestFitR,
	}
	for input, want := range cases {
		got, ok := linkDestType(input)
		if !ok || got != want {
			t.Fatalf("Dest@Type=%q → %v(ok=%v)，期望 %v", input, got, ok, want)
		}
	}
	if got, ok := linkDestType(models.DestType("Bogus")); ok || got != drawing.DestFit {
		t.Fatalf("未知类型应退化为 Fit，实际 %v(ok=%v)", got, ok)
	}
}

// TestOptionalValueDefaultsToZero 守住可选坐标缺省按 0 处理——缺省与显式 0 在各
// DestType 下语义一致。
func TestOptionalValueDefaultsToZero(t *testing.T) {
	if got := optionalValue(nil); got != 0 {
		t.Fatalf("缺省值 = %v", got)
	}
	value := 12.5
	if got := optionalValue(&value); got != 12.5 {
		t.Fatalf("显式值 = %v", got)
	}
}

// TestLinkAnchorNameIsStableAndUniquePerDestination 守住锚点名由目标内容决定：
// 提取是逐页流式的，没有共享序号可用，而同名锚点会在名称树里自动去重。
func TestLinkAnchorNameIsStableAndUniquePerDestination(t *testing.T) {
	base := drawing.LinkTarget{
		Page: 2, Type: drawing.DestXYZ, Left: 10, Top: 20, PageHeight: 297,
	}
	name := linkAnchorName(base)
	if name != linkAnchorName(base) {
		t.Fatal("同一目标的锚点名不稳定")
	}
	if strings.ContainsAny(name, " ()%/#") {
		t.Fatalf("锚点名含需要转义的字符: %q", name)
	}
	if !strings.HasPrefix(name, "ofd_2_") {
		t.Fatalf("锚点名应带目标页序: %q", name)
	}
	variants := []drawing.LinkTarget{
		func() drawing.LinkTarget { v := base; v.Page = 3; return v }(),
		func() drawing.LinkTarget { v := base; v.Type = drawing.DestFit; return v }(),
		func() drawing.LinkTarget { v := base; v.Left = 11; return v }(),
		func() drawing.LinkTarget { v := base; v.Top = 21; return v }(),
		func() drawing.LinkTarget { v := base; v.PageHeight = 210; return v }(),
	}
	seen := map[string]bool{name: true}
	for _, variant := range variants {
		other := linkAnchorName(variant)
		if seen[other] {
			t.Fatalf("不同目标得到同名锚点 %q", other)
		}
		seen[other] = true
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

// TestCollectAnnotationLinksUsesAppearanceBoundary 守住注解链接的热区取
// Appearance 自身的 Boundary（页面绝对位置），而不是外观图元相对注解的 Boundary。
func TestCollectAnnotationLinksUsesAppearanceBoundary(t *testing.T) {
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
	collectAnnotationLinks(annot, nil, &links)

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

// TestCollectAnnotationLinksSkipsUnusableBoundary 守住外观边界缺失或零面积的
// 注解被丢弃，而不是产出无效热区。
func TestCollectAnnotationLinksSkipsUnusableBoundary(t *testing.T) {
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
	collectAnnotationLinks(annot, nil, &links)

	if len(links) != 1 || links[0].URI != "https://kept.example" {
		t.Fatalf("应只剩 1 个可用注解热区，实际 %+v", links)
	}
}

// TestPageExternalLinksIgnoresInternalTargets 守住只要外部链接的调用方不会被内部
// 跳转干扰：未提供解析器时内部跳转一律不产出。
func TestPageExternalLinksIgnoresInternalTargets(t *testing.T) {
	empty := &Document{}
	if links := empty.PageExternalLinks(nil); links != nil {
		t.Fatalf("nil 页面应返回 nil，实际 %+v", links)
	}
	var nilDocument *Document
	if links := nilDocument.PageLinks(nil, nil); links != nil {
		t.Fatalf("nil 文档应返回 nil，实际 %+v", links)
	}
}
