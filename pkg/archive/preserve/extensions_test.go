package preserve

import (
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
)

func TestPageAreaNormalizationKeepsForeignMetadata(t *testing.T) {
	for _, area := range []string{
		`<Area xmlns:ext="urn:extension"><PhysicalBox>0 0 200 200</PhysicalBox><ext:Data>unique</ext:Data></Area>`,
		`<Area xmlns:ext="urn:extension" ext:flag="unique"><PhysicalBox>0 0 200 200</PhysicalBox></Area>`,
		`<Area xmlns:ext="urn:extension"><PhysicalBox ext:flag="unique">0 0 200 200</PhysicalBox></Area>`,
		`<Area><PhysicalBox>0 0 200 200</PhysicalBox><!--unique--></Area>`,
	} {
		t.Run(area, func(t *testing.T) {
			files := pageAreaFiles(area, strings.ReplaceAll(area, "unique", "different"))
			var warnings []string
			result, after := appliedChangesBytes(t, buildOFD(t, files), Options{DocType: "OFD-A", OnWarning: func(s string) { warnings = append(warnings, s) }})
			for _, page := range []string{"Doc_0/Pages/Page_0/Content.xml", "Doc_0/Pages/Page_1/Content.xml"} {
				if after[page] != files[page] {
					t.Errorf("含扩展内容的 Area 应保留原字节: %s", after[page])
				}
			}
			if len(warnings) == 0 {
				t.Fatal("无法安全归并时应报告跳过原因")
			}
			for _, change := range result.Changes {
				if strings.HasPrefix(change.Clause, "GB/T 42133 6.2.3 a)") || strings.HasPrefix(change.Clause, "GB/T 42133 6.2.3 b)") {
					t.Fatalf("不应记录未进行的归并: %+v", change)
				}
			}
		})
	}
}

func TestPageAreaNormalizationKeepsDefaultMetadata(t *testing.T) {
	files := pageAreaFiles(areaA4, areaA4)
	files["Doc_0/Document.xml"] = strings.Replace(files["Doc_0/Document.xml"], "</PageArea>", `<ext:Data xmlns:ext="urn:extension">default metadata</ext:Data></PageArea>`, 1)
	_, after := appliedChangesBytes(t, buildOFD(t, files), Options{DocType: "OFD-A"})
	if !strings.Contains(after["Doc_0/Document.xml"], "default metadata") || !strings.Contains(after["Doc_0/Document.xml"], "0 0 100 100") {
		t.Fatalf("不应以纯数值页面设置覆盖带扩展的默认设置: %s", after["Doc_0/Document.xml"])
	}
	if after["Doc_0/Pages/Page_0/Content.xml"] != files["Doc_0/Pages/Page_0/Content.xml"] {
		t.Fatal("未成功归并默认值时应保留页面 Area")
	}
}

func TestPruneActionsKeepsForeignChildrenAndAttributes(t *testing.T) {
	for _, tc := range []struct{ name, attrs, content string }{
		{"foreign child only", ` xmlns:ext="urn:extension"`, `<ext:Keep>keep me</ext:Keep>`},
		{"foreign child with action", ` xmlns:ext="urn:extension"`, actionXML(false) + `<ext:Keep>keep me</ext:Keep>`},
		{"foreign attribute", ` xmlns:ext="urn:extension" ext:flag="keep me"`, actionXML(false)},
		{"comment", "", actionXML(false) + `<!--keep me-->`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := pageAreaFiles("")
			files["Doc_0/Document.xml"] = strings.Replace(files["Doc_0/Document.xml"], "</Document>", "<Actions"+tc.attrs+">"+tc.content+"</Actions></Document>", 1)
			result, after := appliedChangesBytes(t, buildOFD(t, files), Options{DocType: "OFD-A"})
			xml := after["Doc_0/Document.xml"]
			wantContainers := 1
			if tc.name == "comment" {
				wantContainers = 0
			}
			if !strings.Contains(xml, "keep me") || countOFDElements(t, xml, "Actions") != wantContainers || countOFDElements(t, xml, "URI") != 0 {
				t.Fatalf("清理动作后应保留容器中的外来内容: %s", xml)
			}
			count := 0
			for _, change := range result.Changes {
				if change.Clause == "GB/T 42133 6.2.2 c)" {
					count += change.Count
				}
			}
			want := strings.Count(tc.content, "<Action ")
			if count != want {
				t.Fatalf("动作删除计数 %d，期望 %d", count, want)
			}
		})
	}
}

func TestPruneActionsKeepsCommentsWithoutCreatingEmptyContainer(t *testing.T) {
	source, err := creator.Marshal(creator.Document{ID: "comment-actions", PageSize: creator.PageSize{Width: 100, Height: 100}, Pages: []creator.Page{{}}, Actions: []creator.Action{{URI: &creator.URIAction{URI: "https://example.com"}}}})
	if err != nil {
		t.Fatal(err)
	}
	files := readEntries(t, source)
	files["Doc_0/Document.xml"] = strings.Replace(files["Doc_0/Document.xml"], "<ofd:Actions>", "<ofd:Actions><!--keep me--><?vendor keep?>", 1)
	_, after := appliedChangesBytes(t, buildOFD(t, files), Options{DocType: "OFD-A", Validate: true})
	xml := after["Doc_0/Document.xml"]
	if countOFDElements(t, xml, "Actions") != 0 || !strings.Contains(xml, "<!--keep me-->") || !strings.Contains(xml, "<?vendor keep?>") {
		t.Fatalf("应保留注释和处理指令并移除空容器: %s", xml)
	}
}
