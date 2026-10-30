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
	_ "github.com/zc310/ofd/pkg/converter/import/csv"
	_ "github.com/zc310/ofd/pkg/converter/import/eml"
	_ "github.com/zc310/ofd/pkg/converter/import/markdown"
	pdfimport "github.com/zc310/ofd/pkg/converter/import/pdf"
	_ "github.com/zc310/ofd/pkg/converter/import/svg"
	"github.com/zc310/ofd/pkg/crypt"
)

type exampleBufferWriteCloser struct {
	*bytes.Buffer
}

func ExampleConvert_csv() {
	var output bytes.Buffer
	err := converter.Convert(context.Background(), "csv", "ofd",
		[]byte("Code,Description,Amount\n0001,Paper,12.30\n"), &output,
		converter.WithCSVImportOptions(converter.CSVImportOptions{Title: "Inventory"}),
		converter.WithLandscape(true))
	fmt.Println(err == nil && bytes.HasPrefix(output.Bytes(), []byte("PK")))
	// Output: true
}

func ExampleImporterByName_eml() {
	// EML 与 SVG 按需空白导入；实际转换时需要本机 Chrome/Chromium。
	for _, name := range []string{"eml", "svg"} {
		importer, ok := converter.ImporterByName(name)
		if ok {
			fmt.Println(importer.Name(), importer.MIME())
		}
	}
	// Output:
	// eml message/rfc822
	// svg image/svg+xml
}

func ExampleConvert_decryptedView() {
	ctx := context.Background()
	var encrypted, output bytes.Buffer
	_, err := crypt.Encrypt(ctx, "../../testdata/ofd-creator/helloworld.ofd", &encrypted, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "reader", Password: []byte("示例口令")}}})
	if err != nil {
		panic(err)
	}
	view, err := crypt.Open(ctx, encrypted.Bytes(), crypt.OpenOptions{DecryptOptions: crypt.DecryptOptions{Password: []byte("示例口令")}})
	if err != nil {
		panic(err)
	}
	defer view.Close()
	// The converter borrows the view directly; no plaintext ZIP is created.
	err = converter.Convert(ctx, "", "text", view, &output)
	fmt.Println(err == nil && output.Len() > 0)
	fmt.Println(view.EncryptionInfo().State)
	// Output:
	// true
	// unlocked
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
	err := converter.Markdown(ctx, "../../testdata/ofd-creator/helloworld.ofd", &output, converter.Page(1))
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
	err := converter.Image(ctx, "../../testdata/ofd-creator/helloworld.ofd",
		converter.Writer(func(int) (io.WriteCloser, error) {
			return exampleBufferWriteCloser{Buffer: &output}, nil
		}),
		converter.SVG(),
		converter.Page(1),
	)
	fmt.Println(err == nil && bytes.Contains(output.Bytes(), []byte("<svg")))
	// Output: true
}

func ExamplePNGCompressionLevel() {
	var output bytes.Buffer
	err := converter.Convert(context.Background(), "ofd", "png",
		"../../testdata/ofd-creator/helloworld.ofd", &output,
		converter.Page(1), converter.DPI(96), converter.PNGCompressionLevel(1))
	fmt.Println(err == nil && bytes.HasPrefix(output.Bytes(), []byte("\x89PNG\r\n\x1a\n")))
	// Output: true
}

func ExampleHTML() {
	// 转换入口的第一个参数是取消信号；命令行工具通常用 context.Background()，
	// 长任务应传入带超时的 context。
	ctx := context.Background()

	var output bytes.Buffer
	err := converter.HTML(ctx, "../../testdata/ofd-creator/helloworld.ofd", &output, converter.Page(1), converter.DPI(72))
	fmt.Println(err == nil && bytes.Contains(output.Bytes(), []byte("data:image/png;base64,")))
	// Output: true
}

