//go:build js && wasm

package main

import (
	"math"
	"syscall/js"
	"testing"
	"time"

	readercontract "github.com/zc310/ofd/internal/reader/contract"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/reader"
)

func TestOutlineContractConvertsToJS(t *testing.T) {
	value, err := jsonValue(reader.OutlineTree{Nodes: []reader.OutlineNode{{Title: "章节"}}})
	if err != nil {
		t.Fatal(err)
	}
	if value.Get("nodes").Index(0).Get("title").String() != "章节" ||
		value.Get("nodes").Index(0).Get("dest").Type() != js.TypeNull ||
		value.Get("bookmarks").Length() != 0 || value.Get("nodes").Index(0).Get("children").Length() != 0 {
		t.Fatal("大纲 JS 转换错误")
	}
}

func TestPageLinkContractConvertsNestedActionsToJS(t *testing.T) {
	links := readercontract.PageLinks([]reader.PageLink{{Boundary: reader.AnnotationBoundary{X: 1, Width: 2, Height: 3}, TargetPage: 1,
		Actions: []reader.PageLink{{URI: "https://example.test", TargetPage: -1}}}})
	value, err := jsonValue(links)
	if err != nil {
		t.Fatal(err)
	}
	first := value.Index(0)
	if first.Get("boundary").Get("x").Int() != 1 || first.Get("target_page").Int() != 1 || first.Get("actions").Index(0).Get("uri").String() != "https://example.test" || !first.Get("actions").Index(0).Get("boundary").IsNull() || !first.Get("appearance_boundary").IsUndefined() {
		t.Fatalf("链接 JSON 不符合网页契约: %v", first)
	}
}

func TestTextContractConvertsToJS(t *testing.T) {
	value, err := jsonValue(readercontract.TextRuns([]reader.TextRun{{Source: "template", Text: "中", TemplateID: 7, Glyphs: []reader.Glyph{{Text: "中", Angle: 90}}}, {}}))
	if err != nil {
		t.Fatal(err)
	}
	empty, err := jsonValue(readercontract.TextRuns(nil))
	if err != nil {
		t.Fatal(err)
	}
	if value.Index(0).Get("template_id").Int() != 7 || value.Index(0).Get("glyphs").Index(0).Get("angle").Int() != 90 ||
		!value.Index(0).Get("source").IsUndefined() || value.Index(1).Get("glyphs").Length() != 0 || empty.Length() != 0 {
		t.Fatal("文字 JS 转换错误")
	}
}

func TestSearchContractConvertsToJS(t *testing.T) {
	value, err := jsonValue(reader.SearchReport{Scanned: 2, Truncated: true,
		Results: []reader.SearchResult{{Start: 1, End: 2, Rects: []reader.Rect{{Angle: 90}}}, {}}})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := jsonValue(reader.SearchReport{})
	if err != nil {
		t.Fatal(err)
	}
	results := value.Get("results")
	if !value.Get("truncated").Bool() || value.Get("scanned").Int() != 2 ||
		results.Index(0).Get("rects").Index(0).Get("angle").Int() != 90 || results.Index(1).Get("rects").Length() != 0 ||
		empty.Get("results").Length() != 0 {
		t.Fatal("搜索 JS 转换错误")
	}
}

func TestPreferencesContractConvertsToJS(t *testing.T) {
	zoom := 1.25
	value, err := jsonValue(reader.ViewPreferences{Zoom: &zoom, HideToolbar: true})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := jsonValue(reader.ViewPreferences{})
	if err != nil {
		t.Fatal(err)
	}
	if value.Get("zoom").Float() != zoom || !value.Get("hide_toolbar").Bool() ||
		empty.Get("zoom").Type() != js.TypeNull {
		t.Fatal("显示偏好 JS 转换错误")
	}
	nan := math.NaN()
	if _, err := jsonValue(reader.ViewPreferences{Zoom: &nan}); err == nil {
		t.Fatal("无法编码的值必须返回错误")
	}
}

func TestPermissionsContractConvertsToJS(t *testing.T) {
	value, err := permissionsValue([]reader.DocumentPermissions{{Scope: 2, PrintCopies: -1, AccessReason: "expired"}})
	if err != nil {
		t.Fatal(err)
	}
	item := value.Index(0)
	if item.Get("scope").Int() != 2 || item.Get("print_copies").Int() != -1 ||
		item.Get("access_reason").String() != "expired" || item.Get("pages").Length() != 0 {
		t.Fatal("权限 JS 转换错误")
	}
	empty, err := permissionsValue(nil)
	if err != nil || empty.Length() != 0 {
		t.Fatalf("空权限 = %v, %v", empty, err)
	}
}

func TestFontsContractConvertsToJS(t *testing.T) {
	fonts, err := jsonValue([]reader.FontInfo{{ID: 7, TemplateID: 3, Scope: 2, Name: "字体", Embedded: true, FixedWidth: true}})
	if err != nil {
		t.Fatal(err)
	}
	font := fonts.Index(0)
	if font.Get("template_id").Int() != 3 || font.Get("name").String() != "字体" ||
		!font.Get("embedded").Bool() || !font.Get("fixed_width").Bool() || !font.Get("data").IsUndefined() {
		t.Fatal("字体声明 JS 转换错误")
	}
	empty, err := jsonValue([]reader.FontInfo(nil))
	if err != nil || empty.Length() != 0 {
		t.Fatalf("空字体列表 = %v, %v", empty, err)
	}
}

