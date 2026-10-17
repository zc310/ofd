package ses_test

import (
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509/pkix"
	"encoding/asn1"
	"math/big"
	"testing"
	"time"

	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/sm3"
	"github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/ses"
)

// 独立按 CMS/TSTInfo 编码测试令牌，不使用被测的 SES 编解码器。
func makeTimestamp(t *testing.T, f *signingFixture, signature []byte, wrongUsage bool, bindingMode ...string) []byte {
	t.Helper()
	tstOID := asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 4}
	sm3OID := asn1.ObjectIdentifier{1, 2, 156, 10197, 1, 401}
	key, err := sm2.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ekuOID := asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 8}
	if wrongUsage {
		ekuOID = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 3}
	}
	eku, _ := asn1.Marshal([]asn1.ObjectIdentifier{ekuOID})
	certDER, err := smx509.CreateCertificate(rand.Reader, &smx509.Certificate{SerialNumber: big.NewInt(4), Subject: pkix.Name{CommonName: "TSA"}, NotBefore: f.now.Add(-time.Hour), NotAfter: f.now.Add(time.Hour), KeyUsage: smx509.KeyUsageDigitalSignature, ExtraExtensions: []pkix.Extension{{Id: asn1.ObjectIdentifier{2, 5, 29, 37}, Critical: true, Value: eku}}}, f.root, &key.PublicKey, f.rootKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := smx509.ParseCertificate(certDER)
	if err != nil {
		t.Fatal(err)
	}
	imprint := sm3.Sum(signature)
	info := struct {
		Version int
		Policy  asn1.ObjectIdentifier
		Imprint struct {
			Algorithm pkix.AlgorithmIdentifier
			Hash      []byte
		}
		Serial *big.Int
		Time   time.Time `asn1:"generalized"`
	}{Version: 1, Policy: asn1.ObjectIdentifier{1, 2, 3, 4}, Serial: big.NewInt(42), Time: f.now.Add(time.Second)}
	info.Imprint.Algorithm = pkix.AlgorithmIdentifier{Algorithm: sm3OID}
	info.Imprint.Hash = imprint[:]
	content, err := asn1.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	type attribute struct {
		OID   asn1.ObjectIdentifier
		Value asn1.RawValue `asn1:"set"`
	}
	typeValue, _ := asn1.Marshal(tstOID)
	contentHash := sm3.Sum(content)
	digestValue, _ := asn1.Marshal(contentHash[:])
	attrs := []attribute{{OID: asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}, Value: asn1.RawValue{Tag: asn1.TagSet, IsCompound: true, Bytes: typeValue}}, {OID: asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}, Value: asn1.RawValue{Tag: asn1.TagSet, IsCompound: true, Bytes: digestValue}}}
	mode := ""
	if len(bindingMode) > 0 {
		mode = bindingMode[0]
	}
	certHash := sha256.Sum256(certDER)
	if mode == "wrong" {
		certHash[0] ^= 1
	}
	hashBytes := certHash[:]
	bindingOID := asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 47}
	if mode == "v1" {
		sum := sha1.Sum(certDER)
		hashBytes = sum[:]
		bindingOID = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 12}
	}
	binding, err := asn1.Marshal(struct{ Certificates []struct{ Hash []byte } }{Certificates: []struct{ Hash []byte }{{Hash: hashBytes}}})
	if err != nil {
		t.Fatal(err)
	}
	if mode != "missing" {
		attrs = append(attrs, attribute{OID: bindingOID, Value: asn1.RawValue{Tag: asn1.TagSet, IsCompound: true, Bytes: binding}})
	}
	if mode == "duplicate" {
		attrs = append(attrs, attrs[0])
	}
	signedAttrs, err := asn1.MarshalWithParams(attrs, "set")
	if err != nil {
		t.Fatal(err)
	}
	sig, err := key.Sign(rand.Reader, signedAttrs, sm2.NewSM2SignerOption(true, nil))
	if err != nil {
		t.Fatal(err)
	}
	type signerInfo struct {
		Version int
		Issuer  struct {
			Name   asn1.RawValue
			Serial *big.Int
		}
		Digest     pkix.AlgorithmIdentifier
		Attributes []attribute `asn1:"optional,tag:0"`
		Algorithm  pkix.AlgorithmIdentifier
		Signature  []byte
	}
	signer := signerInfo{Version: 1, Digest: pkix.AlgorithmIdentifier{Algorithm: sm3OID}, Attributes: attrs, Algorithm: pkix.AlgorithmIdentifier{Algorithm: asn1.ObjectIdentifier{1, 2, 156, 10197, 1, 501}}, Signature: sig}
	signer.Issuer.Name = asn1.RawValue{FullBytes: cert.RawIssuer}
	signer.Issuer.Serial = cert.SerialNumber
	sd := struct {
		Version    int
		Algorithms []pkix.AlgorithmIdentifier `asn1:"set"`
		Content    struct {
			Type  asn1.ObjectIdentifier
			Bytes []byte `asn1:"explicit,tag:0"`
		}
		Certificates asn1.RawValue
		Signers      []signerInfo `asn1:"set"`
	}{Version: 3, Algorithms: []pkix.AlgorithmIdentifier{{Algorithm: sm3OID}}, Certificates: asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: certDER}, Signers: []signerInfo{signer}}
	sd.Content.Type = tstOID
	sd.Content.Bytes = content
	sdDER, err := asn1.Marshal(sd)
	if err != nil {
		t.Fatal(err)
	}
	token, err := asn1.Marshal(struct {
		Type    asn1.ObjectIdentifier
		Content asn1.RawValue
	}{Type: asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}, Content: asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: sdDER}})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestTimestampRejectsMissingOrMismatchedCertificateBinding(t *testing.T) {
	f := newSigningFixture(t, false)
	for _, mode := range []string{"missing", "wrong", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			signature := []byte("signature value")
			if _, err := ses.CheckTimestamp(makeTimestamp(t, f, signature, false, mode), signature); err == nil {
				t.Fatal("数学签名有效但证书绑定或签名属性无效的令牌被接受")
			}
		})
	}
}

