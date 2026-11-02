package models

import (
	"encoding/xml"
	"fmt"
	"strings"
	"testing"
)

func BenchmarkParseResXML(b *testing.B) {
	data := benchmarkResXML()
	b.Run("标准解析", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var resource Res
			if err := xml.Unmarshal(data, &resource); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("fastxml", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := ParseResXML(data); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func benchmarkResXML() []byte {
	var data strings.Builder
	data.WriteString(`<Res BaseLoc="Res"><ColorSpaces>`)
	for i := range 40 {
		fmt.Fprintf(&data, `<ColorSpace ID="%d" Type="RGB" BitsPerComponent="8"><Palette>`, i+1)
		for range 16 {
			data.WriteString(`<CV>255 128 0</CV>`)
		}
		data.WriteString(`</Palette></ColorSpace>`)
	}
	data.WriteString(`</ColorSpaces><DrawParams>`)
	for i := range 40 {
		fmt.Fprintf(&data, `<DrawParam ID="%d" LineWidth="0.5" DashPattern="2 1"><FillColor Value="0 0 0"/></DrawParam>`, i+1)
	}
	data.WriteString(`</DrawParams><Fonts>`)
	for i := range 40 {
		fmt.Fprintf(&data, `<Font ID="%d" FontName="Example"><FontFile>font.ttf</FontFile></Font>`, i+1)
	}
	data.WriteString(`</Fonts><MultiMedias>`)
	for i := range 40 {
		fmt.Fprintf(&data, `<MultiMedia ID="%d" Type="Image"><MediaFile>image.png</MediaFile></MultiMedia>`, i+1)
	}
	data.WriteString(`</MultiMedias></Res>`)
	return []byte(data.String())
}
