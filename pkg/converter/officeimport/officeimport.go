// Package officeimport 为 converter 注册 Office 文档（doc/docx/odt/rtf/wps/
// pptx/xlsx 等）的导入器，以及到 PDF 的直接转换器。转换通过 LibreOffice
// 命令行完成：X→OFD 走 LibreOffice 转 PDF 后再由 pdf2ofd 生成 OFD；
// X→PDF 则由 LibreOffice 直接输出。
//
// 该包依赖 LibreOffice 可执行文件，单独成包是为了让不需要 Office 转换的
// 使用方不必链接这些依赖；需要时按空白导入启用：
//
//	import _ "github.com/zc310/ofd/pkg/converter/officeimport"
//
// 之后即可使用 converter.Convert("docx", "ofd", ...) 或
// converter.Convert("docx", "pdf", ...)。运行环境必须安装 LibreOffice，
// 否则返回明确错误。
package officeimport

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/zc310/ofd/internal/office"
	"github.com/zc310/ofd/internal/pdf2ofd"
	"github.com/zc310/ofd/pkg/converter"
)

// format 描述一种 Office 输入格式。
type format struct {
	name       string
	extensions []string
	mime       string
}

// formats 是支持的 Office 输入格式。它们都通过 LibreOffice 转换为 PDF。
var formats = []format{
	{name: "docx", extensions: []string{".docx", ".docm", ".dotx", ".dotm"}, mime: "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
	{name: "doc", extensions: []string{".doc", ".dot"}, mime: "application/msword"},
	{name: "odt", extensions: []string{".odt", ".ott", ".fodt"}, mime: "application/vnd.oasis.opendocument.text"},
	{name: "rtf", extensions: []string{".rtf"}, mime: "application/rtf"},
	{name: "wps", extensions: []string{".wps"}, mime: "application/vnd.ms-works"},
	{name: "pptx", extensions: []string{".pptx", ".pptm", ".ppsx", ".potx"}, mime: "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
	{name: "ppt", extensions: []string{".ppt", ".pps", ".pot"}, mime: "application/vnd.ms-powerpoint"},
	{name: "odp", extensions: []string{".odp", ".otp", ".fodp"}, mime: "application/vnd.oasis.opendocument.presentation"},
	{name: "xlsx", extensions: []string{".xlsx", ".xlsm", ".xltx", ".xltm"}, mime: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
	{name: "xls", extensions: []string{".xls", ".xlt"}, mime: "application/vnd.ms-excel"},
	{name: "ods", extensions: []string{".ods", ".ots", ".fods"}, mime: "application/vnd.oasis.opendocument.spreadsheet"},
}

// officeImporter 把 Office 文档导入为 OFD：先由 LibreOffice 转 PDF，再复用
// PDF→OFD 转换。
type officeImporter struct {
	format format
}

func (o *officeImporter) Name() string         { return o.format.name }
func (o *officeImporter) Extensions() []string { return o.format.extensions }
func (o *officeImporter) MIME() string         { return o.format.mime }

func (o *officeImporter) Import(input any, output io.Writer, conv *converter.Converter) error {
	pdf, err := convertToPDF(input, conv)
	if err != nil {
		return err
	}
	return pdf2ofd.Convert(pdf, output)
}

// officeTransformer 把 Office 文档直接转换为 PDF。
type officeTransformer struct {
	from []string
}

func (o *officeTransformer) From() []string { return o.from }
func (o *officeTransformer) To() string     { return "pdf" }

func (o *officeTransformer) Transform(input any, output io.Writer, conv *converter.Converter) error {
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

// convertToPDF 把 Office 输入（路径、[]byte 或 io.Reader）交给 LibreOffice
// 转换为 PDF 字节。非路径输入会先写入临时文件。
func convertToPDF(input any, conv *converter.Converter) ([]byte, error) {
	tempDir := ""
	if conv != nil {
		tempDir = conv.TempDir()
	}
	path, cleanup, err := officeInputPath(input, tempDir)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	options := office.Options{TempDir: tempDir}
	if conv != nil {
		options.Soffice = conv.SofficePath()
		options.Timeout = conv.OfficeTimeout()
	}
	return office.ConvertToPDF(context.Background(), path, options)
}

// officeInputPath 把输入统一为本地文件路径。非路径输入写入临时文件，返回的
// cleanup 负责删除临时文件；路径输入返回空 cleanup。
func officeInputPath(input any, tempDir string) (string, func(), error) {
	switch value := input.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return "", func() {}, fmt.Errorf("Office 输入文件名为空")
		}
		return value, func() {}, nil
	case []byte:
		return writeTempInput(value, tempDir)
	case io.Reader:
		data, err := io.ReadAll(value)
		if err != nil {
			return "", func() {}, fmt.Errorf("读取 Office 输入失败: %w", err)
		}
		return writeTempInput(data, tempDir)
	default:
		return "", func() {}, fmt.Errorf("不支持的 Office 输入类型 %T", input)
	}
}

func writeTempInput(data []byte, tempDir string) (string, func(), error) {
	file, err := os.CreateTemp(tempDir, "ofd-office-input-*")
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
		converter.RegisterImporter(&officeImporter{format: item})
	}
	// 所有 Office 格式都注册到 PDF 的直接转换器，避免经过 OFD 中间格式。
	converter.RegisterTransformer(&officeTransformer{from: names})
}
