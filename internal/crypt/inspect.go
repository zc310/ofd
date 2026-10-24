package crypt

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/zc310/ofd/internal/container"
)

type inspector struct {
	ctx            context.Context
	opt            InspectOptions
	pkg            *container.Package
	info           *Info
	files          map[string]container.Entry
	remaining      int64
	seeds          map[string]SeedInfo
	mappings       int
	recipients     int
	decrypting     bool
	rawSeeds       map[string]seedXML
	mapData        map[string][]byte
	inputRemaining int64
	inputSeen      map[string]bool
	descriptorOnly bool
	allowLayers    bool
	attempts       *int
}

// Present 识别包根目录的加密标记，即使大小写歧义导致 container.Package.Lookup 无法选择条目。
// 后续检查会报告该歧义。
func Present(pkg *container.Package) bool {
	present := false
	_ = pkg.WalkEntries(func(entry container.Entry) bool {
		present = strings.EqualFold(entry.Path, EntryPoint)
		return !present
	})
	return present
}

func NormalizeOptions(opt InspectOptions) (InspectOptions, error) {
	if opt.Mode == "" {
		opt.Mode = ModeStrict
	}
	if opt.Mode != ModeStrict && opt.Mode != ModeCompat {
		return opt, fmt.Errorf("未知加密元数据读取模式 %q", opt.Mode)
	}
	for _, item := range []struct {
		value    *int64
		fallback int64
	}{
		{&opt.Limits.MaxInputBytes, 512 << 20},
		{&opt.Limits.MaxMetadataBytes, 4 << 20},
		{&opt.Limits.MaxTotalMetadataBytes, 32 << 20},
		{&opt.Limits.MaxEntryBytes, 64 << 20},
		{&opt.Limits.MaxTotalBytes, 512 << 20},
		{&opt.Limits.MaxProcessedBytes, 2 << 30},
	} {
		if *item.value < 0 {
			return opt, fmt.Errorf("加密读取限制不能为负数")
		}
		if *item.value == 0 {
			*item.value = item.fallback
		}
	}
	for _, item := range []struct {
		value    *int
		fallback int
	}{
		{&opt.Limits.MaxEntries, 10000}, {&opt.Limits.MaxXMLNodes, 100000},
		{&opt.Limits.MaxXMLDepth, 64}, {&opt.Limits.MaxOperations, 64},
		{&opt.Limits.MaxRecipients, 1024}, {&opt.Limits.MaxMappings, 10000},
		{&opt.Limits.MaxCredentialAttempts, 32},
		{&opt.Limits.MaxLayers, 32},
	} {
		if *item.value < 0 {
			return opt, fmt.Errorf("加密读取限制不能为负数")
		}
		if *item.value == 0 {
			*item.value = item.fallback
		}
	}
	return opt, nil
}

// InspectPackage 借用 pkg。发生错误时，非 nil 的 Info 仅包含失败前已成功检查的元数据，
// 不得据此允许后续处理。
func InspectPackage(ctx context.Context, pkg *container.Package, opt InspectOptions) (*Info, error) {
	return inspectWith(ctx, pkg, opt, &inspector{})
}

