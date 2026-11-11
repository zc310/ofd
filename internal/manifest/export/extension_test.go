package export

import (
	"bytes"
	"encoding/xml"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/manifest"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
)

// 检查真实创建链经 YAML/JSON 清单重建后，扩展文本、带命名空间的 XML 和外部文件仍可读取。
func TestExtensionDataSurvivesManifestRoundTrip(t *testing.T) {
	const text = "甲 <配置> & 尾"
	const whitespace = " \n\t "
	date := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	data, err := creator.Marshal(creator.Document{
		ID: "extension-manifest", Pages: []creator.Page{{}}, Extensions: []creator.Extension{
			{AppName: "text", RefID: 1, Data: text, Date: date},
			{AppName: "xml", RefID: 1, DataXML: []byte(`<v:Config xmlns:v="urn:vendor" v:key="a"><v:Extension><v:Data>只属于内联</v:Data></v:Extension>值&amp;</v:Config>`)},
			{AppName: "external", RefID: 1, DataFile: []byte("扩展文件"), DataName: "extension.bin"},
			{AppName: "whitespace", RefID: 1, Data: whitespace},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			root := t.TempDir()
			var output bytes.Buffer
			if err := WriteManifest(data, &output, Options{AssetRoot: filepath.Join(root, "assets"), AssetPrefix: "assets", Format: format}); err != nil {
				t.Fatal(err)
			}
			value, err := manifest.Parse(output.Bytes(), format)
			if err != nil {
				t.Fatal(err)
			}
			if len(value.Document.Extensions) != 4 || strings.Contains(output.String(), "<nil>") {
				t.Fatalf("扩展丢失或包含错误的 <nil> 文本: %s", output.String())
			}
			document, err := value.Build(root, "")
			if err != nil {
				t.Fatal(err)
			}
			roundtrip, err := creator.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			ofd, err := parser.NewOFD(roundtrip)
			if err != nil {
				t.Fatal(err)
			}
			defer ofd.Close()
			doc := ofd.Documents[0]
			extensions, err := doc.LoadExtensions()
			if err != nil {
				t.Fatal(err)
			}
			if extensions == nil || len(extensions.Extensions) != 4 {
				t.Fatalf("清单重建丢失扩展: %+v", extensions)
			}
			plain, inline, external, spaces := extensions.Extensions[0], extensions.Extensions[1], extensions.Extensions[2], extensions.Extensions[3]
			if plain.AppName != "text" || plain.Data == nil || plain.Data.Text != text || plain.Date == nil || !plain.Date.Equal(date) || spaces.Data == nil || spaces.Data.Text != whitespace {
				t.Fatal("扩展文本、空白或日期在清单往返中改变")
			}
			var config struct {
				XMLName   xml.Name `xml:"urn:vendor Config"`
				Key       string   `xml:"urn:vendor key,attr"`
				Text      string   `xml:",chardata"`
				Extension struct {
					Data string `xml:"urn:vendor Data"`
				} `xml:"urn:vendor Extension"`
			}
			if inline.AppName != "xml" || inline.Data == nil {
				t.Fatal("清单重建丢失内联 XML")
			}
			if err := xml.Unmarshal([]byte(inline.Data.InnerXML), &config); err != nil {
				t.Fatal(err)
			}
			if config.XMLName != (xml.Name{Space: "urn:vendor", Local: "Config"}) || config.Key != "a" || config.Text != "值&" || config.Extension.Data != "只属于内联" {
				t.Fatalf("内联 XML 的命名空间、属性或文本改变: %+v", config)
			}
			if external.AppName != "external" || external.ExtendData == nil {
				t.Fatal("清单重建丢失外部扩展数据路径")
			}
			listPath := doc.Extensions.Resolve(doc.BaseLoc)
			payload, err := doc.FileCache.Read(external.ExtendData.Resolve(listPath.Dir()).String())
			if err != nil {
				t.Fatal(err)
			}
			if string(payload) != "扩展文件" {
				t.Fatal("外部扩展数据在清单往返中改变")
			}
		})
	}
}

func TestExtensionDataXMLReadsOnlyDirectListAndDataChildren(t *testing.T) {
	data := []byte(`<Extensions><Unknown><Extension><Data>忽略</Data></Extension></Unknown><Extension><Unknown><Data>忽略</Data></Unknown><Data> 正文 </Data></Extension><Extension><Unknown><Data>忽略</Data></Unknown></Extension><Extension><Data>下一项</Data></Extension></Extensions>`)
	for index, want := range []string{" 正文 ", "", "下一项", ""} {
		if got := extensionDataXML(data, index); got != want {
			t.Fatalf("扩展 %d 的内联内容 = %q，期望 %q", index, got, want)
		}
	}
}
