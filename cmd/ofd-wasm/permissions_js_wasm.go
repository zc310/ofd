//go:build js && wasm

package main

import (
	"errors"
	"syscall/js"

	"github.com/zc310/ofd/pkg/permission"
	ofdreader "github.com/zc310/ofd/pkg/reader"
)

func permissionsValue(permissions []ofdreader.DocumentPermissions) (js.Value, error) {
	return jsonValue(permissions)
}

// permissionErrorValue补充稳定原因，浏览器无需解析中文错误文案。
func permissionErrorDetails(result js.Value, err error) {
	if denied, ok := errors.AsType[*permission.DeniedError](err); ok {
		result.Set("action", string(denied.Action))
		result.Set("scope", denied.Scope)
		result.Set("reason", string(denied.Reason))
	}
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
	value, err := permissionsValue(permissions)
	if err != nil {
		return errorValue(err)
	}
	return value
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
	if err := reader.CheckPermission(ofdreader.PermissionAction(args[0].String()), indices); err != nil {
		return errorValue(err)
	}
	return js.ValueOf(true)
}
