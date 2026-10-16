package converter_test

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"image/color"
	"io"
	"os"
	"path/filepath"

	"github.com/zc310/ofd/pkg/converter"
	_ "github.com/zc310/ofd/pkg/converter/import/markdown"
	pdfimport "github.com/zc310/ofd/pkg/converter/import/pdf"
)

type exampleBufferWriteCloser struct {
	*bytes.Buffer
}

func (exampleBufferWriteCloser) Close() error { return nil }

func ExamplePDF() {
	// 转换入口的第一个参数是取消信号；命令行工具通常用 context.Background()，
	// 长任务应传入带超时的 context。
	ctx := context.Background()

	output, err := os.CreateTemp("", "ofd-example-*.pdf")
	if err == nil {
		defer os.Remove(output.Name())
		err = converter.PDF(ctx, "../../testdata/ofdrw/intro.ofd", output)
		if closeErr := output.Close(); err == nil {
			err = closeErr
		}
	}
	fmt.Println(err == nil)
	// Output: true
}

func ExampleMarkdown() {
	// 转换入口的第一个参数是取消信号；命令行工具通常用 context.Background()，
	// 长任务应传入带超时的 context。
	ctx := context.Background()

	var output bytes.Buffer
	err := converter.Markdown(ctx, "../../testdata/helloworld.ofd", &output, converter.Page(1))
	// 标题使用 OFD 文档的 DocInfo.Title，没有标题时回退为 “OFD 文档”。
	fmt.Println(err == nil && bytes.Contains(output.Bytes(), []byte("# Hello World")))
	// Output: true
}

