package ofdxml

import (
	"bytes"
	"encoding/xml"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/beevik/etree"
)

func TestParseMarshalPreservesXMLSemantics(t *testing.T) {
	for _, input := range []string{
		`<?xml version="1.0" encoding="UTF-8"?><ofd:Page xmlns:ofd="http://www.ofdspec.org/2016"><ofd:Content/></ofd:Page>`,
		"\ufeff" + `<?xml version="1.0"?><Root A="&quot;&apos;&amp;&lt;&gt;&#x9;&#xA;&#xD;">&#65;&#x1F600; &amp; 中文𠀀</Root>`,
		"<Root>\n  <Value>  保留 空格  </Value>\n</Root>",
		`<!--before--><Root><!--kept--><!---->before<![CDATA[a<&b]]><Value/>after</Root><!--tail-->`,
		`<q:Root xmlns:q="http://www.ofdspec.org/2016" xmlns:ofd="urn:foreign" ofd:flag="keep"><q:Value/><ofd:Value/><Value xmlns="urn:default"><Value xmlns=""/></Value></q:Root>`,
		`<ext:Root xmlns:ext="urn:extension"><q:Page xmlns:q="http://www.ofdspec.org/2016"><q:Content/></q:Page></ext:Root>`,
		`<q:Root xmlns:q="http://www.ofdspec.org/2016"><q:Value/><q:Value xmlns:q="urn:foreign"><q:Nested/></q:Value></q:Root>`,
		`<?xml version='1.0' encoding='UTF-8' standalone='yes'?><?vendor a='b'?><Root/>`,
		"<Root A=\"literal\nnewline\tand\rreturn\">line\r\nbreak\rand\ttab</Root>",
		`<根 属性="值"><子节点>正文</子节点></根>`,
		`<Root>&#00000000000000000000000000000065;</Root>`,
		"<?xml\r\nversion = '1.0'\tencoding = 'uTf-8'\nstandalone = 'no' ?>\r\n<Root/>",
		`<?vendor free text with 'unmatched quote and > & symbols?><Root><?nested a="b" plain text?></Root><?empty?>`,
		"<Root\r\n\tA = 'a\tb\nc\rd\r\ne&#x9;&#xA;&#xD;'\nB = \"x\" >\r\n\t<![CDATA[a\rb\r\nc]]>\r\n</Root\t>",
	} {
		t.Run(input, func(t *testing.T) {
			original := []byte(input)
			doc, err := Parse(original)
			if err != nil {
				t.Fatal(err)
			}
			if string(original) != input {
				t.Fatal("解析改写了输入切片")
			}
			data, err := Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			assertXMLSemantics(t, []byte(input), data)
		})
	}
}

func TestParsePreservesInstructionContents(t *testing.T) {
	for _, input := range []string{
		`<?producer text with > & and 'unmatched quote?><Root/>`,
		"<?vendor \t keep\r\nlines and\tspaces ?><Root><!--keep\r\ncomment--></Root>",
	} {
		t.Run(input, func(t *testing.T) {
			reference := etree.NewDocument()
			reference.ReadSettings.PreserveCData = true
			if err := reference.ReadFromBytes(referenceXMLInput([]byte(input))); err != nil {
				t.Fatal(err)
			}
			doc, err := Parse([]byte(input))
			if err != nil {
				t.Fatal(err)
			}
			want, err := Marshal(reference)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Marshal(doc)
			if err != nil || !bytes.Equal(want, got) {
				t.Fatalf("指令内容与已有解析语义不一致:\nwant %s\ngot %s (err=%v)", want, got, err)
			}
		})
	}
}

