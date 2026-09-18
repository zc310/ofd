package converter_test

import (
	"bytes"
	"fmt"
	"image/color"
	"io"
	"os"
	"path/filepath"

	"github.com/zc310/ofd/pkg/converter"
	_ "github.com/zc310/ofd/pkg/converter/mdimport"
	_ "github.com/zc310/ofd/pkg/converter/pdfimport"
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

func ExampleHTML() {
	var output bytes.Buffer
	err := converter.HTML("../../test/testdata/helloworld.ofd", &output, converter.Page(1), converter.DPI(72))
	fmt.Println(err == nil && bytes.Contains(output.Bytes(), []byte("data:image/png;base64,")))
	// Output: true
}

func ExampleHTMLSVG() {
	var output bytes.Buffer
	err := converter.HTML("../../test/testdata/helloworld.ofd", &output, converter.HTMLSVG(), converter.Page(1))
	fmt.Println(err == nil && bytes.Contains(output.Bytes(), []byte("<svg")))
	// Output: true
}

func ExampleHTMLJPG() {
	var output bytes.Buffer
	err := converter.HTML("../../test/testdata/helloworld.ofd", &output, converter.HTMLJPG(), converter.Page(1))
	fmt.Println(err == nil && bytes.Contains(output.Bytes(), []byte("data:image/jpeg;base64,")))
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

func ExampleConvert() {
	var output bytes.Buffer
	err := converter.Convert("ofd", "pdf", "../../test/testdata/helloworld.ofd", &output)
	fmt.Println(err == nil && bytes.HasPrefix(output.Bytes(), []byte("%PDF-")))
	// Output: true
}

func ExampleConvert_pdfToOFD() {
	var output bytes.Buffer
	err := converter.Convert("pdf", "ofd", "../../test/testdata/pdf/sample0.pdf", &output)
	fmt.Println(err == nil && bytes.HasPrefix(output.Bytes(), []byte("PK")))
	// Output: true
}

func ExampleConvert_markdownToOFD() {
	var output bytes.Buffer
	err := converter.Convert("md", "ofd", "../../test/testdata/markdown/sample.md", &output)
	fmt.Println(err == nil && bytes.HasPrefix(output.Bytes(), []byte("PK")))
	// Output: true
}
