package validator

import (
	"context"
	"path"
	"strings"

	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/spec"
)

type SignatureVerificationOptions = parser.SignatureVerificationOptions

// WithSignatureVerification 启用完整 SES 验证。缺少必需证据时报告 incomplete，
// 并作为未完成的必要检查使校验失败；不会将其误报为文档签名数学错误。
func WithSignatureVerification(options *SignatureVerificationOptions) Option {
	return func(o *Options) { o.SignatureVerification = options }
}

func (v *Validator) checkSignatures(ctx context.Context, documents map[string]*xmlDocument, pkg *container.Package, report *Report) {
	invalid, incomplete := false, false
	types := map[string]parser.Signature{}
	listFor := map[string]string{}
	byID := map[string]map[string]string{}
	for _, name := range sortedDocumentNames(documents) {
		if documents[name].rootName != "Signatures" {
			continue
		}
		var list parser.Signatures
		if err := pkg.ReadXML(name, &list); err != nil {
			continue
		}
		byID[name] = map[string]string{}
		for _, item := range list.Signatures {
			target := strings.TrimPrefix(item.BaseLoc.Resolve(models.StLoc(path.Dir(name))).String(), "/")
			types[target], listFor[target], byID[name][item.ID] = item, name, target
		}
	}
	results := map[string]*parser.SignatureVerificationResult{}
	for _, name := range sortedDocumentNames(documents) {
		if ctx.Err() != nil {
			report.addIssue(Issue{Severity: SeverityError, Stage: StageSignature, Code: "signature.canceled", Message: ctx.Err().Error()}, v.opts.MaxErrors)
			report.setCheck("signatures", "incomplete")
			return
		}
		doc := documents[name]
		if !doc.ofd || doc.rootName != "Signature" {
			continue
		}
		issue := func(code, message string) {
			report.addIssue(Issue{Severity: SeverityError, Stage: StageSignature, Code: code, File: name, Message: message}, v.opts.MaxErrors)
		}
		fail := func(code, message string) { invalid = true; issue(code, message) }
		var signature models.Signature
		if err := pkg.ReadXML(name, &signature); err != nil {
			fail("signature.xml", err.Error())
			continue
		}
		data, err := pkg.Read(signature.SignedValue.Resolve(models.StLoc(path.Dir(name))).String())
		if err != nil {
			fail("signature.value_missing", err.Error())
			continue
		}
		value := &parser.SignedValue{Raw: data, Format: "GB/T 35275 SignedData"}
		if types[name].Type != spec.SigTypeSign {
			value, err = parser.ParseSignedValue(data)
			if err != nil {
				fail("signature.value_invalid", err.Error())
				continue
			}
		}
		signature.Type, signature.Relative, signature.FilePath = types[name].Type, types[name].Relative, name
		result, err := parser.VerifyDocumentSignature(ctx, pkg, &signature, value, v.opts.SignatureVerification)
		if err != nil {
			incomplete = true
			issue("signature.unsupported", err.Error())
			continue
		}
		results[name] = result
		for _, check := range result.Validation.Checks {
			if check.Status == "invalid" {
				fail("signature."+check.Code, check.Message)
			}
			if check.Status == "incomplete" {
				incomplete = true
				issue("signature.incomplete."+check.Code, check.Message)
			}
		}
	}
	for name, item := range types {
		seen := map[string]bool{}
		for id := item.Relative; id != ""; {
			target := byID[listFor[name]][id]
			if target == "" || seen[id] || len(seen) >= 32 {
				invalid = true
				report.addIssue(Issue{Severity: SeverityError, Stage: StageSignature, Code: "signature.relative", File: name, Message: "Relative 悬空、循环或超过 32 层"}, v.opts.MaxErrors)
				break
			}
			seen[id] = true
			base := results[target]
			if base == nil || !base.FullyValid() {
				incomplete = true
				report.addIssue(Issue{Severity: SeverityError, Stage: StageSignature, Code: "signature.relative", File: name, Message: "基础签名未完整通过验证: " + id}, v.opts.MaxErrors)
			}
			id = types[target].Relative
		}
	}
	status := "passed"
	if incomplete {
		status = "incomplete"
	}
	if invalid {
		status = "failed"
	}
	report.setCheck("signatures", status)
}