func TestParseNormalizesLiteralWhitespaceAndPreservesReferences(t *testing.T) {
	doc, err := Parse([]byte("<Root A='a\tb\nc\rd\r\ne&#x9;&#xA;&#xD;'>x\r&#xA;y&#xD;\nz<![CDATA[a\r\nb\rc]]></Root>"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := doc.Root().SelectAttrValue("A", ""), "a b c d e\t\n\r"; got != want {
		t.Fatalf("属性空白或字符引用改变: got %q, want %q", got, want)
	}
	if got, want := doc.Root().Text(), "x\n\ny\r\nza\nb\nc"; got != want {
		t.Fatalf("文本换行改变: got %q, want %q", got, want)
	}
}

func TestParseAllowsAttributeTextAndStructuralEdits(t *testing.T) {
	const input = `<q:Page xmlns:q="http://www.ofdspec.org/2016" xmlns:ext="urn:extension"><q:Content><q:Layer ID="1"><q:TextObject ID="2"><q:TextCode>旧文字</q:TextCode></q:TextObject><ext:Keep ext:flag="yes"/></q:Layer></q:Content></q:Page>`
	doc, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	layer := doc.Root().FindElement("./Content/Layer")
	layer.CreateAttr("ID", "10")
	layer.RemoveChild(layer.SelectElement("q:TextObject"))
	text := etree.NewElement("q:TextObject")
	text.CreateAttr("ID", "11")
	text.CreateElement("q:TextCode").SetText(" 新文字 & < > \r ")
	layer.InsertChild(layer.Child[0], text)
	before, err := doc.WriteToBytes()
	if err != nil {
		t.Fatal(err)
	}
	data, err := Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	want := `<q:Page xmlns:q="http://www.ofdspec.org/2016" xmlns:ext="urn:extension"><q:Content><q:Layer ID="10"><q:TextObject ID="11"><q:TextCode> 新文字 &amp; &lt; &gt; &#xD; </q:TextCode></q:TextObject><ext:Keep ext:flag="yes"/></q:Layer></q:Content></q:Page>`
	assertXMLSemantics(t, []byte(want), data)
	after, err := doc.WriteToBytes()
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("写回修改了节点树: err=%v", err)
	}
}

func TestParseReportsMalformedDocuments(t *testing.T) {
	for _, input := range []string{
		"", "<!--no root-->", "<!-->", "<Root>", "<Root></Other>", "<Root/><Other/>",
		"text<Root/>", "<Root/>text", "<![CDATA[ ]]><Root/>",
		`<Root A="unterminated/>`, `<Root A=no/>`, `<Root A="x"B="y"/>`, `<Root A="1" A="2"/>`,
		`<Root A="<"/>`, `<Root>&unknown;</Root>`, `<Root>&#0;</Root>`,
		`<Root>&#x110000;</Root>`, `<Root>&#xD800;</Root>`, `<Root>&#X41;</Root>`,
		`<Root>]]></Root>`, `<Root><!--bad--comment--></Root>`,
		`<Root><![CDATA[unterminated</Root>`, "<Root>\x00</Root>", "<Root>\xff</Root>",
		"<1Root/>", "<\u0301Root/>", "<\u037eRoot/>", "<a::Root/>", "<Root:/>",
		`<?xml?><Root/>`, `<?xml version="1.0"version="1.0"?><Root/>`,
		`<?xml encoding="UTF-8" version="1.0"?><Root/>`,
		`<?xml version="1.0" standalone="maybe"?><Root/>`,
		`<?xml version="1.0" unexpected="yes"?><Root/>`,
		`<Root><?xml version="1.0"?></Root>`, ` <?xml version="1.0"?><Root/>`,
		`<?XML version="1.0"?><Root/>`, `<?vendor unclosed<Root/>`,
	} {
		t.Run(input, func(t *testing.T) {
			if doc, err := Parse([]byte(input)); err == nil || doc != nil {
				t.Fatalf("应拒绝无效 XML: doc=%v, err=%v", doc, err)
			}
		})
	}
}

func TestParseReportsUnsupportedVersionOrEncoding(t *testing.T) {
	for _, input := range []string{
		`<?xml version="1.1"?><Root/>`,
		`<?xml version="1.0" encoding="GBK"?><Root/>`,
		`<?xml version="1.0" encoding="UTF-16"?><Root/>`,
	} {
		if doc, err := Parse([]byte(input)); doc != nil || err == nil || !strings.Contains(err.Error(), "UTF-8 XML 1.0") {
			t.Fatalf("应说明支持的版本和编码: input=%q, doc=%v, err=%v", input, doc, err)
		}
	}
}

func TestParseRejectsDocumentTypeDeclarations(t *testing.T) {
	if _, err := Parse([]byte(`<!DOCTYPE Root><Root/>`)); err == nil || !strings.Contains(err.Error(), "DOCTYPE") {
		t.Fatalf("OFD XML 读取应明确报告不支持的 DOCTYPE: %v", err)
	}
}

func TestParseEnforcesDepthLimit(t *testing.T) {
	input := strings.Repeat("<A>", 1025) + strings.Repeat("</A>", 1025)
	if _, err := Parse([]byte(input)); err == nil {
		t.Fatal("应拒绝超出深度上限的文档")
	}
}

func TestParseTreeAndMarshalOutputHaveIndependentOwnership(t *testing.T) {
	input := []byte(`<q:Root xmlns:q="urn:root" q:flag="value"><q:Value>text</q:Value></q:Root>`)
	doc, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Clone(input)
	clear(input)
	output, err := Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	doc.Root().CreateAttr("q:flag", "changed")
	for i := 0; i < 10; i++ {
		if _, err := Marshal(doc); err != nil {
			t.Fatal(err)
		}
	}
	assertXMLSemantics(t, want, output)
}

func TestParseTreatsCDATAAsOrdinaryText(t *testing.T) {
	input := []byte(`<Root>before<![CDATA[a<&b &amp; &#x41;]]><Child/>after</Root>`)
	doc, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, child := range doc.Root().Child {
		if text, ok := child.(*etree.CharData); ok && text.IsCData() {
			t.Fatal("读取后不应保留 CDATA 标记")
		}
	}
	if got, want := doc.Root().Text(), "beforea<&b &amp; &#x41;"; got != want {
		t.Fatalf("CDATA 内容被错误解码: got %q, want %q", got, want)
	}
	data, err := Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("<![CDATA[")) {
		t.Fatalf("写回不应生成 CDATA: %s", data)
	}
	assertXMLSemantics(t, input, data)
}

