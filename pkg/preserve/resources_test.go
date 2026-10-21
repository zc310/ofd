package preserve

import (
	"slices"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
)

func TestPreserveCleansCompositeGraphicActionsInAllResourceScopes(t *testing.T) {
	for _, scope := range []string{"public", "document", "page", "template"} {
		t.Run(scope, func(t *testing.T) {
			files := pageAreaFiles("")
			resource := "Doc_0/Resources/custom.xml"
			files[resource] = `<Res` + ns + `><CompositeGraphicUnits><CompositeGraphicUnit ID="100" Width="10" Height="10"><Content><PathObject ID="101" Boundary="0 0 10 10"><Actions>` + actionXML(false) + actionXML(true) + `</Actions><AbbreviatedData>M 0 0 L 10 10</AbbreviatedData></PathObject></Content></CompositeGraphicUnit></CompositeGraphicUnits></Res>`
			switch scope {
			case "public", "document":
				tag := "PublicRes"
				if scope == "document" {
					tag = "DocumentRes"
				}
				files["Doc_0/Document.xml"] = strings.Replace(files["Doc_0/Document.xml"], "</CommonData>", "<"+tag+">/"+resource+"</"+tag+"></CommonData>", 1)
			case "page":
				files["Doc_0/Pages/Page_0/Content.xml"] = strings.Replace(files["Doc_0/Pages/Page_0/Content.xml"], "<Content>", `<PageRes>../../Resources/custom.xml</PageRes><Content>`, 1)
			case "template":
				files["Doc_0/Document.xml"] = strings.Replace(files["Doc_0/Document.xml"], "</CommonData>", `<TemplatePage ID="10" BaseLoc="Templates/page.xml"/></CommonData>`, 1)
				files["Doc_0/Templates/page.xml"] = `<Page` + ns + `><PageRes>../Resources/custom.xml</PageRes></Page>`
			}
			result, after := appliedChangesBytes(t, buildOFD(t, files), Options{DocType: "OFD-A"})
			if countOFDElements(t, after[resource], "URI") != 0 || countOFDElements(t, after[resource], "Goto") != 1 {
				t.Fatalf("资源图元的非 Goto 动作未清理: %s", after[resource])
			}
			if !slices.Contains(result.Entries, resource) {
				t.Fatalf("路径清单遗漏资源: %v", result.Entries)
			}
			count := 0
			for _, change := range result.Changes {
				if change.Entry == resource && change.Clause == "GB/T 42133 6.3.3 b)" {
					count += change.Count
				}
			}
			if count != 1 {
				t.Fatalf("资源动作改动应记录一次，实际 %d", count)
			}
		})
	}
}

func TestPreserveCleansCreatorCompositeWithValidation(t *testing.T) {
	edit := true
	source, err := creator.Marshal(creator.Document{
		ID: "composite-actions", PageSize: creator.PageSize{Width: 100, Height: 100}, Permissions: &creator.Permissions{Edit: &edit},
		Pages:      []creator.Page{{Items: []creator.Item{creator.Composite{X: 1, Y: 1, Width: 10, Height: 10, ResourceID: 100}}}},
		Composites: []creator.CompositeGraphicUnit{{ID: 100, Width: 10, Height: 10, Items: []creator.Item{creator.Path{X: 0, Y: 0, Width: 10, Height: 10, Data: "M 0 0 L 10 10", Stroke: true, Actions: []creator.Action{{URI: &creator.URIAction{URI: "https://example.com"}}}}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, after := appliedChangesBytes(t, source, Options{DocType: "OFD-A", Validate: true})
	for name, xml := range after {
		if strings.Contains(xml, "https://example.com") {
			t.Errorf("转换后资源 %s 仍包含 URI 动作", name)
		}
	}
}

func TestPreserveCleansSharedResourcesOnlyOnce(t *testing.T) {
	files := pageAreaFiles("", "")
	files["Doc_0/shared.xml"] = `<Res` + ns + `><CompositeGraphicUnits><CompositeGraphicUnit ID="100" Width="10" Height="10"><Content><PathObject ID="101" Boundary="0 0 10 10"><Actions>` + actionXML(false) + `</Actions><AbbreviatedData>M 0 0 L 10 10</AbbreviatedData></PathObject></Content></CompositeGraphicUnit></CompositeGraphicUnits></Res>`
	files["Doc_0/Document.xml"] = strings.Replace(files["Doc_0/Document.xml"], "</CommonData>", `<PublicRes>shared.xml</PublicRes><DocumentRes>shared.xml</DocumentRes></CommonData>`, 1)
	result, _ := appliedChangesBytes(t, buildOFD(t, files), Options{DocType: "OFD-A"})
	count := 0
	for _, change := range result.Changes {
		if change.Entry == "Doc_0/shared.xml" {
			count += change.Count
		}
	}
	if count != 1 {
		t.Fatalf("多次声明同一资源时不应重复清理: %+v", result.Changes)
	}
}
