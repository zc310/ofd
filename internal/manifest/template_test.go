package manifest

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"go.yaml.in/yaml/v3"
)

func templateTestManifest() Manifest {
	return Manifest{Version: 1, Document: Document{ID: "template-test"}, Pages: []Page{{Items: []Item{{
		Type: "text", X: 10, Y: 10, Width: 80, Height: 10, Size: 4,
		Value: "OLD SECRET", TextCodes: []TextCode{{Value: "OLD POSITIONED SECRET", DeltaX: []float64{9}}},
		CGTransforms: []CGTransform{{CodeCount: 1, GlyphCount: 1, Glyphs: []int{3}}},
		Bind:         &Binding{Path: "value"},
	}}}}}
}

func TestTemplateRepeatsPagesWithoutMutatingOriginalAndRemapsReferences(t *testing.T) {
	m := templateTestManifest()
	m.Pages[0].Repeat = "records"
	m.Pages[0].Items[0].Actions = []Action{{Goto: &GotoAction{Page: 0}}}
	m.Pages = append(m.Pages, Page{Items: []Item{{Type: "text", Width: 80, Height: 10, Bind: &Binding{Path: "$.company"}}}})
	m.Document.Bookmarks = []Bookmark{{Name: "tail", Goto: GotoAction{Page: 1}}}
	m.Document.Annotations = []AnnotationPage{{Page: 0, Items: []Annotation{{ID: 800, Type: "Stamp", Creator: "test", LastModDate: "2026-10-26", Items: []Item{{Type: "text", Width: 80, Height: 10, Bind: &Binding{Path: "value"}}}}}}}
	before, _ := json.Marshal(m)
	for _, format := range []string{"yaml", "json"} {
		t.Run(format, func(t *testing.T) {
			encoded, err := json.Marshal(m)
			if format == "yaml" {
				encoded, err = yaml.Marshal(m)
			}
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := Parse(encoded, format)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := loaded.RenderTemplate(strings.NewReader(`{"company":"Shared","records":[{"value":"A & <B>"},{"value":9007199254740993}]}`), "", "", TemplateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(doc.Pages) != 3 || doc.Bookmarks[0].Goto.Page != 2 {
				t.Fatalf("页序或书签错误: %+v", doc)
			}
			for i, expected := range []string{"A & <B>", "9007199254740993", "Shared"} {
				text := doc.Pages[i].Items[0].(creator.Text)
				if text.Value != "" || len(text.CGTransforms) != 0 || len(text.TextCodes) != 1 || text.TextCodes[0].Value != expected {
					t.Fatalf("第 %d 页替换失败: %+v", i, text)
				}
				if i < 2 && text.Actions[0].Goto.Page != i {
					t.Fatalf("自链接未定位当前重复页: %+v", text.Actions)
				}
			}
			if len(doc.Annotations) != 2 || doc.Annotations[1].Page != 1 || doc.Annotations[0].Items[0].ID == doc.Annotations[1].Items[0].ID {
				t.Fatalf("批注未重复或编号未重新分配: %+v", doc.Annotations)
			}
			if _, err := creator.Marshal(doc); err != nil {
				t.Fatal(err)
			}
		})
	}
	after, _ := json.Marshal(m)
	if !bytes.Equal(before, after) {
		t.Fatal("模板被修改")
	}
}

func TestTemplateTextContainmentUsesFontMetrics(t *testing.T) {
	for _, overflow := range []string{"error", "wrap", "shrink", "ellipsis"} {
		t.Run(overflow, func(t *testing.T) {
			m := templateTestManifest()
			item := &m.Pages[0].Items[0]
			item.Width, item.Height = 24, 20
			item.Bind.Overflow, item.Bind.Align = overflow, "right"
			if overflow == "shrink" {
				item.Bind.MinSize = 1
			}
			doc, err := m.RenderTemplate(strings.NewReader(`{"value":"Alpha Beta Gamma Delta"}`), "", "", TemplateOptions{})
			if overflow == "error" {
				if err == nil || !strings.Contains(err.Error(), "宽度") {
					t.Fatalf("预期溢出定位，得到 %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			text := doc.Pages[0].Items[0].(creator.Text)
			if overflow == "wrap" && len(text.TextCodes) < 2 {
				t.Fatalf("未换行: %+v", text)
			}
			if overflow == "shrink" && (text.Size >= 4 || text.Size < 1) {
				t.Fatalf("缩小字号错误: %v", text.Size)
			}
			if overflow == "ellipsis" && !strings.HasSuffix(text.TextCodes[0].Value, "…") {
				t.Fatalf("未添加省略号: %+v", text)
			}
			for _, code := range text.TextCodes {
				if *code.X < -1e-7 || *code.Y > text.Height {
					t.Fatalf("文字越界: %+v", code)
				}
			}
			if _, err := creator.Marshal(doc); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTemplateDefaultsAndOptionalFieldsRemoveOldContent(t *testing.T) {
	m := templateTestManifest()
	optional := false
	m.Pages[0].Items[0].Bind.Required = &optional
	defaultValue := "Fallback"
	copy := m.Pages[0].Items[0]
	copy.Bind = &Binding{Path: "other", Default: &defaultValue}
	m.Pages[0].Items = append(m.Pages[0].Items, copy)
	doc, err := m.RenderTemplate(strings.NewReader(`{"value":null}`), "", "", TemplateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages[0].Items) != 1 || doc.Pages[0].Items[0].(creator.Text).TextCodes[0].Value != "Fallback" {
		t.Fatalf("缺省行为不正确: %+v", doc.Pages)
	}
}

func TestTemplateRejectsInvalidDataAndLayoutWithFieldLocation(t *testing.T) {
	tests := []struct {
		name, data, match string
		edit              func(*Manifest)
	}{
		{"missing", `{}`, "value", nil},
		{"null-root", `null`, "JSON 对象", nil},
		{"array-root", `[]`, "解析模板", nil},
		{"extra-data", `{} {}`, "一个 JSON", nil},
		{"object-text", `{"value":{}}`, "字符串", nil},
		{"missing-glyph", `{"value":"中文"}`, "缺少字符", nil},
		{"newline", `{"value":"A\nB"}`, "wrap", nil},
		{"height", `{"value":"A"}`, "高度", func(m *Manifest) { m.Pages[0].Items[0].Height = 0.1 }},
		{"minimum", `{"value":"A"}`, "min_size", func(m *Manifest) { m.Pages[0].Items[0].Bind.Overflow = "shrink" }},
		{"lines", `{"value":"A\nB"}`, "max_lines", func(m *Manifest) { m.Pages[0].Items[0].Bind.Overflow = "wrap"; m.Pages[0].Items[0].Bind.MaxLines = 1 }},
		{"repeat-type", `{"records":{}}`, "需要数组", func(m *Manifest) { m.Pages[0].Repeat = "records" }},
		{"empty-all", `{"records":[]}`, "没有页面", func(m *Manifest) { m.Pages[0].Repeat = "records" }},
		{"invalid-empty", `{"records":[]}`, "overflow", func(m *Manifest) { m.Pages[0].Repeat = "records"; m.Pages[0].Items[0].Bind.Overflow = "typo" }},
		{"logical-font", `{"value":"A"}`, "完整字体", func(m *Manifest) { m.Pages[0].Items[0].Font = "Logical" }},
		{"invalid-path", `{"value":"A"}`, "点分隔", func(m *Manifest) { m.Pages[0].Items[0].Bind.Path = "$.value..name" }},
		{"unknown-meta", `{"value":"A"}`, "内置字段", func(m *Manifest) { m.Pages[0].Items[0].Bind.Path = "@typo" }},
		{"mixed-page", `{"value":""}`, "layers", func(m *Manifest) { m.Pages[0].Layers = []Layer{{Type: "Body"}} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := templateTestManifest()
			if tt.edit != nil {
				tt.edit(&m)
			}
			_, err := m.RenderTemplate(strings.NewReader(tt.data), "", "", TemplateOptions{})
			if err == nil || !strings.Contains(err.Error(), tt.match) {
				t.Fatalf("预期 %q，得到 %v", tt.match, err)
			}
		})
	}
}

func TestTemplateExplicitTextDeltasIncludeHorizontalScale(t *testing.T) {
	m := templateTestManifest()
	plain, err := m.RenderTemplate(strings.NewReader(`{"value":"Scale"}`), "", "", TemplateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	m.Pages[0].Items[0].HScale = 2
	scaled, err := m.RenderTemplate(strings.NewReader(`{"value":"Scale"}`), "", "", TemplateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	a := plain.Pages[0].Items[0].(creator.Text).TextCodes[0].DeltaX
	b := scaled.Pages[0].Items[0].(creator.Text).TextCodes[0].DeltaX
	for i := range a {
		if b[i] != a[i]*2 {
			t.Fatalf("显式 DeltaX 未计入水平缩放: %v -> %v", a, b)
		}
	}
}

func TestTemplateSharedResourcesKeepIdenticalPageFilenamesSeparate(t *testing.T) {
	m := templateTestManifest()
	m.Pages = nil
	image := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aN1cAAAAASUVORK5CYII="
	for p := 0; p < 2; p++ {
		rawID, imageID := uint64(9001+p), uint64(10001+p)
		raw := fmt.Sprintf(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><MultiMedias><MultiMedia ID="%d" Type="Image" Format="PNG"><MediaFile>same.png</MediaFile></MultiMedia></MultiMedias></Res>`, rawID)
		m.Pages = append(m.Pages, Page{Repeat: "records", Resources: []PageResource{
			{DataBase64: base64.StdEncoding.EncodeToString([]byte(raw)), Files: []ResourceFile{{Path: "same.png", DataBase64: image}}},
			{Images: []PageImage{{ID: imageID, Name: "same.png", Format: "PNG", DataBase64: image}}},
		}, Items: []Item{
			{Type: "image", X: 10, Y: 10, Width: 10, Height: 10, ResourceID: rawID},
			{Type: "image", X: 30, Y: 10, Width: 10, Height: 10, ResourceID: imageID},
		}})
	}
	doc, err := m.RenderTemplate(strings.NewReader(`{"records":[{},{}]}`), "", "", TemplateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages) != 4 {
		t.Fatalf("页数 %d", len(doc.Pages))
	}
	if _, err := creator.Marshal(doc); err != nil {
		t.Fatalf("不同原型页的同名资源发生冲突: %v", err)
	}
}

func TestTemplateImageBindingReplacesReferencesAndRestrictsResourcePaths(t *testing.T) {
	for _, value := range []string{`"logo.png"`, `{"file":"logo.png","format":"PNG"}`, fmt.Sprintf(`{"data_base64":%q}`, base64.StdEncoding.EncodeToString([]byte("image")))} {
		m := templateTestManifest()
		m.Pages[0].Items[0].Type = "image"
		m.Pages[0].Items[0].ResourceID = 900
		doc, err := m.RenderTemplate(strings.NewReader(`{"value":`+value+`}`), "", "", TemplateOptions{BuildOptions: BuildOptions{LoadAsset: func(name string) ([]byte, error) {
			if name != "logo.png" {
				t.Fatalf("资源路径 %q", name)
			}
			return []byte("image"), nil
		}}})
		if err != nil {
			t.Fatal(err)
		}
		image := doc.Pages[0].Items[0].(creator.Image)
		if image.ResourceID != 0 || string(image.Data) != "image" {
			t.Fatalf("图片未替换: %+v", image)
		}
	}
	for _, value := range []string{`"../secret.png"`, `"/secret.png"`, `{"file":"a.png","data_base64":"aA=="}`, `{"unknown":"x"}`} {
		m := templateTestManifest()
		m.Pages[0].Items[0].Type = "image"
		_, err := m.RenderTemplate(strings.NewReader(`{"value":`+value+`}`), "", "", TemplateOptions{})
		if err == nil {
			t.Fatalf("未拒绝图片数据 %s", value)
		}
	}
}

func TestTemplateEmptyPageAndPageLimitPreservePageMapping(t *testing.T) {
	m := templateTestManifest()
	m.Pages[0].Repeat = "records"
	m.Pages = append(m.Pages, Page{})
	doc, err := m.RenderTemplate(strings.NewReader(`{"records":[]}`), "", "", TemplateOptions{})
	if err != nil || len(doc.Pages) != 1 {
		t.Fatalf("空数组未跳过: %v %+v", err, doc.Pages)
	}
	_, err = m.RenderTemplate(strings.NewReader(`{"records":[{"value":"A"},{"value":"B"}]}`), "", "", TemplateOptions{MaxPages: 2})
	if err == nil || !strings.Contains(err.Error(), "max_pages") {
		t.Fatalf("页数限制无效: %v", err)
	}
	m.Document.Bookmarks = []Bookmark{{Name: "removed", Goto: GotoAction{Page: 0}}}
	_, err = m.RenderTemplate(strings.NewReader(`{"records":[]}`), "", "", TemplateOptions{})
	if err == nil || !strings.Contains(err.Error(), "移除") {
		t.Fatalf("跳转指向被移除的页面应报错: %v", err)
	}
}

func TestOrdinaryBuildRejectsUnresolvedTemplateAndStrictParsing(t *testing.T) {
	m := templateTestManifest()
	if _, err := m.Build("", ""); err == nil {
		t.Fatal("普通创建不能忽略 bind")
	}
	m.Pages[0].Items = nil
	m.Pages[0].Repeat = "records"
	if _, err := m.Build("", ""); err == nil {
		t.Fatal("普通创建不能忽略 repeat")
	}
	for _, input := range []struct{ format, data string }{
		{"yaml", "version: 1\npages:\n  - items:\n      - bind: {path: value, typo: true}\n"},
		{"json", `{"version":1,"pages":[{"items":[{"bind":{"path":"value","typo":true}}]}]}`},
		{"yaml", "version: 1\n---\nversion: 1\n"},
		{"json", `{"version":1} {"version":1}`},
	} {
		if _, err := Parse([]byte(input.data), input.format); err == nil {
			t.Fatalf("未拒绝 %s", input.data)
		}
	}
}
