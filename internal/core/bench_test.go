package core

import (
	"archive/zip"
	"bytes"
	"fmt"
	"testing"
)

func coreBenchArchive(b *testing.B, entries int) []byte {
	b.Helper()
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	for index := 0; index < entries; index++ {
		file, err := writer.Create(fmt.Sprintf("Doc_0/Pages/Page_%d/Content.xml", index))
		if err != nil {
			b.Fatal(err)
		}
		if _, err := file.Write([]byte(`<Page><Area><PhysicalBox>0 0 210 297</PhysicalBox></Area></Page>`)); err != nil {
			b.Fatal(err)
		}
	}
	file, err := writer.Create("OFD.xml")
	if err != nil {
		b.Fatal(err)
	}
	if _, err := file.Write([]byte(`<OFD><DocBody><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`)); err != nil {
		b.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		b.Fatal(err)
	}
	return data.Bytes()
}

// BenchmarkOpenAndIndex measures archive construction and first index creation.
func BenchmarkOpenAndIndex(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("%dEntries", count), func(b *testing.B) {
			data := coreBenchArchive(b, count)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				archive, err := OpenBytes(data)
				if err != nil {
					b.Fatal(err)
				}
				if _, ok := archive.Lookup("OFD.xml"); !ok {
					b.Fatal("OFD.xml not found")
				}
				_ = archive.Close()
			}
		})
	}
}

// BenchmarkLookupRepeated measures hot exact-name lookups after building the index.
func BenchmarkLookupRepeated(b *testing.B) {
	data := coreBenchArchive(b, 10000)
	archive, err := OpenBytes(data)
	if err != nil {
		b.Fatal(err)
	}
	defer archive.Close()
	if _, ok := archive.Lookup("Doc_0/Pages/Page_9999/Content.xml"); !ok {
		b.Fatal("benchmark page not found")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := archive.Lookup("Doc_0/Pages/Page_9999/Content.xml"); !ok {
			b.Fatal("benchmark page not found")
		}
	}
}

// BenchmarkReadXML measures indexed XML entry decoding.
func BenchmarkReadXML(b *testing.B) {
	data := coreBenchArchive(b, 1000)
	archive, err := OpenBytes(data)
	if err != nil {
		b.Fatal(err)
	}
	defer archive.Close()
	var target struct {
		Area struct {
			PhysicalBox string `xml:"PhysicalBox"`
		} `xml:"Area"`
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := archive.ReadXML("Doc_0/Pages/Page_500/Content.xml", &target); err != nil {
			b.Fatal(err)
		}
	}
}
