package merge

import (
	"bytes"
	"strings"
	"testing"

	"github.com/beevik/etree"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/spec"
)

func assertOFDElementsPrefixed(t *testing.T, data string) {
	t.Helper()
	doc := etree.NewDocument()
	if err := doc.ReadFromString(data); err != nil {
		t.Fatal(err)
	}
	walkElements(doc.Root(), func(element *etree.Element) {
		if element.NamespaceURI() == spec.Namespace && element.Space != "ofd" {
			t.Errorf("OFD 元素 %s 缺少 ofd: 前缀", element.FullTag())
		}
	})
	if doc.Root().NamespaceURI() != spec.Namespace || doc.Root().Space != "ofd" {
		t.Fatalf("根元素没有正确声明 ofd 命名空间: %s", data)
	}
}

func TestMergeGeneratedXMLUsesOFDPrefix(t *testing.T) {
	input := writeSignatureLayout(t, "Doc_0/custom-list.xml", "/Doc_0/Signatures/custom.xml", "/Doc_0/Document.xml")
	var out bytes.Buffer
	if err := Files([]string{input, input}, &out, Options{Signatures: creator.SignatureRewrite}); err != nil {
		t.Fatal(err)
	}
	pkg := openMerged(t, out.Bytes())
	for _, name := range []string{"OFD.xml", "Doc_1/custom-list.xml", "Doc_1/Signatures/custom.xml"} {
		t.Run(name, func(t *testing.T) { assertOFDElementsPrefixed(t, readEntry(t, pkg, name)) })
	}
}

func TestRemappedResourceXMLUsesOFDPrefix(t *testing.T) {
	data, err := remapResXML([]byte(`<Res xmlns="http://www.ofdspec.org/2016"><Fonts><Font ID="10" FontName="Test"/></Fonts></Res>`), "s1_", &resourceRemap{ids: map[uint64]uint64{}, fonts: map[string]string{}, drawParams: map[string]string{}}, newIDRemap())
	if err != nil {
		t.Fatal(err)
	}
	assertOFDElementsPrefixed(t, string(data))
}

func TestClonedBodyPreservesExtensionNamespaces(t *testing.T) {
	doc := etree.NewDocument()
	if err := doc.ReadFromString(`<OFD xmlns="http://www.ofdspec.org/2016" xmlns:ext="urn:example"><DocBody><DocInfo><CustomDatas><CustomData Name="x">before<ext:Value ext:flag="yes">value</ext:Value>after</CustomData></CustomDatas></DocInfo><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`); err != nil {
		t.Fatal(err)
	}
	body := cloneElement(directChild(doc.Root(), "DocBody"))
	data, err := buildRootXML([]*etree.Element{body}, rootAttrs{version: "1.0", docType: "OFD"})
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.ReadFromBytes(data); err != nil {
		t.Fatal(err)
	}
	assertOFDElementsPrefixed(t, string(data))
	var extension *etree.Element
	walkElements(doc.Root(), func(e *etree.Element) {
		if e.Tag == "Value" {
			extension = e
		}
	})
	if extension == nil || extension.Space != "ext" || extension.NamespaceURI() != "urn:example" || extension.SelectAttrValue("ext:flag", "") != "yes" {
		t.Fatalf("扩展命名空间丢失: %+v", extension)
	}
	xml, err := doc.WriteToString()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(xml, ">before<ext:Value") || !strings.Contains(xml, "</ext:Value>after</ofd:CustomData>") {
		t.Fatalf("混合文本丢失: %s", xml)
	}
}
