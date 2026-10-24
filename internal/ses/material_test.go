package ses_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/emmansun/gmsm/sm3"
	"github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/internal/ses"
)

func TestPrivateKeyPEMAndDERRoundTrip(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	key, public, certificate, err := ses.NewSelfSignedCertificate("密钥导入测试", "OFD test", now)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := ses.MarshalPrivateKeyPEM(key)
	if err != nil {
		t.Fatal(err)
	}
	block, rest := pem.Decode(encoded)
	if block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		t.Fatal("应输出单个 PKCS#8 PRIVATE KEY PEM 块")
	}
	for _, data := range [][]byte{encoded, block.Bytes} {
		parsed, err := ses.ParsePrivateKey(data)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.D.Cmp(key.D) != 0 || !public.Equal(parsed.Public()) {
			t.Fatal("导入后的 SM2 密钥与原密钥不一致")
		}
		if err := ses.ValidateSigningKey(certificate, parsed, now); err != nil {
			t.Fatalf("导入的私钥应与原证书匹配: %v", err)
		}
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate})
	for _, data := range [][]byte{certificate, certificatePEM} {
		der, err := ses.ParseCertificateDER(data)
		if err != nil || !bytes.Equal(der, certificate) {
			t.Fatalf("证书导入应保留 DER，实际错误 %v", err)
		}
		if _, err := smx509.ParseCertificate(der); err != nil {
			t.Fatal(err)
		}
	}
}

func TestParsePrivateKeyRejectsMalformedAndNonSM2Keys(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		data []byte
		want string
	}{
		{"empty", nil, "解析私钥失败"},
		{"malformed DER", []byte{0x30, 0xff}, "解析私钥失败"},
		{"malformed PEM payload", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("bad")}), "解析私钥失败"},
		{"ECDSA instead of SM2", der, "不是 SM2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ses.ParsePrivateKey(test.data)
			if got != nil || err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("应拒绝无效私钥并说明原因，实际 key=%v, err=%v", got, err)
			}
		})
	}
}

func TestCertificateHashSupportsAliasesAndRejectsUnknownAlgorithms(t *testing.T) {
	certificate := []byte("certificate DER bytes")
	sm3Digest, shaDigest := sm3.Sum(certificate), sha256.Sum256(certificate)
	for _, test := range []struct {
		method string
		want   []byte
	}{
		{"SM3", sm3Digest[:]}, {" sm3 ", sm3Digest[:]}, {"1.2.156.10197.1.401", sm3Digest[:]},
		{"SHA256", shaDigest[:]}, {"sha-256", shaDigest[:]}, {"2.16.840.1.101.3.4.2.1", shaDigest[:]},
	} {
		got, err := ses.CertificateHash(test.method, certificate)
		if err != nil || !bytes.Equal(got, test.want) {
			t.Errorf("CertificateHash(%q) = %x, %v，期望 %x", test.method, got, err, test.want)
		}
	}
	if digest, err := ses.CertificateHash("SHA1", certificate); err == nil || digest != nil {
		t.Fatalf("未知授权杂凑算法应失败，实际 %x, %v", digest, err)
	}
}

func TestCertificateAuthorizedChecksWholeList(t *testing.T) {
	f := newSigningFixture(t, false)
	digest := sha256.Sum256(f.signer.Raw)
	for _, test := range []struct {
		name  string
		typ   int64
		list  any
		valid bool
	}{
		{"certificate match", 1, [][]byte{f.root.Raw, f.signer.Raw}, true},
		{"certificate mismatch", 1, [][]byte{f.root.Raw}, false},
		{"empty certificates", 1, [][]byte{}, false},
		{"invalid certificate after match", 1, [][]byte{f.signer.Raw, []byte("bad certificate")}, false},
		{"digest match", 2, []ses.CertificateDigest{{Type: "SHA256", Value: digest[:]}}, true},
		{"digest mismatch", 2, []ses.CertificateDigest{{Type: "SHA256", Value: make([]byte, 32)}}, false},
		{"empty digests", 2, []ses.CertificateDigest{}, false},
		{"short digest after match", 2, []ses.CertificateDigest{{Type: "SHA256", Value: digest[:]}, {Type: "SM3", Value: []byte{1}}}, false},
		{"unsupported digest after match", 2, []ses.CertificateDigest{{Type: "SHA256", Value: digest[:]}, {Type: "SHA1", Value: make([]byte, 20)}}, false},
		{"unsupported list type", 3, [][]byte{f.signer.Raw}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			der, err := asn1.Marshal(test.list)
			if err != nil {
				t.Fatal(err)
			}
			if err := ses.CertificateAuthorized(test.typ, der, f.signer.Raw); (err == nil) != test.valid {
				t.Fatalf("授权结果错误: %v，期望有效=%t", err, test.valid)
			}
			if test.valid {
				if err := ses.CertificateAuthorized(test.typ, append(bytes.Clone(der), 0), f.signer.Raw); err == nil {
					t.Fatal("授权列表不能接受尾随数据")
				}
			}
		})
	}
}
