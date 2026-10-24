package xmlcodec

import (
	"bytes"
	"encoding/xml"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/beevik/etree"
)

func TestMarshalDocumentPreservesNamespaceScopesAndOriginalTree(t *testing.T) {
	doc := etree.NewDocument()
	doc.ReadSettings.PreserveCData = true
	input := "\ufeff" + `<?xml version="1.0" encoding="UTF-8"?><?producer keep?><q:Res xmlns:q="http://www.ofdspec.org/2016" xmlns:ext="urn:extension"><q:Fonts><q:Font ID="1" FontName="a &amp; b"/></q:Fonts><ext:Data xmlns="">before<![CDATA[a<&b]]><!--keep--><Value>no namespace</Value>after</ext:Data></q:Res>`
	if err := doc.ReadFromString(input); err != nil {
		t.Fatal(err)
	}
	data, err := MarshalDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Root().Space != "q" {
		t.Fatal("写出不应修改输入树的命名空间前缀")
	}
	for _, fragment := range []string{"<ofd:Res", "<ofd:Fonts>", `<ofd:Font ID="1" FontName="a &amp; b"/>`, "<?producer keep?>", "a&lt;&amp;b", "<!--keep-->", "<Value>no namespace</Value>"} {
		if !strings.Contains(string(data), fragment) {
			t.Errorf("输出未保留 %q: %s", fragment, data)
		}
	}
	if strings.Contains(string(data), "\ufeff") {
		t.Fatal("输出声明之后不应带 BOM")
	}
	parsed := etree.NewDocument()
	if err := parsed.ReadFromBytes(data); err != nil {
		t.Fatal(err)
	}
	extension := parsed.Root().FindElement("./Data")
	if extension == nil || extension.NamespaceURI() != "urn:extension" || extension.FindElement("./Value").NamespaceURI() != "" {
		t.Fatalf("扩展命名空间作用域发生变化: %s", data)
	}
}

func TestMarshalDocumentDeclaresOFDInNestedScopes(t *testing.T) {
	for _, input := range []string{
		`<ext:Root xmlns:ext="urn:extension" xmlns="http://www.ofdspec.org/2016"><Page ID="1"/></ext:Root>`,
		`<ext:Root xmlns:ext="urn:extension"><Wrapper xmlns="http://www.ofdspec.org/2016"><Page/></Wrapper><Page xmlns="http://www.ofdspec.org/2016"/></ext:Root>`,
		`<ext:Root xmlns:ext="urn:extension" xmlns:q="http://www.ofdspec.org/2016"><q:Page><q:Content/></q:Page></ext:Root>`,
	} {
		t.Run(input, func(t *testing.T) {
			doc := etree.NewDocument()
			if err := doc.ReadFromString(input); err != nil {
				t.Fatal(err)
			}
			data, err := MarshalDocument(doc)
			if err != nil {
				t.Fatal(err)
			}
			assertExpandedXMLNames(t, []byte(input), data)
			if !bytes.Contains(data, []byte(`<ofd:Page`)) {
				t.Fatalf("OFD 元素未归一化: %s", data)
			}
		})
	}
}

func TestMarshalDocumentPreservesForeignOFDBindings(t *testing.T) {
	for _, input := range []string{
		`<Root xmlns:ofd="urn:foreign" xmlns:q="http://www.ofdspec.org/2016"><q:Child/><ofd:Foreign/></Root>`,
		`<q:Root xmlns:q="http://www.ofdspec.org/2016" xmlns:ofd="urn:foreign" ofd:flag="root"><q:Child ofd:flag="child"/><ofd:Foreign/></q:Root>`,
		`<Root xmlns="http://www.ofdspec.org/2016" xmlns:ofd="urn:foreign" ofd:flag="keep"><Child/><ofd:Foreign/></Root>`,
		`<ofd:Root xmlns:ofd="http://www.ofdspec.org/2016"><ext:Wrapper xmlns:ext="urn:extension" xmlns:ofd="urn:foreign" xmlns:q="http://www.ofdspec.org/2016"><q:Child ofd:flag="keep"/><ofd:Foreign/></ext:Wrapper><ofd:Later/></ofd:Root>`,
		`<q:Root xmlns:q="http://www.ofdspec.org/2016" xmlns:ofd="urn:foreign"><q:Child xmlns:ofd="http://www.ofdspec.org/2016"><q:Leaf/></q:Child><q:Other ofd:flag="keep"/></q:Root>`,
	} {
		t.Run(input, func(t *testing.T) {
			doc := etree.NewDocument()
			if err := doc.ReadFromString(input); err != nil {
				t.Fatal(err)
			}
			before, err := doc.WriteToBytes()
			if err != nil {
				t.Fatal(err)
			}
			for _, marshal := range []func(*etree.Document) ([]byte, error){MarshalDocument, Marshal} {
				data, err := marshal(doc)
				if err != nil {
					t.Fatal(err)
				}
				assertExpandedXMLNames(t, []byte(input), data)
			}
			after, err := doc.WriteToBytes()
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("序列化修改了原始树: err=%v", err)
			}
		})
	}
}

