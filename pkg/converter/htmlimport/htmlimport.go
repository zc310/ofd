// Package htmlimport 为 converter 注册 HTML/MHTML 导入器，以及到 PDF 的直接
// 转换器。渲染通过 chromedp 驱动 Chrome/Chromium 的打印引擎完成：X→OFD 走
// Chrome 转 PDF 后再由 pdf2ofd 生成 OFD；X→PDF 则由 Chrome 直接输出。
//
// 该包依赖 Chrome/Chromium 可执行文件，单独成包是为了让不需要 HTML 转换的
// 使用方不必链接 chromedp；需要时按空白导入启用：
//
//	import _ "github.com/zc310/ofd/pkg/converter/htmlimport"
//
// 之后即可使用 converter.Convert("mhtml", "pdf", ...) 或
// converter.Convert("html", "ofd", ...)。运行环境必须安装 Chrome/Chromium，
// 否则返回明确错误。
package htmlimport

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/zc310/ofd/internal/browser"
	"github.com/zc310/ofd/internal/pdf2ofd"
	"github.com/zc310/ofd/pkg/converter"
)

// format 描述一种 HTML 输入格式。
type format struct {
	name       string
	extensions []string
	mime       string
}

// formats 是支持的 HTML 输入格式，都通过 Chrome 打印引擎渲染。
var formats = []format{
	{name: "mhtml", extensions: []string{".mhtml", ".mht"}, mime: "multipart/related"},
	{name: "html", extensions: []string{".html", ".htm", ".xhtml"}, mime: "text/html"},
}

// htmlImporter 把 HTML/MHTML 导入为 OFD：先由 Chrome 转 PDF，再复用 PDF→OFD。
type htmlImporter struct {
	format format
}

func (h *htmlImporter) Name() string         { return h.format.name }
func (h *htmlImporter) Extensions() []string { return h.format.extensions }
func (h *htmlImporter) MIME() string         { return h.format.mime }

func (h *htmlImporter) Import(input any, output io.Writer, conv *converter.Converter) error {
	pdf, err := convertToPDF(input, conv)
	if err != nil {
		return err
	}
	return pdf2ofd.Convert(pdf, output)
}

// htmlTransformer 把 HTML/MHTML 直接转换为 PDF。
type htmlTransformer struct {
	from []string
}

func (h *htmlTransformer) From() []string { return h.from }
func (h *htmlTransformer) To() string     { return "pdf" }

func (h *htmlTransformer) Transform(input any, output io.Writer, conv *converter.Converter) error {
	if output == nil {
		return fmt.Errorf("未设置 PDF 输出参数")
	}
	pdf, err := convertToPDF(input, conv)
	if err != nil {
		return err
	}
	_, err = output.Write(pdf)
	return err
}

// convertToPDF 把 HTML 输入（路径、[]byte 或 io.Reader）交给 Chrome 渲染为
// PDF 字节。非路径输入会先写入临时文件。
func convertToPDF(input any, conv *converter.Converter) ([]byte, error) {
	tempDir := ""
	if conv != nil {
		tempDir = conv.TempDir()
	}
	path, cleanup, err := htmlInputPath(input, tempDir)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	options := browser.Options{TempDir: tempDir}
	if conv != nil {
		paper := conv.Paper()
		options.Chrome = conv.ChromePath()
		options.Timeout = conv.OfficeTimeout()
		// 传入未交换的纸张尺寸，由 Chrome 依据 Landscape 决定方向。
		options.Width = paper.Width
		options.Height = paper.Height
		options.Landscape = paper.Landscape
		options.PrintBackground = conv.PrintBackground()
		options.AllowRemoteResources = conv.AllowRemoteResources()
		options.NoSandbox = conv.NoSandbox()
	}
	return browser.ConvertFileToPDF(context.Background(), path, options)
}

// htmlInputPath 把输入统一为本地文件路径。非路径输入写入临时文件，返回的
// cleanup 负责删除临时文件；路径输入返回空 cleanup。
func htmlInputPath(input any, tempDir string) (string, func(), error) {
	switch value := input.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return "", func() {}, fmt.Errorf("HTML 输入文件名为空")
		}
		return value, func() {}, nil
	case []byte:
		return writeTempInput(value, tempDir)
	case io.Reader:
		data, err := io.ReadAll(value)
		if err != nil {
			return "", func() {}, fmt.Errorf("读取 HTML 输入失败: %w", err)
		}
		return writeTempInput(data, tempDir)
	default:
		return "", func() {}, fmt.Errorf("不支持的 HTML 输入类型 %T", input)
	}
}

func writeTempInput(data []byte, tempDir string) (string, func(), error) {
	file, err := os.CreateTemp(tempDir, "ofd-html-input-*.html")
	if err != nil {
		return "", func() {}, fmt.Errorf("创建临时文件失败: %w", err)
	}
	path := file.Name()
	cleanup := func() { os.Remove(path) }
	if _, err := file.Write(data); err != nil {
		file.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("关闭临时文件失败: %w", err)
	}
	return path, cleanup, nil
}

func init() {
	names := make([]string, 0, len(formats))
	for _, item := range formats {
		names = append(names, item.name)
		converter.RegisterImporter(&htmlImporter{format: item})
	}
	// 所有 HTML 格式都注册到 PDF 的直接转换器，避免经过 OFD 中间格式。
	converter.RegisterTransformer(&htmlTransformer{from: names})
}
