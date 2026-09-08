package creator

import (
	"io"
	"testing"
)

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
			Name: "benchmark-param", LineWidth: 0.5, Cap: "Round", Join: "Miter",
			StrokeColor: &Color{R: 20, G: 40, B: 80},
		}},
		Fonts:       []Font{{Name: "BenchmarkFont", Format: "ttf", Data: []byte("benchmark-font-data")}},
		ColorSpaces: []ColorSpace{{ID: 20, Type: "RGB", BitsPerComponent: 8}},
		Media:       []Media{{ID: 2000, Type: "Audio", Format: "wav", Data: []byte("benchmark-audio")}},
		Pages:       pages,
	}
}
