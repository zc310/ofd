package main

import (
	"bytes"
	"github.com/tdewolff/canvas"
	"strings"
	"testing"
	"time"

	"github.com/emmansun/gmsm/sm3"
	"github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/internal/parser"
)

func TestSignProducesVerifiableSignedValue(t *testing.T) {
	signatureXML := []byte("<Signature>demo Signature.xml</Signature>")
	signedValue, err := sign(signatureXML)
	if err != nil {
		t.Fatalf("sign 失败: %v", err)
	}

	parsed, err := parser.ParseSignedValue(signedValue)
	if err != nil {
		t.Fatalf("ParseSignedValue 失败: %v", err)
	}
	if parsed.Format != "SES" || parsed.SES == nil {
		t.Fatalf("格式应为 SES，实际 %q", parsed.Format)
	}

	result, err := parser.VerifySESSignedValue(parsed)
	if err != nil {
		t.Fatalf("VerifySESSignedValue 失败: %v", err)
	}
	if !result.Valid || !result.Seal.Valid || !result.Outer.Valid {
		t.Fatalf("两层 SM2 签名应全部有效: %+v", result)
	}
}

func TestSignedValueIdentifiesProducer(t *testing.T) {
	signedValue, err := sign([]byte("<Signature>producer</Signature>"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parser.ParseSignedValue(signedValue)
	if err != nil {
		t.Fatal(err)
	}
	seal := parsed.SES.TBS.Seal.SealInfo
	for name, value := range map[string]string{
		"ESID":       seal.ESID,
		"印章名称":       seal.Property.Name,
		"Header.VID": seal.Header.VID,
	} {
		if !strings.Contains(value, demoProvider) {
			t.Fatalf("%s 应包含提供者标识 %q，实际 %q", name, demoProvider, value)
		}
	}
	if !strings.Contains(seal.ESID, producer) || !strings.Contains(seal.Property.Name, producer) {
		t.Fatalf("ESID/印章名称应包含项目来源 %q", producer)
	}
}

func TestSignedValueUsesStandardSESHeader(t *testing.T) {
	signedValue, err := sign([]byte("<Signature>header</Signature>"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parser.ParseSignedValue(signedValue)
	if err != nil {
		t.Fatal(err)
	}
	header := parsed.SES.TBS.Seal.SealInfo.Header
	if header.ID != headerID {
		t.Fatalf("SES_Header.ID 必须为固定的 %q，实际 %q", headerID, header.ID)
	}
	if header.Version != sealVersion || parsed.SES.TBS.Version != sealVersion {
		t.Fatalf("版本号应为 %d，Header=%d TBS=%d", sealVersion, header.Version, parsed.SES.TBS.Version)
	}
	propertyInfo := parsed.SES.TBS.PropertyInfo
	if !strings.HasPrefix(propertyInfo, "/") || !strings.Contains(propertyInfo, "/Signatures/Signature_") {
		t.Fatalf("TBS_Sign.PropertyInfo 应为签名 XML 包内路径，实际 %q", propertyInfo)
	}
}

func TestSignHonoursSignedXMLDataHash(t *testing.T) {
	signedValue, err := sign([]byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parser.ParseSignedValue(signedValue)
	if err != nil {
		t.Fatal(err)
	}
	expected := parsed.SES.TBS.DataHash.Bytes
	sum := sm3.Sum([]byte("payload"))
	if !bytes.Equal(expected, sum[:]) {
		t.Fatal("DataHash 应为输入 Signature.xml 的 SM3 摘要")
	}
}

func TestPlaceholderSealIsRedRingSVG(t *testing.T) {
	seal := placeholderSeal()
	if !bytes.HasPrefix(bytes.TrimSpace(seal), []byte("<svg")) {
		t.Fatalf("印章应为 SVG:\n%s", seal)
	}
	if !bytes.Contains(seal, []byte(`stroke="#E60012"`)) {
		t.Fatal("印章颜色应为 #E60012")
	}
	if !bytes.Contains(seal, []byte("<circle")) {
		t.Fatal("印章应包含圆环")
	}
	if _, err := canvas.ParseSVG(bytes.NewReader(seal)); err != nil {
		t.Fatalf("印章 SVG 应可解析: %v", err)
	}
}

func TestCertificateIsSelfSignedSM2(t *testing.T) {
	_, _, certificateDER, err := newCertificate(time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := smx509.ParseCertificate(certificateDER)
	if err != nil {
		t.Fatalf("解析演示证书失败: %v", err)
	}
	if certificate.SignatureAlgorithm != smx509.SM2WithSM3 {
		t.Fatalf("证书签名算法应为 SM2WithSM3，实际 %v", certificate.SignatureAlgorithm)
	}
	if err := certificate.CheckSignature(certificate.SignatureAlgorithm, certificate.RawTBSCertificate, certificate.Signature); err != nil {
		t.Fatalf("自签名证书校验失败: %v", err)
	}
}

func TestBuildSignedValueFromReader(t *testing.T) {
	var output bytes.Buffer
	if err := writeSignedValue(&output, bytes.NewReader([]byte("<Signature>reader</Signature>"))); err != nil {
		t.Fatalf("writeSignedValue 失败: %v", err)
	}
	if output.Len() == 0 {
		t.Fatal("writeSignedValue 应写出 SignedValue.dat")
	}
}
