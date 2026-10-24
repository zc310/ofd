package signeddata

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"encoding/asn1"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/emmansun/gmsm/pkcs7"
	"github.com/emmansun/gmsm/sm2"
	"github.com/zc310/ofd/internal/ses"
)

func testSigningMaterial(t *testing.T) (*sm2.PrivateKey, []byte, time.Time) {
	t.Helper()
	now := time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.UTC)
	key, _, cert, err := ses.NewSelfSignedCertificate("SignedData test", "OFD test", now)
	if err != nil {
		t.Fatal(err)
	}
	return key, cert, now
}

func marshalSignedData(t *testing.T, sd *signedData) []byte {
	t.Helper()
	inner, err := asn1.Marshal(*sd)
	if err != nil {
		t.Fatal(err)
	}
	value, err := asn1.Marshal(contentInfo{Type: pkcs7.SM2OIDSignedData, Content: asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: inner}})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestCreateVerifyPreservesCertificateTimeAndCustomUID(t *testing.T) {
	key, cert, now := testSigningMaterial(t)
	_, _, intermediate, err := ses.NewSelfSignedCertificate("Intermediate test", "OFD test", now)
	if err != nil {
		t.Fatal(err)
	}
	original := []byte("<Signature>\r\n原文字节\n</Signature>")
	uid := []byte("custom signer UID")
	value, err := Create(original, cert, key, Options{Time: now, UID: uid, Intermediates: [][]byte{intermediate}})
	if err != nil {
		t.Fatal(err)
	}
	proof, err := Verify(value, original, uid)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(proof.Signer.Raw, cert) || len(proof.Certificates) != 2 || !proof.SigningTime.Equal(now.Truncate(time.Second)) {
		t.Fatalf("签名者、附带证书或签名时间错误: %+v", proof)
	}
	if proof.Timestamp != nil || proof.TimestampError != nil {
		t.Fatal("未请求时间戳时不应附带时间戳证据")
	}
	for _, test := range []struct {
		name          string
		original, uid []byte
	}{
		{"modified original", append(bytes.Clone(original), '\n'), uid},
		{"default UID", original, nil},
		{"wrong UID", original, []byte("another UID")},
	} {
		t.Run(test.name, func(t *testing.T) {
			if proof, err := Verify(value, test.original, test.uid); !errors.Is(err, ErrInvalid) || proof != nil {
				t.Fatalf("原文或 UID 不匹配应失败，实际 proof=%v, err=%v", proof, err)
			}
		})
	}
}

func TestVerifyAttachedContentAndSignaturesWithoutAttributes(t *testing.T) {
	key, cert, now := testSigningMaterial(t)
	original := []byte("original bytes")
	value, err := Create(original, cert, key, Options{Time: now})
	if err != nil {
		t.Fatal(err)
	}
	sd, err := parse(value)
	if err != nil {
		t.Fatal(err)
	}
	content, err := asn1.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	sd.Content.Content = asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: content}
	attached := marshalSignedData(t, sd)
	if _, err := Verify(attached, original, nil); err != nil {
		t.Fatalf("内嵌原文匹配时应通过验签: %v", err)
	}
	if _, err := Verify(attached, []byte("other bytes"), nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("内嵌原文不匹配应被拒绝: %v", err)
	}
	// 无签名属性的消息直接签署完整原文字节。
	sd.Signers[0].Signed = nil
	sd.Signers[0].Signature, err = key.Sign(rand.Reader, original, sm2.NewSM2SignerOption(true, nil))
	if err != nil {
		t.Fatal(err)
	}
	proof, err := Verify(marshalSignedData(t, sd), original, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !proof.SigningTime.IsZero() {
		t.Fatal("未携带签名时间属性时不应凭空生成签名时间")
	}
}

