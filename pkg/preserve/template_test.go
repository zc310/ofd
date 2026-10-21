package preserve

import (
	"slices"
	"strings"
	"testing"
)

func TestPreserveCleansTemplateGraphicActionsWithoutNormalizingTemplateArea(t *testing.T) {
	for _, location := range []string{"Tpls/template.xml", "/Doc_0/Tpls/template.xml"} {
		t.Run(location, func(t *testing.T) {
			files := pageAreaFiles("")
			files["Doc_0/Document.xml"] = strings.Replace(files["Doc_0/Document.xml"], "</CommonData>", `<TemplatePage ID="10" BaseLoc="`+location+`"/></CommonData>`, 1)
			files["Doc_0/Pages/Page_0/Content.xml"] = strings.Replace(files["Doc_0/Pages/Page_0/Content.xml"], "<Content>", `<Template TemplateID="10"/><Content>`, 1)
			files["Doc_0/Tpls/template.xml"] = strings.Replace(pageXML("10", `<Area><PhysicalBox>0 0 999 999</PhysicalBox></Area>`), "</TextObject>", "<Actions>"+actionXML(false)+actionXML(true)+"</Actions></TextObject>", 1)
			result, after := appliedChangesBytes(t, buildOFD(t, files), Options{DocType: "OFD-A"})
			xml := after["Doc_0/Tpls/template.xml"]
			if countOFDElements(t, xml, "URI") != 0 || countOFDElements(t, xml, "Goto") != 1 {
				t.Fatalf("模板图元动作清理错误: %s", xml)
			}
			if !strings.Contains(xml, "0 0 999 999") || !strings.Contains(after["Doc_0/Document.xml"], "0 0 100 100") {
				t.Fatal("模板不应参与正文页树默认设置统计")
			}
			if !slices.Contains(result.Entries, "Doc_0/Tpls/template.xml") {
				t.Fatalf("改动清单未包含模板: %v", result.Entries)
			}
		})
	}
}
