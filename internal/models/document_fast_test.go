package models

import (
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
)

// documentXMLFromOFD 从 OFD 包中取出文档主体 XML 的原始字节。
func documentXMLFromOFD(tb testing.TB, name string) []byte {
	tb.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		tb.Skipf("测试文档 %s 不可用: %v", name, err)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		tb.Fatal(err)
	}
	for _, file := range reader.File {
		if !strings.HasSuffix(file.Name, "Document.xml") {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			tb.Fatal(err)
		}
		raw, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			tb.Fatal(err)
		}
		return raw
	}
	tb.Fatalf("包 %s 内找不到 Document.xml", name)
	return nil
}

// 快速路径必须与 encoding/xml 的解析结果逐字段一致。XMLName 由标准库按根元素
// 命名空间填充，快速路径只填 Local，比较时单独排除。
func TestParseDocumentXMLMatchesEncodingXML(t *testing.T) {
	for _, name := range []string{
		"helloworld.ofd",
		"ofdrw/intro.ofd",
		"preferences.ofd",
		"annotations.ofd",
		"multi_doc.ofd",
		"1000-pages.ofd",
	} {
		t.Run(name, func(t *testing.T) {
			raw := documentXMLFromOFD(t, name)

			var want Document
			if err := xml.Unmarshal(raw, &want); err != nil {
				t.Fatalf("encoding/xml 解析失败: %v", err)
			}
			got, err := ParseDocumentXML(raw)
			if err != nil {
				t.Fatalf("ParseDocumentXML 失败: %v", err)
			}

			got.XMLName = xml.Name{}
			want.XMLName = xml.Name{}
			if !reflect.DeepEqual(*got, want) {
				t.Fatalf("解析结果不一致\n got=%#v\nwant=%#v", *got, want)
			}
			if got.XMLNS != want.XMLNS {
				t.Fatalf("XMLNS 不一致: got=%q want=%q", got.XMLNS, want.XMLNS)
			}
		})
	}
}

func BenchmarkParseDocumentXML(b *testing.B) {
	raw := documentXMLFromOFD(b, "1000-pages.ofd")
	b.SetBytes(int64(len(raw)))
	b.Run("encoding/xml", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var doc Document
			if err := xml.Unmarshal(raw, &doc); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("lexer", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := ParseDocumentXML(raw); err != nil {
				b.Fatal(err)
			}
		}
	})
}
