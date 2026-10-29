package preserve

import (
	"fmt"
	"strings"
	"testing"

	"github.com/beevik/etree"
)

func pageAreaFiles(areas ...string) map[string]string {
	files := map[string]string{"OFD.xml": ofdXML()}
	var pages strings.Builder
	pages.WriteString("<Pages>")
	for i, area := range areas {
		name := fmt.Sprintf("Pages/Page_%d/Content.xml", i)
		fmt.Fprintf(&pages, `<Page ID="%d" BaseLoc="%s"/>`, i+1, name)
		files["Doc_0/"+name] = pageXML(fmt.Sprint(i+1), area)
	}
	pages.WriteString("</Pages>")
	files["Doc_0/Document.xml"] = `<Document` + ns + `><CommonData><MaxUnitID>99</MaxUnitID><PageArea><PhysicalBox>0 0 100 100</PhysicalBox></PageArea></CommonData>` + pages.String() + `<Permissions/></Document>`
	return files
}

func TestPageAreaCountsInheritedSettingsAndPreservesSizes(t *testing.T) {
	const other = `<Area><PhysicalBox>0 0 200 200</PhysicalBox><ContentBox>1 2 190 180</ContentBox></Area>`
	for _, tc := range []struct {
		name       string
		areas      []string
		defaultBox string
	}{
		{"inherited majority", []string{"", "", other}, "0 0 100 100"},
		{"explicit majority", []string{"", other, other}, "0 0 200 200"},
		{"tie follows page order", []string{"", other}, "0 0 100 100"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := pageAreaFiles(tc.areas...)
			_, after := appliedChangesBytes(t, buildOFD(t, files), Options{DocType: "OFD-A"})
			doc := etree.NewDocument()
			if err := doc.ReadFromString(after["Doc_0/Document.xml"]); err != nil {
				t.Fatal(err)
			}
			common := findChild(doc.Root(), "CommonData")
			defaultArea := findChild(common, "PageArea")
			if got := findChild(defaultArea, "PhysicalBox").Text(); got != tc.defaultBox {
				t.Fatalf("默认尺寸 %q，期望 %q", got, tc.defaultBox)
			}
			for i, original := range tc.areas {
				page := etree.NewDocument()
				if err := page.ReadFromString(after[fmt.Sprintf("Doc_0/Pages/Page_%d/Content.xml", i)]); err != nil {
					t.Fatal(err)
				}
				area := findChild(page.Root(), "Area")
				if area == nil {
					area = defaultArea
				}
				want := "0 0 100 100"
				if original != "" {
					want = "0 0 200 200"
				}
				if got := findChild(area, "PhysicalBox").Text(); got != want {
					t.Errorf("第 %d 页尺寸改变: %q，期望 %q", i+1, got, want)
				}
				content := findChild(area, "ContentBox")
				if original != "" && (content == nil || content.Text() != "1 2 190 180") {
					t.Errorf("第 %d 页内容区域丢失", i+1)
				}
				if original == "" && content != nil {
					t.Errorf("第 %d 页错误继承其他页面的内容区域", i+1)
				}
			}
		})
	}
}

func TestPageAreaMovePreservesInheritedNamespaces(t *testing.T) {
	for _, prefixes := range [][2]string{{"ofd", ""}, {"", "p"}} {
		t.Run(fmt.Sprint(prefixes), func(t *testing.T) {
			files := pageAreaFiles("", `<Area><PhysicalBox>0 0 200 200</PhysicalBox></Area>`, `<Area><PhysicalBox>0 0 200 200</PhysicalBox></Area>`)
			files["Doc_0/Document.xml"] = xmlPrefix(t, files["Doc_0/Document.xml"], prefixes[0])
			for i := 0; i < 3; i++ {
				name := fmt.Sprintf("Doc_0/Pages/Page_%d/Content.xml", i)
				files[name] = xmlPrefix(t, files[name], prefixes[1])
			}
			_, after := appliedChangesBytes(t, buildOFD(t, files), Options{DocType: "OFD-A"})
			doc := etree.NewDocument()
			if err := doc.ReadFromString(after["Doc_0/Document.xml"]); err != nil {
				t.Fatal(err)
			}
			common := findChild(doc.Root(), "CommonData")
			if common == nil || findChild(common, "PageArea") == nil {
				t.Fatalf("跨文件提升后的 PageArea 命名空间丢失: %s", after["Doc_0/Document.xml"])
			}
			page := etree.NewDocument()
			if err := page.ReadFromString(after["Doc_0/Pages/Page_0/Content.xml"]); err != nil {
				t.Fatal(err)
			}
			area := findChild(page.Root(), "Area")
			if area == nil || findChild(area, "PhysicalBox") == nil || findChild(area, "PhysicalBox").Text() != "0 0 100 100" {
				t.Fatalf("跨文件补回的 Area 命名空间或尺寸丢失: %s", after["Doc_0/Pages/Page_0/Content.xml"])
			}
		})
	}
}