func TestWriteElementDeclaresNormalizedPrefixUnderExtensionRoot(t *testing.T) {
	doc := etree.NewDocument()
	if err := doc.ReadFromString(`<q:Page xmlns:q="http://www.ofdspec.org/2016"><q:Content/></q:Page>`); err != nil {
		t.Fatal(err)
	}
	w := New()
	defer Put(w)
	w.StartQualified("ext:Root")
	w.Attr("xmlns:ext", "urn:extension")
	if err := WriteElement(w, doc.Root()); err != nil {
		t.Fatal(err)
	}
	w.End()
	want := []byte(`<ext:Root xmlns:ext="urn:extension"><q:Page xmlns:q="http://www.ofdspec.org/2016"><q:Content/></q:Page></ext:Root>`)
	assertExpandedXMLNames(t, want, w.Bytes())
}

func TestMarshalDocumentPreservesForeignPrefixInQNameValue(t *testing.T) {
	doc := etree.NewDocument()
	if err := doc.ReadFromString(`<q:Root xmlns:q="http://www.ofdspec.org/2016" xmlns:ofd="urn:foreign" Type="ofd:Extension"/>`); err != nil {
		t.Fatal(err)
	}
	data, err := MarshalDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	parsed := etree.NewDocument()
	if err := parsed.ReadFromBytes(data); err != nil {
		t.Fatal(err)
	}
	if parsed.Root().SelectAttrValue("xmlns:ofd", "") != "urn:foreign" || parsed.Root().SelectAttrValue("Type", "") != "ofd:Extension" {
		t.Fatalf("QName 值的前缀含义改变: %s", data)
	}
}

func assertExpandedXMLNames(t *testing.T, want, got []byte) {
	t.Helper()
	type name struct {
		URI, Local, Value string
	}
	names := func(data []byte) []name {
		// etree 会合并同名属性；独立检查原始输出，避免重复声明被掩盖。
		decoder := xml.NewDecoder(bytes.NewReader(data))
		for {
			token, err := decoder.RawToken()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("无效的 XML: %v\n%s", err, data)
			}
			if element, ok := token.(xml.StartElement); ok {
				seen := map[xml.Name]bool{}
				for _, attr := range element.Attr {
					if seen[attr.Name] {
						t.Fatalf("属性或命名空间声明重复: %+v\n%s", attr.Name, data)
					}
					seen[attr.Name] = true
				}
			}
		}
		doc := etree.NewDocument()
		if err := doc.ReadFromBytes(data); err != nil {
			t.Fatal(err)
		}
		var result []name
		var walk func(*etree.Element)
		walk = func(element *etree.Element) {
			uri := element.NamespaceURI()
			if element.Space != "" && uri == "" {
				t.Fatalf("元素 %s 的前缀未声明: %s", element.FullTag(), data)
			}
			result = append(result, name{URI: uri, Local: element.Tag})
			for _, attr := range element.Attr {
				if attr.Space == "xmlns" || attr.Space == "" && attr.Key == "xmlns" {
					continue
				}
				uri := attr.NamespaceURI()
				if attr.Space != "" && uri == "" {
					t.Fatalf("属性 %s 的前缀未声明: %s", attr.FullKey(), data)
				}
				result = append(result, name{URI: uri, Local: attr.Key, Value: attr.Value})
			}
			for child := range element.ChildElementsSeq() {
				walk(child)
			}
		}
		walk(doc.Root())
		return result
	}
	if !reflect.DeepEqual(names(want), names(got)) {
		t.Fatalf("元素或属性的命名空间发生变化:\nwant %s\ngot %s", want, got)
	}
}
