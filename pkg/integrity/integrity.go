// Package integrity 实现 GM/T 0099 §7.4、附录 D 的清单保护与防夹带检查。
package integrity

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zip"
	"github.com/knroy/go-xml/v2/xdm"
	"github.com/knroy/go-xml/v2/xsd"
	"github.com/zc310/ofd/internal/container"
	protocol "github.com/zc310/ofd/internal/crypt"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/physicalsig"
	"github.com/zc310/ofd/internal/schema"
	"github.com/zc310/ofd/internal/securexml"
	"github.com/zc310/ofd/internal/version"
	"github.com/zc310/ofd/pkg/crypt"
	"github.com/zc310/ofd/pkg/sign"
	"github.com/zc310/ofd/pkg/spec"
	"github.com/zc310/ofd/pkg/validator"
)

const EntryPoint = spec.IntegrityEntryPoint

var (
	ErrInvalid   = errors.New("integrity: 无效的完整性元数据")
	ErrProtected = errors.New("integrity: 已有完整性保护")
)

// Options 的零预算使用 crypt 的有限默认值。不会隐式访问网络。
type Options struct {
	Limits       crypt.Limits
	Verification sign.VerificationOptions
	// Decrypt 用于遍历加密映射与逻辑文档，不生成明文 ZIP。
	Decrypt *crypt.DecryptOptions
}

type File struct {
	ID       string `xml:"ID,attr" json:"id"`
	Location string `xml:"FileLoc,attr" json:"location"`
}

type Manifest struct {
	XMLName        xml.Name `json:"-"`
	ID             string   `xml:"ID,attr" json:"id"`
	CreatorName    string   `xml:"CreatorName,attr,omitempty" json:"creator_name,omitempty"`
	Version        string   `xml:"Version,attr,omitempty" json:"version,omitempty"`
	CreationDate   string   `xml:"CreationDate,attr,omitempty" json:"creation_date,omitempty"`
	Files          []File   `xml:"FileList>File" json:"files"`
	SignedValueLoc string   `xml:"SignedValueLoc" json:"signed_value_location,omitempty"`
}

type Report struct {
	Present            bool                              `json:"present"`
	Status             string                            `json:"status"`
	Manifest           *Manifest                         `json:"manifest,omitempty"`
	Checks             []sign.VerificationCheck          `json:"checks,omitempty"`
	Signature          *sign.SignatureVerificationResult `json:"signature,omitempty"`
	PhysicalSignatures []sign.Verification               `json:"physical_signatures,omitempty"`
	Unlisted           []string                          `json:"unlisted,omitempty"`
	Missing            []string                          `json:"missing,omitempty"`
	Unreferenced       []string                          `json:"unreferenced,omitempty"`
	Unprotected        []string                          `json:"unprotected,omitempty"`
	Diagnostics        []string                          `json:"diagnostics,omitempty"`
}

func (r *Report) check(code, status, message string) {
	r.Checks = append(r.Checks, sign.VerificationCheck{Code: code, Status: status, Message: message})
	r.Status = "valid"
	for _, check := range r.Checks {
		if check.Status == "invalid" {
			r.Status = "invalid"
			return
		}
		if check.Status == "incomplete" {
			r.Status = "incomplete"
		}
	}
}

type packageData struct {
	pkg    *container.Package
	files  map[string][]byte
	names  []string
	limits crypt.Limits
}

