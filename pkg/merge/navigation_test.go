package merge

import (
	"bytes"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/spec"
)

func TestPagesOffsetsSecondSourcePageActions(t *testing.T) {
	var out bytes.Buffer
	if err := Pages([]Source{{Data: documentWithNavigation(t, "a")}, {Data: documentWithNavigation(t, "b")}}, &out, PageOptions{}); err != nil {
		t.Fatal(err)
	}
	assertPageGoto(t, out.Bytes(), 2, 3)
}

func assertPageGoto(t *testing.T, data []byte, page, target int) {
	t.Helper()
	ofd, err := parser.NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := ofd.Documents[0]
	if err := doc.Pages[page].EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	actions := doc.Pages[page].Actions()
	if actions == nil || len(actions.Action) != 1 || actions.Action[0].Goto == nil || actions.Action[0].Goto.Dest == nil {
		t.Fatalf("输出第 %d 页没有预期的跳转动作: %+v", page+1, actions)
	}
	if got := actions.Action[0].Goto.Dest.PageID; uint64(got) != uint64(doc.Pages[target].ID) {
		t.Fatalf("输出第 %d 页跳转页面 ID %d，期望第 %d 页的 ID %d", page+1, got, target+1, doc.Pages[target].ID)
	}
}

func TestPagesOffsetsNestedObjectTemplateAndAnnotationActions(t *testing.T) {
	actions := []creator.Action{{Goto: &creator.GotoAction{Page: 1}}}
	item := creator.Path{X: 1, Y: 1, Width: 10, Height: 10, Data: "M 0 0 L 10 10", Stroke: true, Actions: actions}
	source, err := creator.Marshal(creator.Document{
		ID: "nested-navigation", PageSize: creator.PageSize{Width: 100, Height: 100},
		Pages: []creator.Page{
			{Items: []creator.Item{creator.PageBlock{Items: []creator.Item{item}}}, Templates: []creator.TemplateRef{{ID: 100}}},
			{Items: []creator.Item{item}},
		},
		Templates:   []creator.TemplatePage{{ID: 100, Items: []creator.Item{item}}},
		Annotations: []creator.AnnotationPage{{Page: 0, Items: []creator.Annotation{{ID: 200, Type: spec.AnnotationTypeHighlight, Creator: "tester", LastModDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Items: []creator.Item{item}}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Pages([]Source{{Data: smallDocument(t, "first")}, {Data: source}}, &out, PageOptions{}); err != nil {
		t.Fatal(err)
	}
	// 导出再导入为 creator 模型，以实际目的页索引核对所有对象容器。
	ofd, err := parser.NewOFD(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	assets := map[string][]byte{}
	doc, err := buildPageDocument(ofd, 0, "check", assets, func(name string) ([]byte, error) { return assets[name], nil })
	if err != nil {
		t.Fatal(err)
	}
	check := func(label string, item creator.Item) {
		t.Helper()
		value, ok := item.(creator.Path)
		if !ok {
			t.Fatalf("%s 对象类型错误: %T", label, item)
		}
		if len(value.Actions) != 1 || value.Actions[0].Goto.Page != 2 {
			t.Errorf("%s 没有跳转合并后的第 3 页: %+v", label, value.Actions)
		}
	}
	check("普通对象", doc.Pages[2].Layers[0].Items[0])
	block := doc.Pages[1].Layers[0].Items[0].(creator.PageBlock)
	check("嵌套对象", block.Items[0])
	check("模板对象", doc.Templates[0].Layers[0].Items[0])
	check("注解对象", doc.Annotations[0].Items[0].Items[0])
}

func TestPagesRepeatedSelectionRemapsNavigationOnce(t *testing.T) {
	actions := []creator.Action{{Goto: &creator.GotoAction{Page: 1}}}
	item := creator.Path{X: 1, Y: 1, Width: 10, Height: 10, Data: "M 0 0 L 10 10", Stroke: true, Actions: actions}
	source, err := creator.Marshal(creator.Document{
		ID: "duplicate-navigation", PageSize: creator.PageSize{Width: 100, Height: 100},
		Pages: []creator.Page{{Actions: actions, Items: []creator.Item{creator.PageBlock{Items: []creator.Item{item}}}}, {}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		pages  []int
		target int
	}{
		{"reordered", []int{2, 1, 1}, 0},
		{"repeated target", []int{2, 1, 1, 2}, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := Pages([]Source{{Data: source}}, &out, PageOptions{Pages: tc.pages}); err != nil {
				t.Fatal(err)
			}
			for _, index := range []int{1, 2} {
				assertPageGoto(t, out.Bytes(), index, tc.target)
			}
			ofd, err := parser.NewOFD(out.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			defer ofd.Close()
			assets := map[string][]byte{}
			doc, err := buildPageDocument(ofd, 0, "check", assets, func(name string) ([]byte, error) { return assets[name], nil })
			if err != nil {
				t.Fatal(err)
			}
			for _, index := range []int{1, 2} {
				block := doc.Pages[index].Layers[0].Items[0].(creator.PageBlock)
				path := block.Items[0].(creator.Path)
				if len(path.Actions) != 1 || path.Actions[0].Goto.Page != tc.target {
					t.Errorf("重复页 %d 的对象跳转错误: %+v", index+1, path.Actions)
				}
			}
		})
	}
}

func TestPagesKeepsBookmarkActionWhenFirstPageUnselected(t *testing.T) {
	data, err := creator.Marshal(creator.Document{
		ID: "bookmark-navigation", PageSize: creator.PageSize{Width: 100, Height: 100},
		Pages:     []creator.Page{{}, {Actions: []creator.Action{{Goto: &creator.GotoAction{Bookmark: "target"}}}}},
		Bookmarks: []creator.Bookmark{{Name: "target", Goto: creator.GotoAction{Page: 1}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Pages([]Source{{Data: data}}, &out, PageOptions{Pages: []int{2}}); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	page := ofd.Documents[0].Pages[0]
	if err := page.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	if actions := page.Actions(); actions == nil || len(actions.Action) != 1 || actions.Action[0].Goto.Bookmark == nil || actions.Action[0].Goto.Bookmark.Name != "target" {
		t.Fatalf("指向保留书签的动作被删除: %+v", actions)
	}
}

func TestPagesDropsActionsReferencingRemovedBookmarks(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "removed-bookmark", PageSize: creator.A4,
		Pages:     []creator.Page{{Actions: []creator.Action{{Goto: &creator.GotoAction{Bookmark: "removed"}}}}, {}},
		Bookmarks: []creator.Bookmark{{Name: "removed", Goto: creator.GotoAction{Page: 1}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Pages([]Source{{Data: data}}, &out, PageOptions{Pages: []int{1}}); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	page := ofd.Documents[0].Pages[0]
	if err := page.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	if actions := page.Actions(); actions != nil && len(actions.Action) != 0 {
		t.Fatal("指向被删除书签的动作未移除")
	}
}
