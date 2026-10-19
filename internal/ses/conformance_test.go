package ses_test

import (
	"bytes"
	"crypto/rand"
	"crypto/x509/pkix"
	"encoding/asn1"
	"math/big"
	"testing"
	"time"

	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/ses"
)

type signingFixture struct {
	now                          time.Time
	root, maker, signer          *smx509.Certificate
	rootKey, makerKey, signerKey *sm2.PrivateKey
	seal                         *ses.Seal
	xml                          []byte
	value                        []byte
	options                      *parser.SignatureVerificationOptions
}

func newSigningFixture(t *testing.T, digests bool) *signingFixture {
	t.Helper()
	return newSigningFixtureAt(t, digests, time.Now().UTC().Truncate(time.Second))
}

func newSigningFixtureAt(t *testing.T, digests bool, at time.Time) *signingFixture {
	t.Helper()
	f := &signingFixture{now: at, xml: []byte("<Signature>protected</Signature>")}
	issue := func(serial int64, parent *smx509.Certificate, parentKey *sm2.PrivateKey) (*smx509.Certificate, *sm2.PrivateKey) {
		t.Helper()
		key, err := sm2.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		template := &smx509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: "SES test " + big.NewInt(serial).String()}, NotBefore: f.now.Add(-time.Hour), NotAfter: f.now.Add(time.Hour), BasicConstraintsValid: true, KeyUsage: smx509.KeyUsageDigitalSignature}
		if parent == nil {
			template.IsCA = true
			template.KeyUsage |= smx509.KeyUsageCertSign | smx509.KeyUsageCRLSign
			parent = template
			parentKey = key
		}
		data, err := smx509.CreateCertificate(rand.Reader, template, parent, &key.PublicKey, parentKey)
		if err != nil {
			t.Fatal(err)
		}
		cert, err := smx509.ParseCertificate(data)
		if err != nil {
			t.Fatal(err)
		}
		return cert, key
	}
	f.root, f.rootKey = issue(1, nil, nil)
	f.maker, f.makerKey = issue(2, f.root, f.rootKey)
	f.signer, f.signerKey = issue(3, f.root, f.rootKey)
	params := ses.SealParams{Provider: "SES/test", ESID: "seal@test", Name: "ASCII seal", PictureType: "png", PictureData: makeTestSealPNG(t), Width: 40, Height: 30, ValidFrom: f.now.Add(-time.Minute), ValidTo: f.now.Add(time.Hour), SignerCertificates: [][]byte{f.signer.Raw}, Extensions: []ses.Extension{{OID: asn1.ObjectIdentifier{1, 2, 3, 4}, Value: []byte("optional")}}}
	if digests {
		digest, err := ses.CertificateHash("SM3", f.signer.Raw)
		if err != nil {
			t.Fatal(err)
		}
		params.SignerCertificates = nil
		params.SignerDigests = []ses.CertificateDigest{{Type: "SM3", Value: digest}}
	}
	var err error
	f.seal, err = ses.BuildSeal(params, f.maker.Raw, f.makerKey, f.now)
	if err != nil {
		t.Fatal(err)
	}
	f.value, err = ses.BuildSignedValueWithOptions(f.xml, f.seal, f.signer.Raw, f.signerKey, "/Doc_0/Signatures/Signature.xml", f.now, ses.SigningOptions{Extensions: params.Extensions})
	if err != nil {
		t.Fatal(err)
	}
	roots := smx509.NewCertPool()
	roots.AddCert(f.root)
	crl, err := smx509.CreateRevocationList(rand.Reader, &smx509.RevocationList{Number: big.NewInt(1), ThisUpdate: f.now.Add(-time.Hour), NextUpdate: f.now.Add(time.Hour)}, f.root, f.rootKey)
	if err != nil {
		t.Fatal(err)
	}
	f.options = &parser.SignatureVerificationOptions{CurrentTime: f.now, Trust: &parser.CertificateTrustOptions{Roots: roots}, Revocation: &parser.CertificateRevocationOptions{CRLs: [][]byte{crl}, Issuers: [][]byte{f.root.Raw}}, SealStatus: func(id string, at time.Time) (string, error) {
		if id != "seal@test" || at.IsZero() {
			t.Errorf("wrong seal query: %s %v", id, at)
		}
		return "good", nil
	}}
	return f
}

