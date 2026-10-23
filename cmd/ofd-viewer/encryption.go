package main

import (
	"bytes"
	"context"
	"encoding/pem"
	"errors"
	"io"
	"unicode/utf8"

	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/crypt"
)

const viewerMaxInputBytes = 512 << 20
const viewerMaxPasswordBytes = 64 << 10
const viewerMaxPrivateKeyBytes = 4 << 20

// reusableViewerInput 将一次性读取器转换为有界字节数据，供解锁重试复用。
// 输入读取器仍由加载流程统一关闭。
func reusableViewerInput(input any) (any, error) {
	reader, ok := input.(io.Reader)
	if !ok {
		return input, nil
	}
	data, err := io.ReadAll(io.LimitReader(reader, viewerMaxInputBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > viewerMaxInputBytes {
		return nil, errors.New(tr("OFD 文件超过 512 MiB"))
	}
	return data, nil
}

// openViewerOFD 将普通文档或完全解锁的逻辑视图交给解析器，不生成明文 ZIP。
// 解析器持有独立租约，文档会话关闭时释放解密资源。
func openViewerOFD(ctx context.Context, input any, credentials crypt.DecryptOptions) (*parser.OFD, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ofd, err := openOFD(input)
	if !errors.Is(err, crypt.ErrLocked) {
		return ofd, err
	}
	if ofd != nil {
		_ = ofd.Close()
	}
	credentials.Mode = crypt.ModeCompat
	credentials.Limits.MaxInputBytes = viewerMaxInputBytes
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
	view, err := crypt.Open(ctx, input, crypt.OpenOptions{DecryptOptions: credentials})
	if err != nil {
		return nil, err
	}
	defer view.Close()
	return openOFD(view)
}

func viewerNeedsCredentials(err error) bool {
	return errors.Is(err, crypt.ErrCredentialsRequired) || errors.Is(err, crypt.ErrDecryptFailed)
}

func viewerPassword(data []byte) ([]byte, error) {
	if len(data) > viewerMaxPasswordBytes || !utf8.Valid(data) {
		return nil, errors.New(tr("口令必须为 UTF-8，且不能超过 64 KiB"))
	}
	password := make([]byte, len(data))
	copy(password, data)
	return password, nil
}

func viewerPrivateKey(data []byte) (*sm2.PrivateKey, error) {
	if len(data) == 0 || len(data) > viewerMaxPrivateKeyBytes {
		return nil, errors.New(tr("私钥文件为空或超过 4 MiB"))
	}
	der := data
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("-----BEGIN")) {
		block, rest := pem.Decode(data)
		if block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
			return nil, errors.New(tr("请选择未加密的 SM2 PKCS#8 私钥（PEM 或 DER）"))
		}
		der = block.Bytes
		defer clear(der)
	}
	key, err := smx509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, errors.New(tr("请选择未加密的 SM2 PKCS#8 私钥（PEM 或 DER）"))
	}
	sm2Key, ok := key.(*sm2.PrivateKey)
	if !ok {
		return nil, errors.New(tr("请选择未加密的 SM2 PKCS#8 私钥（PEM 或 DER）"))
	}
	return sm2Key, nil
}
