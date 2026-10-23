package main

import (
	"bytes"
	"context"
	"encoding/pem"
	"errors"
	"fmt"

	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/pkg/crypt"
	"github.com/zc310/ofd/pkg/webreader"
)

const wasmMaxInputBytes = 256 << 20
const wasmMaxCredentialBytes = 4 << 20

var errInvalidCredentials = errors.New("凭据参数无效")

// openWASMDocument 复用只读解密视图；Reader 持有独立租约，关闭时释放解密资源。
func openWASMDocument(ctx context.Context, data []byte, options webreader.OpenOptions, credentials crypt.DecryptOptions) (*webreader.Reader, error) {
	reader, err := webreader.OpenWithOptions(data, options)
	if !errors.Is(err, crypt.ErrLocked) {
		return reader, err
	}
	credentials.Limits.MaxInputBytes = wasmMaxInputBytes
	credentials.Mode = crypt.ModeCompat
	// ofdrw 可能省略方案标识；仅在缺失时使用用户选择的凭据类型。
	credentials.Resolve = func(_ context.Context, request crypt.CredentialRequest) (crypt.Credentials, error) {
		selected := crypt.Credentials{Password: credentials.Password, PrivateKey: credentials.PrivateKey, UserName: credentials.UserName}
		if request.Scheme == "" {
			switch {
			case credentials.PrivateKey != nil:
				selected.MissingScheme = crypt.CertificateScheme
			case credentials.Password != nil:
				selected.MissingScheme = crypt.PasswordScheme
			default:
				return crypt.Credentials{}, crypt.ErrCredentialsRequired
			}
		}
		return selected, nil
	}
	view, err := crypt.Open(ctx, data, crypt.OpenOptions{DecryptOptions: credentials})
	if err != nil {
		return nil, err
	}
	defer view.Close()
	return webreader.OpenInput(view, options)
}

func parseWASMPrivateKey(data []byte) (*sm2.PrivateKey, error) {
	der := data
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("-----BEGIN")) {
		block, rest := pem.Decode(data)
		if block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
			return nil, fmt.Errorf("%w: 需要单个未加密的 PRIVATE KEY PEM 块或 PKCS#8 DER", errInvalidCredentials)
		}
		der = block.Bytes
		defer clear(der)
	}
	key, err := smx509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("%w: 无法解析 SM2 PKCS#8 私钥", errInvalidCredentials)
	}
	sm2Key, ok := key.(*sm2.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%w: 私钥不是 SM2 类型", errInvalidCredentials)
	}
	return sm2Key, nil
}

// wasmErrorCode 将错误类型传递到 Worker 和页面，不依赖错误文案判断是否允许重试。
func wasmErrorCode(err error) string {
	switch {
	case errors.Is(err, errInvalidCredentials):
		return "invalid_credentials"
	case errors.Is(err, crypt.ErrLimitExceeded):
		return "limit_exceeded"
	case errors.Is(err, crypt.ErrInvalidMetadata):
		return "invalid_encryption"
	case errors.Is(err, crypt.ErrUnsupported):
		return "unsupported_encryption"
	case errors.Is(err, crypt.ErrDecryptFailed):
		return "decrypt_failed"
	case errors.Is(err, crypt.ErrCredentialsRequired), errors.Is(err, crypt.ErrLocked):
		return "credentials_required"
	default:
		return ""
	}
}
