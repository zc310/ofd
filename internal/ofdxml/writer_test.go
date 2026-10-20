package ofdxml

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"math"
	"strings"
	"sync"
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

func TestWriterTypedElementsRoundTrip(t *testing.T) {
	w := New()
	defer Put(w)
	w.Start("Root")
	w.ElementUint("MaxUnitID", math.MaxUint64)
	w.ElementInt("Index", -17)
	w.ElementFloat("Zoom", 4.2333333333)
	w.ElementBox("PhysicalBox", -0.00001, 2.5, 210, 297)
	w.ElementFloatList("Deltas", []float64{1, 0, 0, 1, 12.34567, -0.00001})
	w.ElementIntList("Glyphs", []int{0, 23, -1})
	w.End()

	var parsed struct {
		MaxUnitID   string `xml:"http://www.ofdspec.org/2016 MaxUnitID"`
		Index       string `xml:"http://www.ofdspec.org/2016 Index"`
		Zoom        string `xml:"http://www.ofdspec.org/2016 Zoom"`
		PhysicalBox string `xml:"http://www.ofdspec.org/2016 PhysicalBox"`
		Deltas      string `xml:"http://www.ofdspec.org/2016 Deltas"`
		Glyphs      string `xml:"http://www.ofdspec.org/2016 Glyphs"`
	}
	if err := xml.Unmarshal(w.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"MaxUnitID": "18446744073709551615", "Index": "-17", "Zoom": "4.2333",
		"PhysicalBox": "0 2.5 210 297", "Deltas": "1 0 0 1 12.3457 0", "Glyphs": "0 23 -1",
	}
	for name, got := range map[string]string{
		"MaxUnitID": parsed.MaxUnitID, "Index": parsed.Index, "Zoom": parsed.Zoom,
		"PhysicalBox": parsed.PhysicalBox, "Deltas": parsed.Deltas, "Glyphs": parsed.Glyphs,
	} {
		if got != want[name] {
			t.Errorf("元素 %s: got %q, want %q", name, got, want[name])
		}
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

// useSingleWriterPool 保证池命中与池丢弃对象后都取回同一个 Writer，
// 避免 GC 或 race 模式使复用测试退化为新对象测试。使用者不能并行运行，
// 且必须归还当前 Writer 后才能再次 New。
func useSingleWriterPool(t *testing.T, w *Writer) {
	t.Helper()
	writerPool = sync.Pool{New: func() any { return w }}
	t.Cleanup(func() {
		writerPool = sync.Pool{New: func() any { return newWriter() }}
	})
}

// TestWriterPoolResetsReusedState 验证未闭合元素直接 Put 后再次 New，
// 同一个 Writer 的缓冲区、元素栈和开始标签状态都已复位。
func TestWriterPoolResetsReusedState(t *testing.T) {
	for _, text := range []bool{false, true} {
		name := "open start tag"
		if text {
			name = "text content"
		}
		t.Run(name, func(t *testing.T) {
			original := newWriter()
			useSingleWriterPool(t, original)
			w := New()
			w.Start("First")
			w.Attr("ID", "1")
			w.Start("Child")
			w.Attr("Name", "old")
			if text {
				w.Text("value")
			}
			oldStack := w.stack
			Put(w)

			reused := New()
			defer Put(reused)
			if reused != original {
				t.Fatal("未复用指定 Writer")
			}
			if reused.open || len(reused.stack) != 0 {
				t.Fatalf("复用后状态未清空: open=%v stack=%v", reused.open, reused.stack)
			}
			for _, name := range oldStack {
				if name != (elementName{}) {
					t.Fatalf("元素栈仍持有旧名称 %+v", name)
				}
			}
			reused.Element("Second", "two")
			want := `<?xml version="1.0" encoding="UTF-8"?><ofd:Second xmlns:ofd="http://www.ofdspec.org/2016">two</ofd:Second>`
			if got := string(reused.Bytes()); got != want {
				t.Fatalf("池复用后状态未复位: got %q, want %q", got, want)
			}
		})
	}
}

func TestPutRetainsOnlyBuffersWithinCapacityLimit(t *testing.T) {
	for _, test := range []struct {
		name     string
		capacity int
		retain   bool
	}{
		{"at pool limit", maxPooledCapacity, true},
		{"above pool limit", maxPooledCapacity + 1, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := &Writer{buf: make([]byte, 0, test.capacity)}
			useSingleWriterPool(t, original)
			w := New()
			w.Start("Unfinished")
			buffer := &w.buf[0]
			Put(w)

			reused := New()
			defer Put(reused)
			if reused != original {
				t.Fatal("未复用指定 Writer")
			}
			if retained := &reused.buf[0] == buffer; retained != test.retain {
				t.Fatalf("缓冲区保留 = %v, want %v", retained, test.retain)
			}
			if cap(reused.buf) > maxPooledCapacity {
				t.Fatalf("复用后仍保留超限缓冲区: cap=%d", cap(reused.buf))
			}
		})
	}
}

// TestDoneReturnsIndependentBytes 强制复用同一个 Writer 后重写文档，
// 验证复制和移交两条路径的返回值都不会被覆盖。
func TestDoneReturnsIndependentBytes(t *testing.T) {
	for _, test := range []struct {
		name     string
		capacity int
		value    string
	}{
		{"copied buffer", 4096, "value"},
		{"transferred buffer", maxPooledCapacity + 512, strings.Repeat("x", maxPooledCapacity)},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := &Writer{buf: make([]byte, 0, test.capacity)}
			useSingleWriterPool(t, original)
			w := New()
			w.Element("First", test.value)
			buffer := &w.buf[0]
			data := w.Done()

			reused := New()
			defer Put(reused)
			if reused != original {
				t.Fatal("未复用指定 Writer")
			}
			if retained := &reused.buf[0] == buffer; retained != (test.capacity <= maxPooledCapacity) {
				t.Fatalf("复用 Writer 的缓冲区保留状态错误: retained=%v", retained)
			}
			reused.Element("Second", "a much longer payload to force overwrite")
			_ = reused.Bytes()

			want := `<?xml version="1.0" encoding="UTF-8"?><ofd:First xmlns:ofd="http://www.ofdspec.org/2016">` + test.value + `</ofd:First>`
			if string(data) != want {
				t.Fatal("Done 返回值被后续复用污染")
			}
		})
	}
}

func TestDoneCopiesPooledBuffersAndTransfersOversizedBuffers(t *testing.T) {
	for _, test := range []struct {
		name     string
		capacity int
		value    string
		transfer bool
	}{
		{"small", 4096, "value", false},
		{"at pool limit", maxPooledCapacity, "value", false},
		{"above pool limit with short output", maxPooledCapacity + 1, "value", true},
		{"large output", 2<<20 + 1024, strings.Repeat("x", 2<<20), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := &Writer{buf: make([]byte, 0, test.capacity)}
			w.Reset()
			w.Start("Root")
			w.Text(test.value)
			original := &w.buf[0]
			data := w.Done()
			if transferred := &data[0] == original; transferred != test.transfer {
				t.Fatalf("缓冲区移交 = %v, want %v", transferred, test.transfer)
			}
			var parsed struct {
				Value string `xml:",chardata"`
			}
			if err := xml.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("Done 未生成完整 XML: %v", err)
			}
			if parsed.Value != test.value {
				t.Fatal("Done 改变了文本内容")
			}
		})
	}
}

func TestPutNilIsNoop(t *testing.T) {
	Put(nil)
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
