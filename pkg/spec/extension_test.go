package spec_test

import (
	"encoding/xml"
	"testing"
	"time"

	"github.com/zc310/ofd/pkg/spec"
)

func TestExtensionsDecodePreservesInlineXMLAttributesAndMixedContent(t *testing.T) {
	const content = `前&amp;<v:Node v:key="a">后代文本</v:Node><!--注释--><?vendor keep?><![CDATA[末<尾>]]>`
	data := `<Extensions xmlns="http://www.ofdspec.org/2016" xmlns:v="urn:vendor"><Extension AppName="测试" Company="" AppVersion="2" Date="2026-01-02T03:04:05.123456789+08:00" RefId="7"><Property Name="mode" Type="string">a&amp;b</Property><Property Name="empty"/><Data v:mode="mixed" xmlns:local="urn:local">` + content + `</Data><ExtendData>Data/../Data/file.bin</ExtendData></Extension></Extensions>`
	var extensions spec.Extensions
	if err := xml.Unmarshal([]byte(data), &extensions); err != nil {
		t.Fatal(err)
	}
	if extensions.XMLName.Space != spec.Namespace || len(extensions.Extensions) != 1 {
		t.Fatalf("扩展根命名空间或列表改变: %+v", extensions)
	}
	extension := extensions.Extensions[0]
	if extension.AppName != "测试" || extension.Company == nil || *extension.Company != "" || extension.AppVersion == nil || *extension.AppVersion != "2" || extension.RefID != 7 {
		t.Fatalf("扩展属性或空字符串状态丢失: %+v", extension)
	}
	if extension.Date == nil || extension.Date.Format(time.RFC3339Nano) != "2026-01-02T03:04:05.123456789+08:00" {
		t.Fatalf("扩展日期精度或时区改变: %v", extension.Date)
	}
	if len(extension.Properties) != 2 || extension.Properties[0].Value != "a&b" || extension.Properties[0].Type == nil || *extension.Properties[0].Type != "string" || extension.Properties[1].Type != nil {
		t.Fatalf("扩展属性顺序或可选类型改变: %+v", extension.Properties)
	}
	inline := extension.Data
	if inline == nil || inline.InnerXML != content || inline.Text != "前&末<尾>" {
		t.Fatalf("原始 XML 或直接字符数据丢失: %+v", inline)
	}
	if len(inline.Attrs) != 2 || inline.Attrs[0].Name != (xml.Name{Space: "urn:vendor", Local: "mode"}) || inline.Attrs[0].Value != "mixed" || inline.Attrs[1].Name != (xml.Name{Space: "xmlns", Local: "local"}) || inline.Attrs[1].Value != "urn:local" {
		t.Fatalf("Data 自身属性或局部命名空间丢失: %+v", inline.Attrs)
	}
	if extension.ExtendData == nil || *extension.ExtendData != "Data/../Data/file.bin" {
		t.Fatal("扩展路径不应提前清理或加载")
	}
}

func TestExtensionDecodeDistinguishesMissingEmptyAndTextData(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		present bool
		text    string
	}{
		{name: "省略"},
		{name: "空元素", content: `<Data/>`, present: true},
		{name: "空白文本", content: "<Data> \n\t </Data>", present: true, text: " \n\t "},
		{name: "转义文本", content: `<Data>&lt;Config&gt;&amp;</Data>`, present: true, text: "<Config>&"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var extension spec.Extension
			if err := xml.Unmarshal([]byte(`<Extension AppName="测试" RefId="1">`+tc.content+`</Extension>`), &extension); err != nil {
				t.Fatal(err)
			}
			if (extension.Data != nil) != tc.present {
				t.Fatalf("Data 存在状态改变: %+v", extension.Data)
			}
			if extension.Data != nil && extension.Data.Text != tc.text {
				t.Fatalf("Data 文本 = %q，期望 %q", extension.Data.Text, tc.text)
			}
		})
	}
}

func TestExtensionDecodeRetainsDateAndIDCompatibility(t *testing.T) {
	// 非 RFC3339 日期继续忽略；非数字标识沿用基础类型映射，不代表通过标准校验。
	var extension spec.Extension
	data := `<Extension AppName="测试" Date="2026-01-02" RefId="legacy-ref"/>`
	if err := xml.Unmarshal([]byte(data), &extension); err != nil {
		t.Fatal(err)
	}
	var id spec.StID
	if err := id.UnmarshalText([]byte("legacy-ref")); err != nil {
		t.Fatal(err)
	}
	if extension.Date != nil || extension.RefID != spec.StRefID(id) {
		t.Fatalf("既有兼容解码改变: %+v", extension)
	}
}

func TestExtensionDecodeIgnoresKnownNamesInsideUnknownSubtrees(t *testing.T) {
	data := `<Extension AppName="测试" RefId="1"><Unknown><Property Name="hidden">不应读取</Property><Data>不应读取</Data><ExtendData>hidden.bin</ExtendData></Unknown><Property Name="visible">保留</Property></Extension>`
	var extension spec.Extension
	if err := xml.Unmarshal([]byte(data), &extension); err != nil {
		t.Fatal(err)
	}
	if len(extension.Properties) != 1 || extension.Properties[0].Name != "visible" || extension.Data != nil || extension.ExtendData != nil {
		t.Fatalf("未知子树中的同名元素泄漏到扩展声明: %+v", extension)
	}
}

func TestExtensionDecodeReplacesPreviousState(t *testing.T) {
	var extension spec.Extension
	for _, data := range []string{
		`<Extension AppName="旧" RefId="1" Company="厂商" Date="2026-01-02T03:04:05Z"><Property Name="旧"/><Data>旧</Data><ExtendData>old.bin</ExtendData></Extension>`,
		`<Extension AppName="新" RefId="2"/>`,
	} {
		if err := xml.Unmarshal([]byte(data), &extension); err != nil {
			t.Fatal(err)
		}
	}
	if extension.AppName != "新" || extension.RefID != 2 || extension.Company != nil || extension.Date != nil || len(extension.Properties) != 0 || extension.Data != nil || extension.ExtendData != nil {
		t.Fatalf("复用解码目标残留旧扩展声明: %+v", extension)
	}
}

func TestExtensionDecodeRejectsMalformedInlineAndUnknownXML(t *testing.T) {
	for _, data := range []string{
		`<Extension><Data><Node></Data></Extension>`,
		`<Extension><Unknown><Node></Unknown></Extension>`,
	} {
		var extension spec.Extension
		if err := xml.Unmarshal([]byte(data), &extension); err == nil {
			t.Fatalf("接受了损坏 XML: %s", data)
		}
	}
}