func inspectWith(ctx context.Context, pkg *container.Package, opt InspectOptions, s *inspector) (*Info, error) {
	opt, err := NormalizeOptions(opt)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info := &Info{State: "none"}
	*s = inspector{ctx: ctx, opt: opt, pkg: pkg, info: info, files: map[string]container.Entry{}, remaining: opt.Limits.MaxTotalMetadataBytes, seeds: map[string]SeedInfo{}, decrypting: s.decrypting, descriptorOnly: s.descriptorOnly, allowLayers: s.allowLayers, rawSeeds: map[string]seedXML{}, mapData: map[string][]byte{}, inputRemaining: opt.Limits.MaxTotalBytes, inputSeen: map[string]bool{}}
	// 仅检查包根目录的加密标记，同名附件不作为加密入口。
	var entries []container.Entry
	err = pkg.WalkEntries(func(entry container.Entry) bool {
		entries = append(entries, entry)
		if strings.EqualFold(entry.Path, EntryPoint) {
			info.Encrypted, info.State, info.EntryPoint = true, "locked", entry.Path
		}
		return len(entries) <= opt.Limits.MaxEntries && ctx.Err() == nil
	})
	if err != nil {
		return info, err
	}
	if err := ctx.Err(); err != nil {
		return info, err
	}
	if len(entries) > opt.Limits.MaxEntries {
		return info, limitError("", "ZIP 条目数超过上限")
	}
	if !info.Encrypted {
		return info, nil
	}
	if err := s.indexEntries(entries); err != nil {
		return info, err
	}
	if info.EntryPoint != EntryPoint {
		if opt.Mode == ModeStrict {
			return info, invalid(info.EntryPoint, "解密入口文件名应为 Encryptions.xml")
		}
		s.diag("encryption.entry_point_case", info.EntryPoint, "兼容读取解密入口文件名的大小写变体")
	}
	data, err := s.read(info.EntryPoint)
	if err != nil {
		return info, err
	}
	var root encryptionXML
	if err := s.decodeXML(data, info.EntryPoint, []string{"Encryptions"}, &root); err != nil {
		return info, err
	}
	if len(root.Operations) == 0 {
		return info, invalid(info.EntryPoint, "缺少 EncryptInfo")
	}
	if len(root.Operations) > opt.Limits.MaxOperations {
		return info, limitError(info.EntryPoint, "加密操作数超过上限")
	}
	ids := map[string]string{}
	for _, raw := range root.Operations {
		if !validID(raw.ID) {
			if !decimalID(raw.ID) || opt.Mode == ModeStrict {
				return info, invalid(info.EntryPoint, "加密操作 ID 不是合法 xs:ID: %q", raw.ID)
			}
			s.diag("encryption.id_nonconforming", info.EntryPoint, "兼容读取非 xs:ID 的加密操作标识: "+raw.ID)
		}
		if _, exists := ids[raw.ID]; exists {
			return info, invalid(info.EntryPoint, "加密操作 ID 重复: %s", raw.ID)
		}
		ids[raw.ID] = raw.Relative
		op := Operation{ID: raw.ID, Relative: raw.Relative, Scope: strings.TrimSpace(raw.Scope), EncryptDate: raw.EncryptDate, MapState: "unknown", Provider: Provider{Name: raw.Provider.Name, Version: raw.Provider.Version, Company: raw.Provider.Company}}
		if op.Provider.Name == "" || op.Provider.Version == "" || op.Scope == "" {
			return info, invalid(info.EntryPoint, "缺少 Provider.Name/Version 或 EncryptScope")
		}
		if op.DecryptSeedLoc, err = packagePath(info.EntryPoint, strings.TrimSpace(raw.DecryptSeedLoc), false); err != nil {
			return info, invalid(info.EntryPoint, "%v", err)
		}
		if op.EntriesMapLoc, err = packagePath(info.EntryPoint, strings.TrimSpace(raw.EntriesMapLoc), false); err != nil {
			return info, invalid(info.EntryPoint, "%v", err)
		}
		info.Operations = append(info.Operations, op)
	}
	// Relative 定义操作依赖，XML 顺序不决定依赖关系。遍历次数以操作数量为界，
	// 在解密前就拒绝循环和悬空引用。
	for _, operation := range info.Operations {
		seen := map[string]bool{}
		for current := operation.ID; current != ""; current = ids[current] {
			if _, ok := ids[current]; !ok {
				return info, invalid(info.EntryPoint, "Relative 引用不存在的操作: %s", current)
			}
			if seen[current] {
				return info, invalid(info.EntryPoint, "Relative 存在循环")
			}
			seen[current] = true
			if len(seen) > opt.Limits.MaxLayers {
				return info, limitError(info.EntryPoint, "加密依赖深度超过上限")
			}
		}
	}
	previous := map[string]bool{}
	for _, operation := range info.Operations {
		if operation.Relative != "" {
			previous[operation.Relative] = true
		}
	}
	if s.descriptorOnly {
		return info, nil
	}
	for i := range info.Operations {
		op := &info.Operations[i]
		// 较早加密层的控制文件本身可能已被外层加密。
		// P0 报告该依赖关系，不将其误报为文件缺失。
		if previous[op.ID] {
			op.Deferred = true
			s.diag("encryption.layer_deferred", info.EntryPoint, "操作 "+op.ID+" 的元数据需在后续加密层解锁后检查")
			continue
		}
		op.Seed, err = s.seed(op.DecryptSeedLoc, op.ID)
		if err != nil {
			return info, err
		}
		if err := s.entries(op); err != nil {
			return info, err
		}
	}
	return info, ctx.Err()
}

func (s *inspector) indexEntries(entries []container.Entry) error {
	seenLower := map[string]bool{}
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name, "/")
		if _, err := packagePath("", name, false); err != nil || strings.HasPrefix(name, "/") {
			return invalid(entry.Name, "ZIP 条目路径无效")
		}
		lower := strings.ToLower(name)
		if seenLower[lower] {
			return invalid(name, "ZIP 条目重复或存在大小写歧义")
		}
		seenLower[lower] = true
		s.files[name] = entry
	}
	return nil
}

