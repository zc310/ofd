package models

import (
	"encoding/xml"
	"reflect"
	"testing"
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