func TestSESV4DifferentMakerAndAuthorizedSigner(t *testing.T) {
	for _, digestList := range []bool{false, true} {
		name := "certificates"
		if digestList {
			name = "certificate_digests"
		}
		t.Run(name, func(t *testing.T) {
			f := newSigningFixture(t, digestList)
			value, err := parser.ParseSignedValue(f.value)
			if err != nil {
				t.Fatal(err)
			}
			if value.SES.TBS.Seal.SealInfo.Property.Name != "ASCII seal" || len(value.SES.TBS.Extensions) != 1 || value.SES.TBS.Extensions[0].Critical {
				t.Fatal("ASCII UTF8String、[0] 扩展或 DEFAULT FALSE 编码不正确")
			}
			if value.SES.TBS.Seal.SealInfo.Picture.Width != 40 {
				t.Fatal("毫米尺寸被像素尺寸覆盖")
			}
			result, err := parser.VerifySESSignedValueWithOptions(value, f.options)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Valid || result.Validation.Status != "incomplete" {
				t.Fatalf("未绑定原文前结果 = %+v", result)
			}
			parser.BindSignatureDigest(result, &parser.SignatureDigestResult{Valid: true, References: []parser.ReferenceDigestResult{{Match: true}}, DataHash: &parser.DataHashResult{Match: true, ScopeMatch: true}})
			if result.Validation.Status != "valid" {
				t.Fatalf("完整验证未通过: %+v", result.Validation)
			}
			result, err = parser.VerifySESSignedValue(value)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Valid || result.Validation.Status != "incomplete" {
				t.Fatal("缺少信任与状态证据不应得到完整通过结论")
			}
		})
	}
}