func (s *inspector) read(name string) ([]byte, error) {
	data, err := s.readEntry(name, min(s.opt.Limits.MaxMetadataBytes, s.remaining))
	if err == nil {
		s.remaining -= int64(len(data))
	}
	return data, err
}

func (s *inspector) readEntry(name string, maximum int64) ([]byte, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	entry, ok := s.files[name]
	if !ok || entry.IsDir {
		return nil, invalid(name, "加密描述引用的文件不存在或为目录")
	}
	if s.decrypting && !s.inputSeen[name] {
		maximum = min(maximum, s.inputRemaining)
	}
	if entry.UncompressedSize > uint64(maximum) {
		return nil, limitError(name, "条目超过读取大小上限")
	}
	r, err := s.pkg.OpenEntry(entry)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: 打开失败: %w", ErrInvalidMetadata, name, err)
	}
	probe := maximum
	if probe < 1<<63-1 {
		probe++
	}
	data, readErr := io.ReadAll(io.LimitReader(contextReader{s.ctx, r}, probe))
	closeErr := r.Close()
	if err := s.ctx.Err(); err != nil {
		clear(data)
		return nil, err
	}
	if readErr != nil {
		clear(data)
		return nil, fmt.Errorf("%w: %s: 读取失败: %w", ErrInvalidMetadata, name, readErr)
	}
	if closeErr != nil {
		clear(data)
		return nil, fmt.Errorf("%w: %s: 关闭失败: %w", ErrInvalidMetadata, name, closeErr)
	}
	if int64(len(data)) > maximum {
		clear(data)
		return nil, limitError(name, "条目超过读取大小上限")
	}
	if s.decrypting && !s.inputSeen[name] {
		s.inputRemaining -= int64(len(data))
		s.inputSeen[name] = true
	}
	return data, nil
}

func (s *inspector) seed(name, id string) (SeedInfo, error) {
	if seed, ok := s.seeds[name]; ok {
		if seed.ID != id {
			return SeedInfo{}, invalid(name, "密钥描述 ID 与加密操作不一致")
		}
		return seed, nil
	}
	data, err := s.read(name)
	if err != nil {
		return SeedInfo{}, err
	}
	var raw seedXML
	if err := s.decodeXML(data, name, []string{"DecyptSeed"}, &raw); err != nil {
		return SeedInfo{}, err
	}
	seed := SeedInfo{ID: raw.ID, Scheme: raw.Scheme, Recipients: []Recipient{}}
	if raw.ID != id {
		return seed, invalid(name, "密钥描述 ID 与加密操作不一致")
	}
	if raw.Scheme == "" {
		if s.opt.Mode == ModeStrict {
			return seed, invalid(name, "缺少 EncryptCaseId")
		}
		s.diag("encryption.scheme_missing", name, "缺少 EncryptCaseId，未推断加密方案")
	} else if raw.Scheme != PasswordScheme && raw.Scheme != CertificateScheme {
		s.diag("encryption.scheme_unsupported", name, "尚不支持的加密方案: "+raw.Scheme)
	}
	if raw.ExtendParams == nil {
		if s.opt.Mode == ModeStrict {
			return seed, invalid(name, "缺少 ExtendParams")
		}
		s.diag("encryption.extend_params_missing", name, "密钥描述缺少 ExtendParams")
	}
	if len(raw.Users) == 0 {
		return seed, invalid(name, "密钥描述缺少 UserInfo")
	}
	s.recipients += len(raw.Users)
	if s.recipients > s.opt.Limits.MaxRecipients {
		return seed, limitError(name, "接收者总数超过上限")
	}
	for _, user := range raw.Users {
		if user.Name == "" {
			return seed, invalid(name, "缺少 UserName")
		}
		item := Recipient{UserName: user.Name, UserType: user.Type, IVDefaulted: user.IV == nil}
		if item.UserType == "" {
			item.UserType = "User"
		}
		wk, err := decodeBase64(user.WrappedKey)
		if err != nil || len(wk) == 0 {
			return seed, invalid(name, "EncryptedWK 为空或不是有效 Base64")
		}
		if raw.Scheme == PasswordScheme && len(wk) != 32 {
			return seed, invalid(name, "口令方案包装密钥应为 32 字节（16 字节 FEK 加 PKCS#7 填充）")
		}
		if user.IV != nil {
			iv, err := decodeBase64(*user.IV)
			if err != nil || len(iv) != 16 {
				return seed, invalid(name, "IVValue 应为 16 字节")
			}
		}
		cert, err := decodeBase64(user.Certificate)
		if err != nil {
			return seed, invalid(name, "UserCert 不是有效 Base64")
		}
		if raw.Scheme == CertificateScheme && len(cert) == 0 {
			return seed, invalid(name, "证书方案缺少 UserCert")
		}
		if len(cert) != 0 {
			item.CertificateSHA256 = fmt.Sprintf("%x", sha256.Sum256(cert))
		}
		seed.Recipients = append(seed.Recipients, item)
	}
	s.seeds[name] = seed
	if s.decrypting {
		s.rawSeeds[name] = raw
	}
	return seed, nil
}