func ExampleDOCX() {
	// 转换入口的第一个参数是取消信号；命令行工具通常用 context.Background()，
	// 长任务应传入带超时的 context。
	ctx := context.Background()

	var output bytes.Buffer
	err := converter.DOCX(ctx, "../../testdata/ofd-creator/helloworld.ofd", &output, converter.Page(1))
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
	err := converter.HTML(ctx, "../../testdata/ofd-creator/helloworld.ofd", &output, converter.HTMLSVG(), converter.Page(1))
	fmt.Println(err == nil && bytes.Contains(output.Bytes(), []byte("<svg")))
	// Output: true
}

func ExampleHTMLJPG() {
	// 转换入口的第一个参数是取消信号；命令行工具通常用 context.Background()，
	// 长任务应传入带超时的 context。
	ctx := context.Background()

	var output bytes.Buffer
	err := converter.HTML(ctx, "../../testdata/ofd-creator/helloworld.ofd", &output, converter.HTMLJPG(), converter.Page(1))
	fmt.Println(err == nil && bytes.Contains(output.Bytes(), []byte("data:image/jpeg;base64,")))
	// Output: true
}

func ExampleEPS() {
	// 转换入口的第一个参数是取消信号；命令行工具通常用 context.Background()，
	// 长任务应传入带超时的 context。
	ctx := context.Background()

	var output bytes.Buffer
	err := converter.Image(ctx, "../../testdata/ofd-creator/helloworld.ofd",
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
	err := converter.Image(ctx, "../../testdata/ofd-creator/helloworld.ofd",
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
	err := converter.Convert(ctx, "", "pdf", "../../testdata/ofd-creator/helloworld.ofd", &output)
	fmt.Println(err == nil && bytes.HasPrefix(output.Bytes(), []byte("%PDF-")))
	// Output: true
}

func ExampleConvert_pdfToOFD() {
	// 转换入口的第一个参数是取消信号；命令行工具通常用 context.Background()，
	// 长任务应传入带超时的 context。
	ctx := context.Background()

	var output bytes.Buffer
	err := converter.Convert(ctx, "pdf", "ofd", "../../testdata/libofd/sample0.pdf", &output)
	fmt.Println(err == nil && bytes.HasPrefix(output.Bytes(), []byte("PK")))
	// Output: true
}

func ExampleWithPDFImportOptions() {
	var output bytes.Buffer
	var diagnostics []converter.PDFImportDiagnostic
	err := converter.Convert(context.Background(), "pdf", "ofd", "../../testdata/libofd/sample0.pdf", &output,
		converter.WithPDFImportOptions(converter.PDFImportOptions{
			SaveRepairedFonts: true,
			Limits:            converter.PDFImportLimits{MaxInputBytes: 32 << 20, MaxImagePixels: 20_000_000},
			ImageMode:         converter.PDFImageModeCompact,
			JPEGQuality:       converter.PDFDefaultJPEGQuality,
			JPXBackend:        converter.PDFJPXBackendGo,
			OnDiagnostic:      func(d converter.PDFImportDiagnostic) { diagnostics = append(diagnostics, d) },
		}))
	// Strict: true 可要求遇到已报告的内容缺失或近似转换时直接失败。
	fmt.Println(err == nil && bytes.HasPrefix(output.Bytes(), []byte("PK")))
	// Output: true
}

func ExampleConvert_pdfImportEntrypoints() {
	ctx := context.Background()
	options := pdfimport.Options{Limits: pdfimport.Limits{MaxInputBytes: 32 << 20}}
	var output bytes.Buffer
	err := pdfimport.ConvertWithOptions(ctx, "../../testdata/libofd/sample0.pdf", &output, "", options)
	fmt.Println(err == nil && bytes.HasPrefix(output.Bytes(), []byte("PK")))

	dir, err := os.MkdirTemp("", "pdf-import-example-*")
	if err == nil {
		defer os.RemoveAll(dir)
		err = pdfimport.ConvertFileWithOptions(ctx, "../../testdata/libofd/sample0.pdf", filepath.Join(dir, "output.ofd"), "", options)
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