func TestPageAndDocumentInfoContractsConvertToJS(t *testing.T) {
	page, err := jsonValue(reader.PageInfo{Index: 2, Width: 210.5, Height: 297})
	if err != nil {
		t.Fatal(err)
	}
	if page.Get("index").Int() != 2 || page.Get("width").Float() != 210.5 || page.Get("height").Int() != 297 {
		t.Fatal("页面信息 JS 转换错误")
	}
	pages, err := jsonValue([]reader.PageInfo(nil))
	if err != nil || pages.Length() != 0 {
		t.Fatalf("空页面列表 = %v, %v", pages, err)
	}
	info, err := jsonValue(reader.DocumentInfo{DocID: "doc", Title: "标题", CreationDate: "2026"})
	if err != nil {
		t.Fatal(err)
	}
	if info.Get("docID").String() != "doc" || info.Get("title").String() != "标题" || info.Get("creationDate").String() != "2026" {
		t.Fatal("文档信息 JS 转换错误")
	}
}

func TestFontUsageContractsConvertToJS(t *testing.T) {
	usage, err := jsonValue(reader.FontUsage{Scanned: 2, Truncated: true})
	if err != nil {
		t.Fatal(err)
	}
	if usage.Get("pages").Length() != 0 || usage.Get("scanned").Int() != 2 || !usage.Get("truncated").Bool() {
		t.Fatal("字体使用统计 JS 转换错误")
	}
	report, err := jsonValue(reader.FontUsageReport{Fonts: []reader.FontUsageSummary{{ID: 7, Scope: 2, TemplateID: 3, Pages: []int{0, 4}}, {}}, Scanned: 5})
	if err != nil {
		t.Fatal(err)
	}
	fonts := report.Get("fonts")
	if fonts.Index(0).Get("template_id").Int() != 3 || fonts.Index(0).Get("pages").Index(1).Int() != 4 ||
		fonts.Index(1).Get("pages").Length() != 0 || report.Get("scanned").Int() != 5 {
		t.Fatal("批量字体使用统计 JS 转换错误")
	}
	empty, err := jsonValue(reader.FontUsageReport{})
	if err != nil || empty.Get("fonts").Length() != 0 {
		t.Fatalf("空字体使用统计 = %v, %v", empty, err)
	}
}

func TestReaderMetadataMethodsUseJSONContracts(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "contract", Title: "测试文档", Pages: []creator.Page{{}},
		Fonts:       []creator.Font{{Name: "ExampleFont"}},
		Attachments: []creator.Attachment{{ID: "attach7", Name: "说明.txt", FileName: "note.txt", Data: []byte("内容")}},
		Media:       []creator.Media{{ID: 200, Type: "Audio", Format: "wav", Name: "sound.wav", Data: []byte("audio")}},
		Annotations: []creator.AnnotationPage{{Page: 0, Items: []creator.Annotation{{ID: 42, Type: "Stamp", Creator: "测试", LastModDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Remark: "测试备注", Boundary: &creator.Box{X: 5, Y: 6, Width: 10, Height: 10}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	document, err := reader.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = document.Close() })
	app := &wasmApp{reader: document}
	for _, test := range []struct {
		name  string
		call  func(js.Value, []js.Value) any
		args  []js.Value
		check func(js.Value) bool
	}{
		{"信息", app.info, nil, func(v js.Value) bool {
			return v.Get("title").String() == "测试文档" && v.Get("fonts").Length() == 1 && v.Get("permissions").Length() == 1 && v.Get("security").Get("state").String() == "none"
		}},
		{"页面列表", app.pages, nil, func(v js.Value) bool { return v.Length() == 1 && v.Index(0).Get("index").Int() == 0 }},
		{"页面信息", app.pageInfo, []js.Value{js.ValueOf(0)}, func(v js.Value) bool { return v.Get("index").Int() == 0 && v.Get("width").Float() > 0 }},
		{"图层清单", app.layers, []js.Value{js.ValueOf(0)}, func(v js.Value) bool { return v.Length() >= 0 }},
		{"字体统计", app.fontUsageAll, nil, func(v js.Value) bool { return v.Get("scanned").Int() == 1 && v.Get("fonts").Length() == 0 }},
		{"资源统计", app.stats, nil, func(v js.Value) bool { return v.Get("fonts").Int() == 1 && v.Get("annotation_pages").Int() == 1 }},
		{"附件清单", app.attachments, nil, func(v js.Value) bool {
			return v.Length() == 1 && v.Index(0).Get("name").String() == "说明.txt" && v.Index(0).Get("exists").Bool()
		}},
		{"资源清单", app.media, nil, func(v js.Value) bool {
			return v.Length() == 1 && v.Index(0).Get("name").String() == "sound.wav" && v.Index(0).Get("exists").Bool() && v.Index(0).Get("size").Int() == 5
		}},
		{"单页注解", app.annotations, []js.Value{js.ValueOf(0)}, func(v js.Value) bool {
			return v.Length() == 1 && v.Index(0).Get("remark").String() == "测试备注" && v.Index(0).Get("boundary").Get("x").Int() == 5 && v.Index(0).Get("dest").IsNull()
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			value, ok := test.call(js.Undefined(), test.args).(js.Value)
			if !ok || !value.Get("error").IsUndefined() {
				t.Fatalf("返回值 = %v", value)
			}
			if !test.check(value) {
				t.Fatalf("结果不符合契约: %v", value)
			}
		})
	}
}