func TestTimestampBindsSignatureAndRequiresTimestampCertificate(t *testing.T) {
	f := newSigningFixture(t, false)
	signature := []byte("signature value")
	token := makeTimestamp(t, f, signature, false)
	proof, err := ses.CheckTimestamp(token, signature)
	if err != nil {
		t.Fatal(err)
	}
	if !proof.Time.Equal(f.now.Add(time.Second)) {
		t.Fatal("时间戳时刻错误")
	}
	if _, err := ses.CheckTimestamp(makeTimestamp(t, f, signature, false, "v1"), signature); err != nil {
		t.Fatalf("ESS v1 应可验证: %v", err)
	}
	if _, err := ses.CheckTimestamp(token, []byte("other signature")); err == nil {
		t.Fatal("时间戳未绑定签名值")
	}
	if _, err := ses.CheckTimestamp(makeTimestamp(t, f, signature, true), signature); err == nil {
		t.Fatal("非时间戳用途证书被接受")
	}
	token[len(token)-1] ^= 1
	if _, err := ses.CheckTimestamp(token, signature); err == nil {
		t.Fatal("篡改时间戳签名被接受")
	}
}

func TestSESTimestampGenerationAndCompleteVerification(t *testing.T) {
	f := newSigningFixture(t, false)
	data, err := ses.BuildSignedValueWithOptions(f.xml, f.seal, f.signer.Raw, f.signerKey, "/Doc_0/Signatures/Signature.xml", f.now, ses.SigningOptions{Timestamp: func(signature []byte) ([]byte, error) { return makeTimestamp(t, f, signature, false), nil }})
	if err != nil {
		t.Fatal(err)
	}
	value, err := parser.ParseSignedValue(data)
	if err != nil {
		t.Fatal(err)
	}
	f.options.CurrentTime = f.now.Add(2 * time.Second)
	result, err := parser.VerifySESSignedValueWithOptions(value, f.options)
	if err != nil {
		t.Fatal(err)
	}
	parser.BindSignatureDigest(result, &parser.SignatureDigestResult{Valid: true, References: []parser.ReferenceDigestResult{{Match: true}}, DataHash: &parser.DataHashResult{Match: true, ScopeMatch: true}})
	if result.Validation.Status != "valid" {
		t.Fatalf("时间戳完整验证失败: %+v", result.Validation)
	}
	value.SES.TimeStamp.Bytes[len(value.SES.TimeStamp.Bytes)-1] ^= 1
	result, err = parser.VerifySESSignedValueWithOptions(value, f.options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Validation.Status != "invalid" {
		t.Fatal("时间戳失效不应保留完整通过结论")
	}
}

func TestTimestampRequiresRevocationEvidenceForIntermediateCA(t *testing.T) {
	f := newSigningFixture(t, false)
	key, err := sm2.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &smx509.Certificate{SerialNumber: big.NewInt(10), Subject: pkix.Name{CommonName: "TSA intermediate"}, NotBefore: f.now.Add(-time.Hour), NotAfter: f.now.Add(time.Hour), BasicConstraintsValid: true, IsCA: true, KeyUsage: smx509.KeyUsageCertSign | smx509.KeyUsageCRLSign}
	der, err := smx509.CreateCertificate(rand.Reader, template, f.root, &key.PublicKey, f.rootKey)
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := smx509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	tsaFixture := *f
	tsaFixture.root = issuer
	tsaFixture.rootKey = key
	data, err := ses.BuildSignedValueWithOptions(f.xml, f.seal, f.signer.Raw, f.signerKey, "/Doc_0/Signatures/Signature.xml", f.now, ses.SigningOptions{Timestamp: func(signature []byte) ([]byte, error) { return makeTimestamp(t, &tsaFixture, signature, false), nil }})
	if err != nil {
		t.Fatal(err)
	}
	value, err := parser.ParseSignedValue(data)
	if err != nil {
		t.Fatal(err)
	}
	issuerCRL, err := smx509.CreateRevocationList(rand.Reader, &smx509.RevocationList{Number: big.NewInt(1), ThisUpdate: f.now, NextUpdate: f.now.Add(time.Hour)}, issuer, key)
	if err != nil {
		t.Fatal(err)
	}
	rootCRL, err := smx509.CreateRevocationList(rand.Reader, &smx509.RevocationList{Number: big.NewInt(2), ThisUpdate: f.now.Add(time.Second), NextUpdate: f.now.Add(time.Hour), RevokedCertificateEntries: []smx509.RevocationListEntry{{SerialNumber: issuer.SerialNumber, RevocationTime: f.now}}}, f.root, f.rootKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, want string
		crls       [][]byte
	}{
		{"missing_tsa_issuer_crl", "incomplete", f.options.Revocation.CRLs},
		{"complete_chain_evidence", "valid", append(append([][]byte{}, f.options.Revocation.CRLs...), issuerCRL)},
		{"revoked_intermediate", "invalid", [][]byte{issuerCRL, rootCRL}},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := *f.options
			options.CurrentTime = f.now.Add(2 * time.Second)
			options.TimestampTrust = &parser.CertificateTrustOptions{Roots: f.options.Trust.Roots, Intermediates: [][]byte{der}}
			options.Revocation = &parser.CertificateRevocationOptions{CRLs: test.crls, Issuers: [][]byte{f.root.Raw, der}}
			result, err := parser.VerifySESSignedValueWithOptions(value, &options)
			if err != nil {
				t.Fatal(err)
			}
			parser.BindSignatureDigest(result, &parser.SignatureDigestResult{Valid: true, References: []parser.ReferenceDigestResult{{Match: true}}, DataHash: &parser.DataHashResult{Match: true, ScopeMatch: true}})
			if result.Validation.Status != test.want {
				t.Fatalf("完整时间戳结论=%+v，期望 %s", result.Validation, test.want)
			}
		})
	}
}

