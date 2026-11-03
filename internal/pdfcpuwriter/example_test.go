package pdfcpuwriter_test

import (
	"bytes"
	"context"
	"fmt"

	"github.com/zc310/ofd/internal/pdfcpuwriter"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

func Example() {
	doc, err := pdfcpuwriter.New(context.Background(), 210, 297)
	if err != nil {
		fmt.Println(err)
		return
	}
	page := canvas.NewContext(doc)
	page.SetFillColor(canvas.Black)
	page.DrawPath(10, 20, canvas.Rectangle(30, 15))
	if err := doc.NextPage(297, 210); err != nil {
		fmt.Println(err)
		return
	}
	page = canvas.NewContext(doc)
	page.DrawPath(20, 30, canvas.Rectangle(40, 10))
	var output bytes.Buffer
	if err := doc.Write(&output); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(doc.Stats().Pages, bytes.HasPrefix(output.Bytes(), []byte("%PDF-")))
	// Output: 2 true
}

func ExampleNewWithOptions() {
	doc, err := pdfcpuwriter.NewWithOptions(context.Background(), 210, 297, pdfcpuwriter.Options{LossyImages: true})
	if err != nil {
		fmt.Println(err)
		return
	}
	if err := doc.AddLinks([]drawing.PageLink{{URI: "https://example.com", X: 10, Y: 10, Width: 30, Height: 10}}); err != nil {
		fmt.Println(err)
		return
	}
	if err := doc.AddOutlines([]drawing.PageOutline{{Title: "首页", Target: &drawing.LinkTarget{Page: 0, Type: drawing.DestFit}}}); err != nil {
		fmt.Println(err)
		return
	}
	var output bytes.Buffer
	if err := doc.Write(&output); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(bytes.HasPrefix(output.Bytes(), []byte("%PDF-")))
	// Output: true
}
