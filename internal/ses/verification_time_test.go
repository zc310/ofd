package ses_test

import (
	"crypto/rand"
	"math/big"
	"testing"
	"time"

	"github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/ses"
	"github.com/zc310/ofd/pkg/sign"
)

func TestSESFutureSigningTimeCannotPassCompleteVerification(t *testing.T) {
	future := time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC)
	f := newSigningFixtureAt(t, false, future)
	data := signedOFD(t, f, false, "/Doc_0/Signatures/Signature.xml")
	for _, explicitClock := range []bool{false, true} {
		name := "system_clock"
		if explicitClock {
			name = "explicit_clock"
		}
		t.Run(name, func(t *testing.T) {
			options := *f.options
			options.CurrentTime = time.Time{}
			if explicitClock {
				options.CurrentTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			}
			// 即使组件诊断被显式配置为按未来时刻验证，也不能改变完整验证时钟。
			trust := *options.Trust
			trust.CurrentTime = future
			options.Trust = &trust
			revocation := *options.Revocation
			revocation.CurrentTime = future
			options.Revocation = &revocation
			results, err := sign.Verify(t.Context(), data, options)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || results[0].Error != "" || results[0].Result == nil {
				t.Fatalf("未取得完整结果: %+v", results)
			}
			r := results[0].Result
			if !results[0].DigestValid || !r.Valid || !r.Trusted || !r.RevocationValid ||
				!r.Seal.CertificateValid || !r.Outer.CertificateValid {
				t.Fatalf("测试应具备正确的摘要、数学签名与签章时刻证书: %+v", r)
			}
			if r.FullyValid() || r.Validation.Status != "invalid" {
				t.Fatalf("未来签章不能通过完整验证: %+v", r.Validation)
			}
			assertVerificationCheck(t, r, "signing.time", "invalid")
		})
	}
}

func TestSESVerificationClockPreservesHistoricalCertificateValidation(t *testing.T) {
	signingTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	now := signingTime.AddDate(6, 0, 0)
	f := newSigningFixtureAt(t, false, signingTime)
	data := signedOFD(t, f, false, "/Doc_0/Signatures/Signature.xml")
	// 当前证据可以在证书过期之后发布，历史有效性仍按实际签章时刻判定（§7.3 f）。
	crl, err := smx509.CreateRevocationList(rand.Reader, &smx509.RevocationList{
		Number: big.NewInt(2), ThisUpdate: now.Add(-time.Hour), NextUpdate: now.Add(time.Hour),
	}, f.root, f.rootKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"default", "trust_override", "revocation_override"} {
		t.Run(mode, func(t *testing.T) {
			options := *f.options
			options.CurrentTime = now
			trust := *options.Trust
			options.Trust = &trust
			options.Revocation = &parser.CertificateRevocationOptions{
				CRLs: [][]byte{crl}, Issuers: [][]byte{f.root.Raw},
			}
			switch mode {
			case "trust_override":
				options.Trust.CurrentTime = now
			case "revocation_override":
				options.Revocation.CurrentTime = signingTime
			}
			results, err := sign.Verify(t.Context(), data, options)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || results[0].Result == nil {
				t.Fatalf("未取得验证结果: %+v", results)
			}
			r := results[0].Result
			if !r.FullyValid() || !r.VerificationTime.Equal(signingTime) ||
				!r.Seal.CertificateValid || !r.Outer.CertificateValid {
				t.Fatalf("历史签章应按签章时间与当前 CRL 验证: %+v", r)
			}
			if r.Trusted != (mode != "trust_override") || r.RevocationValid != (mode != "revocation_override") {
				t.Fatalf("组件时间覆盖不应改变完整策略的时间: %+v", r)
			}
			assertVerificationCheck(t, r, "signing.time", "valid")
			assertVerificationCheck(t, r, "certificates.trust", "valid")
			assertVerificationCheck(t, r, "certificates.revocation", "valid")
		})
	}
}

func TestSESFutureSigningBoundaryUsesCallerClock(t *testing.T) {
	f := newSigningFixtureAt(t, false, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	data := signedOFD(t, f, false, "/Doc_0/Signatures/Signature.xml")
	for _, test := range []struct {
		name string
		now  time.Time
		want string
	}{
		{"before_signing", f.now.Add(-time.Second), "invalid"},
		{"at_signing", f.now, "valid"},
		{"after_signing", f.now.Add(time.Second), "valid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := *f.options
			options.CurrentTime = test.now
			results, err := sign.Verify(t.Context(), data, options)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || results[0].Result == nil {
				t.Fatalf("未取得验证结果: %+v", results)
			}
			r := results[0].Result
			assertVerificationCheck(t, r, "signing.time", test.want)
			if r.FullyValid() != (test.want == "valid") {
				t.Fatalf("未来时间边界判定错误: %+v", r.Validation)
			}
		})
	}
}

func TestSESRejectsEmptySigningTime(t *testing.T) {
	f := newSigningFixtureAt(t, false, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	// 空时间无法查询历史印章状态；验证仍须保留已确认的时间错误。
	f.options.SealStatus = nil
	data := resignValue(t, f, func(v *ses.SignedValue) { v.TBS.SignTime = time.Time{} })
	value, err := parser.ParseSignedValue(data)
	if err != nil {
		t.Fatal(err)
	}
	result, err := parser.VerifySESSignedValueWithOptions(value, f.options)
	if err != nil {
		t.Fatal(err)
	}
	assertVerificationCheck(t, result, "signing.time", "invalid")
	if result.FullyValid() || result.Validation.Status != "invalid" {
		t.Fatalf("空签章时间不能通过: %+v", result.Validation)
	}
}

func TestSESTimestampUsesVerificationClock(t *testing.T) {
	f := newSigningFixtureAt(t, false, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	data, err := ses.BuildSignedValueWithOptions(f.xml, f.seal, f.signer.Raw, f.signerKey,
		"/Doc_0/Signatures/Signature.xml", f.now, ses.SigningOptions{
			Timestamp: func(signature []byte) ([]byte, error) {
				return makeTimestamp(t, f, signature, false), nil
			},
		})
	if err != nil {
		t.Fatal(err)
	}
	value, err := parser.ParseSignedValue(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		offset time.Duration
		want   string
	}{
		{"future_timestamp", 0, "invalid"},
		{"at_timestamp", time.Second, "valid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := *f.options
			options.CurrentTime = f.now.Add(test.offset)
			result, err := parser.VerifySESSignedValueWithOptions(value, &options)
			if err != nil {
				t.Fatal(err)
			}
			assertVerificationCheck(t, result, "signing.time", "valid")
			assertVerificationCheck(t, result, "timestamp", test.want)
			if test.want == "invalid" && (result.FullyValid() || result.Validation.Status != "invalid") {
				t.Fatalf("未来时间戳不能通过: %+v", result.Validation)
			}
		})
	}
}

func assertVerificationCheck(t *testing.T, result *parser.SignatureVerificationResult, code, status string) {
	t.Helper()
	for _, check := range result.Validation.Checks {
		if check.Code == code {
			if check.Status != status {
				t.Fatalf("%s = %+v，期望 %s", code, check, status)
			}
			return
		}
	}
	t.Fatalf("缺少检查 %s: %+v", code, result.Validation)
}
