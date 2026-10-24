package xmlcodec

import (
	"bytes"
	"encoding/xml"
	"io"
	"reflect"
	"testing"
)

func TestQualifiedElementsPreserveNamespacesAndOFDChildren(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "memory"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			w := New()
			defer Put(w)
			var out bytes.Buffer
			if stream {
				w.ResetTo(&out)
			}
			w.Directive(`DOCTYPE ofd:Res`)
			w.Start("Res")
			w.Attr("xmlns:ext", "urn:extension")
			w.StartQualified("ext:Value")
			w.Attr("ext:flag", "a & b")
			w.Text("before")
			w.Start("Font")
			w.AttrUint("ID", 1)
			w.End()
			w.Text("after")
			w.End()
			w.StartQualified("Payload")
			w.Attr("xmlns", "urn:default")
			w.StartQualified("Child")
			w.Attr("xmlns", "")
			w.Text("plain")
			w.End()
			w.End()
			w.End()
			var data []byte
			if stream {
				if err := w.Finish(); err != nil {
					t.Fatal(err)
				}
				data = out.Bytes()
			} else {
				data = w.Bytes()
			}
			decoder := xml.NewDecoder(bytes.NewReader(data))
			var elements []xml.Name
			for {
				token, err := decoder.Token()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("XML 无法解析: %v\n%s", err, data)
				}
				if element, ok := token.(xml.StartElement); ok {
					elements = append(elements, element.Name)
					if element.Name.Local == "Value" && (len(element.Attr) != 1 || element.Attr[0] != (xml.Attr{Name: xml.Name{Space: "urn:extension", Local: "flag"}, Value: "a & b"})) {
						t.Fatalf("扩展属性未保留命名空间或转义: %+v", element.Attr)
					}
				}
			}
			want := []xml.Name{{Space: Namespace, Local: "Res"}, {Space: "urn:extension", Local: "Value"}, {Space: Namespace, Local: "Font"}, {Space: "urn:default", Local: "Payload"}, {Local: "Child"}}
			if !reflect.DeepEqual(elements, want) {
				t.Fatalf("命名空间错误: got %+v, want %+v", elements, want)
			}
			if !bytes.Contains(data, []byte(`<!DOCTYPE ofd:Res>`)) || !bytes.Contains(data, []byte(`</ext:Value>`)) || !bytes.Contains(data, []byte(`</Payload>`)) {
				t.Fatalf("扩展结束标签或指令错误: %s", data)
			}
		})
	}
}
