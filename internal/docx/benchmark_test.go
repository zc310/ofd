package docx

import (
	"testing"

	"github.com/zc310/ofd/internal/textdoc"
)

func BenchmarkFilterCredibleListItems(b *testing.B) {
	items := make([]ListItem, 1000)
	for i := range items {
		if i%10 < 3 {
			items[i] = ListItem{order: i%10 + 1, marker: "arabic"}
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		FilterCredibleListItems(items)
	}
}

func BenchmarkDropAnnotatedEntries(b *testing.B) {
	for _, annotated := range []bool{false, true} {
		name := "WithoutAnnotations"
		if annotated {
			name = "WithAnnotations"
		}
		b.Run(name, func(b *testing.B) {
			entries := benchmarkEntries()
			if annotated {
				for i := 0; i < len(entries); i += 10 {
					entries[i].Source = textdoc.EntrySourceAnnotation
				}
			}
			b.ReportAllocs()
			for b.Loop() {
				dropAnnotatedEntries(entries)
			}
		})
	}
}

func BenchmarkBlockSequence(b *testing.B) {
	entries := benchmarkEntries()
	b.ReportAllocs()
	for b.Loop() {
		BlockSequence(entries, 297, PageOptions{}, nil)
	}
}

func benchmarkEntries() []textdoc.Entry {
	entries := make([]textdoc.Entry, 1000)
	for i := range entries {
		entries[i] = textdoc.Entry{
			Text: "这是用于评估正文编排开销的文字。", Size: 3.5,
			X: 20 + float64(i%10)*16, Y: 20 + float64(i/10)*2.5, Width: 15,
		}
	}
	return entries
}
