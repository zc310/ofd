package ses_test

import (
	"bytes"
	"encoding/asn1"
	"errors"
	"testing"
	"time"

	"github.com/emmansun/gmsm/sm2"
	"github.com/zc310/ofd/internal/ses"
)

func TestBuildSealRejectsInvalidParameters(t *testing.T) {
	f := newSigningFixture(t, false)
	for _, test := range []struct {
		name   string
		change func(*ses.SealParams)
	}{
		{"empty provider", func(p *ses.SealParams) { p.Provider = "" }},
		{"empty ID", func(p *ses.SealParams) { p.ESID = "" }},
		{"empty image format", func(p *ses.SealParams) { p.PictureType = "" }},
		{"empty image data", func(p *ses.SealParams) { p.PictureData = nil }},
		{"empty name", func(p *ses.SealParams) { p.Name = "" }},
		{"invalid UTF-8 name", func(p *ses.SealParams) { p.Name = "\xff" }},
		{"zero width", func(p *ses.SealParams) { p.Width = 0 }},
		{"negative height", func(p *ses.SealParams) { p.Height = -1 }},
		{"inverted validity", func(p *ses.SealParams) { p.ValidFrom, p.ValidTo = f.now.Add(time.Hour), f.now }},
		{"expired seal", func(p *ses.SealParams) { p.ValidFrom, p.ValidTo = f.now.Add(-time.Hour), f.now.Add(-time.Second) }},
		{"empty authorization", func(p *ses.SealParams) { p.SignerCertificates = [][]byte{} }},
		{"malformed authorized certificate", func(p *ses.SealParams) { p.SignerCertificates = [][]byte{[]byte("invalid")} }},
		{"conflicting authorization", func(p *ses.SealParams) {
			p.SignerCertificates = [][]byte{f.signer.Raw}
			p.SignerDigests = []ses.CertificateDigest{{Type: "SM3", Value: make([]byte, 32)}}
		}},
		{"unsupported authorization digest", func(p *ses.SealParams) {
			p.SignerDigests = []ses.CertificateDigest{{Type: "SHA1", Value: make([]byte, 20)}}
		}},
		{"short authorization digest", func(p *ses.SealParams) { p.SignerDigests = []ses.CertificateDigest{{Type: "SM3", Value: []byte{1}}} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			params := ses.SealParams{Provider: "test", ESID: "seal-1", Name: "测试印章", PictureType: "png", PictureData: makeTestSealPNG(t), Width: 40, Height: 40}
			test.change(&params)
			seal, err := ses.BuildSeal(params, f.maker.Raw, f.makerKey, f.now)
			if err == nil || seal != nil {
				t.Fatalf("无效制章参数应失败，实际 seal=%v, err=%v", seal, err)
			}
		})
	}
}

func TestValidateSigningKeyRejectsMissingOrMalformedMaterial(t *testing.T) {
	f := newSigningFixture(t, false)
	for _, key := range []*sm2.PrivateKey{nil, {}} {
		if err := ses.ValidateSigningKey(f.signer.Raw, key, f.now); err == nil {
			t.Fatal("缺失私钥应被拒绝")
		}
	}
	if err := ses.ValidateSigningKey([]byte("bad certificate"), f.signerKey, f.now); err == nil {
		t.Fatal("无效签名证书应被拒绝")
	}
}

func TestSigningPreflightFailureStopsTimestampAndPreservesInputs(t *testing.T) {
	f := newSigningFixture(t, false)
	before, err := f.seal.MarshalDER()
	if err != nil {
		t.Fatal(err)
	}
	policyError := errors.New("签署前状态检查失败")
	timestampCalled := false
	value, err := ses.BuildSignedValueWithOptions(f.xml, f.seal, f.signer.Raw, f.signerKey, "/Signature.xml", f.now, ses.SigningOptions{
		Validate: func(seal *ses.Seal, certificate []byte, at time.Time) error {
			if seal != f.seal || !bytes.Equal(certificate, f.signer.Raw) || !at.Equal(f.now) {
				t.Error("状态检查收到的签章材料或时间不正确")
			}
			certificate[0] ^= 1
			return policyError
		},
		Timestamp: func([]byte) ([]byte, error) { timestampCalled = true; return nil, nil },
	})
	if !errors.Is(err, policyError) || len(value) != 0 || timestampCalled {
		t.Fatalf("状态检查失败应原样返回错误且停止签署: value=%x, err=%v, timestamp=%t", value, err, timestampCalled)
	}
	after, err := f.seal.MarshalDER()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || f.signer.Raw[0] != 0x30 {
		t.Fatal("失败的状态检查不应修改印章或调用方证书")
	}
}

func TestBuildSignedValueRejectsInvalidInputAndTimestampFailures(t *testing.T) {
	f := newSigningFixture(t, false)
	timestampError := errors.New("时间戳服务不可用")
	for _, test := range []struct {
		name    string
		xml     []byte
		seal    *ses.Seal
		path    string
		options ses.SigningOptions
		want    error
	}{
		{name: "nil seal", xml: f.xml, path: "/Signature.xml"},
		{name: "empty payload", seal: f.seal, path: "/Signature.xml"},
		{name: "empty scope", xml: f.xml, seal: f.seal},
		{name: "non-ASCII scope", xml: f.xml, seal: f.seal, path: "/签名.xml"},
		{name: "timestamp unavailable", xml: f.xml, seal: f.seal, path: "/Signature.xml", options: ses.SigningOptions{Timestamp: func([]byte) ([]byte, error) { return nil, timestampError }}, want: timestampError},
		{name: "empty timestamp", xml: f.xml, seal: f.seal, path: "/Signature.xml", options: ses.SigningOptions{Timestamp: func([]byte) ([]byte, error) { return nil, nil }}},
		{name: "malformed timestamp", xml: f.xml, seal: f.seal, path: "/Signature.xml", options: ses.SigningOptions{Timestamp: func([]byte) ([]byte, error) { return []byte{0x30, 0}, nil }}},
	} {
		t.Run(test.name, func(t *testing.T) {
			value, err := ses.BuildSignedValueWithOptions(test.xml, test.seal, f.signer.Raw, f.signerKey, test.path, f.now, test.options)
			if err == nil || len(value) != 0 || test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("签署失败应返回错误且无产物，实际 value=%x, err=%v", value, err)
			}
		})
	}
}

func TestValidateSealForSignerRejectsTampering(t *testing.T) {
	f := newSigningFixture(t, false)
	for _, test := range []struct {
		name   string
		change func(*ses.Seal)
	}{
		{"wrong header", func(s *ses.Seal) { s.SealInfo.Header.ID = "bad" }},
		{"expired validity", func(s *ses.Seal) { s.SealInfo.Property.ValidTo = f.now.Add(-time.Second) }},
		{"bad maker certificate", func(s *ses.Seal) { s.Certificate = []byte("bad") }},
		{"wrong algorithm", func(s *ses.Seal) { s.SignatureAlgorithm = asn1.ObjectIdentifier{1, 2, 3} }},
		{"misaligned signature", func(s *ses.Seal) { s.Signature.BitLength-- }},
		{"changed seal name", func(s *ses.Seal) { s.SealInfo.Property.Name = "tampered" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			seal := *f.seal
			test.change(&seal)
			if err := ses.ValidateSealForSigner(&seal, f.signer.Raw, f.now); err == nil {
				t.Fatal("篡改后的印章不应通过签署前检查")
			}
		})
	}
	if err := ses.ValidateSealForSigner(nil, f.signer.Raw, f.now); err == nil {
		t.Fatal("空印章应被拒绝")
	}
}
