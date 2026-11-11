//go:build js && wasm

package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"syscall/js"

	readerimage "github.com/zc310/ofd/internal/reader/preview"
	"github.com/zc310/ofd/pkg/reader"
)

var errSealPreviewArguments = errors.New("印章预览参数无效")
var errSealPreviewType = errors.New("此接口仅渲染 OFD 印章")

// signatureSealPreview 独立渲染 OFD 印章；原 signatureSeal API 仍返回原始字节。
func (a *wasmApp) signatureSealPreview(_ js.Value, args []js.Value) any {
	if len(args) != 5 || args[1].Type() != js.TypeString || args[3].Type() != js.TypeBoolean {
		return errorValue(errSealPreviewArguments)
	}
	scope, err := jsIndex(args[0])
	if err != nil {
		return errorValue(err)
	}
	index, err := jsIndex(args[2])
	if err != nil {
		return errorValue(err)
	}
	id, err := renderRequestID(args[4:])
	if err != nil {
		return errorValue(err)
	}
	doc, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	name, thumbnail := args[1].String(), args[3].Bool()
	a.mu.Lock()
	families := make([]string, 0, len(a.fallbackFonts))
	for _, font := range a.fallbackFonts {
		if !slices.Contains(families, font.Family) {
			families = append(families, font.Family)
		}
	}
	a.mu.Unlock()
	return a.renderAsync(id, func(ctx context.Context) (js.Value, error) {
		data, kind, err := doc.SignatureSeal(scope, name, index)
		if err != nil {
			return js.Undefined(), err
		}
		if !strings.EqualFold(kind, "ofd") {
			return js.Undefined(), errSealPreviewType
		}
		edge := 2048
		if thumbnail {
			edge = 256
		}
		data, err = readerimage.OFDSeal(ctx, data, edge, families...)
		if err != nil {
			return js.Undefined(), err
		}
		if err := doc.CheckDocumentPermission(reader.PermissionAccess, scope); err != nil {
			return js.Undefined(), err
		}
		result := js.Global().Get("Uint8Array").New(len(data))
		js.CopyBytesToJS(result, data)
		return result, nil
	})
}
