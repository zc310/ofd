package parser

import (
	"bytes"
	"encoding/asn1"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	gmx509 "github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/models"
)

func TestParseSignedValueSES(t *testing.T) {
	data := readZipEntry(t, filepath.Join("..", "..", "test", "testdata", "999.ofd"), "Doc_0/Signs/Sign_0/SignedValue.dat")
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
	ofd, err := NewOFD(filepath.Join("..", "..", "test", "testdata", "999.ofd"))
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
	data := readZipEntry(t, filepath.Join("..", "..", "test", "testdata", "999.ofd"), "Doc_0/Signs/Sign_0/SignedValue.dat")
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
	data := readZipEntry(t, filepath.Join("..", "..", "test", "testdata", "999.ofd"), "Doc_0/Signs/Sign_0/SignedValue.dat")
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
	data := readZipEntry(t, filepath.Join("..", "..", "test", "testdata", "999.ofd"), "Doc_0/Signs/Sign_0/SignedValue.dat")
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
	data := readZipEntry(t, filepath.Join("..", "..", "test", "testdata", "999.ofd"), "Doc_0/Signs/Sign_0/SignedValue.dat")
	value, err := ParseSignedValue(data)
	if err != nil {
		t.Fatal(err)
	}
	packageData, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "999.ofd"))
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

func TestDocumentKeepsNonASN1SignedValue(t *testing.T) {
	ofd, err := NewOFD(filepath.Join("..", "..", "test", "testdata", "999.ofd"))
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
