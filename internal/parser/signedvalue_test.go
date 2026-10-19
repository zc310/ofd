package parser

import (
	"bytes"
	"encoding/asn1"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gmx509 "github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/models"
)

func TestParseSignedValueSES(t *testing.T) {
	data := readZipEntry(t, filepath.Join("..", "..", "testdata", "ofdrw/999.ofd"), "Doc_0/Signs/Sign_0/SignedValue.dat")
	value, err := ParseSignedValue(data)
	if err != nil {
		t.Fatal(err)
	}
	if value.Format != "SES" || value.SES == nil || value.ASN1 == nil {
		t.Fatalf("format = %q, SES = %#v, ASN1 = %#v", value.Format, value.SES, value.ASN1)
	}
	if !bytes.Equal(value.Raw, data) {
		t.Fatal("raw SignedValue.dat was not preserved")
	}
	if value.SES.TBS.Version != 4 {
		t.Fatalf("TBS version = %d, want 4", value.SES.TBS.Version)
	}
	sealInfo := value.SES.TBS.Seal.SealInfo
	if sealInfo.Header.ID != "ES" || sealInfo.Header.Version != 4 || sealInfo.Header.VID != "GOMAIN" {
		t.Fatalf("seal header = %+v", sealInfo.Header)
	}
	if sealInfo.ESID != "50011200000323" {
		t.Fatalf("ESID = %q", sealInfo.ESID)
	}
	if sealInfo.Property.Name != "测试全国统一发票监制章国家税务总局重庆市税务局" {
		t.Fatalf("seal name = %q", sealInfo.Property.Name)
	}
	if sealInfo.Picture.Type != "ofd" || len(sealInfo.Picture.Data) == 0 || sealInfo.Picture.Width != 30 || sealInfo.Picture.Height != 20 {
		t.Fatalf("picture = type %q, size %d, dimensions %dx%d", sealInfo.Picture.Type, len(sealInfo.Picture.Data), sealInfo.Picture.Width, sealInfo.Picture.Height)
	}
	if len(value.SES.Certificate) == 0 || len(value.SES.TBS.Seal.Certificate) == 0 {
		t.Fatal("certificates were not parsed")
	}
	if value.SES.SignatureAlgorithm.String() != "1.2.156.10197.1.501" || value.SES.TBS.Seal.SignatureAlgorithm.String() != "1.2.156.10197.1.501" {
		t.Fatalf("signature algorithms = %s, %s", value.SES.SignatureAlgorithm, value.SES.TBS.Seal.SignatureAlgorithm)
	}
	if len(value.SES.Signature.Bytes) == 0 || len(value.SES.TBS.Seal.Signature.Bytes) == 0 {
		t.Fatal("signature values were not parsed")
	}
	verification, err := VerifySESSignedValue(value)
	if err != nil || !verification.Valid {
		t.Fatalf("第三方样例的原始签名字节应继续通过数学验证: %v", err)
	}
	found := false
	for _, check := range verification.Validation.Checks {
		if check.Code == "ses.format" {
			found = true
			if check.Status != "invalid" || !strings.Contains(check.Message, "默认值 FALSE 必须省略") {
				t.Fatalf("第三方样例的显式默认值应报告格式错误: %+v", check)
			}
		}
	}
	if !found {
		t.Fatal("缺少完整格式检查")
	}
}

func TestParseSignedValuePreservesGenericASN1(t *testing.T) {
	data := []byte{0x30, 0x03, 0x02, 0x01, 0x01}
	value, err := ParseSignedValue(data)
	if err != nil {
		t.Fatal(err)
	}
	if value.Format != "ASN.1" || value.SES != nil || value.ASN1 == nil || value.ASN1.Tag != 16 {
		t.Fatalf("generic result = %+v", value)
	}
}

func TestParseSignedValueRejectsTrailingBytes(t *testing.T) {
	data := []byte{0x30, 0x03, 0x02, 0x01, 0x01, 0x00}
	if _, err := ParseSignedValue(data); err == nil {
		t.Fatal("trailing bytes were accepted")
	}
}

