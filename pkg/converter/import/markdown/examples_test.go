package markdown_test

import (
	"bytes"
	"context"
	"fmt"

	markdown "github.com/zc310/ofd/pkg/converter/import/markdown"
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
