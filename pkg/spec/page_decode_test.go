package spec_test

import (
	"encoding/xml"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

func TestPageContentDecodePreservesMixedObjectOrderAndRawDeclarations(t *testing.T) {
	input := `<Page xmlns="http://www.ofdspec.org/2016"><Area/><Template TemplateID="9" ZOrder="Foreground"/><PageRes>../Res/./Page.xml</PageRes><Content><Layer ID="1" Type="Body" DrawParam="8"><ImageObject ID="2" Boundary="0 0 10 10" ResourceID="20" Alpha="0"/><TextObject ID="3" Boundary="0 0 30 10" Font="21" Size="3" Fill="false" Visible="false" CTM="1 0 0 1 2 3"><FillColor Value="#FFFF 0 0"/><CGTransform CodePosition="0" CodeCount="2" GlyphCount="1"><Glyphs>17</Glyphs></CGTransform><TextCode X="0" Y="3" DeltaX="1 2">甲\0020乙</TextCode><TextCode>丙</TextCode><CGTransform CodePosition="0" GlyphCount="1"><Glyphs>18</Glyphs></CGTransform></TextObject><PathObject ID="4" Boundary="0 0 10 10" Stroke="false" Fill="true"><FillColor><AxialShd StartPoint="0 0" EndPoint="10 0"><Segment Position="0"><Color Value="0 0 0"/></Segment><Segment><Color Value="255 0 0"/></Segment></AxialShd></FillColor><AbbreviatedData>M 0 0 L 10 0 C</AbbreviatedData></PathObject><PageBlock ID="5"><CompositeObject ID="6" Boundary="0 0 10 10" ResourceID="22"/></PageBlock></Layer><Layer ID="7" Type="Foreground"/></Content></Page>`
	var page spec.PageContent
	if err := xml.Unmarshal([]byte(input), &page); err != nil {
		t.Fatal(err)
	}
	if page.Area == nil || page.Area.PhysicalBox != (spec.StBox{}) || page.PageRes[0] != "../Res/./Page.xml" || page.Template[0].TemplateID != 9 {
		t.Fatal("公开模型不应应用 A4 兜底、区域继承或路径归一化")
	}
	if len(page.Content.Layer) != 2 || page.Content.Layer[0].ID != 1 || page.Content.Layer[1].ID != 7 || page.Content.Layer[0].DrawParam != 8 {
		t.Fatal("图层顺序或属性丢失")
	}
	items := page.Content.Layer[0].Items
	kinds := make([]spec.PageItemKind, 0, len(items))
	for _, item := range items {
		kinds = append(kinds, item.Kind)
	}
	if !reflect.DeepEqual(kinds, []spec.PageItemKind{spec.PageItemImage, spec.PageItemText, spec.PageItemPath, spec.PageItemBlock}) {
		t.Fatalf("混合图元顺序被按类型重排: %v", kinds)
	}
	text := items[1].Text
	if text.ID != 3 || text.Fill.Value(true) || text.VisibleValue() || text.CTM == nil || text.TextCode[0].Value != "甲 乙" || text.TextCode[0].DeltaX[1] != 2 {
		t.Fatal("文字标志、变换或 OFD 转义丢失")
	}
	if text.CGTransform[1].CodePosition != 0 || text.CGTransform[0].Glyphs[0] != 17 {
		t.Fatal("标准声明解码不应展开 CodePosition 或把 Glyphs 解释为 Unicode")
	}
	segments := items[2].Path.FillColor.AxialShd.Segment
	if !segments[0].PositionSet || segments[0].Position != 0 || segments[1].PositionSet {
		t.Fatal("渐变段的省略位置和显式零位置混淆")
	}
	if items[0].Image.Alpha == nil || *items[0].Image.Alpha != 0 || items[3].Block.Items[0].Composite.ID != 6 {
		t.Fatal("图元透明度或嵌套复合对象声明丢失")
	}
}

func TestPublicPageBlockDecodeEnforcesImplementationDepthLimit(t *testing.T) {
	data := `<Content>` + strings.Repeat(`<PageBlock ID="1">`, spec.MaxPageBlockDepth+1) + strings.Repeat(`</PageBlock>`, spec.MaxPageBlockDepth+1) + `</Content>`
	var block spec.CTPageBlock
	if err := xml.Unmarshal([]byte(data), &block); !errors.Is(err, spec.ErrPageBlockDepth) {
		t.Fatalf("超深对象块应返回可识别的实现资源限制错误: %v", err)
	}
}

func TestRepeatedObjectDecodeClearsPreviousAttributesAndChildren(t *testing.T) {
	var object spec.TextObject
	for _, input := range []string{
		`<TextObject ID="1" Boundary="0 0 10 10" Font="2" Size="3" Visible="false"><TextCode>旧正文</TextCode></TextObject>`,
		`<TextObject ID="4" Boundary="0 0 1 1" Font="5" Size="6"/>`,
	} {
		if err := xml.Unmarshal([]byte(input), &object); err != nil {
			t.Fatal(err)
		}
	}
	if object.ID != 4 || len(object.TextCode) != 0 || !object.VisibleValue() {
		t.Fatal("重复解码残留了旧图元的正文或属性")
	}
	var layer spec.Layer
	for _, input := range []string{
		`<Layer ID="1" Type="Foreground"><PageBlock ID="2"/></Layer>`, `<Layer ID="3"/>`,
	} {
		if err := xml.Unmarshal([]byte(input), &layer); err != nil {
			t.Fatal(err)
		}
	}
	if layer.ID != 3 || layer.Type != "" || len(layer.Items) != 0 {
		t.Fatal("重复解码残留了旧图层声明")
	}
}
