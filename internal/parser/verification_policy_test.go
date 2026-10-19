package parser

import (
	"strings"
	"testing"
)

func TestBindSignatureDigestConfirmedFailurePrecedesIncompleteEvidence(t *testing.T) {
	for _, test := range []struct {
		name, want, message string
		change              func(*SignatureDigestResult)
	}{
		{"valid", "valid", "", func(*SignatureDigestResult) {}},
		{"unbound_scope", "incomplete", "范围未确认", func(d *SignatureDigestResult) {
			d.DataHash.ScopeMatch = false
		}},
		{"hash_mismatch_and_unbound_scope", "invalid", "SES 原文摘要", func(d *SignatureDigestResult) {
			d.Valid, d.DataHash.Match, d.DataHash.ScopeMatch = false, false, false
			d.DataHash.Actual = []byte("changed XML")
		}},
		{"unreadable_xml_and_unbound_scope", "invalid", "SES 原文摘要", func(d *SignatureDigestResult) {
			d.Valid, d.DataHash.Match, d.DataHash.ScopeMatch = false, false, false
			d.DataHash.Error = "文件不存在"
		}},
		{"unsupported_reference_algorithm", "incomplete", "未支持算法", func(d *SignatureDigestResult) {
			d.Valid = false
			d.Unsupported = "未支持算法"
			d.References[0] = ReferenceDigestResult{Error: d.Unsupported}
			d.DataHash = &DataHashResult{Error: d.Unsupported}
		}},
		{"unsupported_ses_algorithm", "incomplete", "未支持算法", func(d *SignatureDigestResult) {
			d.Valid = false
			d.Unsupported = "未支持算法"
			d.DataHash = &DataHashResult{Error: d.Unsupported}
		}},
		{"method_mismatch_and_unsupported_algorithm", "invalid", "算法不一致", func(d *SignatureDigestResult) {
			d.Unsupported = "未支持算法"
			d.SignatureMethodError = "算法不一致"
		}},
		{"reference_mismatch_and_unsupported_ses", "invalid", "OFD 引用摘要", func(d *SignatureDigestResult) {
			d.Valid = false
			d.Unsupported = "未支持算法"
			d.References[0] = ReferenceDigestResult{Actual: []byte("changed document")}
			d.DataHash = &DataHashResult{Error: d.Unsupported}
		}},
		{"hash_mismatch_and_unsupported_algorithm", "invalid", "SES 原文摘要", func(d *SignatureDigestResult) {
			d.Valid, d.DataHash.Match = false, false
			d.Unsupported = "未支持算法"
			d.DataHash.Actual = []byte("changed XML")
		}},
		{"missing_data_hash", "incomplete", "缺少 SES", func(d *SignatureDigestResult) {
			d.DataHash = nil
		}},
		{"reference_mismatch_and_missing_data_hash", "invalid", "OFD 引用摘要", func(d *SignatureDigestResult) {
			d.References[0].Match = false
			d.DataHash = nil
		}},
		{"empty_references", "invalid", "不含任何文件引用", func(d *SignatureDigestResult) {
			d.References = nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			digest := &SignatureDigestResult{
				Valid: true, References: []ReferenceDigestResult{{Match: true}},
				DataHash: &DataHashResult{Match: true, ScopeMatch: true, ScopeError: "范围未确认"},
			}
			test.change(digest)
			result := &SignatureVerificationResult{Valid: true, Validation: &SignatureValidation{}}
			BindSignatureDigest(result, digest)
			if result.Validation.Status != test.want || len(result.Validation.Checks) != 1 ||
				!strings.Contains(result.Validation.Checks[0].Message, test.message) {
				t.Fatalf("完整结论 = %+v，期望 %s / %s", result.Validation, test.want, test.message)
			}
			if result.FullyValid() != (test.want == "valid") || !result.Valid {
				t.Fatalf("完整判定与两层签名结果混淆: %+v", result)
			}
		})
	}
}

func TestFullyValidRejectsMissingValidation(t *testing.T) {
	for _, result := range []*SignatureVerificationResult{nil, {}, {Valid: true}} {
		if result.FullyValid() {
			t.Fatalf("缺少完整验证结果不能通过: %+v", result)
		}
	}
	result := &SignatureVerificationResult{Valid: true, Validation: &SignatureValidation{}}
	BindSignatureDigest(result, nil)
	if result.FullyValid() || result.Validation.Status != "incomplete" {
		t.Fatalf("缺少摘要结果不能通过: %+v", result)
	}
}
