//go:build js && wasm

package main

import (
	"syscall/js"
	"testing"

	readercontract "github.com/zc310/ofd/internal/reader/contract"
	"github.com/zc310/ofd/pkg/creator"
	ofdreader "github.com/zc310/ofd/pkg/reader"
)

func TestPageLinkValuePreservesRegionSequenceAndMovieAppearance(t *testing.T) {
	link := ofdreader.PageLink{Scope: 2, Page: 3, URI: "https://example.test/", Boundary: ofdreader.AnnotationBoundary{X: 1, Y: 2, Width: 3, Height: 4}, Region: "M0 0L1 0L0 1Z",
		Actions: []ofdreader.PageLink{
			{Scope: 2, TemplateID: 10, Page: 3, MediaID: 7, MediaKind: "sound", Synchronous: true, TargetPage: -1},
			{Scope: 2, Page: 3, MediaID: 8, MediaKind: "movie", AppearanceBoundary: &ofdreader.AnnotationBoundary{Width: 20, Height: 30}},
		},
	}
	values, err := jsonValue(readercontract.PageLinks([]ofdreader.PageLink{link}))
	if err != nil {
		t.Fatal(err)
	}
	value := values.Index(0)
	if value.Get("region").String() != link.Region || value.Get("actions").Length() != 2 {
		t.Fatalf("热区或动作列表未传给浏览器：%v", value)
	}
	first := value.Get("actions").Index(0)
	if !first.Get("synchronous").Bool() || first.Get("scope").Int() != 2 || first.Get("page").Int() != 3 || first.Get("template_id").Int() != 10 {
		t.Fatalf("声音同步与作用域丢失：%v", first)
	}
	second := value.Get("actions").Index(1)
	if second.Get("appearance_boundary").Get("width").Int() != 20 || second.Get("media_id").Int() != 8 {
		t.Fatalf("影片外观丢失：%v", second)
	}
}

func TestActionMethodsUseSharedJSONContract(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "action-contract", Pages: []creator.Page{
		{Actions: []creator.Action{{Event: creator.ActionEventPO, URI: &creator.URIAction{URI: "https://example.test/entry"}}},
			Items: []creator.Item{creator.Path{X: 1, Y: 2, Width: 10, Height: 10, Data: "M 0 0 L 10 10", Stroke: true,
				Actions: []creator.Action{{Event: creator.ActionEventClick, Goto: &creator.GotoAction{Page: 1}}}}}}, {},
	}})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := ofdreader.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = doc.Close() })
	app := &wasmApp{reader: doc}
	for _, test := range []struct {
		name  string
		call  func(js.Value, []js.Value) any
		args  []js.Value
		field string
		want  string
	}{
		{"全部链接", app.pageLinks, nil, "target_page", "1"},
		{"单页链接", app.pageLinksForPage, []js.Value{js.ValueOf(0)}, "target_page", "1"},
		{"页面进入动作", app.pageEntryActionsForPage, []js.Value{js.ValueOf(0)}, "uri", "https://example.test/entry"},
	} {
		t.Run(test.name, func(t *testing.T) {
			value, ok := test.call(js.Undefined(), test.args).(js.Value)
			if !ok {
				t.Fatal("动作结果不是 JS 值")
			}
			if value.Length() != 1 {
				t.Fatalf("动作结果 = %v", value)
			}
			field := value.Index(0).Get(test.field)
			if (test.field == "target_page" && field.Int() != 1) || (test.field == "uri" && field.String() != test.want) {
				t.Fatalf("动作字段 = %v", field)
			}
			if !value.Index(0).Get("volume").IsNull() || value.Index(0).Get("actions").Type() != js.TypeUndefined {
				t.Fatalf("动作可选字段 = %v", value.Index(0))
			}
		})
	}
	for _, value := range []any{app.pageMediaActions(js.Undefined(), nil), app.pageMediaActionsForPage(js.Undefined(), []js.Value{js.ValueOf(0)})} {
		array, ok := value.(js.Value)
		if !ok {
			t.Fatal("媒体动作结果不是 JS 值")
		}
		if array.Length() != 0 {
			t.Fatalf("空媒体动作 = %v", array)
		}
	}
}
