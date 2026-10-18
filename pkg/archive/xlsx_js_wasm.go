//go:build js && wasm

package archive

import (
	"errors"
	"io"
)

// RenderMatrixXLSX 在浏览器 WASM 中不可用；可使用 JSON、Markdown 或文本报告。
func RenderMatrixXLSX(io.Writer, MatrixReport) error {
	return errors.New("浏览器 WASM 不支持 XLSX 报告，请使用 JSON、Markdown 或文本")
}
