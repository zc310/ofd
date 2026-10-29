package parser

import (
	"bytes"
	"context"
	"testing"
)

func legacyNumericSignaturesPackage(t *testing.T, entries string) []byte {
	t.Helper()
	signature := `<Signature xmlns="http://www.ofdspec.org/2016"><SignedInfo><Provider ProviderName="legacy" Version="1"/><SignatureMethod>1.2.156.10197.1.501</SignatureMethod><References CheckMethod="SM3"/></SignedInfo><SignedValue>SignedValue.dat</SignedValue></Signature>`
	return signaturesPackage(t, map[string]string{
		"Doc_0/Signs/Signatures.xml":         `<Signatures xmlns="http://www.ofdspec.org/2016">` + entries + `</Signatures>`,
		"Doc_0/Signs/Sign_0/Signature.xml":   signature,
		"Doc_0/Signs/Sign_0/SignedValue.dat": "invalid-signed-data",
	})
}

// GB/T 33190—2016 §18.1 表66 要求 xs:ID；真实旧文档的数字 ID 在阅读时
// 按原字符串保留，不改写签名索引或签名数据。签名验证错误仍单独保留。
func TestLegacyNumericSignatureIDsDoNotBlockPageLoading(t *testing.T) {
	data := legacyNumericSignaturesPackage(t, `<Signature ID="1" Type="Sign" BaseLoc="Sign_0/Signature.xml"/><Signature ID="01" Type="Sign" BaseLoc="Sign_0/Signature.xml"/>`)
	parsed, err := NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	defer parsed.Close()
	doc := parsed.Documents[0]
	if err := doc.Pages[0].EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"1", "01"} {
		if doc.GetSignature(id) == nil {
			t.Fatalf("未保留原始签名 ID %q", id)
		}
		value := doc.GetSignedValue(id)
		if value == nil || !bytes.Equal(value.Raw, []byte("invalid-signed-data")) {
			t.Fatal("兼容读取改写了签名值")
		}
		stored := doc.GetVerificationResult(id)
		if doc.GetVerificationError(id) == nil && (stored == nil || stored.FullyValid()) {
			t.Fatal("签名验证失败结果未保留")
		}
		if result, err := doc.VerifySignature(context.Background(), id, nil); err == nil && (result == nil || result.FullyValid()) {
			t.Fatal("无效签名被兼容读取误当作验证通过")
		}
	}
}

func TestLegacySignatureCompatibilityKeepsIDAndRelativeChecks(t *testing.T) {
	for _, tc := range []struct {
		name, entries string
	}{
		{"空标识", `<Signature ID="" Type="Sign" BaseLoc="Sign_0/Signature.xml"/>`},
		{"重复数字标识", `<Signature ID="1" Type="Sign" BaseLoc="Sign_0/Signature.xml"/><Signature ID="1" Type="Sign" BaseLoc="Sign_0/Signature.xml"/>`},
		{"数字后混入字母", `<Signature ID="1a" Type="Sign" BaseLoc="Sign_0/Signature.xml"/>`},
		{"Relative悬空", `<Signature ID="1" Type="Sign" BaseLoc="Sign_0/Signature.xml" Relative="2"/>`},
		{"Relative循环", `<Signature ID="1" Type="Sign" BaseLoc="Sign_0/Signature.xml" Relative="1"/>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := NewOFD(legacyNumericSignaturesPackage(t, tc.entries))
			if parsed != nil {
				defer parsed.Close()
			}
			if err == nil {
				t.Fatal("兼容读取跳过了标识或 Relative 检查")
			}
		})
	}
}
