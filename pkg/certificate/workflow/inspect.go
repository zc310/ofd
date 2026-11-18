package workflow

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/beevik/etree"
	"github.com/zc310/ofd/internal/container"
	protocol "github.com/zc310/ofd/internal/crypt"
	"github.com/zc310/ofd/internal/physicalsig"
	"github.com/zc310/ofd/internal/securexml"
	"github.com/zc310/ofd/internal/xmlcodec"
	"github.com/zc310/ofd/pkg/certificate"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/sign"
	"github.com/zc310/ofd/pkg/spec"
	"github.com/zc310/ofd/pkg/validator"
)

type signatureRecord struct {
	ref       spec.SignatureRef
	name      string
	value     string
	artifacts []string
	signature spec.Signature
}

type snapshot struct {
	pkg        *container.Package
	root       *etree.Document
	body       *etree.Element
	custom     *etree.Element
	metadata   certificate.Metadata
	docRoot    string
	listName   string
	signatures []signatureRecord
}

func validID(id string) bool { return securexml.ValidID(id) }

func (s *snapshot) close() { _ = s.pkg.Close() }

func inspect(ctx context.Context, input []byte) (*snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(input) > 512<<20 {
		return nil, protocol.ErrLimitExceeded
	}
	pkg, err := container.OpenBytes(input)
	if err != nil {
		return nil, err
	}
	s := &snapshot{pkg: pkg}
	ok := false
	defer func() {
		if !ok {
			s.close()
		}
	}()
	// 共用签名链的容器预算与路径歧义检查，不依赖 ZIP 大小写回退。
	if err := physicalsig.CheckContainer(ctx, pkg, protocol.Limits{}); err != nil {
		return nil, err
	}
	if protocol.Present(pkg) || pkg.Has(spec.IntegrityEntryPoint) {
		return nil, fmt.Errorf("%w: 先单独处理加密包或完整性清单", protocol.ErrProtectedContent)
	}
	v, err := validator.New(validator.WithGMT0099(true), validator.WithCertificateMetadata(true),
		validator.WithMaxXMLBytes(4<<20), validator.WithMaxXMLNodes(100000), validator.WithMaxXMLDepth(64))
	if err != nil {
		return nil, err
	}
	report := v.ValidateReader(ctx, bytes.NewReader(input), "certificate.ofd")
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if report.HasErrors() || report.Summary.Warnings != 0 || report.Status != validator.StatusValid {
		return nil, fmt.Errorf("%w: 基础结构、元数据或摘要未通过严格检查", ErrInvalid)
	}
	data, err := readExact(pkg, spec.RootDocument, 4<<20)
	if err != nil {
		return nil, err
	}
	var root spec.OFD
	if err := securexml.Decode(ctx, data, "OFD", &root); err != nil {
		return nil, err
	}
	if len(root.DocBodies) != 1 {
		return nil, fmt.Errorf("%w: 本工作流仅接受单文档体证照，多页不受此限制", ErrInvalid)
	}
	body := root.DocBodies[0]
	s.docRoot, err = securexml.Resolve(spec.RootDocument, body.DocRoot.String())
	if err != nil {
		return nil, err
	}
	s.root, err = xmlcodec.Parse(data)
	if err != nil {
		return nil, err
	}
	s.body = children(s.root.Root(), "DocBody")[0]
	s.custom = children(children(s.body, "DocInfo")[0], "CustomDatas")[0]
	s.metadata = certificate.ParseMetadata(body.DocInfo.CustomDatas.CustomData)
	if body.Signatures != nil {
		s.listName, err = securexml.Resolve(spec.RootDocument, body.Signatures.String())
		if err != nil {
			return nil, err
		}
		if err := s.readSignatures(ctx); err != nil {
			return nil, err
		}
	}
	ok = true
	return s, nil
}

