// Package xps 为 converter 注册 XPS 导入器，以及 XPS→PDF 的直接转换器。
// 转换依赖目标机器已安装 libgxps 的 xpstopdf 命令。
package xps

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/zc310/ofd/internal/convert/xps"
	"github.com/zc310/ofd/internal/pdf2ofd"
	"github.com/zc310/ofd/pkg/converter"
)

type xpsImporter struct{}

func (xpsImporter) Name() string         { return "xps" }
func (xpsImporter) Extensions() []string { return []string{".xps"} }
func (xpsImporter) MIME() string         { return "application/vnd.ms-xpsdocument" }

func (xpsImporter) Import(input any, output io.Writer, conv *converter.Converter) error {
	pdf, err := convertToPDF(input, conv)
	if err != nil {
		return err
	}
	return pdf2ofd.ConvertWithOptions(conv.Context(), pdf, output, "", conv.PDFImportOptions())
}

type xpsTransformer struct{}

func (xpsTransformer) From() []string { return []string{"xps"} }
func (xpsTransformer) To() string     { return "pdf" }

func (xpsTransformer) Transform(input any, output io.Writer, conv *converter.Converter) error {
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

func convertToPDF(input any, conv *converter.Converter) ([]byte, error) {
	tempDir := ""
	if conv != nil {
		tempDir = conv.TempDir()
	}
	path, cleanup, err := inputPath(input, tempDir)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	options := xps.Options{TempDir: tempDir}
	if conv != nil {
		options.Xpstopdf = conv.XpstopdfPath()
		options.Timeout = conv.OfficeTimeout()
	}
	return xps.ConvertToPDF(conv.Context(), path, options)
}

func inputPath(input any, tempDir string) (string, func(), error) {
	switch value := input.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return "", func() {}, fmt.Errorf("XPS 输入文件名为空")
		}
		return value, func() {}, nil
	case []byte:
		return writeTempInput(value, tempDir)
	case io.Reader:
		data, err := io.ReadAll(value)
		if err != nil {
			return "", func() {}, fmt.Errorf("读取 XPS 输入失败: %w", err)
		}
		return writeTempInput(data, tempDir)
	default:
		return "", func() {}, fmt.Errorf("不支持的 XPS 输入类型 %T", input)
	}
}

func writeTempInput(data []byte, tempDir string) (string, func(), error) {
	file, err := os.CreateTemp(tempDir, "ofd-xps-input-*.xps")
	if err != nil {
		return "", func() {}, fmt.Errorf("创建临时 XPS 文件失败: %w", err)
	}
	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("写入临时 XPS 文件失败: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("关闭临时 XPS 文件失败: %w", err)
	}
	return path, cleanup, nil
}

func init() {
	converter.RegisterImporter(xpsImporter{})
	converter.RegisterTransformer(xpsTransformer{})
}
