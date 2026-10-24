// Package crypt 提供 GM/T 0099-2020 OFD 检查、加密和视图功能。
package crypt

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/zc310/ofd/internal/container"
	protocol "github.com/zc310/ofd/internal/crypt"
)

type Info = protocol.Info
type Operation = protocol.Operation
type Provider = protocol.Provider
type SeedInfo = protocol.SeedInfo
type Recipient = protocol.Recipient
type Mapping = protocol.Mapping
type Diagnostic = protocol.Diagnostic
type Mode = protocol.Mode
type Limits = protocol.Limits
type InspectOptions = protocol.InspectOptions
type LockedError = protocol.LockedError
type DecryptOptions = protocol.DecryptOptions
type DecryptReport = protocol.DecryptReport
type EncryptOptions = protocol.EncryptOptions
type EncryptRecipient = protocol.EncryptRecipient
type EncryptReport = protocol.EncryptReport
type PageSelection = protocol.PageSelection
type OpenOptions = protocol.OpenOptions
type View = protocol.View
type Credentials = protocol.Credentials
type CredentialRequest = protocol.CredentialRequest
type CredentialResolver = protocol.CredentialResolver
type OperationResult = protocol.OperationResult
type SeedResult = protocol.SeedResult
type Entry = container.Entry

const (
	ModeStrict        = protocol.ModeStrict
	ModeCompat        = protocol.ModeCompat
	PasswordScheme    = protocol.PasswordScheme
	CertificateScheme = protocol.CertificateScheme
)

var (
	ErrClosed              = container.ErrPackageClosed
	ErrLocked              = protocol.ErrLocked
	ErrInvalidMetadata     = protocol.ErrInvalidMetadata
	ErrLimitExceeded       = protocol.ErrLimitExceeded
	ErrNotEncrypted        = protocol.ErrNotEncrypted
	ErrAlreadyEncrypted    = protocol.ErrAlreadyEncrypted
	ErrUnsupported         = protocol.ErrUnsupported
	ErrCredentialsRequired = protocol.ErrCredentialsRequired
	ErrDecryptFailed       = protocol.ErrDecryptFailed
	ErrProtectedContent    = protocol.ErrProtectedContent
)

// Inspect 在不提供凭据时读取可见的加密元数据。输入可以是文件名、[]byte 或 io.Reader；
// 不会关闭调用方传入的读取器。默认模式为 strict；限制为零值时使用有界默认值，不会无限制。
// 出错时，非 nil 的 Info 只是部分结果，不得据此处理文档内容。State=none 表示未发现根加密标记，
// 并不表示输入是有效 OFD。加密映射表是不透明的，因此受保护路径仍然未知。
func Inspect(ctx context.Context, input any, opt InspectOptions) (*Info, error) {
	opt, err := protocol.NormalizeOptions(opt)
	if err != nil {
		return nil, err
	}
	pkg, closeInput, err := openInput(ctx, input, opt.Limits.MaxInputBytes)
	if err != nil {
		return nil, err
	}
	defer closeInput()
	return protocol.InspectPackage(ctx, pkg, opt)
}

func openInput(ctx context.Context, input any, maximum int64) (*container.Package, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	var pkg *container.Package
	var err error
	closeFile := func() {}
	switch value := input.(type) {
	case container.PackageSource:
		if source, ok := input.(interface{ InputSize() int64 }); ok && source.InputSize() > maximum {
			return nil, nil, fmt.Errorf("%w: 原始输入超过大小上限", ErrLimitExceeded)
		}
		pkg, err = value.OFDPackage()
	case string:
		file, openErr := os.Open(value)
		if openErr != nil {
			return nil, nil, openErr
		}
		closeFile = func() { _ = file.Close() }
		stat, statErr := file.Stat()
		if statErr != nil {
			closeFile()
			return nil, nil, statErr
		}
		if stat.Size() > maximum {
			closeFile()
			return nil, nil, fmt.Errorf("%w: 输入超过大小上限", ErrLimitExceeded)
		}
		pkg, err = container.OpenReaderAt(file, stat.Size())
	case []byte:
		if int64(len(value)) > maximum {
			return nil, nil, fmt.Errorf("%w: 输入超过大小上限", ErrLimitExceeded)
		}
		pkg, err = container.OpenBytes(value)
	case io.Reader:
		// 将额外探测的 1 字节长度限制在 MaxInt64 以内，避免整数溢出。
		probe := maximum
		if probe < 1<<63-1 {
			probe++
		}
		data, readErr := io.ReadAll(io.LimitReader(inputReader{ctx, value}, probe))
		if readErr != nil {
			return nil, nil, readErr
		}
		if int64(len(data)) > maximum {
			return nil, nil, fmt.Errorf("%w: 输入超过大小上限", ErrLimitExceeded)
		}
		pkg, err = container.OpenBytes(data)
	default:
		return nil, nil, fmt.Errorf("不支持的 OFD 输入类型 %T", input)
	}
	if err != nil {
		closeFile()
		return nil, nil, err
	}
	return pkg, func() { _ = pkg.Close(); closeFile() }, nil
}

type inputReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r inputReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
