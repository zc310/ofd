package crypt

import (
	"context"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/sm4"
	"github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/version"
	"github.com/zc310/ofd/pkg/spec"
)

func NormalizeEncryptOptions(opt EncryptOptions) (EncryptOptions, error) {
	normalized, err := NormalizeOptions(InspectOptions{Limits: opt.Limits})
	if err != nil {
		return opt, err
	}
	opt.Limits = normalized.Limits
	if opt.Append != nil {
		previous := *opt.Append
		previous.Limits = opt.Limits
		previous, err = NormalizeDecryptOptions(previous)
		if err != nil {
			return opt, err
		}
		opt.Append = &previous
	}
	if opt.Pages != nil {
		if opt.Entries != nil || opt.Append != nil {
			return opt, fmt.Errorf("Pages 与 Entries、Append 互斥")
		}
		name, err := packagePath("", opt.Pages.Document, false)
		if err != nil || name != opt.Pages.Document || len(opt.Pages.IDs) == 0 {
			return opt, fmt.Errorf("页面范围需要规范 Document.xml 路径和至少一个页面 ID")
		}
		seen := map[string]bool{}
		for _, id := range opt.Pages.IDs {
			n, err := strconv.ParseUint(id, 10, 32)
			if err != nil || n == 0 || strconv.FormatUint(n, 10) != id || seen[id] {
				return opt, fmt.Errorf("页面 ID 必须是唯一的规范正整数")
			}
			seen[id] = true
		}
	}
	if len(opt.Recipients) == 0 {
		return opt, fmt.Errorf("至少需要一个加密接收者")
	}
	if len(opt.Recipients) > opt.Limits.MaxRecipients {
		return opt, limitError("", "接收者数量超过上限")
	}
	var scheme string
	var recipientBytes int64
	for _, r := range opt.Recipients {
		if r.UserName == "" || !utf8.ValidString(r.UserName) {
			return opt, fmt.Errorf("接收者名称不能为空且必须为 UTF-8")
		}
		if r.UserType != "" && r.UserType != "User" && r.UserType != "Owner" {
			return opt, fmt.Errorf("接收者角色必须为 User 或 Owner")
		}
		if (r.Password != nil) == (len(r.Certificate) != 0) {
			return opt, fmt.Errorf("每个接收者必须且只能提供口令或证书")
		}
		current := CertificateScheme
		if r.Password != nil {
			if !utf8.Valid(r.Password) {
				return opt, fmt.Errorf("口令必须是有效 UTF-8 字节")
			}
			current = PasswordScheme
		}
		if scheme != "" && scheme != current {
			return opt, fmt.Errorf("同次操作的接收者必须使用同一加密方案")
		}
		scheme = current
		if int64(len(r.Certificate)) > opt.Limits.MaxMetadataBytes || int64(len(r.UserName)) > opt.Limits.MaxMetadataBytes {
			return opt, limitError("", "接收者元数据超过上限")
		}
		recipientBytes += int64(len(r.Certificate)) + int64(len(r.UserName)) + 256
		if recipientBytes > opt.Limits.MaxMetadataBytes {
			return opt, limitError("", "接收者元数据总量超过上限")
		}
	}
	if opt.Entries != nil && len(opt.Entries) == 0 {
		return opt, fmt.Errorf("显式加密条目列表不能为空")
	}
	seen := map[string]bool{}
	for _, name := range opt.Entries {
		resolved, err := packagePath("", name, false)
		if err != nil || resolved != name {
			return opt, fmt.Errorf("加密条目必须为规范 ZIP 相对路径: %q", name)
		}
		if seen[name] {
			return opt, fmt.Errorf("加密条目重复: %s", name)
		}
		if opt.KeepRoot && name == spec.RootDocument {
			return opt, fmt.Errorf("KeepRoot 与显式加密 OFD.xml 互斥")
		}
		if opt.Append != nil && name == EntryPoint {
			return opt, fmt.Errorf("追加层必须保留明文 Encryptions.xml")
		}
		seen[name] = true
	}
	return opt, nil
}

