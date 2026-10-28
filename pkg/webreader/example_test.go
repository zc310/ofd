package webreader_test

import (
	"context"
	"fmt"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/webreader"
	"golang.org/x/image/font/gofont/goregular"
)

func exampleReader() *webreader.Reader {
	data, err := creator.Marshal(creator.Document{ID: "reader-example", Fonts: []creator.Font{{Name: "Go", Data: goregular.TTF}}, Pages: []creator.Page{{Items: []creator.Item{
		creator.Text{Width: 25, Height: 8, Font: "Go", Size: 4, Value: "Example"},
	}}}})
	if err != nil {
		panic(err)
	}
	r, err := webreader.Open(data)
	if err != nil {
		panic(err)
	}
	return r
}

func ExampleReader_SetTextOptions() {
	r := exampleReader()
	defer r.Close()
	if err := r.SetTextOptions(webreader.TextOptions{IncludeAnnotations: true, IncludeWatermarks: true, IncludeSeals: true}); err != nil {
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
	data, report, err := r.RenderPageWithReport(context.Background(), 0, webreader.RenderOptions{DPI: 72})
	if err != nil {
		panic(err)
	}
	fmt.Println(len(data) > 0, report.Stats.ObjectsVisited > 0, len(report.Diagnostics))
	// Output: true true 0
}
