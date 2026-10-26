//go:build js && wasm

package main

import (
	"bytes"
	"syscall/js"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/crypt"
)

func TestWASMPermissionsForPlainAndEncryptedDocuments(t *testing.T) {
	denied := false
	plain, err := creator.Marshal(creator.Document{ID: "restricted", Permissions: &creator.Permissions{Export: &denied, Print: &creator.PrintSettings{Printable: false}}, Pages: []creator.Page{{}, {}}})
	if err != nil {
		t.Fatal(err)
	}
	var encrypted bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), plain, &encrypted, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "user", Password: []byte("secret")}}}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		data    []byte
		options map[string]any
	}{
		{"plain", plain, map[string]any{}},
		{"encrypted", encrypted.Bytes(), map[string]any{"password": "secret"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := &wasmApp{}
			defer app.closeCurrentReader()
			input := js.Global().Get("Uint8Array").New(len(test.data))
			js.CopyBytesToJS(input, test.data)
			result := app.open(js.Undefined(), []js.Value{input, js.ValueOf(test.options)}).(js.Value)
			permission := result.Get("permissions").Index(0)
			if permission.Get("export").Bool() || permission.Get("printable").Bool() || permission.Get("pages").Length() != 2 {
				t.Fatalf("打开未返回权限: %v", result)
			}
			if app.permissions(js.Undefined(), nil).(js.Value).Index(0).Get("export").Bool() {
				t.Fatal("独立权限接口不一致")
			}
			for _, action := range []string{"copy", "export", "print"} {
				value := app.checkPermission(js.Undefined(), []js.Value{js.ValueOf(action), js.ValueOf([]any{0, 1})}).(js.Value)
				if value.Get("code").String() != "permission_denied" {
					t.Fatalf("%s 未拦截: %v", action, value)
				}
			}
			if value := app.text(js.Undefined(), []js.Value{js.ValueOf(0)}).(js.Value); value.Get("code").String() != "permission_denied" {
				t.Fatalf("文字未拦截: %v", value)
			}
			if value := app.mediaData(js.Undefined(), []js.Value{js.ValueOf(0), js.ValueOf(999), js.ValueOf(0)}).(js.Value); value.Get("code").String() != "permission_denied" {
				t.Fatalf("原始资源下载未先检查导出权限: %v", value)
			}
			for _, purpose := range []string{"export", "print"} {
				options := js.ValueOf(map[string]any{"dpi": 12, "purpose": purpose})
				for _, request := range []any{
					app.renderPage(js.Undefined(), []js.Value{js.ValueOf(0), options}),
					app.renderPages(js.Undefined(), []js.Value{js.ValueOf([]any{0, 1}), options}),
				} {
					value, rejected := awaitRenderPromise(t, request)
					if !rejected || value.Get("code").String() != "permission_denied" {
						t.Fatalf("异步用途 %s 未拦截: %v", purpose, value)
					}
				}
			}
			chunks := 0
			callback := js.FuncOf(func(js.Value, []js.Value) any { chunks++; return nil })
			defer callback.Release()
			for _, format := range []string{"png", "pdf"} {
				for _, indices := range [][]any{{0}, {0, 1}} {
					value := app.renderStream(js.Undefined(), []js.Value{js.ValueOf(indices), js.ValueOf(map[string]any{"format": format}), callback.Value}).(js.Value)
					if value.Get("code").String() != "permission_denied" || chunks != 0 {
						t.Fatalf("%s 流式导出未先拦截: %v chunks=%d", format, value, chunks)
					}
				}
			}
			preview := app.renderPage(js.Undefined(), []js.Value{js.ValueOf(0), js.ValueOf(map[string]any{"dpi": 12})})
			if _, rejected := awaitRenderPromise(t, preview); rejected {
				t.Fatal("禁止导出影响普通预览")
			}
		})
	}
}
