package converter_test

import (
	"bytes"
	"fmt"
	"image/color"
	"io"
	"os"
	"path/filepath"

	"github.com/zc310/ofd/pkg/converter"
)

type exampleBufferWriteCloser struct {
	*bytes.Buffer
}

func (exampleBufferWriteCloser) Close() error { return nil }

func ExamplePDF() {
	output, err := os.CreateTemp("", "ofd-example-*.pdf")
	if err == nil {
		defer os.Remove(output.Name())
		err = converter.PDF("../../test/testdata/intro.ofd", output)
		if closeErr := output.Close(); err == nil {
			err = closeErr
		}
	}
	fmt.Println(err == nil)
	// Output: true
}

func ExampleMarkdown() {
	var output bytes.Buffer
	err := converter.Markdown("../../test/testdata/helloworld.ofd", &output, converter.Page(1))
	fmt.Println(err == nil && bytes.Contains(output.Bytes(), []byte("## 第 1 页")))
	// Output: true
}

func ExamplePNG() {
	dir, err := os.MkdirTemp("", "ofd-example-")
	if err == nil {
		defer os.RemoveAll(dir)
		err = converter.Image("../../test/testdata/ano.ofd",
			converter.Writer(func(page int) (io.WriteCloser, error) {
				return os.Create(filepath.Join(dir, fmt.Sprintf("ano_%d.png", page)))
			}),
			converter.BgColor(color.White),
			converter.PNG(),
		)
	}
	fmt.Println(err == nil)
	// Output: true
}

func ExampleJPG() {
	dir, err := os.MkdirTemp("", "ofd-example-")
	if err == nil {
		defer os.RemoveAll(dir)
		err = converter.Image("../../test/testdata/intro.ofd",
			converter.Writer(func(page int) (io.WriteCloser, error) {
				return os.Create(filepath.Join(dir, fmt.Sprintf("intro_%d.jpg", page)))
			}),
			converter.BgColor(color.White),
			converter.JPG(),
			converter.Page(40),
			converter.DPI(300),
		)
	}
	fmt.Println(err == nil)
	// Output: true
}

func ExampleSVG() {
	var output bytes.Buffer
	err := converter.Image("../../test/testdata/helloworld.ofd",
		converter.Writer(func(int) (io.WriteCloser, error) {
			return exampleBufferWriteCloser{Buffer: &output}, nil
		}),
		converter.SVG(),
		converter.Page(1),
	)
	fmt.Println(err == nil && bytes.Contains(output.Bytes(), []byte("<svg")))
	// Output: true
}

func ExampleEPS() {
	var output bytes.Buffer
	err := converter.Image("../../test/testdata/helloworld.ofd",
		converter.Writer(func(int) (io.WriteCloser, error) {
			return exampleBufferWriteCloser{Buffer: &output}, nil
		}),
		converter.EPS(),
		converter.Page(1),
	)
	fmt.Println(err == nil && bytes.Contains(output.Bytes(), []byte("%!PS-Adobe-3.0 EPSF-3.0")))
	// Output: true
}

func ExampleTeX() {
	var output bytes.Buffer
	err := converter.Image("../../test/testdata/helloworld.ofd",
		converter.Writer(func(int) (io.WriteCloser, error) {
			return exampleBufferWriteCloser{Buffer: &output}, nil
		}),
		converter.TeX(),
		converter.Page(1),
	)
	fmt.Println(err == nil && bytes.Contains(output.Bytes(), []byte("\\begin{pgfpicture}")))
	// Output: true
}
