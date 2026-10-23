// Package crypt provides GM/T 0099-2020 OFD inspection, encryption and views.
package crypt

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/zc310/ofd/internal/core"
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
type Entry = core.Entry

const (
	ModeStrict        = protocol.ModeStrict
	ModeCompat        = protocol.ModeCompat
	PasswordScheme    = protocol.PasswordScheme
	CertificateScheme = protocol.CertificateScheme
)

var (
	ErrClosed              = core.ErrPackageClosed
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

// Inspect reads visible encryption metadata without credentials. Input accepts
// a filename, []byte, or io.Reader; caller-owned readers are never closed.
// The default mode is strict. Zero limits use bounded defaults, never unlimited.
// On error a non-nil Info is partial and must not authorize content processing.
// State=none means no root encryption marker was found, not that the input is a
// valid OFD. Encrypted maps are opaque: protected paths remain unknown.
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

func openInput(ctx context.Context, input any, maximum int64) (*core.Package, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	var pkg *core.Package
	var err error
	closeFile := func() {}
	switch value := input.(type) {
	case core.PackageSource:
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
		pkg, err = core.OpenReaderAt(file, stat.Size())
	case []byte:
		if int64(len(value)) > maximum {
			return nil, nil, fmt.Errorf("%w: 输入超过大小上限", ErrLimitExceeded)
		}
		pkg, err = core.OpenBytes(value)
	case io.Reader:
		// Saturate the one-byte probe at MaxInt64.
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
		pkg, err = core.OpenBytes(data)
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