func TestFullValidationRejectsExplicitDefaultInSealAndTBSExtensions(t *testing.T) {
	fixture := readZipEntry(t, filepath.Join("..", "..", "testdata", "ofdrw/999.ofd"), "Doc_0/Signs/Sign_0/SignedValue.dat")
	decode := func(data []byte) asn1.RawValue {
		t.Helper()
		var value asn1.RawValue
		if rest, err := asn1.Unmarshal(data, &value); err != nil || len(rest) != 0 {
			t.Fatalf("解码失败: %v", err)
		}
		return value
	}
	children := func(value asn1.RawValue) []asn1.RawValue {
		t.Helper()
		fields, err := signedValueSequence(value, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		return fields
	}
	sequence := func(fields []asn1.RawValue) asn1.RawValue {
		t.Helper()
		var data []byte
		for _, field := range fields {
			data = append(data, field.FullBytes...)
		}
		encoded, err := asn1.Marshal(asn1.RawValue{Tag: asn1.TagSequence, IsCompound: true, Bytes: data})
		if err != nil {
			t.Fatal(err)
		}
		return decode(encoded)
	}
	for _, location := range []string{"seal", "tbs"} {
		for _, encoding := range []string{"omitted", "false", "true"} {
			t.Run(location+"/"+encoding, func(t *testing.T) {
				// 独立构造 ExtData，显式 FALSE 是 BER 可表达但 DER 禁止的默认值。
				fields := []asn1.RawValue{decode([]byte{6, 2, 42, 3})}
				if encoding != "omitted" {
					value := byte(0)
					if encoding == "true" {
						value = 255
					}
					fields = append(fields, decode([]byte{1, 1, value}))
				}
				fields = append(fields, decode([]byte{4, 0}))
				extensions := sequence([]asn1.RawValue{sequence(fields)})
				outer := children(decode(fixture))
				tbs := children(outer[0])
				seal := children(tbs[1])
				info := children(seal[0])
				seal[0] = sequence(info[:4]) // 去掉 fixture 的非规范扩展，逐个测试两处域。
				tbs[1] = sequence(seal)
				if location == "seal" {
					info = append(info[:4], extensions)
					seal[0] = sequence(info)
					tbs[1] = sequence(seal)
				} else {
					encoded, err := asn1.Marshal(asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: extensions.FullBytes})
					if err != nil {
						t.Fatal(err)
					}
					tbs = append(tbs[:5], decode(encoded))
				}
				outer[0] = sequence(tbs)
				value, err := ParseSignedValue(sequence(outer).FullBytes)
				if err != nil || value.SES == nil {
					t.Fatalf("扩展诊断信息应可解析: %v", err)
				}
				extracted := value.SES.TBS.Extensions
				if location == "seal" {
					extracted = value.SES.TBS.Seal.SealInfo.Extensions
				}
				if len(extracted) != 1 || extracted[0].Critical != (encoding == "true") {
					t.Fatalf("扩展=%+v", extracted)
				}
				verification, err := VerifySESSignedValue(value)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, check := range verification.Validation.Checks {
					if check.Code == "ses.format" {
						found = true
						if encoding == "false" {
							if check.Status != "invalid" || !strings.Contains(check.Message, "默认值 FALSE 必须省略") {
								t.Fatalf("显式默认值应导致格式失败: %+v", check)
							}
						} else if check.Status != "valid" {
							t.Fatalf("合法 DER 扩展格式检查失败: %+v", check)
						}
					}
				}
				if !found {
					t.Fatal("缺少格式检查")
				}
			})
		}
	}
}

func TestSignedValuePathResolvesRelativeAndAbsoluteLocations(t *testing.T) {
	base := models.NewStLoc("/Doc_0/Signs/Sign_0")
	if got := models.NewStLoc("SignedValue.dat").Resolve(base); got != "/Doc_0/Signs/Sign_0/SignedValue.dat" {
		t.Fatalf("relative SignedValue path = %q", got)
	}
	if got := models.NewStLoc("/Doc_0/Signs/Sign_0/SignedValue.dat").Resolve(base); got != "/Doc_0/Signs/Sign_0/SignedValue.dat" {
		t.Fatalf("absolute SignedValue path = %q", got)
	}
}

