package preserve

import (
	"bytes"
	"strings"
	"testing"

	"github.com/beevik/etree"
	"github.com/zc310/ofd/pkg/spec"
)

func TestPreserveWritesOFDPrefixAndKeepsExtensionContent(t *testing.T) {
	files := pageAreaFiles(areaA4)
	metadata := `<ext:Metadata ext:flag="a &amp; b&#x9;&#xA;&#xD;">before<![CDATA[a<&b]]><!--kept--><?vendor keep?><ext:Nested xmlns:ext="urn:nested">value</ext:Nested>after</ext:Metadata>`
	files["Doc_0/Document.xml"] = strings.Replace(files["Doc_0/Document.xml"], ns, ns+` xmlns:ext="urn:extension"`, 1)
	files["Doc_0/Document.xml"] = strings.Replace(files["Doc_0/Document.xml"], "</Document>", metadata+"</Document>", 1)
	result, after := appliedChangesBytes(t, buildOFD(t, files), Options{DocType: "OFD-A"})
	for _, name := range result.Entries {
		doc := etree.NewDocument()
		if err := doc.ReadFromString(after[name]); err != nil {
			t.Fatal(err)
		}
		var walk func(*etree.Element)
		walk = func(element *etree.Element) {
			if element.NamespaceURI() == spec.Namespace && element.Space != "ofd" {
				t.Errorf("%s 的 %s 缺少 ofd 前缀", name, element.FullTag())
			}
			for _, child := range element.ChildElements() {
				walk(child)
			}
		}
		walk(doc.Root())
	}
	xml := after["Doc_0/Document.xml"]
	for _, fragment := range []string{"a&lt;&amp;b", "<!--kept-->", "<?vendor keep?>", "before", "after</ext:Metadata>"} {
		if !strings.Contains(xml, fragment) {
			t.Errorf("重写后丢失 %s: %s", fragment, xml)
		}
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromString(xml); err != nil {
		t.Fatal(err)
	}
	extension := doc.Root().FindElement("./Metadata")
	if extension == nil || extension.NamespaceURI() != "urn:extension" || extension.SelectAttrValue("ext:flag", "") != "a & b\t\n\r" {
		t.Fatalf("扩展命名空间或属性丢失: %s", xml)
	}
	nested := extension.FindElement("./Nested")
	if nested == nil || nested.NamespaceURI() != "urn:nested" {
		t.Fatalf("嵌套命名空间丢失: %s", xml)
	}
}

func TestPreserveReaderKeepsFormattedTextAndReferencedCharacters(t *testing.T) {
	files := pageAreaFiles("")
	files["Doc_0/Document.xml"] = strings.Replace(files["Doc_0/Document.xml"], "</Document>", `<ext:Data xmlns:ext="urn:extension" ext:flag="a&#xD;&#x9;&#xA;">before&#xD;after<![CDATA[ & < 中文 ]]></ext:Data></Document>`, 1)
	files["Doc_0/Document.xml"] = "\ufeff<?xml version = '1.0' encoding = 'utf-8' standalone = 'yes'?>\r\n" + files["Doc_0/Document.xml"]
	_, after := appliedChangesBytes(t, buildOFD(t, files), Options{DocType: "OFD-A"})
	doc := etree.NewDocument()
	if err := doc.ReadFromString(after["Doc_0/Document.xml"]); err != nil {
		t.Fatal(err)
	}
	metadata := doc.Root().FindElement("./Data")
	if metadata == nil || metadata.NamespaceURI() != "urn:extension" || metadata.SelectAttrValue("ext:flag", "") != "a\r\t\n" || metadata.Text() != "before\rafter & < 中文 " {
		t.Fatalf("读取和写回改变了字符引用或 CDATA: %s", after["Doc_0/Document.xml"])
	}
}

func TestPreserveReaderRejectsUnsupportedXMLBeforeWriting(t *testing.T) {
	for _, tc := range []struct {
		name, xml, message string
	}{
		{"doctype", `<!DOCTYPE Document><Document/>`, "DOCTYPE"},
		{"encoding", `<?xml version="1.0" encoding="GBK"?><Document/>`, "UTF-8 XML 1.0"},
		{"multiple roots", `<Document/><Document/>`, "根元素"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := pageAreaFiles("")
			files["Doc_0/Document.xml"] = tc.xml
			source := buildOFD(t, files)
			if _, err := Plan(source, Options{DocType: "OFD-A"}); err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("Plan 应返回读取器的错误: %v", err)
			}
			var out bytes.Buffer
			if _, err := Apply(source, &out, Options{DocType: "OFD-A"}); err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("Apply 应返回读取器的错误: %v", err)
			}
			if out.Len() != 0 {
				t.Fatal("读取失败后不应写出文件")
			}
		})
	}
}