func ExamplePNG() {
	// 转换入口的第一个参数是取消信号；命令行工具通常用 context.Background()，
	// 长任务应传入带超时的 context。
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "ofd-example-")
	if err == nil {
		defer os.RemoveAll(dir)
		err = converter.Image(ctx, "../../testdata/ofdrw/ano.ofd",
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
	// 转换入口的第一个参数是取消信号；命令行工具通常用 context.Background()，
	// 长任务应传入带超时的 context。
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "ofd-example-")
	if err == nil {
		defer os.RemoveAll(dir)
		err = converter.Image(ctx, "../../testdata/ofdrw/intro.ofd",
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
	// 转换入口的第一个参数是取消信号；命令行工具通常用 context.Background()，
	// 长任务应传入带超时的 context。
	ctx := context.Background()

	var output bytes.Buffer
	err := converter.Image(ctx, "../../testdata/helloworld.ofd",
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
	// 转换入口的第一个参数是取消信号；命令行工具通常用 context.Background()，
	// 长任务应传入带超时的 context。
	ctx := context.Background()

	var output bytes.Buffer
	err := converter.HTML(ctx, "../../testdata/helloworld.ofd", &output, converter.Page(1), converter.DPI(72))
	fmt.Println(err == nil && bytes.Contains(output.Bytes(), []byte("data:image/png;base64,")))
	// Output: true
}

func ExampleDOCX() {
	// 转换入口的第一个参数是取消信号；命令行工具通常用 context.Background()，
	// 长任务应传入带超时的 context。
	ctx := context.Background()

	var output bytes.Buffer
	err := converter.DOCX(ctx, "../../testdata/helloworld.ofd", &output, converter.Page(1))
	fmt.Println(err == nil && docxHasPart(output.Bytes(), "word/document.xml"))
	// Output: true
}

// docxHasPart 报告产物里是否存在指定部件。DOCX 是 ZIP 容器，光判断签名不足以
// 确认包结构正确。
func docxHasPart(data []byte, name string) bool {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false
	}
	for _, file := range reader.File {
		if file.Name == name {
			return true
		}
	}
	return false
}

func ExampleHTMLSVG() {
	// 转换入口的第一个参数是取消信号；命令行工具通常用 context.Background()，
	// 长任务应传入带超时的 context。
	ctx := context.Background()

	var output bytes.Buffer
	err := converter.HTML(ctx, "../../testdata/helloworld.ofd", &output, converter.HTMLSVG(), converter.Page(1))
	fmt.Println(err == nil && bytes.Contains(output.Bytes(), []byte("<svg")))
	// Output: true
}

func ExampleHTMLJPG() {
	// 转换入口的第一个参数是取消信号；命令行工具通常用 context.Background()，
	// 长任务应传入带超时的 context。
	ctx := context.Background()

	var output bytes.Buffer
	err := converter.HTML(ctx, "../../testdata/helloworld.ofd", &output, converter.HTMLJPG(), converter.Page(1))
	fmt.Println(err == nil && bytes.Contains(output.Bytes(), []byte("data:image/jpeg;base64,")))
	// Output: true
}

func ExampleEPS() {
	// 转换入口的第一个参数是取消信号；命令行工具通常用 context.Background()，
	// 长任务应传入带超时的 context。
	ctx := context.Background()

	var output bytes.Buffer
	err := converter.Image(ctx, "../../testdata/helloworld.ofd",
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
	// 转换入口的第一个参数是取消信号；命令行工具通常用 context.Background()，
	// 长任务应传入带超时的 context。
	ctx := context.Background()

	var output bytes.Buffer
	err := converter.Image(ctx, "../../testdata/helloworld.ofd",
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
	// 转换入口的第一个参数是取消信号；命令行工具通常用 context.Background()，
	// 长任务应传入带超时的 context。
	ctx := context.Background()

	var output bytes.Buffer
	// from 为空时按文件扩展名自动识别 OFD 输入。
	err := converter.Convert(ctx, "", "pdf", "../../testdata/helloworld.ofd", &output)
	fmt.Println(err == nil && bytes.HasPrefix(output.Bytes(), []byte("%PDF-")))
	// Output: true
}

func ExampleConvert_pdfToOFD() {
	// 转换入口的第一个参数是取消信号；命令行工具通常用 context.Background()，
	// 长任务应传入带超时的 context。
	ctx := context.Background()

	var output bytes.Buffer
	err := converter.Convert(ctx, "pdf", "ofd", "../../testdata/pdf/sample0.pdf", &output)
	fmt.Println(err == nil && bytes.HasPrefix(output.Bytes(), []byte("PK")))
	// Output: true
}

func ExampleWithPDFImportOptions() {
	var output bytes.Buffer
	var diagnostics []converter.PDFImportDiagnostic
	err := converter.Convert(context.Background(), "pdf", "ofd", "../../testdata/pdf/sample0.pdf", &output,
		converter.WithPDFImportOptions(converter.PDFImportOptions{
			Limits:       converter.PDFImportLimits{MaxInputBytes: 32 << 20, MaxImagePixels: 20_000_000},
			ImageMode:    converter.PDFImageModeCompact,
			JPEGQuality:  converter.PDFDefaultJPEGQuality,
			JPXBackend:   converter.PDFJPXBackendGo,
			OnDiagnostic: func(d converter.PDFImportDiagnostic) { diagnostics = append(diagnostics, d) },
		}))
	// Strict: true 可要求遇到已报告的内容缺失或近似转换时直接失败。
	fmt.Println(err == nil && bytes.HasPrefix(output.Bytes(), []byte("PK")))
	// Output: true
}

func ExampleConvert_pdfImportEntrypoints() {
	ctx := context.Background()
	options := pdfimport.Options{Limits: pdfimport.Limits{MaxInputBytes: 32 << 20}}
	var output bytes.Buffer
	err := pdfimport.ConvertWithOptions(ctx, "../../testdata/pdf/sample0.pdf", &output, "", options)
	fmt.Println(err == nil && bytes.HasPrefix(output.Bytes(), []byte("PK")))

	dir, err := os.MkdirTemp("", "pdf-import-example-*")
	if err == nil {
		defer os.RemoveAll(dir)
		err = pdfimport.ConvertFileWithOptions(ctx, "../../testdata/pdf/sample0.pdf", filepath.Join(dir, "output.ofd"), "", options)
	}
	fmt.Println(err == nil)
	// Output:
	// true
	// true
}

func ExampleConvert_markdownToOFD() {
	// 转换入口的第一个参数是取消信号；命令行工具通常用 context.Background()，
	// 长任务应传入带超时的 context。
	ctx := context.Background()

	var output bytes.Buffer
	err := converter.Convert(ctx, "md", "ofd", "../../testdata/markdown/sample.md", &output)
	fmt.Println(err == nil && bytes.HasPrefix(output.Bytes(), []byte("PK")))
	// Output: true
}
