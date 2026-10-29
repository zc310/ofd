package navigation

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/zc310/ofd/internal/models"
)

func TestOutlinesPromotesMissingParentAndKeepsURIBookmarkHierarchy(t *testing.T) {
	bookmark := &struct {
		Name string `xml:"Name,attr"`
	}{Name: "appendix"}
	source := &models.OutlineList{OutlineElems: []models.CTOutlineElem{{
		Title: "missing page",
		Actions: &models.ActionList{Actions: []models.CtAction{{Goto: &models.ActionGoto{Dest: &models.CtDest{
			Type: models.DestTypeFit, PageID: 99,
		}}}}},
		OutlineElem: []models.CTOutlineElem{{
			Title:   "website",
			Actions: &models.ActionList{Actions: []models.CtAction{{URI: &models.ActionURI{URI: "https://example.com"}}}},
			OutlineElem: []models.CTOutlineElem{{
				Title:   "appendix",
				Actions: &models.ActionList{Actions: []models.CtAction{{Goto: &models.ActionGoto{Bookmark: bookmark}}}},
			}},
		}},
	}}}
	bookmarks := &models.BookmarkList{Bookmarks: []models.CTBookmark{{Name: "appendix", Dest: models.CtDest{Type: models.DestTypeFitH, PageID: 2}}}}
	resolve := staticResolver(2, PageRef{Index: 0, Height: 297})
	got := Outlines(source, bookmarks, resolve)
	if len(got) != 2 || got[0].Level != 0 || got[0].URI != "https://example.com" || got[1].Level != 1 || got[1].Target == nil || got[1].Target.Page != 0 {
		t.Fatalf("大纲提升或 URI/书签层级错误：%+v", got)
	}
	if source.OutlineElems[0].Title != "missing page" || len(source.OutlineElems[0].OutlineElem) != 1 || source.OutlineElems[0].OutlineElem[0].Title != "website" {
		t.Fatal("展平大纲修改了源模型")
	}
	withoutResolver := Outlines(source, bookmarks, nil)
	if len(withoutResolver) != 1 || withoutResolver[0].URI != "https://example.com" {
		t.Fatalf("缺少页面映射时没有仅保留 URI：%+v", withoutResolver)
	}
}

func TestNavigationResultsAreIndependentSnapshots(t *testing.T) {
	resolve := staticResolver(2, PageRef{Index: 1, Height: 297})
	source := []models.PageItem{textItem(models.StBox{Width: 10, Height: 5}, gotoActions(models.DestTypeXYZ, 2))}
	first, second := Links(source, resolve), Links(source, resolve)
	if !reflect.DeepEqual(first, second) || len(first) != 1 || first[0].Target == second[0].Target {
		t.Fatal("链接解析没有返回独立目标快照")
	}
	first[0].Target.Page = 99
	first[0].X = 100
	if third := Links(source, resolve); !reflect.DeepEqual(third, second) {
		t.Fatal("修改链接结果污染了后续解析")
	}
	outline := &models.OutlineList{OutlineElems: []models.CTOutlineElem{{
		Title: "chapter", Actions: &models.ActionList{Actions: source[0].Text.Actions.Action},
	}}}
	a, b := Outlines(outline, nil, resolve), Outlines(outline, nil, resolve)
	if !reflect.DeepEqual(a, b) || len(a) != 1 || a[0].Target == b[0].Target {
		t.Fatal("大纲解析没有返回独立目标快照")
	}
	a[0].Title, a[0].Target.Name = "changed", "changed"
	if got := Outlines(outline, nil, resolve); !reflect.DeepEqual(got, b) {
		t.Fatal("修改大纲结果污染了后续解析")
	}
}

// 深度限制按源树计数，不能被无目标祖先的层级提升绕过。
func TestOutlinesBoundsSourceDepthAndPreservesFollowingSiblings(t *testing.T) {
	const limit = 64
	for _, targeted := range []bool{false, true} {
		t.Run(map[bool]string{false: "无目标祖先", true: "有目标祖先"}[targeted], func(t *testing.T) {
			actions := &models.ActionList{Actions: []models.CtAction{{Goto: &models.ActionGoto{Dest: &models.CtDest{
				Type: models.DestTypeFit, PageID: 2,
			}}}}}
			var elems []models.CTOutlineElem
			for depth := 20000; depth > 0; depth-- {
				elem := models.CTOutlineElem{Title: strconv.Itoa(depth), OutlineElem: elems}
				if targeted {
					elem.Actions = actions
				}
				elems = []models.CTOutlineElem{elem}
			}
			// 边界层可保留；它的子项不可被提升回允许的层级。
			boundary := &elems[0]
			for range limit - 1 {
				boundary = &boundary.OutlineElem[0]
			}
			boundary.Actions = actions
			boundary.OutlineElem[0].Actions = actions
			source := &models.OutlineList{OutlineElems: []models.CTOutlineElem{elems[0], {Title: "之后的顶层项", Actions: actions}}}
			calls := 0
			resolve := func(id models.StID) (PageRef, bool) {
				calls++
				return staticResolver(2, PageRef{Index: 0, Height: 297})(id)
			}
			got := Outlines(source, nil, resolve)
			want := 2
			if targeted {
				want = limit + 1
			}
			if len(got) != want || calls != want {
				t.Fatalf("超深分支没有在访问目标前截断: entries=%d resolverCalls=%d want=%d", len(got), calls, want)
			}
			for i, outline := range got[:len(got)-1] {
				depth := limit
				if targeted {
					depth = i + 1
				}
				if outline.Title != strconv.Itoa(depth) || outline.Level != i || outline.Target == nil || outline.Target.Page != 0 {
					t.Fatalf("保留节点顺序或层级错误: index=%d outline=%+v", i, outline)
				}
			}
			if last := got[len(got)-1]; last.Title != "之后的顶层项" || last.Level != 0 {
				t.Fatalf("深分支之后的兄弟节点丢失或降级: %+v", last)
			}
			if len(boundary.OutlineElem) != 1 || boundary.OutlineElem[0].Title != "65" {
				t.Fatal("截断展平结果时修改了源树")
			}
		})
	}
}