func (s *inspector) entries(op *Operation) error {
	data, err := s.read(op.EntriesMapLoc)
	if err != nil {
		return err
	}
	if s.decrypting {
		s.mapData[op.EntriesMapLoc] = data
	}
	// 按内容识别 XML，不依赖文件名。二进制映射表在提供凭据前保持不可读状态，
	// 不据此认定其内容正确。
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}))
	if !utf8.Valid(trimmed) || !bytes.HasPrefix(trimmed, []byte("<")) {
		if len(data) == 0 || len(data)%16 != 0 {
			return invalid(op.EntriesMapLoc, "加密映射表长度应为非零的 16 字节倍数")
		}
		op.MapState = "encrypted"
		return nil
	}
	return s.parseEntries(op, data)
}

func (s *inspector) parseEntries(op *Operation, data []byte) error {
	var raw entriesXML
	if err := s.decodeXML(data, op.EntriesMapLoc, []string{"EncryptedEntries", "EncryptEntries"}, &raw); err != nil {
		return err
	}
	if raw.ID != op.ID {
		return invalid(op.EntriesMapLoc, "映射表 ID 与加密操作不一致")
	}
	if len(raw.Entries) == 0 {
		return invalid(op.EntriesMapLoc, "映射表缺少 EncryptEntry")
	}
	s.mappings += len(raw.Entries)
	if s.mappings > s.opt.Limits.MaxMappings {
		return limitError(op.EntriesMapLoc, "映射条目总数超过上限")
	}
	op.MapState = "plaintext"
	op.Entries = nil
	paths := map[string]bool{}
	for _, entry := range raw.Entries {
		var err error
		mapping := Mapping{}
		if mapping.Path, err = packagePath("", entry.Path, true); err != nil {
			return invalid(op.EntriesMapLoc, "%v", err)
		}
		if paths[mapping.Path] {
			return invalid(op.EntriesMapLoc, "明文路径重复: %s", mapping.Path)
		}
		paths[mapping.Path] = true
		if strings.EqualFold(mapping.Path, s.info.EntryPoint) || mapping.Path == op.DecryptSeedLoc || mapping.Path == op.EntriesMapLoc {
			return invalid(op.EntriesMapLoc, "解密入口和本次操作的密钥描述/映射表不能被本次再次加密")
		}
		if entry.EPath == "" {
			s.diag("encryption.epath_missing", op.EntriesMapLoc, "EPath 缺失，标准图表要求不一致，未推断密文路径")
		} else {
			if mapping.EPath, err = packagePath("", entry.EPath, true); err != nil {
				return invalid(op.EntriesMapLoc, "%v", err)
			}
			if cipher, ok := s.files[mapping.EPath]; (!ok || cipher.IsDir) && !s.allowLayers {
				return invalid(mapping.EPath, "映射表引用的密文不存在或为目录")
			}
		}
		if entry.Seed != "" {
			if mapping.DecryptSeedLoc, err = packagePath(op.EntriesMapLoc, entry.Seed, false); err != nil {
				return invalid(op.EntriesMapLoc, "%v", err)
			}
			if !s.allowLayers {
				seed, err := s.seed(mapping.DecryptSeedLoc, op.ID)
				if err != nil {
					return err
				}
				mapping.Seed = &seed
			}
		}
		op.Entries = append(op.Entries, mapping)
	}
	return nil
}

func (s *inspector) diag(code, file, message string) {
	s.info.Diagnostics = append(s.info.Diagnostics, Diagnostic{code, file, message})
}
func invalid(file, format string, args ...any) error {
	return fmt.Errorf("%w: %s: %s", ErrInvalidMetadata, file, fmt.Sprintf(format, args...))
}
func limitError(file, message string) error {
	return fmt.Errorf("%w: %s: %s", ErrLimitExceeded, file, message)
}
func decodeBase64(value string) ([]byte, error) {
	return base64.StdEncoding.Strict().DecodeString(strings.Join(strings.Fields(value), ""))
}
func validID(value string) bool {
	for i, r := range value {
		if r == '_' || unicode.IsLetter(r) {
			continue
		}
		if i > 0 && (unicode.IsDigit(r) || r == '-' || r == '.') {
			continue
		}
		return false
	}
	return value != ""
}

func decimalID(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
