package flow_test

import (
	"bytes"
	"context"
	"fmt"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
)

func ExampleDocument_Write() {
	doc := flow.New()
	doc.Metadata.Title = "Quarterly report"
	doc.AddHeading(1, "Quarterly report")
	doc.AddParagraph("Paragraphs wrap automatically inside the page margins.")
	doc.AddList(true, []string{"Prepare content", "Create a standard OFD"})
	doc.AddTable(flow.Table{Header: []string{"Item", "Status"}, Rows: [][]string{{"Report", "Ready"}}})
	var output bytes.Buffer
	if err := doc.Write(context.Background(), &output, creator.CreateOptions{Deterministic: true}); err != nil {
		panic(err)
	}
	fmt.Println(bytes.HasPrefix(output.Bytes(), []byte("PK")))
	// Output: true
}

func ExampleTable() {
	opts := flow.DefaultOptions()
	opts.GenerateOutlines = true
	doc := flow.New(opts)
	doc.AddHeading(1, "Regional report")
	doc.AddTable(flow.Table{HeaderRows: 2, Grid: [][]flow.TableCell{
		{{Text: "Region", RowSpan: 2}, {Text: "Performance", ColSpan: 2}},
		{{Text: "Actual"}, {Text: "Plan"}},
		{{Text: "North", RowSpan: 2}, {Text: "120"}, {Text: "150"}},
		{{Text: "135"}, {Text: "155"}},
	}})
	model, err := doc.Build(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Println(len(model.Pages), len(model.Outlines))
	// Output: 1 1
}

func ExampleDocument_AddTOC() {
	doc := flow.New()
	doc.AddTOC(flow.TOC{Title: "Contents", MaxLevel: 2})
	doc.AddPageBreak()
	doc.Blocks = append(doc.Blocks, flow.Block{Runs: []flow.Run{{Text: "Appendix page "}, {PageRef: "appendix"}}})
	doc.Blocks = append(doc.Blocks, flow.Block{ID: "appendix", Kind: "heading", Level: 1, Text: "Appendix", PageBreakBefore: true})
	model, err := doc.Build(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Println(len(model.Pages))
	// Output: 3
}

func ExampleDocument_AddSection() {
	opts := flow.DefaultOptions()
	opts.ReferencePageNumbers = "section"
	doc := flow.New(opts)
	hide, show, start := false, true, 1
	doc.AddSection(flow.Section{PageNumbers: &hide})
	doc.AddParagraph("Cover")
	doc.AddSection(flow.Section{PageNumbers: &show, StartPageNumber: &start})
	doc.AddHeading(1, "Body")
	doc.AddSection(flow.Section{PageSize: &creator.PageSize{Width: 297, Height: 210}})
	doc.AddHeading(1, "Wide table section")
	model, err := doc.Build(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Println(len(model.Pages), model.Pages[2].Area.PhysicalBox.Width)
	// Output: 3 297
}

func ExampleFontFamily() {
	opts := flow.DefaultOptions()
	opts.FontFamilies = []flow.FontFamily{{Regular: "Flow-Regular", Bold: "Flow-Bold", Italic: "Flow-Italic", BoldItalic: "Flow-BoldItalic"}}
	doc := flow.New(opts)
	doc.Blocks = []flow.Block{{Runs: []flow.Run{{Text: "Bold italic", Font: "Flow-Italic", Bold: true}}}}
	model, err := doc.Build(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Println(model.Pages[0].Items[0].(creator.Text).Font)
	// Output: Flow-BoldItalic
}

func ExampleFluent2Theme() {
	opts := flow.DefaultOptions()
	opts.Theme = flow.Fluent2Theme()
	doc := flow.New(opts)
	doc.AddHeading(1, "Delivery report")
	doc.AddParagraph("The selected theme controls typography and spacing.")
	model, err := doc.Build(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Println(opts.Theme.Body.Size, len(model.Pages))
	// Output: 10.5 1
}

func ExampleMaterial2Theme() {
	opts := flow.DefaultOptions()
	opts.Theme = flow.Material2Theme()
	doc := flow.New(opts)
	doc.AddHeading(1, "Report")
	doc.AddParagraph("Material 2 type roles are mapped to this print document.")
	model, err := doc.Build(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Println(opts.Theme.Headings[0].Size, opts.Theme.Body.Size, len(model.Pages))
	// Output: 72 12 1
}

func ExampleMaterial3Theme() {
	opts := flow.DefaultOptions()
	opts.Theme = flow.Material3Theme()
	doc := flow.New(opts)
	doc.AddHeading(1, "Delivery report")
	doc.AddParagraph("Material 3 type roles are mapped to this print document.")
	model, err := doc.Build(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Println(opts.Theme.Body.Size, len(model.Pages))
	// Output: 12 1
}

func ExampleCarbonTheme() {
	opts := flow.DefaultOptions()
	opts.Theme = flow.CarbonTheme()
	doc := flow.New(opts)
	doc.AddHeading(1, "Delivery report")
	doc.AddParagraph("Carbon type roles are mapped to this print document.")
	model, err := doc.Build(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Println(opts.Theme.Headings[0].Size, opts.Theme.Table.Size, len(model.Pages))
	// Output: 31.5 10.5 1
}

func ExampleBootstrap5Theme() {
	opts := flow.DefaultOptions()
	opts.Theme = flow.Bootstrap5Theme()
	doc := flow.New(opts)
	doc.AddHeading(1, "Delivery report")
	doc.AddParagraph("Bootstrap heading ratios are mapped to this print document.")
	model, err := doc.Build(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Println(opts.Theme.Headings[0].Size, opts.Theme.Caption.Size, len(model.Pages))
	// Output: 30 10.5 1
}

func ExampleGBT9704Options() {
	// 此处用已有 Go 字体演示调用；正式中文公文需注册对应的五类实际字体。
	opts := flow.GBT9704Options(flow.OfficialFonts{Body: "Flow-Regular", Title: "Flow-Regular", Hei: "Flow-Bold", Kai: "Flow-Italic", Song: "Flow-Regular"})
	doc := flow.New(opts)
	doc.AddHeading(1, "Notice")
	doc.AddRecipient("All departments:")
	doc.AddParagraph("Review the source records and preserve the final document.")
	model, err := doc.Build(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Println(model.Pages[0].Area.ContentBox.Width, model.Pages[0].Area.ContentBox.Height)
	// Output: 156 225
}
