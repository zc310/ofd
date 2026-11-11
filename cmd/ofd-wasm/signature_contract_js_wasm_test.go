//go:build js && wasm

package main

import (
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/reader"
)

func TestSignatureSharedContractPreservesBrowserFields(t *testing.T) {
	value := signaturesValue([]reader.SignatureInfo{{Scope: 2, ID: "s1", HasDigest: true, DigestValid: false,
		Validation:   &parser.SignatureValidation{Status: "incomplete", Checks: []parser.VerificationCheck{{Code: "certificates.trust", Status: "incomplete"}}},
		Stamps:       []reader.SignatureStamp{{ID: "a", Page: -1, HasSeal: true, SealType: "png", Boundary: &reader.AnnotationBoundary{X: 1, Width: 3}}},
		Certificates: []reader.CertificateDetail{{SlotKey: "outer", Subject: "证书主体", TrustChecked: false}},
	}})
	info := value.Index(0)
	if info.Get("scope").Int() != 2 || info.Get("id").String() != "s1" || !info.Get("has_digest").Bool() || info.Get("digest_valid").Bool() {
		t.Fatal("签名基本字段错误")
	}
	stamp := info.Get("stamps").Index(0)
	if stamp.Get("boundary").Get("x").Int() != 1 || !stamp.Get("visible_boundary").IsNull() || stamp.Get("page").Int() != -1 || !stamp.Get("has_seal").Bool() {
		t.Fatal("印章边界、未知页码或预览字段错误")
	}
	check := info.Get("validation").Get("checks").Index(0)
	if check.Get("status").String() != "incomplete" || check.Get("message").String() != "" || check.Get("message").IsUndefined() {
		t.Fatal("验证明细或空 message 字段丢失")
	}
	cert := info.Get("certificates").Index(0)
	if cert.Get("slot_key").String() != "outer" || cert.Get("subject").String() != "证书主体" || cert.Get("trust_checked").Bool() {
		t.Fatal("证书字段错误")
	}
	if info.Get("references").Length() != 0 {
		t.Fatal("空引用清单必须是数组")
	}
	empty := signaturesValue(nil)
	if empty.Length() != 0 {
		t.Fatal("空签名清单必须是数组")
	}
	missing := signaturesValue([]reader.SignatureInfo{{}, {Validation: &parser.SignatureValidation{Status: "incomplete"}}})
	if !missing.Index(0).Get("validation").IsUndefined() || missing.Index(1).Get("validation").Get("checks").Length() != 0 {
		t.Fatal("未执行验证与空检查列表不能混淆")
	}
}