func TestCreateRejectsInvalidMaterialAndPropagatesSignerErrors(t *testing.T) {
	key, cert, now := testSigningMaterial(t)
	otherKey, _, _ := testSigningMaterial(t)
	signerError := errors.New("签名设备离线")
	for _, test := range []struct {
		name        string
		certificate []byte
		signer      crypto.Signer
		options     Options
		want        error
	}{
		{name: "bad certificate", certificate: []byte("invalid"), signer: key, want: ErrInvalid},
		{name: "missing signer", certificate: cert, want: ErrInvalid},
		{name: "mismatched key", certificate: cert, signer: otherKey, want: ErrInvalid},
		{name: "expired certificate", certificate: cert, signer: key, options: Options{Time: now.AddDate(11, 0, 0)}},
		{name: "signer error", certificate: cert, signer: failingSigner{Signer: key, err: signerError}, want: signerError},
		{name: "bad intermediate", certificate: cert, signer: key, options: Options{Intermediates: [][]byte{[]byte("bad")}}},
		{name: "too many intermediates", certificate: cert, signer: key, options: Options{Intermediates: make([][]byte, 128)}, want: ErrInvalid},
		{name: "invalid signer output", certificate: cert, signer: failingSigner{Signer: key}, want: ErrInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.options.Time.IsZero() {
				test.options.Time = now
			}
			value, err := Create([]byte("payload"), test.certificate, test.signer, test.options)
			if err == nil || len(value) != 0 || test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("应拒绝无效材料或传播签名错误，实际 value=%x, err=%v", value, err)
			}
		})
	}
}

func TestCreateRejectsUnavailableOrInvalidTimestamps(t *testing.T) {
	key, cert, now := testSigningMaterial(t)
	unavailable := errors.New("时间戳服务离线")
	for _, test := range []struct {
		name      string
		token     []byte
		err, want error
	}{
		{"service failure", nil, unavailable, unavailable},
		{"empty token", nil, nil, ErrInvalid},
		{"malformed token", []byte{0x30, 0}, nil, ErrInvalid},
		{"oversized token", make([]byte, MaxBytes+1), nil, ErrInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			value, err := Create([]byte("payload"), cert, key, Options{Time: now, Timestamp: func(signature []byte) ([]byte, error) {
				called = true
				if len(signature) == 0 {
					t.Error("时间戳服务应收到非空签名值")
				}
				return test.token, test.err
			}})
			if !called || len(value) != 0 || !errors.Is(err, test.want) {
				t.Fatalf("时间戳失败应阻止生成消息并保留错误: called=%t, value=%x, err=%v", called, value, err)
			}
		})
	}
}

func TestVerifyRejectsMalformedEnvelopesAndUnsignedAttributes(t *testing.T) {
	for _, data := range [][]byte{nil, {0x30, 0xff}, make([]byte, MaxBytes+1)} {
		if _, err := Verify(data, []byte("payload"), nil); !errors.Is(err, ErrInvalid) {
			t.Fatalf("非法或超限消息应返回 ErrInvalid: %v", err)
		}
	}
	key, cert, now := testSigningMaterial(t)
	value, err := Create([]byte("payload"), cert, key, Options{Time: now})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		attrs []attribute
		want  error
	}{
		{"unknown unsigned attribute", []attribute{{Type: asn1.ObjectIdentifier{1, 2, 3}}}, ErrUnsupported},
		{"duplicate timestamps", []attribute{{Type: timestampOID}, {Type: timestampOID}}, ErrUnsupported},
		{"timestamp is not SET", []attribute{{Type: timestampOID, Values: asn1.RawValue{Tag: asn1.TagSequence, IsCompound: true}}}, ErrInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			sd, err := parse(value)
			if err != nil {
				t.Fatal(err)
			}
			sd.Signers[0].Unsigned = test.attrs
			if _, err := Verify(marshalSignedData(t, sd), []byte("payload"), nil); !errors.Is(err, test.want) {
				t.Fatalf("非法非签名属性应被拒绝: %v，期望 %v", err, test.want)
			}
		})
	}
	sd, err := parse(value)
	if err != nil {
		t.Fatal(err)
	}
	sd.Signers[0].Unsigned = []attribute{{Type: timestampOID, Values: asn1.RawValue{Tag: asn1.TagSet, IsCompound: true, Bytes: []byte{0x30, 0}}}}
	proof, err := Verify(marshalSignedData(t, sd), []byte("payload"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if proof.Timestamp != nil || proof.TimestampError == nil {
		t.Fatal("非法时间戳应在数学签名证明中保留明确的时间戳错误")
	}
}

type failingSigner struct {
	crypto.Signer
	err error
}

func (s failingSigner) Sign(io.Reader, []byte, crypto.SignerOpts) ([]byte, error) {
	return []byte("invalid signature"), s.err
}
