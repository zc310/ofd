package models

import (
	"encoding/xml"
	"reflect"
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

func TestParseResXMLMatchesStandardDecoder(t *testing.T) {
	data := []byte(`<?xml version="1.0"?>
<ofd:Res xmlns:ofd="urn:ofd" BaseLoc="Assets&amp;Fonts">
  <ofd:ColorSpaces>
    <ofd:ColorSpace ID="7" Type="RGB" BitsPerComponent="8" Profile="colors&amp;profile.icc">
      <ofd:Palette><ofd:CV> #F &#x30; 0 </ofd:CV><ofd:CV><![CDATA[0 15 0]]></ofd:CV></ofd:Palette>
    </ofd:ColorSpace>
    <ofd:ColorSpace ID="8" Type="GRAY"/>
  </ofd:ColorSpaces>
  <ofd:DrawParams><ofd:DrawParam ID="9" Relative="8" LineWidth="0" Join="Round" Cap="Square" DashOffset="0" DashPattern="1.5 2" MiterLimit="4"><ofd:FillColor Value="1 2 3"/><ofd:StrokeColor Value="4 5 6"/></ofd:DrawParam></ofd:DrawParams>
  <ofd:Fonts><ofd:Font ID="10" FontName="Example"><ofd:FontFile>font&amp;file.ttf</ofd:FontFile></ofd:Font></ofd:Fonts>
  <ofd:MultiMedias><ofd:MultiMedia ID="11" Type="Image"><ofd:MediaFile>image.png</ofd:MediaFile></ofd:MultiMedia></ofd:MultiMedias>
  <ofd:CompositeGraphicUnits><ofd:CompositeGraphicUnit ID="12" Width="10" Height="20"><ofd:Content/></ofd:CompositeGraphicUnit></ofd:CompositeGraphicUnits>
</ofd:Res>`)

	var want Res
	if err := xml.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	got, err := ParseResXML(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, &want) {
		t.Fatalf("快速解析结果与标准解析不一致:\n got: %#v\nwant: %#v", got, &want)
	}
}

func TestParseResXMLEmptyDocument(t *testing.T) {
	got, err := ParseResXML([]byte(`<Res/>`))
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ColorSpaces != nil {
		t.Fatalf("空资源文档解析结果异常: %#v", got)
	}
}

func TestResourceAliasesMatchSpecDeclarationsInFastAndStandardDecoders(t *testing.T) {
	data := []byte(`<Res BaseLoc="../Assets"><ColorSpaces><ColorSpace ID="3" Type="RGB" BitsPerComponent="16" Profile="../Profiles/./rgb.icc"><Palette><CV>65535 0 0</CV><CV>0 65535 0</CV></Palette></ColorSpace><ColorSpace ID="2" Type="GRAY"/></ColorSpaces><Fonts><Font ID="5" FontName="内嵌字体" FamilyName="测试族" Charset="prc" Bold="true" Italic="true" Serif="true" FixedWidth="true"><FontFile>../Fonts/./font.otf</FontFile></Font><Font ID="4" FontName="逻辑字体"/></Fonts><MultiMedias><MultiMedia ID="7" Type="Video" Format="AVS"><MediaFile>../Media/./video.avs</MediaFile></MultiMedia><MultiMedia ID="6" Type="Image"><MediaFile>./image.bin</MediaFile></MultiMedia></MultiMedias></Res>`)
	// 公共声明可以直接作为资源文件子树使用，不依赖内部资源总表或加载器。
	var public struct {
		XMLName     xml.Name          `xml:"Res"`
		BaseLoc     spec.StLoc        `xml:"BaseLoc,attr"`
		ColorSpaces *spec.ColorSpaces `xml:"ColorSpaces"`
		Fonts       *spec.Fonts       `xml:"Fonts"`
		MultiMedias *spec.MultiMedias `xml:"MultiMedias"`
	}
	if err := xml.Unmarshal(data, &public); err != nil {
		t.Fatal(err)
	}
	fast, err := ParseResXML(data)
	if err != nil {
		t.Fatal(err)
	}
	var standard Res
	if err := xml.Unmarshal(data, &standard); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*fast, standard) || !reflect.DeepEqual(fast.ColorSpaces, public.ColorSpaces) || !reflect.DeepEqual(fast.Fonts, public.Fonts) || !reflect.DeepEqual(fast.MultiMedias, public.MultiMedias) {
		t.Fatal("迁移后的资源声明在快速入口、标准入口和公开模型间不一致")
	}
}