// EncryptPackage 按附录 A、C 写入一次新加密操作，文档条目保持原始字节。
func EncryptPackage(ctx context.Context, pkg *core.Package, opt EncryptOptions, emit func(string, []byte) error) (*EncryptReport, error) {
	opt, err := NormalizeEncryptOptions(opt)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if Present(pkg) && opt.Append == nil {
		return nil, ErrAlreadyEncrypted
	}
	s := &inspector{ctx: ctx, pkg: pkg, info: &Info{}, opt: InspectOptions{Mode: ModeStrict, Limits: opt.Limits}, files: map[string]core.Entry{}, remaining: opt.Limits.MaxTotalMetadataBytes, inputRemaining: opt.Limits.MaxTotalBytes, inputSeen: map[string]bool{}, decrypting: true}
	var physical []core.Entry
	err = pkg.WalkEntries(func(entry core.Entry) bool {
		physical = append(physical, entry)
		return len(physical) <= opt.Limits.MaxEntries && ctx.Err() == nil
	})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(physical) > opt.Limits.MaxEntries {
		return nil, limitError("", "ZIP 条目数超过上限")
	}
	if err := s.indexEntries(physical); err != nil {
		return nil, err
	}
	if err := s.checkInputBudget(); err != nil {
		return nil, err
	}
	id, relative, oldRoot, err := s.prepareEncryption(opt)
	if err != nil {
		return nil, err
	}
	if opt.Pages != nil {
		opt.Entries, err = s.pageEntries(*opt.Pages)
		if err != nil {
			return nil, err
		}
	}
	selected, names, prefix, err := s.encryptionPlan(opt)
	if err != nil {
		return nil, err
	}
	fek, iv := make([]byte, 16), make([]byte, 16)
	defer clear(fek)
	defer clear(iv)
	if _, err := io.ReadFull(rand.Reader, fek); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, err
	}
	metadata, report, err := s.encryptionMetadata(opt, selected, prefix, id, relative, oldRoot, fek, iv)
	if err != nil {
		return nil, err
	}
	defer func() {
		for _, entry := range metadata {
			clear(entry.data)
		}
	}()
	protection := newProtection(s, nil, selected)
	write := func(name string, data []byte) error {
		if int64(len(data)) > opt.Limits.MaxEntryBytes || int64(len(data)) > opt.Limits.MaxTotalBytes-report.OutputBytes {
			return limitError(name, "加密输出规模超过上限")
		}
		if report.OutputEntries >= opt.Limits.MaxEntries {
			return limitError(name, "输出条目数量超过上限")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := emit(name, data); err != nil {
			return err
		}
		report.OutputEntries++
		report.OutputBytes += int64(len(data))
		return nil
	}
	for _, name := range names {
		data, err := s.readEntry(name, opt.Limits.MaxEntryBytes)
		if err != nil {
			return nil, err
		}
		err = func() error {
			defer clear(data)
			// 追加已在逻辑明文上完成保护预检；物理原路径可能是占位文件。
			if opt.Append == nil {
				if err := protection.observe(name, data); err != nil {
					if name == spec.RootDocument && errors.Is(err, ErrDecryptFailed) {
						return invalid(name, "主入口 XML 无效")
					}
					return err
				}
			}
			if target, ok := selected[name]; ok {
				key, vector := fek, iv
				if opt.IndependentKeys {
					key, vector = make([]byte, 16), make([]byte, 16)
					defer clear(key)
					defer clear(vector)
					if _, err := io.ReadFull(rand.Reader, key); err != nil {
						return err
					}
					if _, err := io.ReadFull(rand.Reader, vector); err != nil {
						return err
					}
					seedPath := independentSeedPath(target)
					seed, _, _, err := s.encryptionSeed(opt, id, seedPath, key, vector)
					if err != nil {
						return err
					}
					defer clear(seed)
					if err := write(seedPath, seed); err != nil {
						return err
					}
				}
				ciphertext, err := encryptCBC(ctx, data, key, vector)
				if err != nil {
					return err
				}
				defer clear(ciphertext)
				return write(target, ciphertext)
			}
			return write(name, data)
		}()
		if err != nil {
			return nil, err
		}
	}
	if err := protection.check(); err != nil {
		return nil, err
	}
	for _, entry := range metadata {
		if err := write(entry.name, entry.data); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return report, nil
}

func (s *inspector) encryptionPlan(opt EncryptOptions) (map[string]string, []string, string, error) {
	names := make([]string, 0, len(s.files))
	for name, entry := range s.files {
		if opt.Append != nil && name == EntryPoint {
			continue
		}
		if strings.EqualFold(strings.SplitN(name, "/", 2)[0], EntryPoint) {
			return nil, nil, "", invalid(name, "既有路径与新解密入口冲突")
		}
		if strings.EqualFold(name, spec.IntegrityEntryPoint) {
			return nil, nil, "", fmt.Errorf("%w: 加密前需处理现有 OFDEntries.xml", ErrProtectedContent)
		}
		if !entry.IsDir {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		parts := strings.Split(name, "/")
		for i := 1; i < len(parts); i++ {
			if parent, ok := s.files[strings.Join(parts[:i], "/")]; ok && !parent.IsDir {
				return nil, nil, "", invalid(name, "文件/目录路径冲突")
			}
		}
	}
	var prefix string
	for i := 1; i <= len(s.files)+1; i++ {
		candidate := fmt.Sprintf("Encrypt_%d", i)
		used := false
		for name := range s.files {
			if strings.EqualFold(name, candidate) || strings.HasPrefix(strings.ToLower(name), strings.ToLower(candidate)+"/") {
				used = true
				break
			}
		}
		if !used {
			prefix = candidate
			break
		}
	}
	selection := opt.Entries
	if selection == nil {
		selection = make([]string, 0, len(names))
		for _, name := range names {
			if !opt.KeepRoot || name != spec.RootDocument {
				selection = append(selection, name)
			}
		}
	}
	if len(selection) == 0 {
		return nil, nil, "", fmt.Errorf("没有可加密的文件")
	}
	if len(selection) > opt.Limits.MaxMappings-s.mappings {
		return nil, nil, "", limitError("", "加密映射数量超过上限")
	}
	if opt.IndependentKeys && len(selection)+1 > (opt.Limits.MaxRecipients-s.recipients)/len(opt.Recipients) {
		return nil, nil, "", limitError("", "独立 seed 的接收者累计数量超过上限")
	}
	extra := 3
	if opt.IndependentKeys {
		extra += len(selection)
	}
	if len(names) > opt.Limits.MaxEntries-extra {
		return nil, nil, "", limitError("", "新增加密控制文件后条目数量超过上限")
	}
	ordered := append([]string(nil), selection...)
	sort.Strings(ordered)
	selected := map[string]string{}
	for i, name := range ordered {
		if entry, ok := s.files[name]; !ok || entry.IsDir {
			return nil, nil, "", invalid(name, "选中的条目不存在或为目录")
		}
		selected[name] = fmt.Sprintf("%s/entries/%06d.dat", prefix, i)
	}
	return selected, names, prefix, nil
}

type outputEntry struct {
	name string
	data []byte
}
type outputProvider struct {
	Name    string `xml:"Name,attr"`
	Version string `xml:"Version,attr"`
}
type outputInfo struct {
	ID       string         `xml:"ID,attr"`
	Relative string         `xml:"Relative,attr,omitempty"`
	Provider outputProvider `xml:"ofd:Provider"`
	Scope    string         `xml:"ofd:EncryptScope"`
	Seed     string         `xml:"ofd:DecryptSeedLoc"`
	Map      string         `xml:"ofd:EntriesMapLoc"`
}
type outputUser struct {
	Name        string `xml:"UserName,attr"`
	Type        string `xml:"UserType,attr"`
	Certificate string `xml:"ofd:UserCert,omitempty"`
	WK          string `xml:"ofd:EncryptedWK"`
	IV          string `xml:"ofd:IVValue"`
}
type outputMapping struct {
	Path  string `xml:"Path,attr"`
	EPath string `xml:"EPath,attr"`
	Seed  string `xml:"DecryptSeedLoc,attr,omitempty"`
}

// encryptionSeed 对同一 FEK 分别包装；计数在整个写入过程中累计。
func (s *inspector) encryptionSeed(opt EncryptOptions, id, seedPath string, fek, iv []byte) ([]byte, string, []Recipient, error) {
	if len(opt.Recipients) > opt.Limits.MaxRecipients-s.recipients {
		return nil, "", nil, limitError(seedPath, "独立 seed 的接收者累计数量超过上限")
	}
	s.recipients += len(opt.Recipients)
	var scheme string
	summaries := make([]Recipient, 0, len(opt.Recipients))
	users := make([]outputUser, 0, len(opt.Recipients))
	for _, recipient := range opt.Recipients {
		if err := s.ctx.Err(); err != nil {
			return nil, "", nil, err
		}
		role := recipient.UserType
		if role == "" {
			role = "User"
		}
		user := outputUser{Name: recipient.UserName, Type: role, IV: base64.StdEncoding.EncodeToString(iv)}
		summary := Recipient{UserName: recipient.UserName, UserType: role}
		var wk []byte
		var err error
		if recipient.Password != nil {
			scheme = PasswordScheme
			kek := passwordKEK(recipient.Password)
			wk, err = encryptCBC(s.ctx, fek, kek, iv)
			clear(kek)
		} else {
			scheme = CertificateScheme
			cert, parseErr := smx509.ParseCertificate(recipient.Certificate)
			if parseErr != nil {
				return nil, "", nil, fmt.Errorf("接收者 SM2 证书无法解析")
			}
			pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
			if !ok || !sm2.IsSM2PublicKey(pub) {
				return nil, "", nil, fmt.Errorf("接收者证书必须包含有效 SM2 公钥")
			}
			if cert.KeyUsage != 0 && cert.KeyUsage&(smx509.KeyUsageKeyEncipherment|smx509.KeyUsageDataEncipherment) == 0 {
				return nil, "", nil, fmt.Errorf("接收者证书用途不允许加密")
			}
			wk, err = sm2.EncryptASN1(rand.Reader, pub, fek)
			user.Certificate = base64.StdEncoding.EncodeToString(recipient.Certificate)
			summary.CertificateSHA256 = fmt.Sprintf("%x", sha256.Sum256(recipient.Certificate))
		}
		if err != nil {
			return nil, "", nil, err
		}
		user.WK = base64.StdEncoding.EncodeToString(wk)
		clear(wk)
		users = append(users, user)
		summaries = append(summaries, summary)
	}
	seed := struct {
		XMLName xml.Name     `xml:"ofd:DecyptSeed"`
		NS      string       `xml:"xmlns:ofd,attr"`
		ID      string       `xml:"ID,attr"`
		Scheme  string       `xml:"EncryptCaseId,attr"`
		Users   []outputUser `xml:"ofd:UserInfo"`
		Extend  struct{}     `xml:"ofd:ExtendParams"`
	}{NS: Namespace, ID: id, Scheme: scheme, Users: users}
	seedData, err := marshalMetadata(seed)
	if err != nil {
		return nil, "", nil, err
	}
	var seedCheck seedXML
	if err := s.decodeXML(seedData, seedPath, []string{"DecyptSeed"}, &seedCheck); err != nil {
		return nil, "", nil, err
	}
	if err := s.checkWrittenMetadata(seedPath, seedData); err != nil {
		return nil, "", nil, err
	}
	return seedData, scheme, summaries, nil
}

func independentSeedPath(target string) string {
	return strings.TrimSuffix(target, ".dat") + ".seed"
}

func (s *inspector) checkWrittenMetadata(name string, data []byte) error {
	if int64(len(data)) > s.opt.Limits.MaxMetadataBytes || int64(len(data)) > s.remaining {
		return limitError(name, "加密元数据累计规模超过上限")
	}
	s.remaining -= int64(len(data))
	return nil
}

func (s *inspector) encryptionMetadata(opt EncryptOptions, selected map[string]string, prefix, id, relative string, oldRoot, fek, iv []byte) ([]outputEntry, *EncryptReport, error) {
	seedPath, mapPath := prefix+"/decryptseed.dat", prefix+"/entriesmap.dat"
	seedData, scheme, recipients, err := s.encryptionSeed(opt, id, seedPath, fek, iv)
	if err != nil {
		return nil, nil, err
	}
	report := &EncryptReport{State: "locked", OperationID: id, Relative: relative, IndependentKeys: opt.IndependentKeys, EncryptedEntries: len(selected), Scheme: scheme, Recipients: recipients}
	mappings := make([]outputMapping, 0, len(selected))
	names := make([]string, 0, len(selected))
	for name := range selected {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		mapping := outputMapping{Path: "/" + name, EPath: "/" + selected[name]}
		if opt.IndependentKeys {
			mapping.Seed = "/" + independentSeedPath(selected[name])
		}
		mappings = append(mappings, mapping)
	}
	if opt.Pages != nil {
		report.SelectedEntries = append([]string(nil), names...)
		report.SharedResources = true
	}
	entryMap := struct {
		XMLName xml.Name        `xml:"ofd:EncryptedEntries"`
		NS      string          `xml:"xmlns:ofd,attr"`
		ID      string          `xml:"ID,attr"`
		Entries []outputMapping `xml:"ofd:EncryptEntry"`
	}{NS: Namespace, ID: id, Entries: mappings}
	mapData, err := marshalMetadata(entryMap)
	if err != nil {
		return nil, nil, err
	}
	if int64(len(mapData)) > s.opt.Limits.MaxMetadataBytes {
		return nil, nil, limitError(mapPath, "映射表超过大小上限")
	}
	var mapCheck entriesXML
	if err := s.decodeXML(mapData, mapPath, []string{"EncryptedEntries"}, &mapCheck); err != nil {
		return nil, nil, err
	}
	if !opt.PlaintextMap {
		encrypted, err := encryptCBC(s.ctx, mapData, fek, iv)
		clear(mapData)
		if err != nil {
			return nil, nil, err
		}
		mapData = encrypted
	}
	if err := s.checkWrittenMetadata(mapPath, mapData); err != nil {
		return nil, nil, err
	}
	scope := "All"
	if opt.Entries != nil || opt.KeepRoot {
		scope = "Explicit entries"
	}
	info := outputInfo{ID: id, Relative: relative, Provider: outputProvider{"ofd-crypt", version.Version}, Scope: scope, Seed: "/" + seedPath, Map: "/" + mapPath}
	root := struct {
		XMLName xml.Name   `xml:"ofd:Encryptions"`
		NS      string     `xml:"xmlns:ofd,attr"`
		Info    outputInfo `xml:"ofd:EncryptInfo"`
	}{NS: Namespace, Info: info}
	rootData, err := marshalMetadata(root)
	if err != nil {
		return nil, nil, err
	}
	if len(oldRoot) != 0 {
		rootData, err = appendEncryptionInfo(oldRoot, info)
		if err != nil {
			return nil, nil, err
		}
	}
	var rootCheck encryptionXML
	if err := s.decodeXML(rootData, EntryPoint, []string{"Encryptions"}, &rootCheck); err != nil {
		return nil, nil, err
	}
	if err := s.checkWrittenMetadata(EntryPoint, rootData); err != nil {
		return nil, nil, err
	}
	return []outputEntry{{EntryPoint, rootData}, {seedPath, seedData}, {mapPath, mapData}}, report, nil
}

func marshalMetadata(value any) ([]byte, error) {
	data, err := xml.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), data...), nil
}

func encryptCBC(ctx context.Context, data, key, iv []byte) ([]byte, error) {
	if len(iv) != sm4.BlockSize {
		return nil, fmt.Errorf("SM4 CBC IV 长度必须为 16 字节")
	}
	block, err := sm4.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padding := 16 - len(data)%16
	result := make([]byte, len(data)+padding)
	copy(result, data)
	for i := len(data); i < len(result); i++ {
		result[i] = byte(padding)
	}
	cbc := cipher.NewCBCEncrypter(block, iv)
	for offset := 0; offset < len(result); {
		if err := ctx.Err(); err != nil {
			clear(result)
			return nil, err
		}
		end := min(offset+64*1024, len(result))
		cbc.CryptBlocks(result[offset:end], result[offset:end])
		offset = end
	}
	return result, ctx.Err()
}
