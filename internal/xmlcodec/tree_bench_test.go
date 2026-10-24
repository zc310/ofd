package xmlcodec

import (
	"fmt"
	"strings"
	"testing"

	"github.com/beevik/etree"
)

func benchmarkXML(objects int) []byte {
	var xml strings.Builder
	xml.WriteString(`<?xml version="1.0" encoding="UTF-8"?><ofd:Page xmlns:ofd="http://www.ofdspec.org/2016"><ofd:Content><ofd:Layer ID="1">`)
	for i := range objects {
		fmt.Fprintf(&xml, `<ofd:TextObject ID="%d" Font="1" Size="4.2333" Boundary="0 0 100 10"><ofd:TextCode X="1" Y="5">中文正文 &amp; text %d</ofd:TextCode></ofd:TextObject>`, i+2, i)
	}
	xml.WriteString(`</ofd:Layer></ofd:Content></ofd:Page>`)
	return []byte(xml.String())
}

func BenchmarkXMLEdit(b *testing.B) {
	page := string(benchmarkXML(1000))
	for _, input := range []struct {
		name string
		data []byte
	}{
		{"Small", benchmarkXML(4)},
		{"Page", []byte(page)},
		{"Large", benchmarkXML(10000)},
		{"PageCRLF", []byte(strings.ReplaceAll(page, "><", ">\r\n\t<"))},
		{"PageAttrWhitespace", []byte(strings.ReplaceAll(page, ` Font="1"`, "\r\n\tFont=\"1\" Note=\"a\tb\r\nc&#x9;&#xD;\""))},
		{"PageDeclaration", []byte(strings.ReplaceAll(page,
			`<?xml version="1.0" encoding="UTF-8"?>`,
			`<?xml version = '1.0' encoding = 'UTF-8' standalone = 'yes'?>`))},
	} {
		data := input.data
		for _, implementation := range []string{"Etree", "OFDXML"} {
			b.Run(input.name+"/"+implementation, func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(data)))
				for b.Loop() {
					var doc *etree.Document
					var err error
					if implementation == "Etree" {
						doc = etree.NewDocument()
						err = doc.ReadFromBytes(data)
					} else {
						doc, err = Parse(data)
					}
					if err != nil {
						b.Fatal(err)
					}
					doc.Root().FindElement("./Content/Layer/TextObject").CreateAttr("ID", "999999")
					if implementation == "Etree" {
						benchWriterDoneOutput, err = doc.WriteToBytes()
					} else {
						benchWriterDoneOutput, err = Marshal(doc)
					}
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func BenchmarkXMLMarshal(b *testing.B) {
	data := benchmarkXML(1000)
	doc, err := Parse(data)
	if err != nil {
		b.Fatal(err)
	}
	for _, implementation := range []string{"Etree", "OFDXML", "NormalizeOFD"} {
		b.Run(implementation, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				switch implementation {
				case "Etree":
					benchWriterDoneOutput, err = doc.WriteToBytes()
				case "OFDXML":
					benchWriterDoneOutput, err = Marshal(doc)
				case "NormalizeOFD":
					benchWriterDoneOutput, err = MarshalDocument(doc)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