func open(ctx context.Context, input any, limits crypt.Limits) (*packageData, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	normalized, err := protocol.NormalizeOptions(protocol.InspectOptions{Limits: limits})
	if err != nil {
		return nil, err
	}
	limits = normalized.Limits
	var pkg *container.Package
	switch input := input.(type) {
	case string:
		stat, err := os.Stat(input)
		if err != nil {
			return nil, err
		}
		if stat.Size() > limits.MaxInputBytes {
			return nil, crypt.ErrLimitExceeded
		}
		pkg, err = container.OpenFile(input)
	case []byte:
		if int64(len(input)) > limits.MaxInputBytes {
			return nil, crypt.ErrLimitExceeded
		}
		pkg, err = container.OpenBytes(input)
	case io.Reader:
		probe := limits.MaxInputBytes
		if probe < 1<<63-1 {
			probe++
		}
		data, readErr := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, r: input}, probe))
		if readErr != nil {
			return nil, readErr
		}
		if int64(len(data)) > limits.MaxInputBytes {
			return nil, crypt.ErrLimitExceeded
		}
		pkg, err = container.OpenBytes(data)
	default:
		return nil, fmt.Errorf("完整性保护需要原始物理 ZIP 路径、字节或 Reader")
	}
	if err != nil {
		return nil, err
	}
	p := &packageData{pkg: pkg, files: map[string][]byte{}, limits: limits}
	ok := false
	defer func() {
		if !ok {
			pkg.Close()
		}
	}()
	entries := pkg.Entries()
	if len(entries) > limits.MaxEntries {
		return nil, crypt.ErrLimitExceeded
	}
	remaining := limits.MaxTotalBytes
	lower := map[string]bool{}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(entry.Name, "/")
		if container.ValidateEntryName(name) != nil || strings.Contains(name, ":") || lower[strings.ToLower(name)] {
			return nil, fmt.Errorf("%w: ZIP 路径重复或无效", ErrInvalid)
		}
		lower[strings.ToLower(name)] = true
		if entry.IsDir {
			if entry.UncompressedSize != 0 {
				return nil, ErrInvalid
			}
			continue
		}
		if entry.UncompressedSize > uint64(limits.MaxEntryBytes) || entry.UncompressedSize > uint64(remaining) {
			return nil, crypt.ErrLimitExceeded
		}
		r, err := pkg.OpenEntry(entry)
		if err != nil {
			return nil, err
		}
		maximum := min(limits.MaxEntryBytes, remaining)
		probe := maximum
		if probe < 1<<63-1 {
			probe++
		}
		data, readErr := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, r: r}, probe))
		closeErr := r.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if int64(len(data)) > maximum {
			return nil, crypt.ErrLimitExceeded
		}
		remaining -= int64(len(data))
		p.files[name], p.names = data, append(p.names, name)
	}
	sort.Strings(p.names)
	for _, name := range p.names {
		for i := strings.IndexByte(name, '/'); i >= 0; {
			if _, exists := p.files[name[:i]]; exists {
				return nil, ErrInvalid
			}
			next := strings.IndexByte(name[i+1:], '/')
			if next < 0 {
				break
			}
			i += next + 1
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ok = true
	return p, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func inspect(ctx context.Context, p *packageData) (*Report, error) {
	r := &Report{Status: "absent"}
	data, exists := p.files[EntryPoint]
	if !exists {
		for name := range p.files {
			if strings.EqualFold(name, EntryPoint) {
				return r, fmt.Errorf("%w: 清单文件名大小写不规范", ErrInvalid)
			}
		}
		return r, nil
	}
	r.Present = true
	if int64(len(data)) > p.limits.MaxMetadataBytes {
		return r, crypt.ErrLimitExceeded
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var root string
	for root == "" {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		token, err := decoder.Token()
		if err != nil {
			return r, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		if start, ok := token.(xml.StartElement); ok {
			root = start.Name.Local
		}
	}
	if root != "OFDEntries" && root != "DocEntries" {
		return r, fmt.Errorf("%w: 未知清单根元素", ErrInvalid)
	}
	var m Manifest
	if err := securexml.DecodeWithLimits(ctx, data, root, &m, p.limits.MaxXMLNodes, p.limits.MaxXMLDepth); err != nil {
		if errors.Is(err, securexml.ErrLimitExceeded) {
			return r, fmt.Errorf("%w: %w", crypt.ErrLimitExceeded, err)
		}
		return r, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	schemas, err := schema.Default()
	if err != nil {
		return r, err
	}
	structure, err := xdm.Parse(bytes.NewReader(data), xdm.ParseOptions{MaxBytes: p.limits.MaxMetadataBytes, MaxNodes: p.limits.MaxXMLNodes, MaxDepth: p.limits.MaxXMLDepth})
	if err != nil {
		return r, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	manifestSchema, _ := schemas.SecuritySchema(root)
	if err := manifestSchema.Validate(structure.Root, xsd.ValidateOptions{MaxErrors: 1}); err != nil {
		return r, fmt.Errorf("%w: 清单模式检查失败: %v", ErrInvalid, err)
	}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	if root == "DocEntries" {
		r.Diagnostics = append(r.Diagnostics, "采用表 D.1 的 DocEntries 根拼写；图 D.1 为 OFDEntries")
	}
	if !securexml.ValidID(m.ID) || len(m.Files) == 0 || len(m.Files) > p.limits.MaxEntries {
		return r, fmt.Errorf("%w: 清单 ID 或 FileList 无效", ErrInvalid)
	}
	if m.CreationDate != "" {
		if _, err := time.Parse("2006-01-02", m.CreationDate); err != nil {
			if _, err := time.Parse("2006-01-02Z07:00", m.CreationDate); err != nil {
				return r, fmt.Errorf("%w: CreationDate 不是 xs:date", ErrInvalid)
			}
		}
	}
	ids, paths := map[string]bool{m.ID: true}, map[string]bool{}
	for i := range m.Files {
		file := &m.Files[i]
		name, err := securexml.Resolve(EntryPoint, file.Location)
		if err != nil || !securexml.ValidID(file.ID) || ids[file.ID] || paths[strings.ToLower(name)] {
			return r, fmt.Errorf("%w: 文件 ID 或路径重复/无效", ErrInvalid)
		}
		ids[file.ID], paths[strings.ToLower(name)] = true, true
		file.Location = name
		if _, exists := p.files[name]; !exists {
			r.Missing = append(r.Missing, name)
		}
	}
	if m.SignedValueLoc != "" {
		name, err := securexml.Resolve(EntryPoint, m.SignedValueLoc)
		if err != nil || name == EntryPoint {
			return r, fmt.Errorf("%w: SignedValueLoc 无效", ErrInvalid)
		}
		m.SignedValueLoc = name
	}
	for _, name := range p.names {
		if name != EntryPoint && name != m.SignedValueLoc && !paths[strings.ToLower(name)] {
			r.Unlisted = append(r.Unlisted, name)
		}
	}
	r.Manifest = &m
	status, message := "valid", ""
	if len(r.Missing)+len(r.Unlisted) != 0 {
		status, message = "invalid", "物理条目与 FileList 不一致"
	}
	r.check("manifest.files", status, message)
	return r, nil
}

// Inspect 只检查可见清单和物理条目；不作可信或无夹带结论。
func Inspect(ctx context.Context, input any, options Options) (*Report, error) {
	p, err := open(ctx, input, options.Limits)
	if err != nil {
		return nil, err
	}
	defer p.pkg.Close()
	r, err := inspect(ctx, p)
	if err == nil && r.Present {
		r.check("verification", "incomplete", "尚未验证清单签名、时间戳、内容覆盖与引用闭包")
	}
	return r, err
}

// Verify 分别验证可信清单、物理集合、引用闭包与文件内容覆盖。只有所有必需检查
// 完成且通过才返回 Status=valid；未解锁或缺少信任/吊销证据保持 incomplete。
// 返回错误时，已有报告保留已确认的 invalid，否则标为 incomplete；调用方仍须先处理错误。
func Verify(ctx context.Context, input any, options Options) (report *Report, err error) {
	defer func() {
		if err == nil || report == nil {
			return
		}
		status := "incomplete"
		if errors.Is(err, ErrInvalid) || errors.Is(err, crypt.ErrInvalidMetadata) {
			status = "invalid"
		}
		report.check("verification", status, "完整性验证未完成: "+err.Error())
	}()
	if options.Verification.CurrentTime.IsZero() {
		options.Verification.CurrentTime = time.Now()
	}
	p, err := open(ctx, input, options.Limits)
	if err != nil {
		return nil, err
	}
	defer p.pkg.Close()
	r, err := inspect(ctx, p)
	if err != nil || !r.Present {
		return r, err
	}
	value, exists := p.files[r.Manifest.SignedValueLoc]
	if int64(len(value)) > p.limits.MaxMetadataBytes || int64(len(value)+len(p.files[EntryPoint])) > p.limits.MaxTotalMetadataBytes {
		return r, crypt.ErrLimitExceeded
	}
	if !exists {
		r.check("manifest.signature", "invalid", "清单缺少签名值")
	} else {
		r.Signature, err = sign.VerifyDigitalSignedValue(p.files[EntryPoint], value, options.Verification)
		if err != nil {
			return r, err
		}
		parser.RequireTimestamp(r.Signature)
		r.check("manifest.signature", r.Signature.Validation.Status, "清单签名及时间戳验证")
	}
	covered := map[string]bool{EntryPoint: true, r.Manifest.SignedValueLoc: true}
	physicalLimits := p.limits
	usedMetadata := int64(len(value) + len(p.files[EntryPoint]))
	if usedMetadata >= physicalLimits.MaxTotalMetadataBytes {
		return r, crypt.ErrLimitExceeded
	}
	physicalLimits.MaxTotalMetadataBytes -= usedMetadata
	graph, physicalErr := physicalsig.Discover(ctx, p.pkg, physicalLimits)
	if physicalErr != nil {
		if ctx.Err() != nil {
			return r, ctx.Err()
		}
		if errors.Is(physicalErr, crypt.ErrLimitExceeded) || errors.Is(physicalErr, container.ErrReadLimitExceeded) || errors.Is(physicalErr, securexml.ErrLimitExceeded) {
			return r, fmt.Errorf("%w: %w", crypt.ErrLimitExceeded, physicalErr)
		}
		status := "invalid"
		if errors.Is(physicalErr, protocol.ErrUnsupported) {
			status = "incomplete"
		}
		r.check("physical.signature", status, physicalErr.Error())
		return r, nil
	}
	if graph != nil {
		verified, err := physicalsig.Verify(ctx, p.pkg, graph, &options.Verification, physicalLimits)
		if err != nil {
			if ctx.Err() != nil {
				return r, ctx.Err()
			}
			if errors.Is(err, crypt.ErrLimitExceeded) || errors.Is(err, container.ErrReadLimitExceeded) {
				return r, fmt.Errorf("%w: %w", crypt.ErrLimitExceeded, err)
			}
			r.check("physical.signature", "invalid", err.Error())
			return r, nil
		}
		for _, item := range verified {
			r.PhysicalSignatures = append(r.PhysicalSignatures, sign.Verification{DocumentIndex: item.Record.DocumentIndex, ID: item.Record.ID, Scope: "physical", DigestValid: item.Digest.Valid, Result: item.Result})
			r.check("physical.signature", item.Result.Validation.Status, "加密物理包签名: "+item.Record.ID)
			if item.Result.Validation.Status == "invalid" {
				return r, nil
			}
			if item.Result.FullyValid() {
				covered[item.Record.Signature.FilePath], covered[item.Record.Value] = true, true
				for _, ref := range item.Digest.References {
					if ref.Match {
						covered[strings.TrimPrefix(ref.ResolvedPath, "/")] = true
					}
				}
			}
		}
	}
	logical := any(packageSource{p.pkg})
	var view *crypt.View
	var info *crypt.Info
	if protocol.Present(p.pkg) {
		if options.Decrypt == nil {
			r.check("references", "incomplete", "加密映射/文档尚未解锁，不能完成引用闭包")
			finishCoverage(p, covered, r)
			return r, nil
		}
		opt := *options.Decrypt
		opt.Limits = p.limits
		used := usedMetadata
		if graph != nil {
			used += graph.MetadataBytes
		}
		if used >= opt.Limits.MaxTotalMetadataBytes {
			return r, crypt.ErrLimitExceeded
		}
		opt.Limits.MaxTotalMetadataBytes -= used
		if graph != nil {
			if graph.ProcessedBytes >= opt.Limits.MaxProcessedBytes {
				return r, crypt.ErrLimitExceeded
			}
			opt.Limits.MaxProcessedBytes -= graph.ProcessedBytes
		}
		view, err = crypt.Open(ctx, packageSource{p.pkg}, crypt.OpenOptions{DecryptOptions: opt})
		if err != nil {
			return r, err
		}
		defer view.Close()
		info, logical = view.EncryptionInfo(), view
		for _, operation := range info.Operations {
			for _, mapping := range operation.Entries {
				if mapping.Path == EntryPoint || mapping.Path == r.Manifest.SignedValueLoc {
					r.check("references", "invalid", "解密映射覆盖了完整性清单或其签名值")
					return r, nil
				}
			}
		}
	}
	if graph != nil {
		lease, err := view.OFDPackage()
		if err != nil {
			return r, err
		}
		defer lease.Close()
		projected, err := physicalsig.LogicalPackage(ctx, lease, graph)
		if err != nil {
			if ctx.Err() != nil {
				return r, ctx.Err()
			}
			r.check("references", "invalid", err.Error())
			return r, nil
		}
		defer projected.Close()
		logical = packageSource{projected}
	}
	refs, err := validator.PackageReferences(ctx, logical, validator.WithGMT0099(true), validator.WithMaxInputSize(p.limits.MaxInputBytes), validator.WithMaxFileSize(p.limits.MaxEntryBytes), validator.WithMaxTotalSize(p.limits.MaxTotalBytes), validator.WithMaxEntries(p.limits.MaxEntries), validator.WithMaxXMLBytes(p.limits.MaxEntryBytes), validator.WithMaxXMLNodes(p.limits.MaxXMLNodes), validator.WithMaxXMLDepth(p.limits.MaxXMLDepth))
	if err != nil {
		return r, err
	}
	reachable := refs.Reachable
	reachable[EntryPoint], reachable[r.Manifest.SignedValueLoc] = true, true
	if graph != nil {
		for name := range graph.Metadata {
			reachable[name] = true
		}
	}
	if info != nil {
		reachable = encryptionReachable(reachable, info)
	}
	for _, name := range p.names {
		if !reachable[name] {
			r.Unreferenced = append(r.Unreferenced, name)
		}
	}
	if !refs.Complete() {
		r.check("references", "incomplete", refs.IncompleteReason())
	} else if len(r.Unreferenced) > 0 {
		r.check("references", "invalid", "充分遍历后存在未被引用的物理文件")
	} else {
		r.check("references", "valid", "")
	}
	if err := contentCoverage(ctx, p, logical, covered, options, r); err != nil {
		return r, err
	}
	return r, ctx.Err()
}

// packageSource 只借用物理包，不转移所有权。
type packageSource struct{ pkg *container.Package }

func (s packageSource) InputSize() int64 { return s.pkg.InputSize() }
func (s packageSource) OFDPackage() (*container.Package, error) {
	return container.NewVirtual(s.pkg.Entries(), s.pkg.Open, nil)
}

func encryptionReachable(logical map[string]bool, info *crypt.Info) map[string]bool {
	for _, op := range info.Operations {
		logical[op.DecryptSeedLoc], logical[op.EntriesMapLoc] = true, true
	}
	done := map[string]bool{}
	for len(done) < len(info.Operations) {
		for _, op := range info.Operations {
			if done[op.ID] || op.Relative != "" && !done[op.Relative] {
				continue
			}
			for _, m := range op.Entries {
				if logical[m.Path] {
					logical[m.EPath] = true
					if m.DecryptSeedLoc != "" {
						logical[m.DecryptSeedLoc] = true
					}
				}
			}
			done[op.ID] = true
		}
	}
	logical[spec.EncryptionEntryPoint] = true
	return logical
}

func contentCoverage(ctx context.Context, p *packageData, input any, covered map[string]bool, options Options, report *Report) error {
	ofd, err := parser.NewOFD(input)
	if err != nil {
		return err
	}
	defer ofd.Close()
	for _, doc := range ofd.Documents {
		var canceled error
		doc.ForEachSignature(func(id string, signature *models.Signature) bool {
			result, err := doc.VerifySignature(ctx, id, &options.Verification)
			if ctx.Err() != nil {
				canceled = ctx.Err()
				return false
			}
			if err != nil {
				report.check("content.signature", "incomplete", err.Error())
				return true
			}
			if result.Validation.Status == "invalid" {
				report.check("content.signature", "invalid", "文档签名或其引用验证失败: "+id)
			}
			if !result.FullyValid() {
				return true
			}
			covered[strings.TrimPrefix(signature.FilePath, "/")] = true
			if name, err := securexml.Resolve(signature.FilePath, signature.SignedValue.String()); err == nil {
				covered[name] = true
			}
			if digest := doc.GetDigestResult(id); digest != nil && digest.Valid {
				for _, ref := range digest.References {
					if ref.Match {
						covered[strings.TrimPrefix(ref.ResolvedPath, "/")] = true
					}
				}
			}
			return true
		})
		if canceled != nil {
			return canceled
		}
	}
	finishCoverage(p, covered, report)
	return ctx.Err()
}

func finishCoverage(p *packageData, covered map[string]bool, report *Report) {
	for _, name := range p.names {
		if !covered[name] {
			report.Unprotected = append(report.Unprotected, name)
		}
	}
	if len(report.Unprotected) > 0 {
		report.check("content.coverage", "incomplete", "清单不含文件摘要；部分文件尚无完整通过验证的签名保护")
	} else {
		report.check("content.coverage", "valid", "")
	}
}

// ProtectOptions 用外部签名器为最终清单提供带时间戳的 GB/T 35275/CMS 签名。
type ProtectOptions struct {
	Options
	ID          string
	CreatorName string
	Date        time.Time
	Sign        func(context.Context, []byte) ([]byte, error)
}

// Protect 增加清单及签名值，保留所有输入文件原字节。不会删除夹带文件或隐式重签文档。
// 任意错误均要求丢弃 Writer 的部分输出。返回状态为 signed，不表示已验证证书信任或文件内容。
func Protect(ctx context.Context, input any, w io.Writer, options ProtectOptions) (*Report, error) {
	if w == nil || options.Sign == nil {
		return nil, fmt.Errorf("需要输出及外部清单签名器")
	}
	if options.ID == "" {
		options.ID = "integrity1"
	}
	if !securexml.ValidID(options.ID) {
		return nil, fmt.Errorf("清单 ID 必须为 xs:ID")
	}
	p, err := open(ctx, input, options.Limits)
	if err != nil {
		return nil, err
	}
	defer p.pkg.Close()
	for _, name := range p.names {
		if strings.EqualFold(name, EntryPoint) {
			return nil, ErrProtected
		}
	}
	if _, root := p.files[spec.RootDocument]; !root && !protocol.Present(p.pkg) {
		return nil, fmt.Errorf("%w: 缺少 OFD 或加密入口", ErrInvalid)
	}
	prefix := ""
	for n := 1; n <= len(p.names)+1; n++ {
		candidate, used := fmt.Sprintf("Integrity_%d", n), false
		for _, name := range p.names {
			used = used || strings.EqualFold(name, candidate) || strings.HasPrefix(strings.ToLower(name), strings.ToLower(candidate)+"/")
		}
		if !used {
			prefix = candidate
			break
		}
	}
	if options.Date.IsZero() {
		options.Date = time.Now()
	}
	m := Manifest{ID: options.ID, CreatorName: options.CreatorName, Version: version.Version, CreationDate: options.Date.Format("2006-01-02"), SignedValueLoc: "/" + prefix + "/SignedValue.dat"}
	for i, name := range p.names {
		fileID := fmt.Sprintf("f%d", i+1)
		if fileID == m.ID {
			fileID = "file-" + fileID
		}
		m.Files = append(m.Files, File{ID: fileID, Location: "/" + name})
	}
	wire := struct {
		XMLName xml.Name `xml:"OFDEntries"`
		NS      string   `xml:"xmlns,attr"`
		Manifest
	}{NS: securexml.Namespace, Manifest: m}
	data, err := xml.Marshal(wire)
	if err != nil {
		return nil, err
	}
	data = append([]byte(xml.Header), data...)
	if int64(len(data)) > p.limits.MaxMetadataBytes {
		return nil, crypt.ErrLimitExceeded
	}
	var checked Manifest
	if err := securexml.DecodeWithLimits(ctx, data, "OFDEntries", &checked, p.limits.MaxXMLNodes, p.limits.MaxXMLDepth); err != nil {
		if errors.Is(err, securexml.ErrLimitExceeded) {
			return nil, fmt.Errorf("%w: %w", crypt.ErrLimitExceeded, err)
		}
		return nil, err
	}
	value, err := options.Sign(ctx, bytes.Clone(data))
	if err != nil {
		return nil, err
	}
	if int64(len(value)) > p.limits.MaxMetadataBytes {
		return nil, crypt.ErrLimitExceeded
	}
	result, err := sign.VerifyDigitalSignedValue(data, value, options.Verification)
	if err != nil {
		return nil, err
	}
	parser.RequireTimestamp(result)
	if !result.Valid {
		return nil, fmt.Errorf("%w: 签名器未返回绑定清单的有效签名", ErrInvalid)
	}
	for _, check := range result.Validation.Checks {
		if check.Status == "invalid" {
			return nil, fmt.Errorf("%w: %s: %s", ErrInvalid, check.Code, check.Message)
		}
	}
	if len(p.names) > p.limits.MaxEntries-2 || int64(len(data)+len(value)) > p.limits.MaxTotalMetadataBytes {
		return nil, crypt.ErrLimitExceeded
	}
	archive := zip.NewWriter(w)
	var total int64
	write := func(name string, data []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if int64(len(data)) > p.limits.MaxEntryBytes || int64(len(data)) > p.limits.MaxTotalBytes-total {
			return crypt.ErrLimitExceeded
		}
		total += int64(len(data))
		entry, err := archive.Create(name)
		if err != nil {
			return err
		}
		_, err = entry.Write(data)
		return err
	}
	for _, name := range p.names {
		if err := write(name, p.files[name]); err != nil {
			return nil, err
		}
	}
	if err := write(EntryPoint, data); err != nil {
		return nil, err
	}
	if err := write(strings.TrimPrefix(m.SignedValueLoc, "/"), value); err != nil {
		return nil, err
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for i := range m.Files {
		m.Files[i].Location = strings.TrimPrefix(m.Files[i].Location, "/")
	}
	m.SignedValueLoc = strings.TrimPrefix(m.SignedValueLoc, "/")
	return &Report{Present: true, Status: "signed", Manifest: &m, Signature: result}, nil
}
