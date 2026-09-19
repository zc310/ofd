package creator

import (
	"bytes"
	"encoding/xml"
	"testing"
)

// TestStreamXMLWriterReplacesInvalidXMLChars 验证文本和属性中的 XML 非法字符
// （如 PDF 文本里的控制符）会被替换为 U+FFFD，否则生成的 OFD 无法解析
// （PCDATA invalid Char value）。
func TestStreamXMLWriterReplacesInvalidXMLChars(t *testing.T) {
	w := newStreamXMLWriter()
	w.Start("TextCode")
	w.Text("A\x1DB\x00C\x0bD\x0cE")
	w.End()
	out := w.Bytes()
	for _, b := range out {
		if b < 0x20 && b != 0x09 && b != 0x0A && b != 0x0D {
			t.Fatalf("输出含非法 XML 字节 %#x:\n%s", b, out)
		}
	}
	if !bytes.Contains(out, []byte("\uFFFD")) {
		t.Fatalf("非法字符未被替换:\n%s", out)
	}
	var parsed struct{}
	if err := xml.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("生成的 XML 无法解析: %v\n%s", err, out)
	}

	attr := newStreamXMLWriter()
	attr.Start("TextObject")
	attr.Attr("Value", "x\x1Dy")
	attr.End()
	for _, b := range attr.Bytes() {
		if b < 0x20 && b != 0x09 && b != 0x0A && b != 0x0D {
			t.Fatalf("属性含非法 XML 字节 %#x:\n%s", b, attr.Bytes())
		}
	}
	if err := xml.Unmarshal(attr.Bytes(), &parsed); err != nil {
		t.Fatalf("属性 XML 无法解析: %v\n%s", err, attr.Bytes())
	}
}
