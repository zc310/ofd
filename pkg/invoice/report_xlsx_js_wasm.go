//go:build js && wasm

package invoice

import (
	"errors"
	"io"
)

// WriteXLSX 在浏览器 WASM 中不可用；发票报表可使用 JSON 或 WriteCSV。
func WriteXLSX(io.Writer, *Batch) error {
	return errors.New("浏览器 WASM 不支持 XLSX 报表，请使用 JSON 或 CSV")
}
