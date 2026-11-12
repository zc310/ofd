// Package markdown 为 converter 注册 Markdown→OFD 导入器（输入格式 "markdown"）。
//
// 该包依赖 creator/flow 与 goldmark，单独成包是为了让只做 OFD→X 的使用方
// 不必链接这些依赖；需要 Markdown→OFD 时按空白导入启用：
//
//	import _ "github.com/zc310/ofd/pkg/converter/import/markdown"
//
// 之后即可使用 converter.Convert(ctx, "md", "ofd", ...)。出于安全和确定性考虑，
// 远程图片不会被下载，只在日志中给出警告。
package markdown

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/zc310/ofd/internal/contextio"
	"github.com/zc310/ofd/internal/version"
	"github.com/zc310/ofd/pkg/converter"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
)

// converterName 写入 OFD DocInfo/Creator，标识转换工具本身。
const converterName = "zc310/ofd"

// converterVersion 写入 OFD DocInfo/CreatorVersion，取自 internal/version，与
// pdf2ofd 的写入同源。此前写死 "0.0.1"，转换产物的元数据因此长期显示着一个早已
// 不存在的版本。
var converterVersion = version.Version

// markdownImporter 把 Markdown 输入导入为 OFD。
type markdownImporter struct{}

func (m *markdownImporter) Name() string         { return "markdown" }
func (m *markdownImporter) Extensions() []string { return []string{".md", ".markdown", ".mdown"} }
func (m *markdownImporter) MIME() string         { return "text/markdown" }

func (m *markdownImporter) Import(input any, output io.Writer, conv *converter.Converter) error {
	return Convert(conv.Context(), input, output)
}

// Convert 将 Markdown 输入转换为 OFD，input 支持文件路径、[]byte 或 io.Reader。
// 传入文件路径时会以该文件所在目录解析相对图片路径；解析和流式排版阶段响应取消。
// 缺省使用逻辑字体；可传入一份 flow.Options 指定实际字体、主题和页面。
//
//nolint:contextcheck // 保留既有 nil context 约定，此时使用 Background；非空上下文原样传递。
func Convert(ctx context.Context, input any, output io.Writer, options ...flow.Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if output == nil {
		return fmt.Errorf("未设置 OFD 输出参数")
	}
	if reader, ok := input.(io.Reader); ok {
		input = contextio.Reader(ctx, reader)
	}
	source, baseDir, name, err := readInput(input)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	document, err := parseMarkdown(source, baseDir, options...)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	if document.Metadata.Title == "" {
		document.Metadata.Title = strings.TrimSuffix(name, filepath.Ext(name))
	}
	document.Metadata.Creator = converterName
	document.Metadata.CreatorVersion = converterVersion
	document.Metadata.CustomDatas = []creator.CustomData{{Name: "SourceFormat", Value: "Markdown"}}
	err = document.Write(ctx, output, creator.CreateOptions{Deterministic: true})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// ConvertFile 是 Convert 的按路径便捷形式。
func ConvertFile(ctx context.Context, mdPath, ofdPath string, options ...flow.Options) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.Create(ofdPath)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
	}()
	return Convert(ctx, mdPath, file, options...)
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
