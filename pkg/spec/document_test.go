package spec_test

import (
	"encoding/json"
	"encoding/xml"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

func TestDocumentDecodePreservesPageNavigationOrderAndLexicalPaths(t *testing.T) {
	input := `<Document xmlns="http://www.ofdspec.org/2016">
<CommonData><MaxUnitID>42</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox><ContentBox>10 20 180 250</ContentBox></PageArea><PublicRes>../Res/Public.xml</PublicRes><PublicRes>./Extra.xml</PublicRes><DocumentRes>Res/../DocumentRes.xml</DocumentRes><TemplatePage ID="10" Name="" ZOrder="Foreground" BaseLoc="./Templates/10.xml"/><TemplatePage ID="11" BaseLoc="Templates/11.xml"/><DefaultCS>0</DefaultCS></CommonData>
<Pages><Page ID="2" BaseLoc="Pages/../2.xml"/><Page ID="1" BaseLoc="./1.xml"/></Pages>
<Outlines><OutlineElem Title="第一章" Count="0" Expanded="false"><Actions><Action Event="CLICK"><Goto><Bookmark Name="chapter"/></Goto></Action><Action Event="CLICK"><URI URI="https://example.test"/></Action></Actions><OutlineElem Title="子章节"/></OutlineElem><OutlineElem Title="第二章"/></Outlines>
<Permissions/>
<Actions><Action Event="DO"><Goto><Dest Type="XYZ" PageID="2" Left="0" Top="0" Zoom="0"/></Goto></Action><Action Event="DO"><Region><Area Start="0 0"><Line Point1="1 1"/><Close/></Area></Region><GotoA AttachID="note" NewWindow="false"/></Action></Actions>
<VPreferences><HideToolbar>false</HideToolbar><ZoomMode>FitWidth</ZoomMode><Zoom>0</Zoom></VPreferences>
<Bookmarks><Bookmark Name="chapter"><Dest Type="FitH" PageID="2" Top="0"/></Bookmark><Bookmark Name="tail"><Dest Type="Fit" PageID="1"/></Bookmark></Bookmarks>
<Annotations>../Annots.xml</Annotations><CustomTags>Tags/../Custom.xml</CustomTags><Attachments>./Attachs.xml</Attachments><Extensions/>
</Document>`
	var decoded spec.Document
	if err := xml.Unmarshal([]byte(input), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Pages.Pages) != 2 || decoded.Pages.Pages[0].ID != 2 || decoded.Pages.Pages[1].ID != 1 || decoded.Pages.Pages[0].BaseLoc != "Pages/../2.xml" || !reflect.DeepEqual(decoded.CommonData.PublicRes, []spec.StLoc{"../Res/Public.xml", "./Extra.xml"}) {
		t.Fatal("页序或词法路径被提前归一化")
	}
	if decoded.CommonData.TemplatePages[0].Name == nil || decoded.CommonData.TemplatePages[1].Name != nil || decoded.CommonData.DefaultCS == nil || *decoded.CommonData.DefaultCS != 0 {
		t.Fatal("模板名或默认颜色空间的显式空值/零值丢失")
	}
	if decoded.Extensions == nil || *decoded.Extensions != "" {
		t.Fatal("显式空路径节点被误认为省略")
	}
	if len(decoded.Outlines.OutlineElems) != 2 || decoded.Outlines.OutlineElems[1].Title != "第二章" || decoded.Outlines.OutlineElems[0].OutlineElem[0].Title != "子章节" {
		t.Fatal("大纲顺序或嵌套层次改变")
	}
	outline := decoded.Outlines.OutlineElems[0]
	if outline.Count == nil || *outline.Count != 0 || outline.Expanded == nil || *outline.Expanded || outline.Actions.Actions[0].Goto.Bookmark.Name != "chapter" || outline.Actions.Actions[1].URI.URI != "https://example.test" {
		t.Fatal("大纲可选零值或动作顺序改变")
	}
	dest := decoded.Actions.Actions[0].Goto.Dest
	if dest.Left == nil || *dest.Left != 0 || dest.Top == nil || *dest.Top != 0 || dest.Zoom == nil || *dest.Zoom != 0 {
		t.Fatal("文档动作的显式零坐标丢失")
	}
	action := decoded.Actions.Actions[1]
	if action.GotoA.NewWindow == nil || *action.GotoA.NewWindow || len(action.Region.Areas[0].Paths) != 2 || action.Region.Areas[0].Paths[1].XMLName.Local != "Close" {
		t.Fatal("附件动作可选标志或动作区域路径顺序改变")
	}
	if len(decoded.Bookmarks.Bookmarks) != 2 || decoded.Bookmarks.Bookmarks[0].Name != "chapter" || decoded.Bookmarks.Bookmarks[1].Name != "tail" {
		t.Fatal("书签顺序改变")
	}
}

func TestDocumentPreservesMissingDeclarationsWithoutApplyingReadingDefaults(t *testing.T) {
	var document spec.Document
	if err := xml.Unmarshal([]byte(`<Document><CommonData><MaxUnitID>0</MaxUnitID><PageArea/></CommonData><Pages/></Document>`), &document); err != nil {
		t.Fatal(err)
	}
	if document.CommonData.PageArea.PhysicalBox != (spec.StBox{}) || document.CommonData.PageArea.BleedBox != nil {
		t.Fatal("标准模型解码不应应用 A4 兜底或区域继承")
	}
	if document.Permissions != nil || document.Outlines != nil || document.Actions != nil || document.Bookmarks != nil || document.VPreferences != nil || document.Annotations != nil {
		t.Fatal("标准模型解码不应补出省略的声明")
	}
}

func TestDocumentDecodePreservesDefaultAndPrefixedNamespace(t *testing.T) {
	for _, input := range []string{
		`<Document xmlns="http://www.ofdspec.org/2016"/>`,
		`<ofd:Document xmlns:ofd="http://www.ofdspec.org/2016"/>`,
	} {
		var document spec.Document
		if err := xml.Unmarshal([]byte(input), &document); err != nil {
			t.Fatal(err)
		}
		if document.XMLName.Local != "Document" || document.XMLName.Space != spec.Namespace {
			t.Fatalf("文档根命名空间未保留: %+v", document.XMLName)
		}
	}
}

func TestVPreferencesDecodePreservesSiblingZoomAndOptionalZeroValues(t *testing.T) {
	for _, tc := range []struct {
		body string
		want spec.VPreferences
	}{
		{},
		{body: `<ZoomMode>FitWidth</ZoomMode>`, want: spec.VPreferences{Zoom: &spec.ZoomSetting{Mode: new("FitWidth")}}},
		{body: `<Zoom>0</Zoom>`, want: spec.VPreferences{Zoom: &spec.ZoomSetting{Value: new(0.0)}}},
		{
			body: `<PageMode>UseOutlines</PageMode><PageLayout>OneColumn</PageLayout><TabDisplay>DocTitle</TabDisplay><HideToolbar>false</HideToolbar><HideMenubar>true</HideMenubar><HideWindowUI>false</HideWindowUI><ZoomMode>Default</ZoomMode><Zoom>0</Zoom>`,
			want: spec.VPreferences{
				PageMode: new(spec.PageMode("UseOutlines")), PageLayout: new(spec.PageLayout("OneColumn")), TabDisplay: new(spec.TabDisplay("DocTitle")),
				HideToolbar: new(false), HideMenubar: new(true), HideWindowUI: new(false),
				Zoom: &spec.ZoomSetting{Mode: new("Default"), Value: new(0.0)},
			},
		},
	} {
		var decoded spec.VPreferences
		if err := xml.Unmarshal([]byte(`<VPreferences>`+tc.body+`</VPreferences>`), &decoded); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(decoded, tc.want) {
			t.Fatalf("视图首选项解码丢失省略或零值: %#v，期望 %#v", decoded, tc.want)
		}
	}
}

func TestVPreferencesDecodeClearsPreviousZoomWhenOmitted(t *testing.T) {
	var preferences spec.VPreferences
	for _, data := range []string{`<VPreferences><ZoomMode>FitWidth</ZoomMode><Zoom>2</Zoom></VPreferences>`, `<VPreferences/>`} {
		if err := xml.Unmarshal([]byte(data), &preferences); err != nil {
			t.Fatal(err)
		}
	}
	if preferences.Zoom != nil {
		t.Fatal("重复解码保留了前一次的缩放设置")
	}
}

func TestVPreferencesXMLDecodeDoesNotFlattenJSONZoom(t *testing.T) {
	var preferences spec.VPreferences
	if err := xml.Unmarshal([]byte(`<VPreferences><ZoomMode>FitWidth</ZoomMode><Zoom>0</Zoom></VPreferences>`), &preferences); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(preferences) //nolint:musttag // 验证 XML 模型迁移前的默认 JSON 字段名和分组，不引入新 JSON 标签。
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"Zoom":{"Mode":"FitWidth","Value":0}`) {
		t.Fatalf("XML 解码改变了 JSON 中的缩放分组: %s", data)
	}
}
