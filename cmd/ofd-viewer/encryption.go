package main

import (
	"bytes"
	"context"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"unicode/utf8"

	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/pkg/crypt"
)

type unlockCredentials struct {
	method, user, path string
	password           []byte
}

func (c *unlockCredentials) clear() {
	if c != nil {
		clear(c.password)
		c.password = nil
	}
}

func openUnlockedDocument(ctx context.Context, filename string, c *unlockCredentials) (*documentModel, error) {
	return openUnlockedDocumentInput(ctx, filename, filename, c)
}

func openUnlockedDocumentInput(ctx context.Context, filename string, input any, c *unlockCredentials) (*documentModel, error) {
	defer c.clear()
	var options crypt.DecryptOptions
	options.Mode = crypt.ModeCompat
	options.Limits.MaxInputBytes = 512 << 20
	if c != nil {
		options.UserName = c.user
		data := c.password
		if c.method != "password" {
			limit := int64(64 << 10)
			if c.method == "key" {
				limit = 4 << 20
			}
			file, err := os.Open(c.path)
			if err != nil {
				return nil, err
			}
			data, err = io.ReadAll(io.LimitReader(file, limit+1))
			closeErr := file.Close()
			defer clear(data)
			if err != nil {
				return nil, err
			}
			if closeErr != nil {
				return nil, closeErr
			}
			if int64(len(data)) > limit {
				return nil, viewerError("凭据文件超过大小限制")
			}
		}
		if c.method == "key" {
			der := data
			if bytes.HasPrefix(bytes.TrimSpace(data), []byte("-----BEGIN")) {
				block, rest := pem.Decode(data)
				if block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
					return nil, viewerError("请选择未加密的 SM2 PKCS#8 私钥")
				}
				der = block.Bytes
				defer clear(der)
			}
			key, err := smx509.ParsePKCS8PrivateKey(der)
			if err != nil {
				return nil, viewerError("请选择未加密的 SM2 PKCS#8 私钥")
			}
			privateKey, ok := key.(*sm2.PrivateKey)
			if !ok {
				return nil, viewerError("请选择未加密的 SM2 PKCS#8 私钥")
			}
			options.PrivateKey = privateKey
			defer privateKey.D.SetInt64(0)
		} else {
			if len(data) > 64<<10 || !utf8.Valid(data) {
				return nil, viewerError("口令必须为 UTF-8，且不能超过 64 KiB")
			}
			options.Password = data
		}
	}
	options.Resolve = func(_ context.Context, req crypt.CredentialRequest) (crypt.Credentials, error) {
		selected := crypt.Credentials{Password: options.Password, PrivateKey: options.PrivateKey, UserName: options.UserName}
		if req.Scheme == "" {
			switch {
			case options.PrivateKey != nil:
				selected.MissingScheme = crypt.CertificateScheme
			case options.Password != nil:
				selected.MissingScheme = crypt.PasswordScheme
			default:
				return crypt.Credentials{}, crypt.ErrCredentialsRequired
			}
		}
		return selected, nil
	}
	view, err := crypt.Open(ctx, input, crypt.OpenOptions{DecryptOptions: options})
	if err != nil {
		return nil, err
	}
	model, err := openDocumentInput(filename, view) //nolint:contextcheck // 解析器打开接口不接收 context。
	if err != nil {
		_ = view.Close()
		return nil, err
	}
	model.view = view
	if data, ok := input.([]byte); ok {
		model.input = data
	}
	return model, nil
}

func needsUnlock(err error) bool {
	return errors.Is(err, crypt.ErrCredentialsRequired) || errors.Is(err, crypt.ErrDecryptFailed)
}
