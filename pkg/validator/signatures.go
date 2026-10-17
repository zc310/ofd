package validator

import (
	"context"
	"path"

	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

type SignatureVerificationOptions = parser.SignatureVerificationOptions

// WithSignatureVerification 启用完整 SES 验证。缺少必需证据时报告 incomplete，
// 并作为未完成的必要检查使校验失败；不会将其误报为文档签名数学错误。
func WithSignatureVerification(options *SignatureVerificationOptions) Option {
	return func(o *Options) { o.SignatureVerification = options }
}

func (v *Validator) checkSignatures(ctx context.Context, documents map[string]*xmlDocument, pkg *core.Package, report *Report) {
	invalid, incomplete := false, false
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
		value, err := parser.ParseSignedValue(data)
		if err != nil {
			fail("signature.value_invalid", err.Error())
			continue
		}
		result, err := parser.VerifySESSignedValueWithOptions(value, v.opts.SignatureVerification)
		if err != nil {
			incomplete = true
			issue("signature.unsupported", err.Error())
			continue
		}
		digest, err := parser.VerifySignatureDigest(pkg, name, &signature, value)
		if err != nil {
			fail("signature.digest", err.Error())
			continue
		}
		parser.BindSignatureDigest(result, digest)
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
	status := "passed"
	if incomplete {
		status = "incomplete"
	}
	if invalid {
		status = "failed"
	}
	report.setCheck("signatures", status)
}
