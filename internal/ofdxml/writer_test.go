package ofdxml

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"math"
	"testing"
)

func TestWriterAddsOFDNamespaceAndPrefix(t *testing.T) {
	w := New()
	w.StartElement("OFD")
	w.Attribute("Version", "1.0")
	w.StartElement("DocBody")
	w.StartElement("DocInfo")
	w.Element("DocID", "hello & <world>")
	w.EndElement()
	w.EndElement()
	w.EndElement()

	got := string(w.Bytes())
	want := `<?xml version="1.0" encoding="UTF-8"?><ofd:OFD xmlns:ofd="http://www.ofdspec.org/2016" Version="1.0"><ofd:DocBody><ofd:DocInfo><ofd:DocID>hello &amp; &lt;world&gt;</ofd:DocID></ofd:DocInfo></ofd:DocBody></ofd:OFD>`
	if got != want {
		t.Fatalf("unexpected XML:\n got: %s\nwant: %s", got, want)
	}

	var value struct{}
	if err := xml.Unmarshal(w.Bytes(), &value); err != nil {
		t.Fatalf("generated XML is invalid: %v", err)
	}
}

func TestWriterAttributesRoundTrip(t *testing.T) {
	w := New()
	w.Start("TextObject")
	value := "中文 & < > \" ' \t\n\r"
	w.Attr("Name", value)
	w.AttrUint("ID", math.MaxUint64)
	w.AttrInt("Index", -17)
	w.AttrFloat("Size", 4.2333333333)
	w.AttrBox("Boundary", -0.00001, 2.5, 210, 297)
	w.AttrFloatList("CTM", []float64{1, 0, 0, 1, 12.34567, -0.00001})
	w.AttrIntList("Glyphs", []int{0, 23, -1})
	w.End()
	var parsed struct {
		XMLName xml.Name
		Attrs   []xml.Attr `xml:",any,attr"`
	}
	if err := xml.Unmarshal(w.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.XMLName != (xml.Name{Space: Namespace, Local: "TextObject"}) {
		t.Fatalf("unexpected element namespace: %+v", parsed.XMLName)
	}
	want := map[string]string{
		"Name": value, "ID": "18446744073709551615", "Index": "-17",
		"Size": "4.2333", "Boundary": "0 2.5 210 297",
		"CTM": "1 0 0 1 12.3457 0", "Glyphs": "0 23 -1",
	}
	for _, attr := range parsed.Attrs {
		if attr.Name.Space == "xmlns" {
			continue
		}
		if attr.Name.Space != "" || attr.Value != want[attr.Name.Local] {
			t.Errorf("attribute %+v, want %q without a namespace", attr, want[attr.Name.Local])
		}
		delete(want, attr.Name.Local)
	}
	if len(want) != 0 {
		t.Fatalf("missing attributes: %v", want)
	}
}

func TestWriteToReportsOutputErrors(t *testing.T) {
	w := New()
	w.Element("DocID", "value")
	var output bytes.Buffer
	n, err := w.WriteTo(&output)
	if err != nil || n != int64(output.Len()) || !bytes.Equal(output.Bytes(), w.Bytes()) {
		t.Fatalf("WriteTo: n=%d, err=%v, output=%s", n, err, output.Bytes())
	}
	sentinel := errors.New("output failed")
	for _, test := range []struct {
		name string
		err  error
		want error
	}{
		{"short write", nil, io.ErrShortWrite},
		{"write failure", sentinel, sentinel},
	} {
		t.Run(test.name, func(t *testing.T) {
			n, err := w.WriteTo(partialWriter{err: test.err})
			if n != 1 || !errors.Is(err, test.want) {
				t.Fatalf("WriteTo: n=%d, err=%v, want n=1, err=%v", n, err, test.want)
			}
		})
	}
}

type partialWriter struct{ err error }

func (w partialWriter) Write([]byte) (int, error) { return 1, w.err }

func BenchmarkWriterReuse(b *testing.B) {
	for _, text := range []string{"plain ascii text", "中文正文与标点", "a & b < c > d"} {
		b.Run(text, func(b *testing.B) {
			w := New()
			b.ReportAllocs()
			for b.Loop() {
				w.Reset()
				w.Start("TextObject")
				w.AttrUint("ID", 42)
				w.AttrBox("Boundary", 10, 20, 80, 8)
				w.AttrFloat("Size", 4.2333)
				w.Element("TextCode", text)
				w.End()
				_ = w.Bytes()
			}
		})
	}
}

func TestWriterReplacesInvalidXMLCharacters(t *testing.T) {
	w := New()
	w.StartElement("TextCode")
	w.Text("A\x00B\x1fC")
	w.EndElement()

	if !bytes.Contains(w.Bytes(), []byte("\uFFFD")) {
		t.Fatalf("invalid XML characters were not replaced: %s", w.Bytes())
	}
}

func TestWriterCanBeReused(t *testing.T) {
	w := New()
	w.StartElement("First")
	w.EndElement()
	w.Reset()
	w.Element("Second", "value")

	if got, want := string(w.Bytes()), `<?xml version="1.0" encoding="UTF-8"?><ofd:Second xmlns:ofd="http://www.ofdspec.org/2016">value</ofd:Second>`; got != want {
		t.Fatalf("unexpected reused output: got %q, want %q", got, want)
	}
}

func TestTextPreservesLineEndings(t *testing.T) {
	for _, value := range []string{"甲\r乙", "甲\r\n乙", "甲\n乙\t丙", "]]>", "&<\"'中文"} {
		w := New()
		w.Element("TextCode", value)
		var parsed struct {
			Value string `xml:",chardata"`
		}
		if err := xml.Unmarshal(w.Bytes(), &parsed); err != nil {
			t.Fatal(err)
		}
		if parsed.Value != value {
			t.Errorf("文本往返后改变: 输入 %q，输出 %q", value, parsed.Value)
		}
	}
}

func TestRawPreservesFragmentAndParentNamespace(t *testing.T) {
	w := New()
	w.Start("Data")
	fragment := []byte(`before<cfg:Config xmlns:cfg="urn:config"><![CDATA[甲<&乙]]>&#x41;</cfg:Config><!--note-->after`)
	w.Raw(fragment)
	w.Element("DocID", "tail & value")
	w.End()
	data := w.Bytes()
	if !bytes.Contains(data, fragment) {
		t.Fatalf("原始片段发生变化: %s", data)
	}
	var parsed struct {
		XMLName xml.Name
		Config  string `xml:"urn:config Config"`
		DocID   string `xml:"http://www.ofdspec.org/2016 DocID"`
	}
	if err := xml.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.XMLName.Space != Namespace || parsed.Config != "甲<&乙A" || parsed.DocID != "tail & value" {
		t.Fatalf("片段写入后元素语义不正确: %+v", parsed)
	}
}
