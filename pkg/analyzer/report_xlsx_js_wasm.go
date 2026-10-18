//go:build js && wasm

package analyzer

import (
	"errors"
	"io"
)

// RenderXLSX 在浏览器 WASM 中不可用；可使用 JSON、RenderMarkdown 或 RenderText。
func RenderXLSX(io.Writer, Report) error {
	return errors.New("浏览器 WASM 不支持 XLSX 报告，请使用 JSON、Markdown 或文本")
}
