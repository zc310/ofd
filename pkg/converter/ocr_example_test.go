package converter_test

import (
	"context"
	"fmt"
	"image"

	"github.com/zc310/ofd/pkg/converter"
)

type exampleOCREngine struct{}

func (exampleOCREngine) Recognize(_ context.Context, _ image.Image) ([]converter.OCRTextBlock, error) {
	return []converter.OCRTextBlock{{Text: "hello", Bounds: image.Rect(0, 0, 20, 10), Confidence: 99}}, nil
}

func ExampleWithImageOCREngine() {
	// 自定义引擎与启用选项一同传给 converter.Convert。
	options := []converter.Option{converter.ImageOCR(true), converter.WithImageOCREngine(exampleOCREngine{})}
	fmt.Println(len(options))
	// Output: 2
}