func (s *snapshot) readSignatures(ctx context.Context) error {
	data, err := readExact(s.pkg, s.listName, 4<<20)
	if err != nil {
		return err
	}
	var list spec.Signatures
	if err := securexml.Decode(ctx, data, "Signatures", &list); err != nil {
		return err
	}
	ids, paths := map[string]bool{}, map[string]bool{}
	for _, ref := range list.Signatures {
		if ref.Type != spec.SigTypeSign || !validID(ref.ID) || ids[ref.ID] {
			return fmt.Errorf("%w: 仅支持 ID 唯一的普通 Sign 签名列表", ErrInvalid)
		}
		ids[ref.ID] = true
		name, err := securexml.Resolve(s.listName, ref.BaseLoc.String())
		if err != nil {
			return err
		}
		if paths[name] {
			return fmt.Errorf("%w: 签名描述路径重复", ErrInvalid)
		}
		paths[name] = true
		data, err := readExact(s.pkg, name, 4<<20)
		if err != nil {
			return err
		}
		record := signatureRecord{ref: ref, name: name}
		if err := securexml.Decode(ctx, data, "Signature", &record.signature); err != nil {
			return err
		}
		if id := record.signature.ID; id != "" && id != ref.ID {
			return fmt.Errorf("%w: 签名描述 ID 与列表不一致", ErrInvalid)
		}
		record.value, err = securexml.Resolve(name, record.signature.SignedValue.String())
		if err != nil {
			return err
		}
		record.artifacts = []string{name, record.value}
		if seal := record.signature.SignedInfo.Seal; seal != nil {
			for _, location := range []spec.StLoc{seal.BaseLoc, seal.ImageLoc} {
				if location == "" {
					continue
				}
				artifact, err := securexml.Resolve(name, location.String())
				if err != nil {
					return err
				}
				record.artifacts = append(record.artifacts, artifact)
			}
		}
		for _, artifact := range record.artifacts {
			if _, err := readExact(s.pkg, artifact, 4<<20); err != nil {
				return err
			}
		}
		s.signatures = append(s.signatures, record)
	}
	return nil
}

func readExact(pkg *container.Package, name string, limit int64) ([]byte, error) {
	entry, exists := pkg.Lookup(name)
	if !exists || entry.IsDir || entry.Path != name {
		return nil, fmt.Errorf("%w: 所需路径不存在或大小写不一致: %s", ErrInvalid, name)
	}
	return pkg.ReadLimit(name, limit)
}

func children(parent *etree.Element, name string) []*etree.Element {
	var result []*etree.Element
	for _, node := range parent.ChildElements() {
		if node.Tag == name && node.NamespaceURI() == spec.Namespace {
			result = append(result, node)
		}
	}
	return result
}

func (s *snapshot) selected(id string) (*signatureRecord, error) {
	for index := range s.signatures {
		if s.signatures[index].ref.ID == id {
			return &s.signatures[index], nil
		}
	}
	return nil, fmt.Errorf("%w: 未找到指定锁定签名", ErrInvalid)
}

func (s *snapshot) checkCoverage(record *signatureRecord) error {
	covered := map[string]bool{}
	for _, ref := range record.signature.SignedInfo.References.Reference {
		name, err := securexml.Resolve(record.name, ref.FileRef.String())
		if err != nil {
			return err
		}
		if covered[name] || name == record.name || name == record.value {
			return fmt.Errorf("%w: 锁定范围包含重复或自身引用", ErrInvalid)
		}
		covered[name] = true
	}
	// GB/T 33190 §18.1 固定列表；GB/T 36905 §7.2 的工作流采用保守整包覆盖策略，
	// 不把“只签了列表”当作证照整体内容已锁定。
	for _, entry := range s.pkg.Entries() {
		if entry.IsDir || entry.Path == record.name || entry.Path == record.value {
			continue
		}
		if !covered[entry.Path] {
			return fmt.Errorf("%w: 锁定签名未覆盖 %s", ErrInvalid, entry.Path)
		}
	}
	return nil
}

