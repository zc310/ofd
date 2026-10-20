package compare_test

import (
	"context"
	"fmt"

	"github.com/zc310/ofd/pkg/compare"
)

func ExampleCompareFiles() {
	file := "../../testdata/ofd-creator/helloworld.ofd"
	report, err := compare.CompareFiles(context.Background(), []string{file, file}, compare.Options{})
	if err != nil {
		panic(err)
	}
	fmt.Println(report.Comparisons[0].Equal)
	// Output: true
}

func ExampleParsePageMap() {
	pairs, err := compare.ParsePageMap("2:3,4:5")
	if err != nil {
		panic(err)
	}
	for _, pair := range pairs {
		fmt.Printf("%d → %d\n", pair.Before, pair.After)
	}
	// Output:
	// 2 → 3
	// 4 → 5
}

func ExamplePageMatchInfo() {
	file := "../../testdata/ofd-creator/helloworld.ofd"
	report, err := compare.CompareFiles(context.Background(), []string{file, file}, compare.Options{})
	if err != nil {
		panic(err)
	}
	fmt.Println(report.Comparisons[0].Pages[0].Match.Method)
	// Output: content
}
