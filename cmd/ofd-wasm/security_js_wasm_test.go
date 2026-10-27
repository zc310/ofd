//go:build js && wasm

package main

import (
	"bytes"
	"strings"
	"syscall/js"
	"testing"

	"github.com/zc310/ofd/pkg/crypt"
)

func TestWASMInfoSecurityFollowsOpenAndClose(t *testing.T) {
	plain := wasmDocumentFixture(t)
	var encrypted bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), plain, &encrypted, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "user", Password: []byte("security-secret")}}}); err != nil {
		t.Fatal(err)
	}
	app := &wasmApp{}
	defer app.closeCurrentReader()
	open := func(data []byte, options map[string]any) js.Value {
		t.Helper()
		input := js.Global().Get("Uint8Array").New(len(data))
		js.CopyBytesToJS(input, data)
		return app.open(js.Undefined(), []js.Value{input, js.ValueOf(options)}).(js.Value)
	}
	for _, test := range []struct {
		data      []byte
		options   map[string]any
		encrypted bool
		state     string
	}{
		{encrypted.Bytes(), map[string]any{"password": "security-secret"}, true, "unlocked"},
		{plain, map[string]any{}, false, "none"},
	} {
		if result := open(test.data, test.options); result.Get("pageCount").Int() != 1 {
			t.Fatalf("打开失败: %v", result)
		}
		info := app.info(js.Undefined(), nil).(js.Value)
		security := info.Get("security")
		if security.Get("encrypted").Bool() != test.encrypted || security.Get("state").String() != test.state {
			t.Fatalf("安全状态不符: %v", security)
		}
		permission := info.Get("permissions").Index(0)
		if !permission.Get("export").Bool() || !permission.Get("printable").Bool() || permission.Get("print_copies").Int() != -1 {
			t.Fatalf("权限信息缺失: %v", info)
		}
		if encoded := js.Global().Get("JSON").Call("stringify", security).String(); strings.Contains(encoded, "security-secret") || strings.Contains(encoded, "user") {
			t.Fatalf("安全信息包含凭据: %s", encoded)
		}
	}
	if result := open(encrypted.Bytes(), map[string]any{"password": "wrong"}); result.Get("code").String() != "decrypt_failed" {
		t.Fatalf("错误口令未拒绝: %v", result)
	}
	if app.encrypted {
		t.Fatal("失败打开保留旧加密状态")
	}
	if result := app.info(js.Undefined(), nil).(js.Value); !result.Get("security").IsUndefined() {
		t.Fatalf("关闭后仍返回旧安全信息: %v", result)
	}
}
