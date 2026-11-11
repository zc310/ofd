package models

import (
	"encoding/xml"
	"reflect"
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

func TestFastPathXMLMatchesPublicPathDataAfterXMLUnescaping(t *testing.T) {
	data := []byte(`<Page><Content><Layer ID="1"><PathObject ID="2" Boundary="0 0 100 100"><FillColor Value="#FFFF 0 0"/><AbbreviatedData>S &#49;.123456789012345 2 3 4 L 5 6 Q 7 8 9 10 B 11 12 13 14 15 16 A -5 -6 30 1 0 17 18 Z</AbbreviatedData></PathObject></Layer></Content></Page>`)
	fast, err := ParsePageContentXML(data)
	if err != nil {
		t.Fatal(err)
	}
	var standard PageContent
	if err := xml.Unmarshal(data, &standard); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fast, &standard) {
		t.Fatal("路径和颜色迁移后快速入口与标准入口不一致")
	}
	path := fast.Content.Layer[0].Items[0].Path
	want, err := spec.ParsePathData("S 1.123456789012345 2 3 4 L 5 6 Q 7 8 9 10 B 11 12 13 14 15 16 A -5 -6 30 1 0 17 18 Z")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(path.AbbreviatedData, want) {
		t.Fatal("快速路径未复用公开模型或未正确反转义 XML")
	}
	color, err := path.FillColor.Value.MarshalXMLAttr(xml.Name{Local: "Value"})
	if err != nil || color.Value != "#FFFF 0 0" {
		t.Fatalf("快速入口丢失原始颜色位深: %v, %v", color, err)
	}
}
