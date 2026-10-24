package validator

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/internal/crypt"
	"github.com/zc310/ofd/internal/physicalsig"
	"github.com/zc310/ofd/pkg/spec"
)

// validatePhysicalView 在原包检查外签，在投影中检查逻辑文档，避免把密文引用
// 当成解密后的文件引用；任一范围的失败均保留在同一报告中。
func (v *Validator) validatePhysicalView(ctx context.Context, physical *container.Package, archive *packageIndex, logical *container.Package, report *Report) bool {
	limits := crypt.Limits{MaxInputBytes: v.opts.MaxInputSize, MaxEntryBytes: v.opts.MaxFileSize, MaxTotalBytes: v.opts.MaxTotalSize, MaxEntries: v.opts.MaxEntries}
	if v.opts.MaxXMLBytes > 0 {
		limits.MaxMetadataBytes = min(v.opts.MaxXMLBytes, 4<<20)
	}
	if v.opts.MaxTotalSize > 0 {
		limits.MaxTotalMetadataBytes = min(v.opts.MaxTotalSize, 32<<20)
	}
	fail := func(err error) {
		code, status := "signature.physical_invalid", "failed"
		if errors.Is(err, crypt.ErrUnsupported) {
			code, status = "signature.physical_unsupported", "incomplete"
		}
		if errors.Is(err, crypt.ErrLimitExceeded) || errors.Is(err, container.ErrReadLimitExceeded) {
			code, status = "signature.physical_limit_exceeded", "incomplete"
		}
		if ctx.Err() != nil {
			code, status = "signature.canceled", "incomplete"
		}
		report.addIssue(Issue{Severity: SeverityError, Stage: StageSignature, Code: code, Message: err.Error()}, v.opts.MaxErrors)
		report.setCheck("signatures", status)
	}
	graph, err := physicalsig.Discover(ctx, physical, limits)
	if err != nil {
		fail(err)
		return true
	}
	if graph == nil {
		return false
	}
	options := SignatureVerificationOptions{CurrentTime: time.Now()}
	if v.opts.SignatureVerification != nil {
		options = *v.opts.SignatureVerification
		if options.CurrentTime.IsZero() {
			options.CurrentTime = time.Now()
		}
	}
	// 同时记录签名值的原字节指纹，投影时检查解密没有覆盖外签元数据。
	verified, err := physicalsig.Verify(ctx, physical, graph, &options, limits)
	if err != nil {
		fail(err)
		return true
	}
	projected, err := physicalsig.LogicalPackage(ctx, logical, graph)
	if err != nil {
		fail(err)
		return true
	}
	defer projected.Close()
	v.validatePackage(ctx, projected, report)
	report.Summary.Files += len(graph.Metadata)

	// 原始主入口也单独校验；其 DocRoot 在逻辑范围检查，列表与外签引用在物理范围检查。
	documents := map[string]*xmlDocument{}
	roots := map[string]string{spec.RootDocument: "OFD"}
	for _, record := range graph.Records {
		roots[record.List], roots[record.Signature.FilePath] = "Signatures", "Signature"
	}
	names := make([]string, 0, len(roots))
	for name := range roots {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if ctx.Err() != nil {
			fail(ctx.Err())
			return true
		}
		doc, refs, valid := v.parseXML(archive.files[name], roots[name], "", "", report)
		if doc != nil {
			documents[name] = doc
		}
		if !valid || name == spec.RootDocument {
			continue
		}
		for _, ref := range refs {
			target, err := resolvePackagePath(ref.base, ref.value)
			if err != nil {
				report.addIssue(issueAt(ref.node, SeverityError, StageReference, "reference.path_escape", err.Error(), name), v.opts.MaxErrors)
			} else if entry, exists := archive.get(target); !exists || entry.isDir {
				report.addIssue(issueAt(ref.node, SeverityError, StageReference, "reference.missing", fmt.Sprintf("物理签名引用的文件不存在：%s", target), name), v.opts.MaxErrors)
			}
		}
	}
	if report.hasStageErrors(StageReference) {
		report.setCheck("references", "failed")
	}
	if v.opts.CheckDigest {
		v.checkDigests(documents, archive, report)
		if report.hasStageErrors(StageDigest) {
			report.setCheck("digest", "failed")
		}
	}
	if v.opts.SignatureVerification != nil {
		for _, item := range verified {
			for _, check := range item.Result.Validation.Checks {
				if check.Status != "invalid" && check.Status != "incomplete" {
					continue
				}
				code, status := "signature.physical."+check.Code, "failed"
				if check.Status == "incomplete" {
					code, status = "signature.incomplete.physical."+check.Code, "incomplete"
				}
				report.addIssue(Issue{Severity: SeverityError, Stage: StageSignature, Code: code, File: item.Record.Signature.FilePath, Message: check.Message}, v.opts.MaxErrors)
				report.setCheck("signatures", status)
			}
		}
	}
	return true
}