func TestTimestampChecksSignerCertificateAtTimestampTime(t *testing.T) {
	for _, test := range []struct {
		name, want             string
		validFor, revokedAfter time.Duration
		missingEvidence        bool
	}{
		{name: "valid", want: "valid"},
		{name: "expired_before_timestamp", want: "invalid", validFor: 2 * time.Second},
		{name: "expires_at_timestamp", want: "valid", validFor: 3 * time.Second},
		{name: "revoked_between_signing_and_timestamp", want: "invalid", revokedAfter: 2 * time.Second},
		{name: "revoked_at_timestamp", want: "invalid", revokedAfter: 3 * time.Second},
		{name: "revoked_after_timestamp", want: "valid", revokedAfter: 4 * time.Second},
		{name: "missing_revocation_evidence", want: "incomplete", missingEvidence: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newSigningFixture(t, false)
			data := f.value
			if test.validFor > 0 {
				template := *f.signer
				template.NotAfter = f.now.Add(test.validFor)
				der, err := smx509.CreateCertificate(rand.Reader, &template, f.root, &f.signerKey.PublicKey, f.rootKey)
				if err != nil {
					t.Fatal(err)
				}
				data = resignValue(t, f, func(value *ses.SignedValue) {
					value.Certificate = der
					list, err := asn1.Marshal([][]byte{der})
					if err != nil {
						t.Fatal(err)
					}
					value.TBS.Seal.SealInfo.Property.CertList = asn1.RawValue{FullBytes: list}
				})
			}
			crl := &smx509.RevocationList{Number: big.NewInt(2), ThisUpdate: f.now.Add(4 * time.Second), NextUpdate: f.now.Add(time.Hour)}
			if test.revokedAfter > 0 {
				crl.RevokedCertificateEntries = []smx509.RevocationListEntry{{SerialNumber: f.signer.SerialNumber, RevocationTime: f.now.Add(test.revokedAfter)}}
			}
			crlDER, err := smx509.CreateRevocationList(rand.Reader, crl, f.root, f.rootKey)
			if err != nil {
				t.Fatal(err)
			}
			options := *f.options
			options.CurrentTime = f.now.Add(5 * time.Second)
			options.Revocation = &parser.CertificateRevocationOptions{CRLs: [][]byte{crlDER}, Issuers: [][]byte{f.root.Raw}}
			if test.missingEvidence {
				options.Revocation = nil
			}
			verify := func(data []byte) *parser.SignatureVerificationResult {
				t.Helper()
				value, err := parser.ParseSignedValue(data)
				if err != nil {
					t.Fatal(err)
				}
				result, err := parser.VerifySESSignedValueWithOptions(value, &options)
				if err != nil {
					t.Fatal(err)
				}
				parser.BindSignatureDigest(result, &parser.SignatureDigestResult{Valid: true, References: []parser.ReferenceDigestResult{{Match: true}}, DataHash: &parser.DataHashResult{Match: true, ScopeMatch: true}})
				return result
			}
			baseline := verify(data)
			if !test.missingEvidence && baseline.Validation.Status != "valid" {
				t.Fatalf("签章时刻应有效: %+v", baseline.Validation)
			}
			// 在已签署的数据外附加独立签名的时间戳，构造数学签名正确的失败样本。
			var wire ses.SignedValue
			if _, err := asn1.Unmarshal(data, &wire); err != nil {
				t.Fatal(err)
			}
			tsa := *f
			tsa.now = f.now.Add(2 * time.Second)
			token := makeTimestamp(t, &tsa, wire.Signature.Bytes, false)
			wire.TimeStamp = asn1.BitString{Bytes: token, BitLength: len(token) * 8}
			data, err = asn1.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			result := verify(data)
			if !result.Valid || result.Validation.Status != test.want {
				t.Fatalf("完整结果=%+v，期望 %s", result, test.want)
			}
			found := false
			for _, check := range result.Validation.Checks {
				if check.Code == "timestamp.signing_time" {
					found = true
					if check.Status != test.want {
						t.Fatalf("时间戳时刻证书检查=%+v，期望 %s", check, test.want)
					}
				}
				if check.Code == "timestamp" && !test.missingEvidence && check.Status != "valid" {
					t.Fatalf("独立 TSA 令牌应有效: %+v", check)
				}
			}
			if !found {
				t.Fatal("缺少时间戳时刻的签章者证书检查")
			}
		})
	}
}
