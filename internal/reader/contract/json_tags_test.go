package contract

import (
	"reflect"
	"testing"

	json "github.com/goccy/go-json/v2"
	"github.com/zc310/ofd/pkg/reader"
)

func TestReaderJSONTagsMatchWebContracts(t *testing.T) {
	zoom := 1.25
	expanded := false
	tests := []struct {
		name   string
		value  any
		mapped any
	}{
		{"权限", []reader.DocumentPermissions{{Scope: 2, Pages: []int{1}, PrintCopies: -1}}, Permissions([]reader.DocumentPermissions{{Scope: 2, Pages: []int{1}, PrintCopies: -1}})},
		{"空权限页", []reader.DocumentPermissions{{}}, Permissions([]reader.DocumentPermissions{{}})},
		{"字体", []reader.FontInfo{{ID: 7, TemplateID: 2, FixedWidth: true}}, Fonts([]reader.FontInfo{{ID: 7, TemplateID: 2, FixedWidth: true}})},
		{"空字体列表", []reader.FontInfo(nil), Fonts(nil)},
		{"大纲", reader.OutlineTree{Nodes: []reader.OutlineNode{{Expanded: &expanded, Dest: &reader.OutlineDest{Zoom: &zoom}}}}, Outline(reader.OutlineTree{Nodes: []reader.OutlineNode{{Expanded: &expanded, Dest: &reader.OutlineDest{Zoom: &zoom}}}})},
		{"空大纲", reader.OutlineTree{}, Outline(reader.OutlineTree{})},
		{"搜索", reader.SearchReport{Results: []reader.SearchResult{{Text: "中", Rects: []reader.Rect{{Angle: 90}}}}, Truncated: true}, Search(reader.SearchReport{Results: []reader.SearchResult{{Text: "中", Rects: []reader.Rect{{Angle: 90}}}}, Truncated: true})},
		{"空搜索", reader.SearchReport{}, Search(reader.SearchReport{})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.value)
			if err != nil {
				t.Fatal(err)
			}
			var actual any
			if err := json.Unmarshal(encoded, &actual); err != nil {
				t.Fatal(err)
			}
			encoded, err = json.Marshal(test.mapped)
			if err != nil {
				t.Fatal(err)
			}
			var expected any
			if err := json.Unmarshal(encoded, &expected); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, expected) {
				t.Fatalf("JSON 字段 = %#v，网页契约 = %#v", actual, expected)
			}
		})
	}
}

func TestTextRunJSONTagsPreserveSourceAndWebFields(t *testing.T) {
	run := reader.TextRun{Source: "template", Text: "字", TemplateID: 7, Glyphs: []reader.Glyph{{Text: "字", Angle: 90}}}
	encoded, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	var actual map[string]any
	if err := json.Unmarshal(encoded, &actual); err != nil {
		t.Fatal(err)
	}
	if actual["source"] != "template" {
		t.Fatalf("文字来源 = %#v", actual["source"])
	}
	delete(actual, "source") // 网页文字层目前不传递来源，Go JSON 仍保留该信息。
	mapped, err := json.Marshal(TextRuns([]reader.TextRun{run})[0])
	if err != nil {
		t.Fatal(err)
	}
	var expected map[string]any
	if err := json.Unmarshal(mapped, &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("文字 JSON = %#v，网页契约 = %#v", actual, expected)
	}
}

func TestPageAndDocumentInfoJSONTags(t *testing.T) {
	zoom := 1.25
	for _, test := range []struct {
		value any
		want  string
	}{
		{reader.PageInfo{Index: 1, Width: 210, Height: 297}, `{"index":1,"width":210,"height":297}`},
		{reader.DocumentInfo{DocID: "id", Title: "标题", CreationDate: "2026"}, `{"docID":"id","title":"标题","author":"","subject":"","abstract":"","creationDate":"2026","modDate":"","creator":"","version":""}`},
		{reader.FontUsage{}, `{"pages":[],"scanned":0,"truncated":false}`},
		{reader.FontUsageReport{}, `{"fonts":[],"scanned":0,"truncated":false}`},
		{reader.ViewPreferences{PageLayout: "TwoPageL", TabDisplay: "DocTitle", DocumentTitle: "标题", HideToolbar: true, ZoomMode: "Zoom", Zoom: &zoom}, `{"tab_display":"DocTitle","document_title":"标题","hide_toolbar":true,"hide_menubar":false,"hide_window_ui":false,"page_layout":"TwoPageL","zoom_mode":"Zoom","zoom":1.25}`},
		{reader.ViewPreferences{}, `{"tab_display":"","document_title":"","hide_toolbar":false,"hide_menubar":false,"hide_window_ui":false,"page_layout":"","zoom_mode":"","zoom":null}`},
	} {
		data, err := json.Marshal(test.value)
		if err != nil || string(data) != test.want {
			t.Errorf("%T JSON = %s, 错误 = %v", test.value, data, err)
		}
	}
}
