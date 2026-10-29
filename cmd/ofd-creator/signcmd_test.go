package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator/merge"
	"github.com/zc310/ofd/pkg/sign"
)

func TestSignatureReportIncludesCompleteValidationAndReasons(t *testing.T) {
	for _, test := range []struct {
		name, state, checkState, message, want string
	}{
		{"valid", "valid", "valid", "", "完整验签通过"},
		{"algorithm_mismatch", "invalid", "invalid", "签名算法不一致", "完整验签失败"},
		{"unbound_scope", "incomplete", "incomplete", "保护范围未确认", "完整验签未完成"},
		{"missing_trust", "incomplete", "incomplete", "未提供证书信任根", "完整验签未完成"},
		{"missing_validation", "", "", "", "完整验签未完成：缺少完整验签结果"},
	} {
		t.Run(test.name, func(t *testing.T) {
			status := merge.SignatureStatus{ID: "sign-1", DigestValid: true, Verified: true}
			if test.state != "" {
				status.Validation = &sign.SignatureValidation{
					Status: test.state,
					Checks: []sign.VerificationCheck{{Code: "document.integrity", Status: test.checkState, Message: test.message}},
				}
			}
			var report bytes.Buffer
			reportSignatureVerification(&report, "ofd-creator merge", []merge.SignatureStatus{status})
			for _, text := range []string{"摘要匹配", "两层密码学签名通过", test.want, test.message} {
				if !strings.Contains(report.String(), text) {
					t.Fatalf("缺少 %q: %s", text, report.String())
				}
			}
			if test.state != "valid" && strings.Contains(report.String(), "完整验签通过") {
				t.Fatalf("部分结果不应报告完整通过: %s", report.String())
			}
		})
	}
}

func TestSignatureReportIncludesVerificationError(t *testing.T) {
	var report bytes.Buffer
	reportSignatureVerification(&report, "ofd-creator replace", []merge.SignatureStatus{{
		ID: "sign-1", VerificationError: "不是可验证的 SES 签名值",
	}})
	for _, text := range []string{"摘要不匹配或未完成", "密码学签名校验失败", "完整验签未完成", "不是可验证的 SES 签名值"} {
		if !strings.Contains(report.String(), text) {
			t.Fatalf("缺少 %q: %s", text, report.String())
		}
	}
}

func TestOrdinaryDigitalSignatureReportUsesSingleSignatureMeaning(t *testing.T) {
	var report bytes.Buffer
	reportSignatureVerification(&report, "ofd-creator merge", []merge.SignatureStatus{{ID: "sign-1", Type: "Sign", DigestValid: true, Verified: true}})
	if !strings.Contains(report.String(), "数字签名通过") || strings.Contains(report.String(), "两层") {
		t.Fatalf("普通签名报告错误: %s", report.String())
	}
}
