package contract

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/reader"
)

func TestSignaturesPreservesVerificationAndEmptyLists(t *testing.T) {
	data := Signatures([]reader.SignatureInfo{{ID: "s", HasDigest: true, DigestValid: false,
		HasVerification: true, Verified: true, TrustChecked: false,
		Validation:   &parser.SignatureValidation{Status: "incomplete", Checks: []parser.VerificationCheck{{Code: "certificates.trust", Status: "incomplete"}}},
		Certificates: []reader.CertificateDetail{{Subject: "测试", TrustChecked: false}},
	}})
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{`"has_digest":true`, `"digest_valid":false`, `"verified":true`, `"trust_checked":false`, `"status":"incomplete"`, `"subject":"测试"`, `"stamps":[]`, `"references":[]`} {
		if !strings.Contains(string(encoded), text) {
			t.Fatalf("缺少 %s: %s", text, encoded)
		}
	}
	if !strings.Contains(string(encoded), `"message":""`) {
		t.Fatalf("浏览器已有的空 message 字段不能省略: %s", encoded)
	}
	if stringData, err := json.Marshal(Signatures(nil)); err != nil || string(stringData) != "[]" {
		t.Fatalf("空签名 = %s, %v", stringData, err)
	}
}
