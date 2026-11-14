package navigation

import (
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/drawing"
)

func TestActionLinkResolvesBookmarkWithoutMutatingSourceAndSkipsMissingTargets(t *testing.T) {
	actions := models.Actions{Action: []models.CtAction{{Event: models.ActionEventClick,
		Goto: &models.ActionGoto{Bookmark: &models.BookmarkRef{Name: "chapter"}}},
		{Event: models.ActionEventClick, URI: &models.ActionURI{URI: "https://example.test"}},
	}}
	top := 42.0
	bookmarks := map[string]models.CtDest{"chapter": {Type: models.DestTypeXYZ, PageID: 7, Top: &top}}
	resolve := staticResolver(7, PageRef{Index: 2, Height: 297})
	link, ok := ActionLinkWithBookmarks(actions, resolve, bookmarks)
	if !ok || link.Target == nil || link.Target.Page != 2 || link.Target.Top != 42 || link.URI != "" {
		t.Fatalf("未解析文档书签: %+v", link)
	}
	if actions.Action[0].Goto.Dest != nil || bookmarks["chapter"].PageID != 7 {
		t.Fatal("解析书签修改了共享动作或书签声明")
	}
	for _, missing := range []map[string]models.CtDest{nil, {"chapter": {Type: models.DestTypeFit, PageID: 99}}} {
		link, ok := ActionLinkWithBookmarks(actions, resolve, missing)
		if !ok || link.Target != nil || link.URI != "https://example.test" {
			t.Fatalf("无效书签应继续检查后续动作: %+v", link)
		}
	}
	if link, ok := ActionLinkWithBookmarks(actions, nil, bookmarks); !ok || link.Target != nil || link.URI == "" {
		t.Fatalf("缺少目标页解析器时不得生成书签跳转: %+v", link)
	}
}

func TestAnnotationLinksResolveBookmarkUsingAppearanceBoundary(t *testing.T) {
	boundary := models.StBox{X: 10, Y: 20, Width: 30, Height: 10}
	annotation := &models.PageAnnot{Annots: []*models.Annot{{Type: models.AnnotTypeLink,
		Appearance: &models.Appearance{Boundary: &boundary, CTPageBlock: models.CTPageBlock{Items: []models.PageItem{
			textItem(models.StBox{Width: 5, Height: 5}, &models.Actions{Action: []models.CtAction{{Event: models.ActionEventClick,
				Goto: &models.ActionGoto{Bookmark: &models.BookmarkRef{Name: "chapter"}}}}}),
		}}},
	}}}
	links := AnnotationLinksWithBookmarks(annotation, staticResolver(7, PageRef{Index: 2, Height: 297}),
		map[string]models.CtDest{"chapter": {Type: models.DestTypeFit, PageID: 7}})
	if len(links) != 1 || links[0].Target == nil || links[0].Target.Type != drawing.DestFit || links[0].Target.Page != 2 || links[0].X != 10 || links[0].Y != 20 || links[0].Width != 30 {
		t.Fatalf("批注书签目标或热区错误: %+v", links)
	}
}
