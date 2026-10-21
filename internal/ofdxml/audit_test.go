package ofdxml

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/beevik/etree"
	"github.com/knroy/go-xml/xdm"
)

func TestWriteElementPreservesInheritedForeignOFDBinding(t *testing.T) {
	const input = `<Root xmlns:q="http://www.ofdspec.org/2016" xmlns:ofd="urn:foreign"><q:Child ofd:flag="keep" Type="ofd:Extension"/></Root>`
	doc := etree.NewDocument()
	if err := doc.ReadFromString(input); err != nil {
		t.Fatal(err)
	}
	w := New()
	defer Put(w)
	w.StartQualified("Root")
	w.Attr("xmlns:q", Namespace)
	w.Attr("xmlns:ofd", "urn:foreign")
	if err := WriteElement(w, doc.Root().ChildElements()[0]); err != nil {
		t.Fatal(err)
	}
	w.End()
	assertExpandedXMLNames(t, []byte(input), w.Bytes())
}

func TestParseWhitespaceMatchesIndependentXMLReader(t *testing.T) {
	input := []byte("<?vendor a\r\nb?><ofd:Font xmlns:ofd='http://www.ofdspec.org/2016' FontName='a\tb\nc\rd\r\ne&#x9;&#xA;&#xD;'><![CDATA[x\r\ny]]><!--a\r\nb--></ofd:Font>")
	reference, err := xdm.Parse(bytes.NewReader(input), xdm.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	root := reference.Root.ChildElements()[0]
	if got, want := doc.Root().SelectAttrValue("FontName", ""), root.Attrs[0].Value; got != want {
		t.Fatalf("XML 属性空白归一化不同: got %q, want %q", got, want)
	}
	if got, want := doc.Root().Text(), root.StringValue(); got != want {
		t.Fatalf("XML 文本换行归一化不同: got %q, want %q", got, want)
	}
}

func TestMarshalPreservesImplicitXMLPrefix(t *testing.T) {
	input := []byte(`<Root xmlns:space="urn:extension" xml:space="preserve"><space:Value xml:lang="zh-CN"/></Root>`)
	doc, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, marshal := range []func(*etree.Document) ([]byte, error){Marshal, MarshalDocument} {
		data, err := marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		assertXMLSemantics(t, input, data)
	}
}

func TestMarshalResolvesManuallyAddedAttributeFromContainingElement(t *testing.T) {
	doc, err := Parse([]byte(`<Root xmlns:ext="urn:extension"/>`))
	if err != nil {
		t.Fatal(err)
	}
	doc.Root().Attr = append(doc.Root().Attr, etree.Attr{Space: "ext", Key: "flag", Value: "keep"})
	for _, marshal := range []func(*etree.Document) ([]byte, error){Marshal, MarshalDocument} {
		data, err := marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		assertXMLSemantics(t, []byte(`<Root xmlns:ext="urn:extension" ext:flag="keep"/>`), data)
	}
}

func TestWriteElementReturnsStreamingFailure(t *testing.T) {
	sentinel := errors.New("stream failed")
	for _, test := range []struct {
		name string
		err  error
		want error
	}{{"write error", sentinel, sentinel}, {"short write", nil, io.ErrShortWrite}} {
		t.Run(test.name, func(t *testing.T) {
			root := etree.NewElement("Root")
			root.CreateText(strings.Repeat("x", 2*streamBufferSize))
			w := New()
			defer Put(w)
			dst := &failingChunkWriter{err: test.err}
			w.ResetTo(dst)
			if err := WriteElement(w, root); !errors.Is(err, test.want) {
				t.Fatalf("WriteElement 丢失流式输出错误: %v", err)
			}
			if err := WriteElement(w, etree.NewElement("Later")); !errors.Is(err, test.want) {
				t.Fatalf("应保留第一个输出错误: %v", err)
			}
			if dst.calls != 1 || !errors.Is(w.Finish(), test.want) {
				t.Fatalf("错误后继续写入: calls=%d, err=%v", dst.calls, w.Err())
			}
		})
	}
}

func TestMarshalRejectsInvalidEditedTree(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*etree.Document)
	}{
		{"multiple roots", func(doc *etree.Document) { doc.CreateElement("Other") }},
		{"document text", func(doc *etree.Document) { doc.CreateText("outside") }},
		{"document CDATA", func(doc *etree.Document) { doc.CreateCData(" ") }},
		{"misplaced declaration", func(doc *etree.Document) { doc.CreateProcInst("xml", `version="1.0"`) }},
		{"unsupported directive", func(doc *etree.Document) { doc.Root().CreateDirective("DOCTYPE Root") }},
		{"comment delimiter", func(doc *etree.Document) { doc.Root().CreateComment("bad--comment") }},
		{"comment trailing dash", func(doc *etree.Document) { doc.Root().CreateComment("bad-") }},
		{"PI target", func(doc *etree.Document) { doc.Root().CreateProcInst("bad target", "data") }},
		{"PI delimiter", func(doc *etree.Document) { doc.Root().CreateProcInst("vendor", "data?>tail") }},
		{"CDATA NUL", func(doc *etree.Document) { doc.Root().CreateCData("bad\x00text") }},
		{"CDATA UTF-8", func(doc *etree.Document) { doc.Root().CreateCData("bad\xfftext") }},
		{"text NUL", func(doc *etree.Document) { doc.Root().SetText("bad\x00text") }},
		{"attribute NUL", func(doc *etree.Document) { doc.Root().CreateAttr("Value", "bad\x00text") }},
		{"element name", func(doc *etree.Document) { doc.Root().CreateElement("bad name") }},
		{"attribute name", func(doc *etree.Document) { doc.Root().CreateAttr("bad name", "value") }},
		{"undeclared element prefix", func(doc *etree.Document) { doc.Root().CreateElement("unknown:Value") }},
		{"undeclared attribute prefix", func(doc *etree.Document) { doc.Root().CreateAttr("unknown:Value", "value") }},
		{"invalid namespace declaration", func(doc *etree.Document) { doc.Root().CreateAttr("xmlns:xml", "urn:foreign") }},
		{"duplicate expanded attributes", func(doc *etree.Document) {
			doc.Root().CreateAttr("xmlns:a", "urn:extension")
			doc.Root().CreateAttr("xmlns:b", "urn:extension")
			doc.Root().CreateAttr("a:Value", "one")
			doc.Root().CreateAttr("b:Value", "two")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, marshal := range []func(*etree.Document) ([]byte, error){Marshal, MarshalDocument} {
				doc := etree.NewDocument()
				doc.CreateElement("Root")
				test.edit(doc)
				if data, err := marshal(doc); err == nil || data != nil {
					t.Fatalf("应拒绝非法编辑结果: err=%v, output=%s", err, data)
				}
			}
		})
	}
}
