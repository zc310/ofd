// Package mdimport 为 converter 注册 Markdown→OFD 导入器（输入格式 "markdown"）。
//
// 该包依赖 internal/layout 与 goldmark，单独成包是为了让只做 OFD→X 的使用方
// 不必链接这些依赖；需要 Markdown→OFD 时按空白导入启用：
//
//	import _ "github.com/zc310/ofd/pkg/converter/mdimport"
//
// 之后即可使用 converter.Convert("md", "ofd", ...)。出于安全和确定性考虑，
// 远程图片不会被下载，只在日志中给出警告。
package mdimport

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/zc310/ofd/internal/layout"
	"github.com/zc310/ofd/pkg/converter"
	"github.com/zc310/ofd/pkg/creator"
)

const (
	converterName    = "zc310/ofd"
	converterVersion = "0.0.1"
)

// markdownImporter 把 Markdown 输入导入为 OFD。
type markdownImporter struct{}

func (m *markdownImporter) Name() string         { return "markdown" }
func (m *markdownImporter) Extensions() []string { return []string{".md", ".markdown", ".mdown"} }
func (m *markdownImporter) MIME() string         { return "text/markdown" }

func (m *markdownImporter) Import(input any, output io.Writer, _ *converter.Converter) error {
	source, baseDir, name, err := readInput(input)
	if err != nil {
		return err
	}
	document, err := parseMarkdown(source, baseDir)
	if err != nil {
		return err
	}
	if document.Title == "" {
		document.Title = strings.TrimSuffix(name, filepath.Ext(name))
	}
	ofd, err := layout.Build(document, layout.DefaultOptions())
	if err != nil {
		return err
	}
	ofd.Creator = converterName
	ofd.CreatorVersion = converterVersion
	ofd.CustomDatas = []creator.CustomData{{Name: "SourceFormat", Value: "Markdown"}}
	return creator.CreateWithOptions(*ofd, output, creator.CreateOptions{Deterministic: true})
}

// Convert 将 Markdown 输入转换为 OFD，input 支持文件路径、[]byte 或 io.Reader。
// 传入文件路径时会以该文件所在目录解析相对图片路径。
func Convert(input any, output io.Writer) error {
	return (&markdownImporter{}).Import(input, output, nil)
}

// ConvertFile 是 Convert 的按路径便捷形式。
func ConvertFile(mdPath, ofdPath string) error {
	file, err := os.Create(ofdPath)
	if err != nil {
		return err
	}
	defer file.Close()
	return Convert(mdPath, file)
}

func readInput(input any) ([]byte, string, string, error) {
	switch value := input.(type) {
	case string:
		data, err := os.ReadFile(value)
		if err != nil {
			return nil, "", "", fmt.Errorf("读取 Markdown 失败: %w", err)
		}
		return data, filepath.Dir(value), filepath.Base(value), nil
	case []byte:
		return value, "", "", nil
	case io.Reader:
		data, err := io.ReadAll(value)
		if err != nil {
			return nil, "", "", fmt.Errorf("读取 Markdown 失败: %w", err)
		}
		return data, "", "", nil
	default:
		return nil, "", "", fmt.Errorf("不支持的 Markdown 输入类型 %T", input)
	}
}

func init() {
	converter.RegisterImporter(&markdownImporter{})
}