func resignValue(t *testing.T, f *signingFixture, change func(*ses.SignedValue)) []byte {
	t.Helper()
	var value ses.SignedValue
	if rest, err := asn1.Unmarshal(f.value, &value); err != nil || len(rest) != 0 {
		t.Fatal(err)
	}
	change(&value)
	sign := func(data any, key *sm2.PrivateKey) asn1.BitString {
		t.Helper()
		der, err := asn1.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		sig, err := key.Sign(rand.Reader, der, sm2.NewSM2SignerOption(true, nil))
		if err != nil {
			t.Fatal(err)
		}
		return asn1.BitString{Bytes: sig, BitLength: len(sig) * 8}
	}
	value.TBS.Seal.Signature = sign(value.TBS.Seal.SealInfo, f.makerKey)
	value.Signature = sign(value.TBS, f.signerKey)
	der, err := asn1.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func TestSESCompleteValidationRejectsCryptographicallyValidViolations(t *testing.T) {
	f := newSigningFixture(t, false)
	for _, test := range []struct {
		name, code string
		change     func(*ses.SignedValue)
	}{
		{"unauthorized_signer", "signer.authorization", func(v *ses.SignedValue) {
			data, _ := asn1.Marshal([][]byte{f.maker.Raw})
			v.TBS.Seal.SealInfo.Property.CertList = asn1.RawValue{FullBytes: data}
		}},
		{"expired_seal", "seal.validity", func(v *ses.SignedValue) { v.TBS.Seal.SealInfo.Property.ValidTo = f.now.Add(-time.Second) }},
		{"future_signature", "signing.time", func(v *ses.SignedValue) { v.TBS.SignTime = f.now.Add(time.Minute) }},
		{"wrong_version", "ses.format", func(v *ses.SignedValue) { v.TBS.Version = 3 }},
		{"wrong_header", "ses.format", func(v *ses.SignedValue) { v.TBS.Seal.SealInfo.Header.ID = "XX" }},
		{"empty_image_size", "ses.format", func(v *ses.SignedValue) { v.TBS.Seal.SealInfo.Picture.Width = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			value, err := parser.ParseSignedValue(resignValue(t, f, test.change))
			if err != nil {
				t.Fatal(err)
			}
			result, err := parser.VerifySESSignedValueWithOptions(value, f.options)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Valid {
				t.Fatalf("测试应保留正确的两层数学签名: %+v", result)
			}
			found := false
			for _, check := range result.Validation.Checks {
				if check.Code == test.code && check.Status == "invalid" {
					found = true
				}
			}
			if !found || result.Validation.Status != "invalid" {
				t.Fatalf("未拒绝标准违规: %+v", result.Validation)
			}
		})
	}
}

func TestSESSigningRejectsUnmatchedKeysAndUnauthorizedSigners(t *testing.T) {
	f := newSigningFixture(t, false)
	if _, err := ses.BuildSignedValue(f.xml, f.seal, f.maker.Raw, f.makerKey, "/Signature.xml", f.now); err == nil {
		t.Fatal("未授权制章者不能使用该印章签章")
	}
	if _, err := ses.BuildSignedValue(f.xml, f.seal, f.signer.Raw, f.makerKey, "/Signature.xml", f.now); err == nil {
		t.Fatal("不匹配的证书与私钥不应签署")
	}
	before, _ := f.seal.MarshalDER()
	if _, err := ses.BuildSignedValue(f.xml, f.seal, f.signer.Raw, f.signerKey, "/Signature.xml", f.now); err != nil {
		t.Fatal(err)
	}
	after, _ := f.seal.MarshalDER()
	if !bytes.Equal(before, after) {
		t.Fatal("签章修改了共享印章")
	}
}

func TestSESV4OptionalTimestampIsRecognized(t *testing.T) {
	f := newSigningFixture(t, false)
	data := resignValue(t, f, func(v *ses.SignedValue) { v.TimeStamp = asn1.BitString{Bytes: []byte{0x30, 0}, BitLength: 16} })
	value, err := parser.ParseSignedValue(data)
	if err != nil {
		t.Fatal(err)
	}
	if value.SES == nil || value.SES.TimeStamp == nil {
		t.Fatal("带 [0] 时间戳的合法 SES 外层结构未识别")
	}
	result, err := parser.VerifySESSignedValue(value)
	if err != nil {
		t.Fatal(err)
	}
	if result.Validation.Status != "invalid" {
		t.Fatal("非法时间戳不能被忽略")
	}
}

func TestSESRevocationUsesCurrentEvidenceAndHistoricalSigningTime(t *testing.T) {
	f := newSigningFixture(t, false)
	value, err := parser.ParseSignedValue(f.value)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, want string
		revokedAt  time.Time
		expired    bool
	}{
		{"not_revoked", "valid", time.Time{}, false},
		{"revoked_before_signing", "invalid", f.now.Add(-time.Second), false},
		{"revoked_after_signing", "valid", f.now.Add(time.Second), false},
		{"expired_evidence", "incomplete", time.Time{}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			crl := &smx509.RevocationList{Number: big.NewInt(2), ThisUpdate: f.now.Add(time.Minute), NextUpdate: f.now.Add(time.Hour)}
			if test.expired {
				crl.NextUpdate = f.now.Add(2 * time.Minute)
			}
			if !test.revokedAt.IsZero() {
				crl.RevokedCertificateEntries = []smx509.RevocationListEntry{{SerialNumber: f.signer.SerialNumber, RevocationTime: test.revokedAt}}
			}
			der, err := smx509.CreateRevocationList(rand.Reader, crl, f.root, f.rootKey)
			if err != nil {
				t.Fatal(err)
			}
			options := *f.options
			options.CurrentTime = f.now.Add(5 * time.Minute)
			options.Revocation = &parser.CertificateRevocationOptions{CRLs: [][]byte{der}, Issuers: [][]byte{f.root.Raw}}
			if !test.expired {
				options.Revocation.CRLs = append(f.options.Revocation.CRLs, der)
			}
			result, err := parser.VerifySESSignedValueWithOptions(value, &options)
			if err != nil {
				t.Fatal(err)
			}
			parser.BindSignatureDigest(result, &parser.SignatureDigestResult{Valid: true, References: []parser.ReferenceDigestResult{{Match: true}}, DataHash: &parser.DataHashResult{Match: true, ScopeMatch: true}})
			if result.Validation.Status != test.want {
				t.Fatalf("历史吊销结果=%+v，期望 %s", result.Validation, test.want)
			}
		})
	}
}

