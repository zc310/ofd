package models

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/pkg/spec"
)

// documentXMLFromOFD 从 OFD 包中取出文档主体 XML 的原始字节。
func documentXMLFromOFD(tb testing.TB, name string) []byte {
	tb.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			tb.Skipf("缺少测试文档 %s: %v", name, err)
		}
		tb.Fatal(err)
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
		"ofd-creator/helloworld.ofd",
		"ofdrw/intro.ofd",
		"ofd-creator/preferences.ofd",
		"ofd-creator/annotations.ofd",
		"multi_doc.ofd",
		"ofd-creator/1000-pages.ofd",
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
	raw := documentXMLFromOFD(b, "ofd-creator/1000-pages.ofd")
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

func TestParseDocumentXMLUsesSpecPermissionsInBothDecodePaths(t *testing.T) {
	for _, permissions := range []string{
		"",
		`<Permissions/>`,
		`<Permissions><Edit>false</Edit><Print/><ValidPeriod/></Permissions>`,
		`<Permissions><Export>false</Export><Print Printable="false" Copies="0"/><ValidPeriod EndDate="2026-01-02T03:04:05.123456789+08:00"/></Permissions>`,
	} {
		raw := []byte(`<Document xmlns="http://www.ofdspec.org/2016"><Pages/>` + permissions + `</Document>`)
		var standard struct {
			Permissions *spec.Permissions `xml:"Permissions"`
		}
		if err := xml.Unmarshal(raw, &standard); err != nil {
			t.Fatal(err)
		}
		fast, err := ParseDocumentXML(raw)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(fast.Permissions, standard.Permissions) {
			t.Fatalf("迁移后快速/标准解析不一致：%s", permissions)
		}
	}
}

func TestFastDocumentParsingMatchesSpecNavigationActionsAndPreferences(t *testing.T) {
	for _, newWindow := range []string{"", ` NewWindow="false"`, ` NewWindow="true"`} {
		data := []byte(`<Document><CommonData><MaxUnitID>42</MaxUnitID><PageArea/><PublicRes>../Public.xml</PublicRes><TemplatePage ID="10" ZOrder="Foreground" BaseLoc="./Template.xml"/><DefaultCS>0</DefaultCS></CommonData><Pages><Page ID="2" BaseLoc="Pages/../2.xml"/><Page ID="1" BaseLoc="./1.xml"/></Pages><Outlines><OutlineElem Title="章节" Count="0" Expanded="false"><Actions><Action Event="CLICK"><Goto><Bookmark Name="chapter"/></Goto></Action></Actions><OutlineElem Title="子章节"/></OutlineElem></Outlines><Actions><Action Event="DO"><GotoA AttachID="note"` + newWindow + `/></Action><Action Event="DO"><Region><Area Start="0 0"><Line Point1="1 1"/><Close/></Area></Region><URI URI="https://example.test"/></Action></Actions><VPreferences><HideToolbar>false</HideToolbar><ZoomMode>FitWidth</ZoomMode><Zoom>0</Zoom></VPreferences><Bookmarks><Bookmark Name="chapter"><Dest Type="XYZ" PageID="2" Left="0" Zoom="0"/></Bookmark></Bookmarks><Annotations>../Annots.xml</Annotations><Attachments>./Attachs.xml</Attachments></Document>`)
		var standard spec.Document
		if err := xml.Unmarshal(data, &standard); err != nil {
			t.Fatal(err)
		}
		fast, err := ParseDocumentXML(data)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(*fast, standard) {
			t.Fatalf("文档迁移后快速入口与公开模型不一致: NewWindow=%q\n得到: %#v\n期望: %#v", newWindow, *fast, standard)
		}
		if fast.CommonData.PageArea.PhysicalBox != (spec.StBox{}) {
			t.Fatal("快速 XML 解析入口不应隐式应用 A4 兜底")
		}
	}
}
