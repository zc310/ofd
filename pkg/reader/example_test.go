package reader_test

import (
	"context"
	"fmt"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/reader"
	"golang.org/x/image/font/gofont/goregular"
)

func exampleDocument() []byte {
	data, err := creator.Marshal(creator.Document{ID: "reader-example", Fonts: []creator.Font{{Name: "Go", Data: goregular.TTF}}, Pages: []creator.Page{{Items: []creator.Item{
		creator.Text{Width: 25, Height: 8, Font: "Go", Size: 4, Value: "Example"},
	}}}})
	if err != nil {
		panic(err)
	}
	return data
}

func exampleReader() *reader.Reader {
	r, err := reader.Open(exampleDocument())
	if err != nil {
		panic(err)
	}
	return r
}

func ExampleOpen() {
	r, err := reader.Open(exampleDocument(), reader.OpenOptions{PageCacheCapacity: 8})
	if err != nil {
		panic(err)
	}
	defer r.Close()
	fmt.Println(r.PageCount())
	// Output: 1
}

func ExampleReader_SetTextOptions() {
	r := exampleReader()
	defer r.Close()
	if err := r.SetTextOptions(reader.TextOptions{IncludeAnnotations: true, IncludeWatermarks: true, IncludeSeals: true}); err != nil {
		panic(err)
	}
	runs, err := r.Text(0)
	if err != nil {
		panic(err)
	}
	fmt.Println(runs[0].Source, runs[0].Text)
	// Output: page Example
}

func ExampleReader_RenderPageWithReport() {
	r := exampleReader()
	defer r.Close()
	data, report, err := r.RenderPageWithReport(context.Background(), 0, reader.RenderOptions{DPI: 72})
	if err != nil {
		panic(err)
	}
	fmt.Println(len(data) > 0, report.Stats.ObjectsVisited > 0, len(report.Diagnostics))
	// Output: true true 0
}
