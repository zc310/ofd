package markdown_test

import (
	"bytes"
	"context"
	"fmt"

	markdown "github.com/zc310/ofd/pkg/converter/import/markdown"
	"github.com/zc310/ofd/pkg/creator/flow"
)

func ExampleConvert() {
	var output bytes.Buffer
	err := markdown.Convert(context.Background(), []byte("# 标题\n\n正文"), &output)
	fmt.Println(err == nil)
	fmt.Println(bytes.HasPrefix(output.Bytes(), []byte("PK")))
	// Output:
	// true
	// true
}

func ExampleConvert_flowOptions() {
	opts := flow.DefaultOptions()
	opts.Theme = flow.Bootstrap5Theme()
	opts.GenerateOutlines = true
	var output bytes.Buffer
	err := markdown.Convert(context.Background(), []byte("# Report\n\nText and `code`."), &output, opts)
	fmt.Println(err == nil)
	fmt.Println(bytes.HasPrefix(output.Bytes(), []byte("PK")))
	// Output:
	// true
	// true
}

func ExampleConvert_alerts() {
	var output bytes.Buffer
	err := markdown.Convert(context.Background(), []byte("> [!NOTE]\n> Useful information.\n"), &output, flow.DefaultOptions())
	fmt.Println(err == nil)
	// Output: true
}

func ExampleConvert_codeHighlight() {
	// 指定语言启用语法高亮；无语言的围栏仍为灰底灰字。
	source := "```go\nfunc main() {\n    println(\"hello\", 42)\n}\n```\n\n```\nplain code\n```\n"
	var output bytes.Buffer
	err := markdown.Convert(context.Background(), []byte(source), &output, flow.DefaultOptions())
	fmt.Println(err == nil)
	fmt.Println(bytes.HasPrefix(output.Bytes(), []byte("PK")))
	// Output:
	// true
	// true
}
