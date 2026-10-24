//go:build js && wasm

package main

import (
	"bytes"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"strings"
	"syscall/js"
	"testing"

	"github.com/emmansun/gmsm/pkcs8"
	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/pkg/crypt"
)

func TestWASMOpenRequiresCredentialsAndReleasesPreviousReader(t *testing.T) {
	plain := wasmDocumentFixture(t)
	var encrypted bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), plain, &encrypted, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "User", Password: []byte("123456")}}}); err != nil {
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
	if result := open(encrypted.Bytes(), map[string]any{}); result.Get("code").String() != "credentials_required" {
		t.Fatalf("缺凭据错误未保留代码: %v", result)
	}
	if result := open(encrypted.Bytes(), map[string]any{"password": "123456", "userName": "User"}); result.Get("pageCount").Int() != 1 {
		t.Fatalf("无法从 JS 解锁: %v", result)
	}
	previous, err := app.currentReader()
	if err != nil {
		t.Fatal(err)
	}
	if result := open(encrypted.Bytes(), map[string]any{"password": "wrong"}); result.Get("code").String() != "decrypt_failed" {
		t.Fatalf("错误口令未保留代码: %v", result)
	}
	if _, err := app.currentReader(); err == nil {
		t.Fatal("打开失败后保留了旧 Reader")
	}
	if _, err := previous.Text(0); err == nil {
		t.Fatal("切换时没有关闭旧 Reader")
	}
	if result := open(plain, map[string]any{}); result.Get("pageCount").Int() != 1 {
		t.Fatalf("失败后不能重开普通文档: %v", result)
	}
	var compatibleSource bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), plain, &compatibleSource, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "User", Password: []byte("123456")}}, PlaintextMap: true}); err != nil {
		t.Fatal(err)
	}
	compatible := wasmOFDRWMetadata(t, compatibleSource.Bytes(), "")
	if result := open(compatible, map[string]any{}); result.Get("code").String() != "credentials_required" {
		t.Fatalf("ofdrw 兼容包未请求解锁: %v", result)
	}
	if result := open(compatible, map[string]any{"password": "123456"}); result.Get("pageCount").Int() != 1 {
		t.Fatalf("无法从 JS 打开 ofdrw 兼容包: %v", result)
	}
}

func TestWASMDecryptOptionsPreserveEmptyPasswordsAndRejectInvalidCredentials(t *testing.T) {
	for _, password := range []string{"", "  中文🔐  ", "interop-password\n", "\uFEFF中文\r\n"} {
		value := js.ValueOf(map[string]any{"password": password, "userName": "User"})
		if strings.HasPrefix(password, "\uFEFF") {
			// Go 向 JS 传字符串也会移除 BOM；从 JS 字符码构造浏览器原始输入。
			codes := []any{}
			for _, r := range password {
				codes = append(codes, int(r))
			}
			value.Set("password", js.Global().Get("String").Call("fromCodePoint", codes...))
		}
		options, err := decryptOptions([]js.Value{value})
		if err != nil || options.Password == nil || string(options.Password) != password || options.UserName != "User" {
			t.Fatalf("口令或接收者被改写: %v", err)
		}
		clear(options.Password)
	}
	for _, options := range []map[string]any{
		{"password": 123456}, {"password": strings.Repeat("x", 65537)}, {"userName": true},
		{"privateKey": "not binary"}, {"privateKey": map[string]any{"byteLength": wasmMaxCredentialBytes + 1}},
		{"privateKey": js.Global().Get("Uint8Array").New(1), "privateKeyPassword": true},
		{"privateKey": js.Global().Get("Uint8Array").New(1), "privateKeyPassword": strings.Repeat("x", 65537)},
	} {
		if _, err := decryptOptions([]js.Value{js.ValueOf(options)}); !errors.Is(err, errInvalidCredentials) || wasmErrorCode(err) != "invalid_credentials" {
			t.Fatalf("非法凭据未拒绝: %v", err)
		}
	}
}

func TestWASMDecryptOptionsAcceptSM2PrivateKeyArrayBuffers(t *testing.T) {
	key, err := sm2.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := smx509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{der, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})} {
		input := js.Global().Get("Uint8Array").New(len(data))
		js.CopyBytesToJS(input, data)
		for _, value := range []js.Value{input, input.Get("buffer")} {
			options, err := decryptOptions([]js.Value{js.ValueOf(map[string]any{"privateKey": value})})
			if err != nil {
				t.Fatal(err)
			}
			parsed, ok := options.PrivateKey.(*sm2.PrivateKey)
			if !ok || parsed.D.Cmp(key.D) != 0 {
				t.Fatal("JS 私钥内容未正确传递到解密选项")
			}
		}
	}
	password := []byte("js-test-key-password")
	encryptedDER, err := pkcs8.MarshalPrivateKey(key, password, pkcs8.DefaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	encryptedPEM := pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: encryptedDER})
	input := js.Global().Get("Uint8Array").New(len(encryptedPEM))
	js.CopyBytesToJS(input, encryptedPEM)
	options, err := decryptOptions([]js.Value{js.ValueOf(map[string]any{
		"privateKey":         input,
		"privateKeyPassword": string(password),
	})})
	if err != nil {
		t.Fatal(err)
	}
	parsed, ok := options.PrivateKey.(*sm2.PrivateKey)
	if !ok || parsed.D.Cmp(key.D) != 0 {
		t.Fatal("JS 加密私钥或私钥密码未正确传递")
	}
}
