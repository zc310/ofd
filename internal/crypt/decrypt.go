package crypt

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/pkg/spec"
)

func NormalizeDecryptOptions(opt DecryptOptions) (DecryptOptions, error) {
	normalized, err := NormalizeOptions(InspectOptions{Mode: opt.Mode, Limits: opt.Limits})
	if err != nil {
		return opt, err
	}
	opt.Mode, opt.Limits = normalized.Mode, normalized.Limits
	if opt.Password != nil && !utf8.Valid(opt.Password) {
		return opt, fmt.Errorf("口令必须是有效 UTF-8 字节")
	}
	if opt.MissingScheme != "" && (opt.Mode != ModeCompat || (opt.MissingScheme != PasswordScheme && opt.MissingScheme != CertificateScheme)) {
		return opt, fmt.Errorf("MissingScheme 只允许在 compat 模式下指定 1.1.1 或 1.1.2")
	}
	return opt, nil
}

// DecryptPackage 输出条目的原始字节，不重新序列化文档 XML。
// 输出回调的错误原样返回；发生任何错误时都必须丢弃输出。
func DecryptPackage(ctx context.Context, pkg *core.Package, opt DecryptOptions, emit func(string, []byte) error) (*DecryptReport, error) {
	var err error
	opt, err = NormalizeDecryptOptions(opt)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !Present(pkg) {
		return nil, ErrNotEncrypted
	}
	view, err := OpenView(ctx, pkg, OpenOptions{DecryptOptions: opt}, nil)
	if err != nil {
		return nil, err
	}
	defer view.Close()
	if !view.info.Encrypted {
		return nil, ErrNotEncrypted
	}
	return view.Export(ctx, emit)
}

func (s *inspector) checkInputBudget() error {
	remaining := s.opt.Limits.MaxTotalBytes
	for name, entry := range s.files {
		if entry.IsDir {
			if entry.UncompressedSize != 0 {
				return invalid(name, "目录条目包含数据")
			}
			continue
		}
		if entry.UncompressedSize > uint64(s.opt.Limits.MaxEntryBytes) || entry.UncompressedSize > uint64(remaining) {
			return limitError(name, "ZIP 解压后规模超过上限")
		}
		remaining -= int64(entry.UncompressedSize)
	}
	return nil
}

func (s *inspector) unlock(op *Operation, opt DecryptOptions) ([]byte, []byte, Recipient, string, error) {
	scheme := op.Seed.Scheme
	if scheme != "" && opt.MissingScheme != "" && opt.MissingScheme != scheme {
		return nil, nil, Recipient{}, "", fmt.Errorf("%w: MissingScheme 与已声明方案不一致", ErrUnsupported)
	}
	if scheme == "" {
		if opt.MissingScheme == "" {
			return nil, nil, Recipient{}, "", fmt.Errorf("%w: 缺少 EncryptCaseId；需显式指定 MissingScheme", ErrUnsupported)
		}
		scheme = opt.MissingScheme
		s.diag("encryption.scheme_selected", op.DecryptSeedLoc, "使用调用方指定的缺省方案: "+scheme)
	}
	if scheme != PasswordScheme && scheme != CertificateScheme {
		return nil, nil, Recipient{}, "", fmt.Errorf("%w: 加密方案 %s", ErrUnsupported, scheme)
	}
	var kek []byte
	var pub *ecdsa.PublicKey
	if scheme == PasswordScheme {
		if opt.Password == nil {
			return nil, nil, Recipient{}, "", ErrCredentialsRequired
		}
		kek = passwordKEK(opt.Password)
		defer clear(kek)
	} else {
		if opt.PrivateKey == nil {
			return nil, nil, Recipient{}, "", ErrCredentialsRequired
		}
		var err error
		pub, err = sm2Public(opt.PrivateKey)
		if err != nil {
			return nil, nil, Recipient{}, "", err
		}
	}
	attempts := 0
	if s.attempts == nil {
		s.attempts = &attempts
	}
	var unsupported error
	for i, user := range s.rawSeeds[op.DecryptSeedLoc].Users {
		if opt.UserName != "" && opt.UserName != user.Name {
			continue
		}
		if err := s.ctx.Err(); err != nil {
			return nil, nil, Recipient{}, "", err
		}
		*s.attempts++
		if *s.attempts > opt.Limits.MaxCredentialAttempts {
			return nil, nil, Recipient{}, "", limitError(op.DecryptSeedLoc, "凭据尝试次数超过上限；请指定 UserName")
		}
		var fek, iv []byte
		var err error
		raw := false
		if scheme == PasswordScheme {
			fek, iv, err = unwrapPassword(s.ctx, user, kek)
		} else {
			fek, iv, raw, err = unwrapCertificate(s.ctx, user, opt.PrivateKey, pub, opt.Mode)
		}
		if err != nil {
			if errors.Is(err, ErrUnsupported) {
				unsupported = err
				continue
			}
			if errors.Is(err, ErrDecryptFailed) {
				continue
			}
			return nil, nil, Recipient{}, "", err
		}
		if op.MapState == "encrypted" {
			plain := bytes.Clone(s.mapData[op.EntriesMapLoc])
			decoded, decodeErr := decryptCBC(s.ctx, plain, fek, iv)
			if decodeErr == nil {
				decodeErr = s.parseEntries(op, decoded)
			}
			clear(plain)
			if decodeErr != nil {
				clear(fek)
				clear(iv)
				if errors.Is(decodeErr, ErrDecryptFailed) {
					continue
				}
				return nil, nil, Recipient{}, "", decodeErr
			}
		}
		if raw {
			s.diag("encryption.sm2_raw", op.DecryptSeedLoc, "兼容读取裸 C1C3C2 包装密钥")
		}
		return fek, iv, op.Seed.Recipients[i], scheme, nil
	}
	if unsupported != nil {
		return nil, nil, Recipient{}, "", unsupported
	}
	return nil, nil, Recipient{}, "", ErrDecryptFailed
}

