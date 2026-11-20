package image_test

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	stdimage "image"
	"image/png"

	imageimport "github.com/zc310/ofd/pkg/converter/import/image"
)

func ExampleConvertZIP() {
	var picture, input, output bytes.Buffer
	_ = png.Encode(&picture, stdimage.NewRGBA(stdimage.Rect(0, 0, 40, 30)))
	writer := zip.NewWriter(&input)
	for _, name := range []string{"10.png", "2.png", "1.png"} {
		part, _ := writer.Create(name)
		_, _ = part.Write(picture.Bytes())
	}
	_ = writer.Close()
	err := imageimport.ConvertZIP(context.Background(), input.Bytes(), &output,
		imageimport.WithOCR(false),
		imageimport.WithMaxArchiveEntries(10),
		imageimport.WithMaxEntryBytes(1<<20),
		imageimport.WithMaxTotalBytes(3<<20))
	fmt.Println(err == nil && bytes.HasPrefix(output.Bytes(), []byte("PK")))
	// Output: true
}