func (s *snapshot) checkRole(role Role) error {
	copyPresent := false
	for _, entry := range s.metadata.Entries() {
		if copyField(entry.Name) != "" {
			copyPresent = true
		}
	}
	if role == Original && copyPresent {
		return fmt.Errorf("%w: 原件用途不能包含加注件元数据", ErrInvalid)
	}
	if role == Copy {
		return s.checkCopyMetadata()
	}
	return nil
}

func verifyLock(ctx context.Context, input []byte, s *snapshot, options VerifyOptions) error {
	if err := s.checkRole(options.Role); err != nil {
		return err
	}
	selected, err := s.selected(options.LockID)
	if err != nil {
		return err
	}
	if err := s.checkCoverage(selected); err != nil {
		return err
	}
	signers, err := verifyAll(ctx, input, s, options.Verification)
	if err != nil {
		return err
	}
	signerDER := signers[options.LockID]
	if len(signerDER) == 0 {
		return fmt.Errorf("%w: 未取得锁定签名的实际签名者证书", ErrInvalid)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := options.Authorize(ctx, Authorization{
		Role: options.Role, SignatureID: options.LockID, Metadata: s.metadata, SignerCertificate: bytes.Clone(signerDER),
	}); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %w", ErrAuthorization, err)
	}
	return ctx.Err()
}

func verifyAll(ctx context.Context, input []byte, s *snapshot, options sign.VerificationOptions) (map[string][]byte, error) {
	results, err := sign.Verify(ctx, input, options)
	if err != nil {
		return nil, err
	}
	if len(results) != len(s.signatures) {
		return nil, fmt.Errorf("%w: 验证结果与签名列表不一致", ErrInvalid)
	}
	signers := map[string][]byte{}
	invalidID, incompleteID := "", ""
	for _, item := range results {
		if _, err := s.selected(item.ID); err != nil {
			return nil, err
		}
		if item.Error != "" || item.Result == nil || !item.DigestValid || !item.Result.Valid {
			if item.Result != nil && item.Result.Validation != nil && item.Result.Validation.Status == "incomplete" && item.DigestValid {
				incompleteID = item.ID
			} else {
				invalidID = item.ID
			}
			continue
		}
		if !item.Result.FullyValid() {
			if item.Result.Validation != nil && item.Result.Validation.Status == "invalid" {
				invalidID = item.ID
			} else {
				incompleteID = item.ID
			}
			continue
		}
		if item.Result.Outer.Certificate == nil {
			invalidID = item.ID
			continue
		}
		signers[item.ID] = bytes.Clone(item.Result.Outer.Certificate.RawDER)
	}
	if invalidID != "" {
		return nil, fmt.Errorf("%w: 签名、摘要或完整结论未通过: %s", ErrInvalid, invalidID)
	}
	if incompleteID != "" {
		return nil, fmt.Errorf("%w: 完整验签未完成: %s", ErrEvidenceRequired, incompleteID)
	}
	return signers, ctx.Err()
}

// checkSignablePaths 在调用签名器前发现默认签署范围无法覆盖的散落文件。
func (s *snapshot) checkSignablePaths() error {
	known := map[string]bool{spec.RootDocument: true, s.listName: true}
	for _, record := range s.signatures {
		for _, artifact := range record.artifacts {
			known[artifact] = true
		}
	}
	directory := path.Dir(s.docRoot)
	for _, entry := range s.pkg.Entries() {
		if !entry.IsDir && creator.IsSignatureEntry(entry.Path) && !known[entry.Path] {
			return fmt.Errorf("%w: 签名目录含未登记的负载 %s", ErrInvalid, entry.Path)
		}
		if !entry.IsDir && !known[entry.Path] && !strings.HasPrefix(entry.Path, directory+"/") {
			return fmt.Errorf("%w: 当前默认签署范围无法覆盖 %s", ErrInvalid, entry.Path)
		}
	}
	return nil
}