func TestDocumentParsesSignedValues(t *testing.T) {
	ofd, err := NewOFD(filepath.Join("..", "..", "testdata", "ofdrw/999.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	if len(ofd.Documents) != 1 {
		t.Fatalf("documents = %d, want 1", len(ofd.Documents))
	}
	value := ofd.Documents[0].GetSignedValue("1")
	if value == nil || value.SES == nil {
		t.Fatalf("signed value = %#v", value)
	}
	digest := ofd.Documents[0].GetDigestResult("1")
	if digest == nil || !digest.Valid || digest.Method != "1.2.156.10197.1.401" {
		t.Fatalf("digest result = %+v", digest)
	}
	if digest.DataHash == nil || !digest.DataHash.Match {
		t.Fatalf("data hash result = %+v", digest.DataHash)
	}
	for _, reference := range digest.References {
		if !reference.Exists || !reference.Match {
			t.Fatalf("reference digest result = %+v", reference)
		}
	}
	verification := ofd.Documents[0].GetVerificationResult("1")
	if verification == nil || !verification.Valid {
		t.Fatalf("verification result = %+v, error = %v", verification, ofd.Documents[0].GetVerificationError("1"))
	}
	if !verification.Seal.Valid || !verification.Outer.Valid || verification.Seal.Certificate == nil || verification.Outer.Certificate == nil {
		t.Fatalf("verification components = %+v", verification)
	}
	if verification.Seal.SignatureFormat != string(SM2SignatureFormatDER) || verification.Outer.SignatureFormat != string(SM2SignatureFormatDER) {
		t.Fatalf("signature formats = %q, %q", verification.Seal.SignatureFormat, verification.Outer.SignatureFormat)
	}
	trustVerification, err := VerifySESSignedValueWithTrust(value, &CertificateTrustOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !trustVerification.Valid || !trustVerification.TrustChecked || trustVerification.Trusted {
		t.Fatalf("trust verification = %+v", trustVerification)
	}
	if trustVerification.Seal.TrustError != "未提供证书信任根" || trustVerification.Outer.TrustError != "未提供证书信任根" {
		t.Fatalf("trust errors = %q, %q", trustVerification.Seal.TrustError, trustVerification.Outer.TrustError)
	}
	roots := gmx509.NewCertPool()
	sealCertificate, err := gmx509.ParseCertificate(value.SES.TBS.Seal.Certificate)
	if err != nil {
		t.Fatal(err)
	}
	outerCertificate, err := gmx509.ParseCertificate(value.SES.Certificate)
	if err != nil {
		t.Fatal(err)
	}
	roots.AddCert(sealCertificate)
	roots.AddCert(outerCertificate)
	trustedVerification, err := VerifySESSignedValueWithTrust(value, &CertificateTrustOptions{Roots: roots})
	if err != nil {
		t.Fatal(err)
	}
	if !trustedVerification.Valid || !trustedVerification.TrustChecked || !trustedVerification.Trusted || !trustedVerification.Seal.Trusted || !trustedVerification.Outer.Trusted {
		t.Fatalf("trusted verification = %+v", trustedVerification)
	}
	combinedVerification, err := VerifySESSignedValueWithOptions(value, &SignatureVerificationOptions{
		SignatureFormat: SM2SignatureFormatDER,
		Trust:           &CertificateTrustOptions{Roots: roots},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !combinedVerification.Valid || !combinedVerification.Trusted || !combinedVerification.Seal.TrustChecked || !combinedVerification.Outer.TrustChecked {
		t.Fatalf("combined verification = %+v", combinedVerification)
	}
	untrustedVerification, err := VerifySESSignedValueWithTrust(value, &CertificateTrustOptions{Roots: gmx509.NewCertPool()})
	if err != nil {
		t.Fatal(err)
	}
	if !untrustedVerification.Valid || !untrustedVerification.TrustChecked || untrustedVerification.Trusted || untrustedVerification.Seal.Trusted || untrustedVerification.Outer.Trusted || untrustedVerification.Seal.TrustError == "" || untrustedVerification.Outer.TrustError == "" {
		t.Fatalf("untrusted verification = %+v", untrustedVerification)
	}
}

func TestVerifySESSignedValueDetectsTamperedSignedData(t *testing.T) {
	data := readZipEntry(t, filepath.Join("..", "..", "testdata", "ofdrw/999.ofd"), "Doc_0/Signs/Sign_0/SignedValue.dat")
	value, err := ParseSignedValue(data)
	if err != nil {
		t.Fatal(err)
	}
	value.SES.TBS.Seal.SealInfo.Raw[len(value.SES.TBS.Seal.SealInfo.Raw)-1] ^= 0xff
	result, err := VerifySESSignedValue(value)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid || result.Seal.Valid || !result.Outer.Valid {
		t.Fatalf("tampered verification result = %+v", result)
	}
}

func TestVerifySESSignedValueReportsUnconfiguredCRLAsUnknown(t *testing.T) {
	data := readZipEntry(t, filepath.Join("..", "..", "testdata", "ofdrw/999.ofd"), "Doc_0/Signs/Sign_0/SignedValue.dat")
	value, err := ParseSignedValue(data)
	if err != nil {
		t.Fatal(err)
	}
	result, err := VerifySESSignedValueWithOptions(value, &SignatureVerificationOptions{
		Revocation: &CertificateRevocationOptions{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid || !result.RevocationChecked || result.RevocationValid || result.Seal.RevocationStatus != "unknown" || result.Outer.RevocationStatus != "unknown" {
		t.Fatalf("revocation result = %+v", result)
	}
}

func TestVerifySESSignedValueSupportsRawSignatureAndUIDOptions(t *testing.T) {
	data := readZipEntry(t, filepath.Join("..", "..", "testdata", "ofdrw/999.ofd"), "Doc_0/Signs/Sign_0/SignedValue.dat")
	value, err := ParseSignedValue(data)
	if err != nil {
		t.Fatal(err)
	}
	sealR, sealS, _, err := parseSM2DERSignature(value.SES.TBS.Seal.Signature.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	outerR, outerS, _, err := parseSM2DERSignature(value.SES.Signature.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	value.SES.TBS.Seal.Signature.Bytes = rawSM2Signature(sealR, sealS)
	value.SES.Signature.Bytes = rawSM2Signature(outerR, outerS)
	result, err := VerifySESSignedValueWithOptions(value, &SignatureVerificationOptions{SignatureFormat: SM2SignatureFormatRaw})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid || result.Seal.SignatureFormat != string(SM2SignatureFormatRaw) || result.Outer.SignatureFormat != string(SM2SignatureFormatRaw) {
		t.Fatalf("raw signature verification = %+v", result)
	}

	value, err = ParseSignedValue(data)
	if err != nil {
		t.Fatal(err)
	}
	result, err = VerifySESSignedValueWithOptions(value, &SignatureVerificationOptions{UID: []byte("non-default-user-id")})
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid || result.Seal.Valid || result.Outer.Valid {
		t.Fatalf("custom UID was unexpectedly accepted = %+v", result)
	}
}

func TestParseSM2SignatureFormats(t *testing.T) {
	der, err := asn1.Marshal(struct{ R, S *big.Int }{big.NewInt(1), big.NewInt(2)})
	if err != nil {
		t.Fatal(err)
	}
	r, s, format, err := parseSM2Signature(der, SM2SignatureFormatAuto)
	if err != nil || r.Cmp(big.NewInt(1)) != 0 || s.Cmp(big.NewInt(2)) != 0 || format != SM2SignatureFormatDER {
		t.Fatalf("DER signature = %v, %v, %q, %v", r, s, format, err)
	}
	raw := rawSM2Signature(big.NewInt(1), big.NewInt(2))
	r, s, format, err = parseSM2Signature(raw, SM2SignatureFormatRaw)
	if err != nil || r.Cmp(big.NewInt(1)) != 0 || s.Cmp(big.NewInt(2)) != 0 || format != SM2SignatureFormatRaw {
		t.Fatalf("raw signature = %v, %v, %q, %v", r, s, format, err)
	}
	if _, _, _, err = parseSM2Signature(append(der, 0), SM2SignatureFormatDER); err == nil {
		t.Fatal("DER signature with trailing bytes was accepted")
	}
}

func rawSM2Signature(r, s *big.Int) []byte {
	result := make([]byte, 64)
	r.FillBytes(result[:32])
	s.FillBytes(result[32:])
	return result
}

func TestVerifySignatureDigestDetectsChangedReference(t *testing.T) {
	data := readZipEntry(t, filepath.Join("..", "..", "testdata", "ofdrw/999.ofd"), "Doc_0/Signs/Sign_0/SignedValue.dat")
	value, err := ParseSignedValue(data)
	if err != nil {
		t.Fatal(err)
	}
	packageData, err := os.ReadFile(filepath.Join("..", "..", "testdata", "ofdrw/999.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	packageReader, err := core.OpenBytes(packageData)
	if err != nil {
		t.Fatal(err)
	}
	defer packageReader.Close()

	var signature Signatures
	if err := packageReader.ReadXML("Doc_0/Signs/Signatures.xml", &signature); err != nil {
		t.Fatal(err)
	}
	var model models.Signature
	if err := packageReader.ReadXML("Doc_0/Signs/Sign_0/Signature.xml", &model); err != nil {
		t.Fatal(err)
	}
	model.SignedInfo.References.Reference[0].CheckValue[0] ^= 0xff
	result, err := VerifySignatureDigest(packageReader, "Doc_0/Signs/Sign_0/Signature.xml", &model, value)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid || result.References[0].Match {
		t.Fatalf("changed reference was accepted: %+v", result.References[0])
	}
}

func TestVerifySignatureDigestRetainsMethodMismatchWithUnsupportedHash(t *testing.T) {
	pkg, err := core.OpenFile(filepath.Join("..", "..", "testdata", "ofdrw/999.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer pkg.Close()
	const signaturePath = "Doc_0/Signs/Sign_0/Signature.xml"
	var signature models.Signature
	if err := pkg.ReadXML(signaturePath, &signature); err != nil {
		t.Fatal(err)
	}
	data, err := pkg.Read("Doc_0/Signs/Sign_0/SignedValue.dat")
	if err != nil {
		t.Fatal(err)
	}
	value, err := ParseSignedValue(data)
	if err != nil {
		t.Fatal(err)
	}
	signature.SignedInfo.SignatureMethod = "1.2.840.113549.1.1.11"
	signature.SignedInfo.References.CheckMethod = "unsupported"
	digest, err := VerifySignatureDigest(pkg, signaturePath, &signature, value)
	if err != nil {
		t.Fatal(err)
	}
	if digest.SignatureMethodError == "" || digest.Unsupported == "" {
		t.Fatalf("算法一致性检查不应被未知摘要算法跳过: %+v", digest)
	}
	result := &SignatureVerificationResult{Validation: &SignatureValidation{}}
	BindSignatureDigest(result, digest)
	if result.FullyValid() || result.Validation.Status != "invalid" {
		t.Fatalf("已确认的算法冲突应报告失败: %+v", result.Validation)
	}
}

func TestDocumentKeepsNonASN1SignedValue(t *testing.T) {
	ofd, err := NewOFD(filepath.Join("..", "..", "testdata", "ofdrw/999.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	// 该测试保护签名值的存储约定。生产者自定义的非 ASN.1 数据由创建器测试覆盖，
	// 不能因此中止整个 OFD 包的解析。
	if ofd.Documents[0].GetSignedValue("1") == nil && ofd.Documents[0].GetSignedValueError("1") == nil {
		t.Fatal("signed value maps were not initialized")
	}
}

func readZipEntry(t *testing.T, filename, entryName string) []byte {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	packageReader, err := core.OpenBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	defer packageReader.Close()
	content, err := packageReader.Read(entryName)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