func TestMarshalWritesCDATAAsEscapedText(t *testing.T) {
	for _, text := range []string{"before]]>after", "before\rafter", strings.Repeat("x", 2<<20)} {
		doc := etree.NewDocument()
		doc.CreateElement("Root").CreateCData(text)
		data, err := Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("<![CDATA[")) {
			t.Fatal("外部树中的 CDATA 也应作为普通文本写出")
		}
		parsed, err := Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		var got strings.Builder
		for _, child := range parsed.Root().Child {
			if text, ok := child.(*etree.CharData); ok {
				got.WriteString(text.Data)
			}
		}
		if got.String() != text {
			t.Fatal("CDATA 写回改变了文本")
		}
	}
}

func TestMarshalRejectsMissingRootAndNestedDeclaration(t *testing.T) {
	for _, doc := range []*etree.Document{nil, etree.NewDocument()} {
		if _, err := Marshal(doc); err == nil {
			t.Fatal("应拒绝缺少根元素的文档")
		}
	}
	doc := etree.NewDocument()
	doc.CreateElement("Root").CreateProcInst("xml", `version="1.0"`)
	if _, err := Marshal(doc); err == nil {
		t.Fatal("应拒绝嵌套 XML 声明")
	}
}

func FuzzParseMatchesEtree(f *testing.F) {
	for _, input := range []string{
		`<Root A="value">text &amp; 中文</Root>`,
		`<?xml version="1.0" encoding="UTF-8"?><q:Root xmlns:q="urn:root"><q:Value/></q:Root>`,
		`<!--before--><Root><![CDATA[a<&b]]><!--keep--></Root>`,
		"<Root A=\"a\nb\">\r\ntext\t</Root>",
		`<Root>&#65;&#x1F600;&#xD;</Root>`,
		`<?vendor keep?><Root/>`,
		`<?vendor free ' " > & text?><Root/>`,
		`<?xml version = '1.0' encoding = 'uTf-8' standalone = 'yes'?><根 属性="值"/>`,
		`<Root A="x"B="y"/>`, `<Root>&#xD800;</Root>`, `<Root/>tail`, "<Root>", "<!-->",
	} {
		f.Add([]byte(input))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		reference := etree.NewDocument()
		reference.ReadSettings.PreserveCData = true
		referenceErr := reference.ReadFromBytes(referenceXMLInput(input))
		doc, err := Parse(input)
		if err != nil {
			return
		}
		if referenceErr != nil {
			// 标准库使用 XML 旧版名称表；名称范围差异不属于树构建差异。
			if strings.Contains(referenceErr.Error(), "invalid XML name") {
				return
			}
			t.Fatalf("快速读取接受了 etree 拒绝的输入: %v", referenceErr)
		}
		want, referenceErr := Marshal(reference)
		got, err := Marshal(doc)
		if (referenceErr == nil) != (err == nil) || !bytes.Equal(want, got) {
			t.Fatalf("快速读取与 etree 的树写回不同:\nwant %s (err=%v)\ngot  %s (err=%v)", want, referenceErr, got, err)
		}
	})
}

func assertXMLSemantics(t *testing.T, want, got []byte) {
	t.Helper()
	tokens := func(data []byte) []xml.Token {
		decoder := xml.NewDecoder(bytes.NewReader(referenceXMLInput(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))))
		var result []xml.Token
		for {
			token, err := decoder.Token()
			if err == io.EOF {
				return result
			}
			if err != nil {
				t.Fatalf("无法解析 XML: %v\n%s", err, data)
			}
			if pi, ok := token.(xml.ProcInst); ok && pi.Target == "xml" {
				continue
			}
			if text, ok := token.(xml.CharData); ok && len(result) > 0 {
				if previous, ok := result[len(result)-1].(xml.CharData); ok {
					result[len(result)-1] = append(previous, text...)
					continue
				}
			}
			result = append(result, xml.CopyToken(token))
		}
	}
	if !reflect.DeepEqual(tokens(want), tokens(got)) {
		t.Fatalf("XML 语义改变:\nwant %s\ngot  %s", want, got)
	}
}

// encoding/xml 没有实现 XML 1.0 的完整空白归一化。对照读取前先按 §2.11
// 合并字面换行，再只把开始标签引号内的字面 tab/LF 转为空格；字符引用不变。
func referenceXMLInput(input []byte) []byte {
	input = bytes.ReplaceAll(bytes.ReplaceAll(input, []byte("\r\n"), []byte("\n")), []byte("\r"), []byte("\n"))
	if !bytes.ContainsAny(input, "\t\n") {
		return input
	}
	result := bytes.Clone(input)
	decoder := xml.NewDecoder(bytes.NewReader(input))
	for {
		start := int(decoder.InputOffset())
		token, err := decoder.RawToken()
		if err != nil {
			return result
		}
		if _, ok := token.(xml.StartElement); !ok {
			continue
		}
		quote := byte(0)
		for i := start; i < int(decoder.InputOffset()); i++ {
			c := input[i]
			if quote == 0 {
				if c == '\'' || c == '"' {
					quote = c
				}
			} else if c == quote {
				quote = 0
			} else if c == '\t' || c == '\n' {
				result[i] = ' '
			}
		}
	}
}
