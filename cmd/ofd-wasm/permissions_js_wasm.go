//go:build js && wasm

package main

import (
	"errors"
	"syscall/js"

	"github.com/zc310/ofd/pkg/webreader"
)

func permissionsValue(permissions []webreader.DocumentPermissions) js.Value {
	result := js.Global().Get("Array").New(len(permissions))
	for index, permission := range permissions {
		result.SetIndex(index, objectValue(map[string]any{
			"scope": permission.Scope, "pages": intSliceValue(permission.Pages), "declared": permission.Declared,
			"edit": permission.Edit, "annot": permission.Annot, "export": permission.Export,
			"signature": permission.Signature, "watermark": permission.Watermark, "print_screen": permission.PrintScreen,
			"printable": permission.Printable, "print_copies": permission.PrintCopies,
			"start_date": permission.StartDate, "end_date": permission.EndDate,
		}))
	}
	return result
}

func (a *wasmApp) permissions(_ js.Value, _ []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	permissions, err := reader.Permissions()
	if err != nil {
		return errorValue(err)
	}
	return permissionsValue(permissions)
}

func (a *wasmApp) checkPermission(_ js.Value, args []js.Value) any {
	if len(args) != 2 || args[0].Type() != js.TypeString {
		return errorValue(errors.New("ofd.checkPermission 需要操作名称和页面索引数组"))
	}
	indices, err := jsIndices(args[1], wasmMaxStreamPages)
	if err != nil {
		return errorValue(err)
	}
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	if err := reader.CheckPermission(webreader.PermissionAction(args[0].String()), indices); err != nil {
		return errorValue(err)
	}
	return js.ValueOf(true)
}
