package split_test

import (
	"bytes"
	"context"
	"fmt"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/split"
)

func ExampleSession() {
	ctx := context.Background()
	input, err := creator.Marshal(creator.Document{ID: "split-example", PageSize: creator.A4, Pages: []creator.Page{{}, {}, {}}})
	if err != nil {
		panic(err)
	}
	session, err := split.Open(ctx, input, split.Options{Deterministic: true})
	if err != nil {
		panic(err)
	}
	defer session.Close()
	parts, err := session.Plan(split.Selection{Every: 2})
	if err != nil {
		panic(err)
	}
	for _, part := range parts {
		var output bytes.Buffer
		report, err := session.Write(ctx, part, &output)
		if err != nil {
			panic(err)
		}
		fmt.Println(report.Pages, output.Len() > 0)
	}
	// Output:
	// [1 2] true
	// [3] true
}
