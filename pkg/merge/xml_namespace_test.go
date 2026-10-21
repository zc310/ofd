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

func TestMergeXMLSerializationPreservesForeignOFDBindings(t *testing.T) {
	const input = `<q:OFD xmlns:q="http://www.ofdspec.org/2016" xmlns:ofd="urn:foreign" ofd:flag="root"><q:DocBody ofd:flag="body"><q:DocRoot>Doc_0/Document.xml</q:DocRoot><ofd:Metadata ofd:flag="extension"/></q:DocBody></q:OFD>`
	for _, operation := range []string{"marshal", "clone"} {
		t.Run(operation, func(t *testing.T) {
			doc := etree.NewDocument()
			if err := doc.ReadFromString(input); err != nil {
				t.Fatal(err)
			}
			var data []byte
			var err error
			if operation == "marshal" {
				data, err = marshalOFDXML(doc)
			} else {
				body := cloneElement(doc.Root().FindElement("./DocBody"))
				data, err = buildRootXML([]*etree.Element{body}, rootAttrs{})
			}
			if err != nil {
				t.Fatal(err)
			}
			parsed := etree.NewDocument()
			if err := parsed.ReadFromBytes(data); err != nil {
				t.Fatal(err)
			}
			body := parsed.Root().FindElement("./DocBody")
			if body == nil || body.NamespaceURI() != spec.Namespace {
				t.Fatalf("OFD 文档体命名空间改变: %s", data)
			}
			flag := body.SelectAttr("ofd:flag")
			metadata := body.FindElement("./Metadata")
			if flag == nil || flag.NamespaceURI() != "urn:foreign" || metadata == nil || metadata.NamespaceURI() != "urn:foreign" {
				t.Fatalf("外部 ofd 绑定被预处理覆盖: %s", data)
			}
			if operation == "marshal" {
				flag := parsed.Root().SelectAttr("ofd:flag")
				if flag == nil || flag.NamespaceURI() != "urn:foreign" {
					t.Fatalf("根元素属性的命名空间改变: %s", data)
				}
			}
		})
	}
}

func TestMergeXMLSerializationPreservesExplicitEmptyNamespace(t *testing.T) {
	const input = `<ofd:Res xmlns:ofd="http://www.ofdspec.org/2016" xmlns:ext="urn:extension"><ofd:Fonts/><ext:Data xmlns=""><Value><Child/></Value></ext:Data></ofd:Res>`
	for _, clone := range []bool{false, true} {
		doc := etree.NewDocument()
		if err := doc.ReadFromString(input); err != nil {
			t.Fatal(err)
		}
		if clone {
			doc.SetRoot(cloneElement(doc.Root()))
		}
		data, err := marshalOFDXML(doc)
		if err != nil {
			t.Fatal(err)
		}
		parsed := etree.NewDocument()
		if err := parsed.ReadFromBytes(data); err != nil {
			t.Fatal(err)
		}
		value := parsed.Root().FindElement("./Data/Value")
		if value == nil || value.NamespaceURI() != "" || value.FindElement("./Child").NamespaceURI() != "" {
			t.Fatalf("无命名空间的扩展元素被改为 OFD 元素: clone=%v, output=%s", clone, data)
		}
	}
}