func xmlPrefix(t *testing.T, input, prefix string) string {
	t.Helper()
	doc := etree.NewDocument()
	if err := doc.ReadFromString(input); err != nil {
		t.Fatal(err)
	}
	var walk func(*etree.Element)
	walk = func(element *etree.Element) {
		element.Space = prefix
		for _, child := range element.ChildElements() {
			walk(child)
		}
	}
	walk(doc.Root())
	doc.Root().RemoveAttr("xmlns")
	declaration := "xmlns"
	if prefix != "" {
		declaration += ":" + prefix
	}
	doc.Root().CreateAttr(declaration, "http://www.ofdspec.org/2016")
	data, err := doc.WriteToString()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPageAreaFrequencyCountsRepeatedPageReferences(t *testing.T) {
	files := pageAreaFiles(`<Area><PhysicalBox>0 0 200 200</PhysicalBox></Area>`, "")
	files["Doc_0/Document.xml"] = strings.Replace(files["Doc_0/Document.xml"], "</Pages>", `<Page ID="3" BaseLoc="Pages/Page_1/Content.xml"/></Pages>`, 1)
	_, after := appliedChangesBytes(t, buildOFD(t, files), Options{DocType: "OFD-A"})
	doc := etree.NewDocument()
	if err := doc.ReadFromString(after["Doc_0/Document.xml"]); err != nil {
		t.Fatal(err)
	}
	area := findChild(findChild(doc.Root(), "CommonData"), "PageArea")
	if got := findChild(area, "PhysicalBox").Text(); got != "0 0 100 100" {
		t.Fatalf("复用同一 Content.xml 的两个页面应分别参与频次统计，实际默认尺寸 %s", got)
	}
}

func TestPageAreaMaterializesSharedInheritedPageOnlyOnce(t *testing.T) {
	files := pageAreaFiles("", `<Area><PhysicalBox>0 0 200 200</PhysicalBox></Area>`)
	files["Doc_0/Document.xml"] = strings.Replace(files["Doc_0/Document.xml"], "</Pages>", `<Page ID="3" BaseLoc="Pages/Page_0/Content.xml"/><Page ID="4" BaseLoc="Pages/Page_1/Content.xml"/><Page ID="5" BaseLoc="Pages/Page_1/Content.xml"/></Pages>`, 1)
	_, after := appliedChangesBytes(t, buildOFD(t, files), Options{DocType: "OFD-A"})
	page := after["Doc_0/Pages/Page_0/Content.xml"]
	if countOFDElements(t, page, "Area") != 1 {
		t.Fatalf("共享页面的旧默认 Area 应只补写一次: %s", page)
	}
}

func TestPageAreaSkipsIncompleteExplicitSettings(t *testing.T) {
	files := pageAreaFiles(`<Area><ContentBox>0 0 20 20</ContentBox></Area>`)
	var warnings []string
	_, after := appliedChangesBytes(t, buildOFD(t, files), Options{DocType: "OFD-A", OnWarning: func(message string) { warnings = append(warnings, message) }})
	if after["Doc_0/Pages/Page_0/Content.xml"] != files["Doc_0/Pages/Page_0/Content.xml"] {
		t.Fatal("缺少 PhysicalBox 的页面设置不应被提升为零尺寸默认值并删除")
	}
	if !strings.Contains(after["Doc_0/Document.xml"], "0 0 100 100") {
		t.Fatal("未知有效尺寸时应保留旧默认值")
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "有效页面设置无法确定") {
		t.Fatalf("跳过归并时应说明原因: %v", warnings)
	}
}

func TestPageAreaDoesNotPromoteInvalidBoxValues(t *testing.T) {
	for _, box := range []string{"0 0 NaN 200", "0 0 +Inf 200", "-Inf 0 200 200", "0 0 -10 200", "0 0 200 0", "0 0 0 200", "0 0 1e999 200"} {
		for _, tag := range []string{"PhysicalBox", "ContentBox"} {
			t.Run(tag+"_"+box, func(t *testing.T) {
				area := "<Area><PhysicalBox>0 0 200 200</PhysicalBox><ContentBox>1 1 190 190</ContentBox></Area>"
				old := "0 0 200 200"
				if tag == "ContentBox" {
					old = "1 1 190 190"
				}
				area = strings.Replace(area, old, box, 1)
				files := pageAreaFiles(area)
				var warnings []string
				_, after := appliedChangesBytes(t, buildOFD(t, files), Options{DocType: "OFD-A", OnWarning: func(s string) { warnings = append(warnings, s) }})
				if after["Doc_0/Pages/Page_0/Content.xml"] != files["Doc_0/Pages/Page_0/Content.xml"] {
					t.Fatal("非法设置不能被省略或重新写回")
				}
				if !strings.Contains(after["Doc_0/Document.xml"], "0 0 100 100") {
					t.Fatal("不能用非法区域替换正常默认设置")
				}
				if len(warnings) == 0 {
					t.Fatal("跳过非法区域应报告原因")
				}
			})
		}
	}
}
