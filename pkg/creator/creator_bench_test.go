package creator

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/xmlcodec"
	"github.com/zc310/ofd/pkg/spec"
)

func BenchmarkStreamColor(b *testing.B) {
	w := xmlcodec.New()
	color := &Color{R: 128, G: 190, B: 220}
	b.ReportAllocs()
	for b.Loop() {
		w.Reset()
		streamColor(w, "FillColor", color)
		_ = w.Bytes()
	}
}

func BenchmarkPreparePublicResources(b *testing.B) {
	document := Document{ID: "public-resources", Pages: []Page{{}}}
	for index := 0; index < 10; index++ {
		document.PublicRes = append(document.PublicRes, PublicResource{
			Name:  fmt.Sprintf("Assets/%d/Res.xml", index),
			Data:  []byte(fmt.Sprintf(`<ofd:Res xmlns:ofd="http://www.ofdspec.org/2016" BaseLoc="."><ofd:MultiMedias><ofd:MultiMedia ID="%d" Type="Audio" Format="wav"><ofd:MediaFile>media.bin</ofd:MediaFile></ofd:MultiMedia></ofd:MultiMedias></ofd:Res>`, 100+index)),
			Files: []PublicResourceFile{{Path: "media.bin", Data: []byte("audio")}},
		})
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := prepare(document, slicePages{pages: document.Pages}, CreateOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkR(b *testing.B) {
	document := benchmarkDocument()

	b.Run("build", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := build(document); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("create", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := Create(document, io.Discard); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("marshal", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := Marshal(document); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkCreateMetadataXML 覆盖辅助 XML 生成后同步写入 ZIP 的完整路径。
func BenchmarkCreateMetadataXML(b *testing.B) {
	for _, size := range []struct {
		name string
		n    int
	}{{"Small", 256}, {"256KiB", 256 << 10}} {
		b.Run(size.name, func(b *testing.B) {
			document := Document{
				ID:       "metadata-benchmark",
				Abstract: strings.Repeat("x", size.n),
				Pages:    []Page{{}},
			}
			b.SetBytes(int64(size.n))
			b.ReportAllocs()
			for b.Loop() {
				if err := Create(document, io.Discard); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkCreateLargeSinglePage 使用共享的文本输入生成大单页，区分模型内存与 XML 输出内存。
func BenchmarkCreateLargeSinglePage(b *testing.B) {
	for _, size := range []struct {
		name string
		n    int
	}{{"2MiB", 2 << 20}, {"16MiB", 16 << 20}} {
		b.Run(size.name, func(b *testing.B) {
			payload := strings.Repeat("正文 text & <value> ", 256)
			items := make([]Item, size.n/len(payload))
			for index := range items {
				items[index] = Text{Width: 20, Height: 10, Value: payload}
			}
			document := Document{ID: "large-single-page", Pages: []Page{{Items: items}}}
			for _, mode := range []CompressionMode{CompressionStore, CompressionDeflate} {
				b.Run(string(mode), func(b *testing.B) {
					b.SetBytes(int64(len(items) * len(payload)))
					b.ReportAllocs()
					for b.Loop() {
						if err := CreateWithOptions(document, io.Discard, CreateOptions{Compression: mode}); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}

func benchmarkDocument() Document {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3, 4}
	pages := make([]Page, 24)
	for pageIndex := range pages {
		items := make([]Item, 0, 16)
		for itemIndex := 0; itemIndex < 8; itemIndex++ {
			items = append(items,
				Text{
					X: 10 + float64(itemIndex), Y: 12 + float64(itemIndex),
					Width: 80, Height: 8, Value: "benchmark text", Font: "BenchmarkFont",
					DrawParam: "benchmark-param",
				},
				Path{
					X: 10, Y: 30 + float64(itemIndex), Width: 60, Height: 20,
					Data: "M 0 0 L 60 20 L 20 40 C", Fill: true,
					DrawParam: "benchmark-param",
					FillColor: &Color{ColorSpace: 20, Components: []int{32, 96, 180}},
				},
			)
		}
		items = append(items, Image{
			X: 10, Y: 80, Width: 40, Height: 30, ResourceID: uint64(1000 + pageIndex),
		})
		pages[pageIndex] = Page{
			Resources: []PageResource{{Images: []PageImage{{
				ID: uint64(1000 + pageIndex), Format: "PNG", Data: png,
			}}}},
			Items: items,
		}
	}
	return Document{
		ID:       "benchmark-r",
		PageSize: A4,
		DrawParams: []DrawParam{{
			Name: "benchmark-param", LineWidth: 0.5, Cap: spec.LineCapRound, Join: spec.LineJoinMiter,
			StrokeColor: &Color{R: 20, G: 40, B: 80},
		}},
		Fonts:       []Font{{Name: "BenchmarkFont", Format: "ttf", Data: []byte("benchmark-font-data")}},
		ColorSpaces: []ColorSpace{{ID: 20, Type: "RGB", BitsPerComponent: 8}},
		Media:       []Media{{ID: 2000, Type: "Audio", Format: "wav", Data: []byte("benchmark-audio")}},
		Pages:       pages,
	}
}

// BenchmarkStreamPageArea 测量页面四个区域框的元素写入，用于对比
// boxString 拼接与 ElementBox 数值直写的分配差异。
func BenchmarkStreamPageArea(b *testing.B) {
	w := xmlcodec.New()
	defer xmlcodec.Put(w)
	area := &PageArea{
		PhysicalBox:    &Box{X: 0, Y: 0, Width: 210, Height: 297},
		ApplicationBox: &Box{X: 1.5, Y: 2.5, Width: 207, Height: 292},
		ContentBox:     &Box{X: 3.25, Y: 4.75, Width: 203.5, Height: 287.5},
		BleedBox:       &Box{X: -0.5, Y: -0.25, Width: 211, Height: 297.5},
	}
	b.ReportAllocs()
	for b.Loop() {
		w.Reset()
		w.Start("Area")
		streamPageArea(w, area, A4)
		w.End()
		_ = w.Bytes()
	}
}
