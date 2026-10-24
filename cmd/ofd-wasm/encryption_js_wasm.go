//go:build js && wasm

package main

import (
	"fmt"
	"syscall/js"

	"github.com/zc310/ofd/pkg/crypt"
)

func decryptOptions(args []js.Value) (options crypt.DecryptOptions, err error) {
	defer func() {
		if err != nil {
			clear(options.Password)
		}
	}()
	if len(args) == 0 || args[0].IsUndefined() || args[0].IsNull() {
		return options, nil
	}
	value := args[0]
	if password := value.Get("password"); !password.IsUndefined() && !password.IsNull() {
		if password.Type() != js.TypeString {
			return options, fmt.Errorf("%w: password 必须是字符串", errInvalidCredentials)
		}
		// syscall/js 的字符串读取会移除开头的 BOM；显式编码保留口令原字节。
		encoded := js.Global().Get("TextEncoder").New().Call("encode", password)
		defer encoded.Call("fill", 0)
		length := encoded.Get("byteLength").Int()
		if length > 64<<10 {
			return options, fmt.Errorf("%w: 口令超过 64 KiB", errInvalidCredentials)
		}
		options.Password = make([]byte, length)
		if js.CopyBytesToGo(options.Password, encoded) != length {
			return options, fmt.Errorf("%w: 无法读取口令", errInvalidCredentials)
		}
	}
	if user := value.Get("userName"); !user.IsUndefined() && !user.IsNull() {
		if user.Type() != js.TypeString {
			return options, fmt.Errorf("%w: userName 必须是字符串", errInvalidCredentials)
		}
		options.UserName = user.String()
	}
	if key := value.Get("privateKey"); !key.IsUndefined() && !key.IsNull() {
		if key.Type() != js.TypeObject || key.Get("byteLength").Type() != js.TypeNumber || key.Get("byteLength").Float() > wasmMaxCredentialBytes {
			return options, fmt.Errorf("%w: privateKey 必须是最多 4 MiB 的 Uint8Array 或 ArrayBuffer", errInvalidCredentials)
		}
		data, readErr := bytesFromJS(key)
		if readErr != nil {
			return options, fmt.Errorf("%w: 无法读取私钥数据", errInvalidCredentials)
		}
		defer clear(data)
		var keyPassword []byte
		if password := value.Get("privateKeyPassword"); !password.IsUndefined() && !password.IsNull() {
			if password.Type() != js.TypeString {
				return options, fmt.Errorf("%w: privateKeyPassword 必须是字符串", errInvalidCredentials)
			}
			encoded := js.Global().Get("TextEncoder").New().Call("encode", password)
			defer encoded.Call("fill", 0)
			length := encoded.Get("byteLength").Int()
			if length > 64<<10 {
				return options, fmt.Errorf("%w: 私钥密码超过 64 KiB", errInvalidCredentials)
			}
			keyPassword = make([]byte, length)
			if js.CopyBytesToGo(keyPassword, encoded) != length {
				clear(keyPassword)
				return options, fmt.Errorf("%w: 无法读取私钥密码", errInvalidCredentials)
			}
			defer clear(keyPassword)
		}
		options.PrivateKey, err = parseWASMPrivateKey(data, keyPassword)
		if err != nil {
			return options, err
		}
	}
	return options, nil
}
