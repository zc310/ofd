package render

import (
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/drawing"
)

// stubDocument 造一个只承载大纲与书签的文档体，避免为提取逻辑准备完整解析结果。
func stubDocument() *Document {
	return &Document{Document: &parser.Document{}}
}

// bookmarkRef 造 ActionGoto.Bookmark 的匿名结构体。
func bookmarkRef(name string) *struct {
	Name string `xml:"Name,attr"`
} {
	return &struct {
		Name string `xml:"Name,attr"`
	}{Name: name}
}

// destOutline 构造一个带 Goto 页面目标的大纲项。
func destOutline(title string, kind models.DestType, pageID models.StRefID, left, top float64) models.CTOutlineElem {
	l, t := left, top
	return models.CTOutlineElem{
		Title: title,
		Actions: &models.ActionList{Actions: []models.CtAction{{
			Event: models.ActionEventClick,
			Goto: &models.ActionGoto{Dest: &models.CtDest{
				Type: kind, PageID: pageID, Left: &l, Top: &t,
			}},
		}}},
	}
}

// TestOutlinesFlattensTreeLevels 守住嵌套大纲被展平为带层级的顺序序列，且同级项
// 不被逐层压成链。
func TestOutlinesFlattensTreeLevels(t *testing.T) {
	doc := stubDocument()
	doc.Document.Outlines = &models.OutlineList{OutlineElems: []models.CTOutlineElem{
		{
			Title: "概述",
			Actions: &models.ActionList{Actions: []models.CtAction{{
				Event: models.ActionEventClick,
				Goto:  &models.ActionGoto{Dest: &models.CtDest{Type: models.DestTypeFit, PageID: 0}},
			}}},
			OutlineElem: []models.CTOutlineElem{
				destOutline("适用范围", models.DestTypeXYZ, 0, 40, 80),
				destOutline("术语", models.DestTypeFitR, 0, 30, 150),
			},
		},
		destOutline("版面", models.DestTypeFitV, 1, 60, 0),
		destOutline("附注", models.DestTypeXYZ, 1, 40, 180),
	}}

	// 两页都在输出里：第 1 页页序 0，第 2 页页序 1。
	got := doc.Outlines(func(id models.StID) (LinkTargetRef, bool) {
		switch id {
		case 0:
			return LinkTargetRef{Index: 0, Height: 297}, true
		case 1:
			return LinkTargetRef{Index: 1, Height: 297}, true
		}
		return LinkTargetRef{}, false
	})
	if len(got) != 5 {
		t.Fatalf("条目数 = %d，期望 5: %+v", len(got), got)
	}
	wantTitles := []string{"概述", "适用范围", "术语", "版面", "附注"}
	wantLevels := []int{0, 1, 1, 0, 0}
	for i, title := range wantTitles {
		if got[i].Title != title {
			t.Errorf("第 %d 条标题 = %q，期望 %q", i, got[i].Title, title)
		}
		if got[i].Level != wantLevels[i] {
			t.Errorf("%s 的层级 = %d，期望 %d", title, got[i].Level, wantLevels[i])
		}
	}
}

// TestOutlinesSkipsUntargetableWithoutLeavingHoles 守住没有页面目标的条目被丢弃，
// 且其子项上移到父项原有的层级，树不会因此散开。
func TestOutlinesSkipsUntargetableWithoutLeavingHoles(t *testing.T) {
	doc := stubDocument()
	doc.Document.Outlines = &models.OutlineList{OutlineElems: []models.CTOutlineElem{
		// 只有 URI 动作，没有页面目标。
		{
			Title: "仅链接",
			Actions: &models.ActionList{Actions: []models.CtAction{{
				Event: models.ActionEventClick,
				URI:   &models.ActionURI{URI: "https://example.com"},
			}}},
			OutlineElem: []models.CTOutlineElem{
				destOutline("孙", models.DestTypeXYZ, 0, 10, 20),
			},
		},
		destOutline("顶层", models.DestTypeFit, 0, 0, 0),
	}}

	got := doc.Outlines(staticResolver(0, LinkTargetRef{Index: 0, Height: 297}))
	if len(got) != 2 {
		t.Fatalf("条目数 = %d，期望 2（丢弃无目标的「仅链接」）: %+v", len(got), got)
	}
	if got[0].Title != "孙" || got[0].Level != 0 {
		t.Errorf("孙应上移为顶层: %+v", got[0])
	}
	if got[1].Title != "顶层" || got[1].Level != 0 {
		t.Errorf("顶层条目应保持层级 0: %+v", got[1])
	}
}

