package ses_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/zc310/ofd/pkg/sign"
	"github.com/zc310/ofd/pkg/validator"
)

func signedOFD(t *testing.T, f *signingFixture, tamper bool, scope string) []byte {
	t.Helper()
	return signedOFDWithMethod(t, f, tamper, scope, "1.2.156.10197.1.501")
}

func signedOFDWithMethod(t *testing.T, f *signingFixture, tamper bool, scope, method string) []byte {
	t.Helper()
	document := []byte(`<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>10</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></Pages></Document>`)
	sum := sha1.Sum(document)
	f.xml = []byte(fmt.Sprintf(`<Signature xmlns="http://www.ofdspec.org/2016"><SignedInfo><Provider ProviderName="SES test"/><SignatureMethod>%s</SignatureMethod><References CheckMethod="SHA1"><Reference FileRef="/Doc_0/Document.xml"><CheckValue>%s</CheckValue></Reference></References></SignedInfo><SignedValue>SignedValue.dat</SignedValue></Signature>`, method, base64.StdEncoding.EncodeToString(sum[:])))
	value, err := sign.CreateSignedValue(f.xml, f.seal, f.signer.Raw, f.signerKey, scope, f.now, sign.SigningOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if tamper {
		document = bytes.ReplaceAll(document, []byte("<MaxUnitID>10"), []byte("<MaxUnitID>11"))
	}
	files := map[string][]byte{
		"OFD.xml":                          []byte(`<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>38540000000000000000000000000001</DocID></DocInfo><DocRoot>Doc_0/Document.xml</DocRoot><Signatures>Doc_0/Signatures.xml</Signatures></DocBody></OFD>`),
		"Doc_0/Document.xml":               document,
		"Doc_0/Pages/Page_0/Content.xml":   []byte(`<Page xmlns="http://www.ofdspec.org/2016"><Content><Layer ID="2"/></Content></Page>`),
		"Doc_0/Signatures.xml":             []byte(`<Signatures xmlns="http://www.ofdspec.org/2016"><MaxSignId>sign-1</MaxSignId><Signature ID="sign-1" Type="Seal" BaseLoc="Signatures/Signature.xml"/></Signatures>`),
		"Doc_0/Signatures/Signature.xml":   f.xml,
		"Doc_0/Signatures/SignedValue.dat": value,
	}
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for name, data := range files {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestOFDCompleteVerificationSeparatesReferenceSHA1AndSESDataHashSM3(t *testing.T) {
	f := newSigningFixture(t, false)
	for _, test := range []struct {
		name        string
		tamper      bool
		scope, want string
		method      string
	}{
		{"valid", false, "/Doc_0/Signatures/Signature.xml", "valid", "1.2.156.10197.1.501"},
		{"changed_reference", true, "/Doc_0/Signatures/Signature.xml", "invalid", "1.2.156.10197.1.501"},
		{"unbound_scope", false, "/Doc_0/OtherSignature.xml", "incomplete", "1.2.156.10197.1.501"},
		{"algorithm_mismatch", false, "/Doc_0/Signatures/Signature.xml", "invalid", "1.2.840.113549.1.1.11"},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := signedOFDWithMethod(t, f, test.tamper, test.scope, test.method)
			results, err := sign.Verify(t.Context(), data, *f.options)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || results[0].Error != "" || results[0].Result == nil {
				t.Fatalf("验证结果不完整: %+v", results)
			}
			if !results[0].Result.Valid || results[0].Result.Validation.Status != test.want {
				t.Fatalf("完整结果 = %+v", results[0].Result)
			}
			if results[0].DigestValid != !test.tamper {
				t.Fatalf("摘要匹配应独立于算法一致性与范围检查: %+v", results[0])
			}
			if results[0].Result.FullyValid() != (test.want == "valid") {
				t.Fatalf("完整验签判定错误: %+v", results[0].Result)
			}
			instance, err := validator.New(validator.WithMode(validator.ModeStructural), validator.WithSignatureVerification(f.options))
			if err != nil {
				t.Fatal(err)
			}
			report := instance.ValidateReader(t.Context(), bytes.NewReader(data), "signed.ofd")
			found := false
			for _, check := range report.Checks {
				if check.Name == "signatures" {
					found = true
					want := "passed"
					if test.want == "invalid" {
						want = "failed"
					}
					if test.want == "incomplete" {
						want = "incomplete"
					}
					if check.Status != want {
						t.Fatalf("校验器签章状态=%s，期望 %s：%+v", check.Status, want, report.Issues)
					}
				}
			}
			if !found {
				t.Fatal("校验器缺少完整签章检查")
			}
		})
	}
}

func TestOFDVerificationDistinguishesSM2HashErrorFromSignatureMismatch(t *testing.T) {
	f := newSigningFixture(t, false)
	data := signedOFD(t, f, false, "/Doc_0/Signatures/Signature.xml")
	for _, test := range []struct {
		name, message string
		uid           []byte
	}{
		{"default_uid", "", nil},
		{"wrong_uid", "SM2 签名验证失败", []byte("wrong-user-id")},
		{"oversized_uid", "计算 SM2 签名摘要失败: sm2: the uid is too long", make([]byte, 8192)},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := *f.options
			options.UID = test.uid
			results, err := sign.Verify(t.Context(), data, options)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || results[0].Error != "" || results[0].Result == nil || !results[0].DigestValid {
				t.Fatalf("应取得文档摘要匹配的验签结果: %+v", results)
			}
			result := results[0].Result
			wantValid := test.message == ""
			if result.Valid != wantValid || result.FullyValid() != wantValid ||
				result.Seal.Valid != wantValid || result.Outer.Valid != wantValid ||
				result.Seal.Error != test.message || result.Outer.Error != test.message {
				t.Fatalf("应区分摘要计算错误与签名不匹配: seal=%+v outer=%+v validation=%+v", result.Seal, result.Outer, result.Validation)
			}
			signatureChecks := 0
			for _, check := range result.Validation.Checks {
				if check.Code == "seal.signature" || check.Code == "signer.signature" {
					signatureChecks++
					if check.Message != test.message || (check.Status == "valid") != wantValid {
						t.Fatalf("完整结论丢失组件错误: %+v", check)
					}
				}
			}
			if signatureChecks != 2 {
				t.Fatalf("缺少两层签名检查: %+v", result.Validation)
			}
			if !wantValid && result.Validation.Status != "invalid" {
				t.Fatalf("失败不能成为完整通过或未完成: %+v", result.Validation)
			}
		})
	}
}