func TestSESDoesNotTreatPartialOrConflictingCRLsAsGoodEvidence(t *testing.T) {
	f := newSigningFixture(t, false)
	value, err := parser.ParseSignedValue(f.value)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"delta", "scope", "critical", "conflict"} {
		t.Run(kind, func(t *testing.T) {
			crl := &smx509.RevocationList{Number: big.NewInt(2), ThisUpdate: f.now, NextUpdate: f.now.Add(time.Hour)}
			switch kind {
			case "delta":
				crl.ExtraExtensions = []pkix.Extension{{Id: asn1.ObjectIdentifier{2, 5, 29, 27}, Critical: true, Value: []byte{2, 1, 1}}}
			case "scope":
				crl.ExtraExtensions = []pkix.Extension{{Id: asn1.ObjectIdentifier{2, 5, 29, 28}, Value: []byte{48, 0}}}
			case "critical":
				crl.ExtraExtensions = []pkix.Extension{{Id: asn1.ObjectIdentifier{1, 2, 3, 4}, Critical: true, Value: []byte{5, 0}}}
			case "conflict":
				crl.ThisUpdate = f.now.Add(-time.Hour)
			}
			der, err := smx509.CreateRevocationList(rand.Reader, crl, f.root, f.rootKey)
			if err != nil {
				t.Fatal(err)
			}
			options := *f.options
			options.Revocation = &parser.CertificateRevocationOptions{CRLs: append(append([][]byte{}, f.options.Revocation.CRLs...), der), Issuers: [][]byte{f.root.Raw}}
			result, err := parser.VerifySESSignedValueWithOptions(value, &options)
			if err != nil {
				t.Fatal(err)
			}
			parser.BindSignatureDigest(result, &parser.SignatureDigestResult{Valid: true, References: []parser.ReferenceDigestResult{{Match: true}}, DataHash: &parser.DataHashResult{Match: true, ScopeMatch: true}})
			if result.Validation.Status != "incomplete" {
				t.Fatalf("部分/冲突吊销证据不得完整通过: %+v", result.Validation)
			}
		})
	}
}

func TestSESRejectsCertificateWithoutSigningUsage(t *testing.T) {
	f := newSigningFixture(t, false)
	template := *f.signer
	template.KeyUsage = smx509.KeyUsageKeyEncipherment
	template.ExtraExtensions = nil
	der, err := smx509.CreateCertificate(rand.Reader, &template, f.root, &f.signerKey.PublicKey, f.rootKey)
	if err != nil {
		t.Fatal(err)
	}
	data := resignValue(t, f, func(v *ses.SignedValue) {
		v.Certificate = der
		list, _ := asn1.Marshal([][]byte{der})
		v.TBS.Seal.SealInfo.Property.CertList = asn1.RawValue{FullBytes: list}
	})
	value, err := parser.ParseSignedValue(data)
	if err != nil {
		t.Fatal(err)
	}
	result, err := parser.VerifySESSignedValueWithOptions(value, f.options)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid || result.Validation.Status != "invalid" {
		t.Fatalf("未拒绝用途不符的证书: %+v", result)
	}
	found := false
	for _, check := range result.Validation.Checks {
		if check.Code == "signer.certificate_usage" && check.Status == "invalid" {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺少密钥用途错误: %+v", result.Validation)
	}
}