func (s *inspector) plan(op *Operation) ([]string, map[string]bool, map[string]string, error) {
	removed := map[string]bool{s.info.EntryPoint: true, op.DecryptSeedLoc: true, op.EntriesMapLoc: true}
	restored := map[string]string{}
	for _, entry := range op.Entries {
		if entry.DecryptSeedLoc != "" {
			removed[entry.DecryptSeedLoc] = true
		}
		if entry.EPath == "" {
			return nil, nil, nil, fmt.Errorf("%w: 缺少 EPath，无法确定密文位置", ErrUnsupported)
		}
		if removed[entry.EPath] {
			return nil, nil, nil, invalid(entry.EPath, "密文与解密控制文件重叠")
		}
		restored[entry.Path] = entry.EPath
	}
	for _, cipher := range restored {
		removed[cipher] = true
	}
	// 条目之间的路径交换和大小写别名可能覆盖其他源条目、遮蔽入口，
	// 或使不同读取器选择不同的原始条目，因此必须检查。
	lower := map[string]string{}
	outputs := map[string]bool{}
	for name, entry := range s.files {
		lower[strings.ToLower(name)] = name
		if !entry.IsDir && !removed[name] {
			outputs[name] = true
		}
	}
	for name, cipher := range restored {
		lowerName := strings.ToLower(name)
		// ofdrw 2.4.0 可能将 SignedValue.dat 映射为 signedvalue.dat。
		// 若大小写不同的物理名称正是当前映射指定的密文，则不存在歧义；
		// 恢复原名之前会移除该密文条目。
		if prior, ok := lower[lowerName]; ok && prior != name && prior != cipher {
			return nil, nil, nil, invalid(name, "恢复路径与其他条目存在大小写歧义")
		}
		lower[lowerName] = name
		if physical, ok := s.files[name]; ok && physical.IsDir {
			return nil, nil, nil, invalid(name, "恢复路径与目录冲突")
		}
		if removed[name] && cipher != name {
			return nil, nil, nil, invalid(name, "恢复路径与其他密文或控制文件重叠")
		}
		outputs[name] = true
	}
	if !outputs[spec.RootDocument] && !s.allowLayers {
		return nil, nil, nil, invalid(spec.RootDocument, "恢复后仍缺少主入口")
	}
	if len(outputs) > s.opt.Limits.MaxEntries {
		return nil, nil, nil, limitError("", "输出条目数超过上限")
	}
	names := make([]string, 0, len(outputs))
	for name := range outputs {
		// 文件路径不能同时作为另一个输出文件的父目录。
		for index := strings.IndexByte(name, '/'); index >= 0; {
			if outputs[name[:index]] {
				return nil, nil, nil, invalid(name, "恢复路径存在文件/目录冲突")
			}
			next := strings.IndexByte(name[index+1:], '/')
			if next < 0 {
				break
			}
			index += next + 1
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, removed, restored, nil
}