// TestOutlinesDropsEntriesWhosePageIsMissing 守住目标页不在输出里的条目被丢弃。
func TestOutlinesDropsEntriesWhosePageIsMissing(t *testing.T) {
	doc := stubDocument()
	doc.Document.Outlines = &models.OutlineList{OutlineElems: []models.CTOutlineElem{
		destOutline("指向被裁掉的页", models.DestTypeXYZ, 9, 10, 20),
		destOutline("指向保留的页", models.DestTypeXYZ, 1, 10, 20),
	}}

	got := doc.Outlines(staticResolver(1, LinkTargetRef{Index: 0, Height: 297}))
	if len(got) != 1 || got[0].Title != "指向保留的页" {
		t.Fatalf("应只剩指向保留页的条目: %+v", got)
	}
}

// TestOutlinesResolvesBookmarkByName 守住按书签名跳转的大纲项能落到目标页。
func TestOutlinesResolvesBookmarkByName(t *testing.T) {
	doc := stubDocument()
	doc.Document.Bookmarks = &models.BookmarkList{Bookmarks: []models.CTBookmark{{
		Name: "附录",
		Dest: models.CtDest{Type: models.DestTypeFitH, PageID: 2, Top: optionalFloat(200)},
	}}}
	name := "附录"
	doc.Document.Outlines = &models.OutlineList{OutlineElems: []models.CTOutlineElem{{
		Title: "引用附录",
		Actions: &models.ActionList{Actions: []models.CtAction{{
			Event: models.ActionEventClick,
			Goto:  &models.ActionGoto{Bookmark: bookmarkRef(name)},
		}}},
	}}}

	got := doc.Outlines(func(id models.StID) (LinkTargetRef, bool) {
		if id != 2 {
			return LinkTargetRef{}, false
		}
		return LinkTargetRef{Index: 1, Height: 297}, true
	})
	if len(got) != 1 {
		t.Fatalf("条目数 = %d，期望 1: %+v", len(got), got)
	}
	target := got[0].Target
	if target == nil || target.Page != 1 || target.Type != drawing.DestFitH || target.Top != 200 {
		t.Fatalf("书签名未解析到目标: %+v", target)
	}
}

// TestOutlinesPrefersDirectDestOverBookmark 守住同时给出页面目标与书签名时优先用
// 页面目标。
func TestOutlinesPrefersDirectDestOverBookmark(t *testing.T) {
	doc := stubDocument()
	doc.Document.Bookmarks = &models.BookmarkList{Bookmarks: []models.CTBookmark{{
		Name: "附录",
		Dest: models.CtDest{Type: models.DestTypeFit, PageID: 2},
	}}}
	doc.Document.Outlines = &models.OutlineList{OutlineElems: []models.CTOutlineElem{{
		Title: "两者都有",
		Actions: &models.ActionList{Actions: []models.CtAction{{
			Event: models.ActionEventClick,
			Goto: &models.ActionGoto{
				Dest:     &models.CtDest{Type: models.DestTypeFitV, PageID: 1, Left: optionalFloat(60)},
				Bookmark: bookmarkRef("附录"),
			},
		}}},
	}}}

	got := doc.Outlines(func(id models.StID) (LinkTargetRef, bool) {
		if id == 1 {
			return LinkTargetRef{Index: 0, Height: 297}, true
		}
		return LinkTargetRef{Index: 1, Height: 297}, true
	})
	if len(got) != 1 {
		t.Fatalf("条目数 = %d", len(got))
	}
	if got[0].Target == nil || got[0].Target.Page != 0 {
		t.Fatalf("应优先用页面目标(第1页)，实际 %+v", got[0].Target)
	}
}

// TestOutlinesEmptyWithoutOutlines 守住没有大纲的文档返回 nil。
func TestOutlinesEmptyWithoutOutlines(t *testing.T) {
	doc := stubDocument()
	if got := doc.Outlines(nil); got != nil {
		t.Fatalf("无大纲应返回 nil，实际 %+v", got)
	}
	var nilDoc *Document
	if got := nilDoc.Outlines(nil); got != nil {
		t.Fatalf("nil 文档应返回 nil，实际 %+v", got)
	}
	if got := (&Document{}).Outlines(nil); got != nil {
		t.Fatalf("无 parser.Document 应返回 nil，实际 %+v", got)
	}
}

// TestOutlinesTitleWithoutTargetKeepsNothing 守住标题为空但有目标的条目仍会输出——
// PDF 允许空标题，这是调用方的选择。
func TestOutlinesTitleWithoutTargetKeepsNothing(t *testing.T) {
	doc := stubDocument()
	doc.Document.Outlines = &models.OutlineList{OutlineElems: []models.CTOutlineElem{
		destOutline("", models.DestTypeFit, 0, 0, 0),
	}}
	got := doc.Outlines(staticResolver(0, LinkTargetRef{Index: 0, Height: 297}))
	if len(got) != 1 || !strings.EqualFold(got[0].Title, "") {
		t.Fatalf("空标题条目应保留: %+v", got)
	}
}

func optionalFloat(v float64) *float64 { return &v }
