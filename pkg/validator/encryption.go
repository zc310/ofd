package validator

import (
	"bytes"
	"context"
	"errors"
	"path"
	"strings"

	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/internal/crypt"
	"github.com/zc310/ofd/pkg/spec"
)

// validateEncryption 检查可见元数据和明文 XML，并将文档检查标记为未完成。
// 即使所有物理路径均存在，也不能将占位文件作为原始内容校验文档（GM/T 0099 C.1）。
func (v *Validator) validateEncryption(ctx context.Context, pkg *container.Package, archive *packageIndex, report *Report) bool {
	if !crypt.Present(pkg) {
		return false
	}
	mode := crypt.ModeStrict
	if v.opts.Mode != ModeStrict {
		mode = crypt.ModeCompat
	}
	opt := crypt.InspectOptions{Mode: mode}
	// 元数据检查有自身的有限默认预算；校验器的限制更严格时，优先采用更严格的值，
	// 防止 .dat 中的元数据绕过 XML 预算。
	if v.opts.MaxXMLBytes > 0 {
		opt.Limits.MaxMetadataBytes = min(v.opts.MaxXMLBytes, 4<<20)
	}
	if v.opts.MaxFileSize > 0 {
		if opt.Limits.MaxMetadataBytes == 0 {
			opt.Limits.MaxMetadataBytes = 4 << 20
		}
		opt.Limits.MaxMetadataBytes = min(opt.Limits.MaxMetadataBytes, v.opts.MaxFileSize)
	}
	if v.opts.MaxTotalSize > 0 {
		opt.Limits.MaxTotalMetadataBytes = min(v.opts.MaxTotalSize, 32<<20)
	}
	if v.opts.MaxEntries > 0 {
		opt.Limits.MaxEntries = v.opts.MaxEntries
	}
	if v.opts.MaxXMLNodes > 0 {
		opt.Limits.MaxXMLNodes = min(v.opts.MaxXMLNodes, 100000)
	}
	if v.opts.MaxXMLDepth > 0 {
		opt.Limits.MaxXMLDepth = min(v.opts.MaxXMLDepth, 64)
	}
	info, err := crypt.InspectPackage(ctx, pkg, opt)
	report.Encryption = info
	report.setCheck("encryption", "incomplete")
	if err != nil {
		code := "encryption.metadata_invalid"
		if errors.Is(err, crypt.ErrLimitExceeded) {
			code = "encryption.limit_exceeded"
		}
		if ctx.Err() != nil {
			code = "validation.canceled"
		}
		report.addIssue(Issue{Severity: SeverityError, Stage: StageContainer, Code: code, File: crypt.EntryPoint, Message: err.Error(), Clause: []string{"GM/T 0099-2020 C.2–C.4"}}, v.opts.MaxErrors)
		report.setCheck("encryption", "failed")
	} else {
		report.addIssue(Issue{Severity: SeverityInfo, Stage: StageContainer, Code: "encryption.locked", File: info.EntryPoint, Message: "OFD 文档已加密且未解锁；文档引用、语义和签名检查未完成", Clause: []string{"GM/T 0099-2020 7.3.5"}}, v.opts.MaxErrors)
		for _, diagnostic := range info.Diagnostics {
			report.addIssue(Issue{Severity: SeverityWarning, Stage: StageContainer, Code: diagnostic.Code, File: diagnostic.File, Message: diagnostic.Message}, v.opts.MaxErrors)
		}
	}
	for _, check := range []string{"xml", "references", "semantic"} {
		report.setCheck(check, "incomplete")
	}
	if v.opts.CheckXSD {
		report.setCheck("xsd", "incomplete")
	}
	if v.opts.CheckDigest {
		report.setCheck("digest", "incomplete")
	}
	if v.opts.SignatureVerification != nil {
		report.setCheck("signatures", "incomplete")
	}

	documents := map[string]*xmlDocument{}
	hidden := map[string]bool{}
	known := err == nil && info != nil && len(info.Operations) > 0
	if info != nil {
		hidden[info.EntryPoint] = true
		for _, op := range info.Operations {
			hidden[op.DecryptSeedLoc], hidden[op.EntriesMapLoc] = true, true
			if op.MapState != "plaintext" {
				known = false
			}
			for _, entry := range op.Entries {
				hidden[entry.Path], hidden[entry.EPath], hidden[entry.DecryptSeedLoc] = true, true, true
			}
		}
	}
	for _, name := range archive.sortedNames() {
		if ctx.Err() != nil {
			break
		}
		file := archive.files[name]
		if file.isDir || hidden[name] || !strings.EqualFold(path.Ext(name), ".xml") {
			continue
		}
		// 映射表不可读时，仅从内容看起来像 XML 的可见根文件中检查其声明的 profile。
		// 其他路径可能包含尚未识别的密文。
		if !known && (name != rootDocument || !bytes.HasPrefix(bytes.TrimSpace(file.data), []byte("<"))) {
			continue
		}
		expected := ""
		if name == rootDocument {
			expected = "OFD"
		}
		doc, _, _ := v.parseXML(file, expected, "", "", report)
		if doc != nil {
			documents[name] = doc
		}
	}
	if ctx.Err() != nil && err == nil {
		report.addIssue(Issue{Severity: SeverityError, Stage: StageContainer, Code: "validation.canceled", Message: ctx.Err().Error()}, v.opts.MaxErrors)
	}
	// 缺少原始文档时，只能判定 profile 中禁止加密的规则，
	// 不能据此认定其余 profile 规则通过。
	docType := v.profileDocType(documents)
	if v.opts.DocType != "" {
		docType = v.opts.DocType
	}
	if docType == spec.DocTypeOFDA || docType == spec.DocTypeOFDH {
		report.setProfile(docType)
		pc := &profileContext{docType: docType, documents: documents, archive: archive, report: report, maxErrors: v.opts.MaxErrors}
		checkNoEncryption(pc.withRule("encrypted", []string{"GB/T 42133 6.16"}))
		report.setCheck("profile", "failed")
	}
	return true
}
