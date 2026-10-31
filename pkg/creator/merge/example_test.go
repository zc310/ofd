package merge_test

import (
	"bytes"
	"fmt"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/merge"
)

func exampleDocument(id string) []byte {
	data, err := creator.Marshal(creator.Document{
		ID:       id,
		PageSize: creator.PageSize{Width: 100, Height: 100},
		Pages: []creator.Page{
			{Items: []creator.Item{creator.Path{X: 5, Y: 5, Width: 20, Height: 20, Data: "M 0 0 L 20 0 L 20 20 C", Stroke: true, LineWidth: 1}}},
			{Items: []creator.Item{creator.Path{X: 30, Y: 30, Width: 20, Height: 20, Data: "M 0 0 L 20 0 L 20 20 C", Stroke: true, LineWidth: 1}}},
		},
	})
	if err != nil {
		panic(err)
	}
	return data
}

// ExampleBytes 演示 ZIP 级合并：每个输入文档体在输出中仍是独立文档体。
func ExampleBytes() {
	var output bytes.Buffer
	err := merge.Bytes([][]byte{exampleDocument("doc-a"), exampleDocument("doc-b")}, &output, merge.Options{})
	fmt.Println(err == nil, output.Len() > 0)
	// Output: true true
}

// ExamplePages 演示模型级拼页：按输入顺序把来源页依次合并到一个文档中。
func ExamplePages() {
	var output bytes.Buffer
	err := merge.Pages([]merge.Source{
		{Name: "a.ofd", Data: exampleDocument("doc-a")},
		{Name: "b.ofd", Data: exampleDocument("doc-b")},
	}, &output, merge.PageOptions{})
	fmt.Println(err == nil, output.Len() > 0)
	// Output: true true
}

// ExamplePages_selectPages 演示选择并重排全局页序。这里输出第二页，然后输出第一页。
func ExamplePages_selectPages() {
	var output bytes.Buffer
	err := merge.Pages(
		[]merge.Source{{Data: exampleDocument("doc-a")}},
		&output,
		merge.PageOptions{Pages: []int{2, 1}},
	)
	fmt.Println(err == nil, output.Len() > 0)
	// Output: true true
}

// ExamplePages_compositions 演示将多来源页面叠加为多个输出页。
func ExamplePages_compositions() {
	var output bytes.Buffer
	err := merge.Pages([]merge.Source{
		{Name: "a.ofd", Data: exampleDocument("doc-a")},
		{Name: "b.ofd", Data: exampleDocument("doc-b")},
	}, &output, merge.PageOptions{
		Compositions: []merge.PageComposition{
			{Pages: []merge.PageRef{{Source: 1, Page: 1}, {Source: 2, Page: 1}}},
			{Pages: []merge.PageRef{{Source: 1, Page: 2}, {Source: 2, Page: 2}}},
		},
	})
	fmt.Println(err == nil, output.Len() > 0)
	// Output: true true
}
