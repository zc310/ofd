package merge

import (
	"bytes"
	"strings"
	"testing"

	"github.com/beevik/etree"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/spec"
)

func TestRewrittenXMLPreservesMixedContentAndNamespaceScopes(t *testing.T) {
	const mixed = `before<![CDATA[a<&b]]><!--kept--><?vendor keep?><ext:Value ext:flag="a &amp; b&#x9;&#xA;&#xD;" xmlns="urn:default"><Value>foreign</Value><ext:Nested xmlns:ext="urn:nested">nested</ext:Nested></ext:Value>after`
	for _, kind := range []string{"signature", "resource"} {
		t.Run(kind, func(t *testing.T) {
			var data []byte
			if kind == "signature" {
				input := []byte(`<?xml version="1.0" encoding="UTF-8"?><?producer keep?><Signature xmlns="http://www.ofdspec.org/2016" xmlns:ext="urn:extension"><SignedInfo><References><Reference FileRef="/Doc_0/Document.xml"/></References>` + mixed + `</SignedInfo><SignedValue>value.dat</SignedValue></Signature><!--tail-->`)
				var changed bool
				var err error
				data, changed, err = rewriteSignatureXML(input, "Doc_0", "Doc_1")
				if err != nil || !changed {
					t.Fatalf("签名重写失败: changed=%v, err=%v", changed, err)
				}
				if !bytes.Contains(data, []byte(`FileRef="/Doc_1/Document.xml"`)) {
					t.Fatalf("签名引用未改写: %s", data)
				}
			} else {
				input := []byte("\ufeff" + `<?xml version="1.0" encoding="UTF-8"?><?producer keep?><Res xmlns="http://www.ofdspec.org/2016" xmlns:ext="urn:extension"><Fonts><Font ID="10" FontName="Test"/></Fonts>` + mixed + `</Res><!--tail-->`)
				var err error
				data, err = remapResXML(input, "source_", &resourceRemap{ids: map[uint64]uint64{}, fonts: map[string]string{}, drawParams: map[string]string{}}, newIDRemap())
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Contains(data, []byte(`FontName="source_Test"`)) {
					t.Fatalf("资源名称未改写: %s", data)
				}
			}
			assertOFDElementsPrefixed(t, string(data))
			if bytes.Contains(data, []byte("\ufeff")) || bytes.Count(data, []byte(`<?xml `)) != 1 {
				t.Fatalf("输出 BOM 或 XML 声明错误: %s", data)
			}
			for _, fragment := range []string{`<?producer keep?>`, `a&lt;&amp;b`, `<!--kept-->`, `<?vendor keep?>`, `after</ofd:`, `<!--tail-->`} {
				if !bytes.Contains(data, []byte(fragment)) {
					t.Errorf("输出缺少 %s: %s", fragment, data)
				}
			}
			doc := etree.NewDocument()
			if err := doc.ReadFromBytes(data); err != nil {
				t.Fatal(err)
			}
			var extension, foreign, nested *etree.Element
			walkElements(doc.Root(), func(element *etree.Element) {
				switch {
				case element.Tag == "Value" && element.Space == "ext":
					extension = element
				case element.Tag == "Value" && element.Space == "":
					foreign = element
				case element.Tag == "Nested":
					nested = element
				}
			})
			if extension == nil || extension.NamespaceURI() != "urn:extension" || extension.SelectAttrValue("ext:flag", "") != "a & b\t\n\r" {
				t.Fatalf("扩展元素或属性发生变化: %+v", extension)
			}
			if foreign == nil || foreign.NamespaceURI() != "urn:default" || nested == nil || nested.NamespaceURI() != "urn:nested" {
				t.Fatalf("嵌套命名空间作用域未保留: foreign=%+v, nested=%+v", foreign, nested)
			}
			count := 0
			for _, attr := range doc.Root().Attr {
				if attr.FullKey() == "xmlns:ofd" && attr.Value == spec.Namespace {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("ofd 命名空间声明重复或缺失: %s", data)
			}
		})
	}
}

func TestUnchangedSignatureKeepsOriginalSerialization(t *testing.T) {
	input := []byte("\ufeff" + `<?xml version="1.0" encoding="UTF-8"?>
<Signature xmlns="http://www.ofdspec.org/2016"><SignedInfo><!--kept--><References><Reference FileRef="../Document.xml"/></References></SignedInfo><SignedValue>value.dat</SignedValue></Signature>`)
	data, changed, err := rewriteSignatureXML(input, "Doc_0", "Doc_1")
	if err != nil || changed || !bytes.Equal(data, input) {
		t.Fatalf("未改动签名的原始字节被修改: changed=%v, err=%v", changed, err)
	}
}

func TestSignatureRewriteReportsMalformedXML(t *testing.T) {
	for _, input := range []string{"<Signature>", "<!--no root-->"} {
		if _, changed, err := rewriteSignatureXML([]byte(input), "Doc_0", "Doc_1"); err == nil || changed {
			t.Fatalf("无效签名应返回错误: input=%q, changed=%v, err=%v", input, changed, err)
		}
	}
}

func TestOFDXMLWriterPreservesTextCharacters(t *testing.T) {
	doc := etree.NewDocument()
	if err := doc.ReadFromString(`<Res xmlns="http://www.ofdspec.org/2016"><Value>中文 &amp; &lt; &gt; &#xD; &#x9;</Value></Res>`); err != nil {
		t.Fatal(err)
	}
	data, err := marshalOFDXML(doc)
	if err != nil {
		t.Fatal(err)
	}
	parsed := etree.NewDocument()
	if err := parsed.ReadFromBytes(data); err != nil {
		t.Fatal(err)
	}
	if got := directChildText(parsed.Root(), "Value"); got != "中文 & < > \r \t" {
		t.Fatalf("文本转义改变了内容: %q", got)
	}
	if !strings.Contains(string(data), "<ofd:Value>") {
		t.Fatalf("元素缺少 ofd 前缀: %s", data)
	}
}

func TestMergeXMLReadersRejectUnsupportedXML(t *testing.T) {
	for _, input := range []string{
		`<!DOCTYPE Signature><Signature/>`,
		`<?xml version="1.0" encoding="GBK"?><Signature/>`,
		`<Signature/><Signature/>`,
	} {
		t.Run(input, func(t *testing.T) {
			if _, _, err := rewriteSignatureXML([]byte(input), "Doc_0", "Doc_1"); err == nil {
				t.Fatal("签名读取器应拒绝不支持的 XML")
			}
			_, err := remapResXML([]byte(input), "source_", &resourceRemap{ids: map[uint64]uint64{}, fonts: map[string]string{}, drawParams: map[string]string{}}, newIDRemap())
			if err == nil {
				t.Fatal("资源读取器应拒绝不支持的 XML")
			}
		})
	}
}

func TestResourceRemapPropagatesXMLReaderFailures(t *testing.T) {
	for _, input := range []string{`<!DOCTYPE Res><Res/>`, `<Res><Fonts>`} {
		for _, public := range []bool{false, true} {
			document := creator.Document{Pages: []creator.Page{{}}}
			if public {
				document.PublicRes = []creator.PublicResource{{Name: "bad.xml", Data: []byte(input)}}
			} else {
				document.Pages[0].Resources = []creator.PageResource{{Data: []byte(input)}}
			}
			if err := remapDocument(&document, "s0_", newIDRemap()); err == nil {
				t.Fatalf("重映射不应忽略资源读取错误: public=%v, input=%q", public, input)
			}
		}
	}
}
