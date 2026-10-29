package signeddata

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func cmsCertificate(t *testing.T, signer crypto.Signer) []byte {
	t.Helper()
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "CMS local test"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, signer.Public(), signer)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func TestCMSInternationalAlgorithmsWithIndependentOpenSSL(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method string
		curve  elliptic.Curve
	}{
		{SHA256WithRSA, nil}, {SHA384WithRSA, nil}, {SHA512WithRSA, nil},
		{ECDSAWithSHA256, elliptic.P256()}, {ECDSAWithSHA384, elliptic.P384()}, {ECDSAWithSHA512, elliptic.P521()},
	} {
		t.Run(tc.method, func(t *testing.T) {
			var signer crypto.Signer = rsaKey
			if tc.curve != nil {
				signer, err = ecdsa.GenerateKey(tc.curve, rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
			}
			cert := cmsCertificate(t, signer)
			data := []byte("<Signature>exact bytes</Signature>\r\n")
			value, err := Create(data, cert, signer, Options{Algorithm: tc.method})
			if err != nil {
				t.Fatal(err)
			}
			proof, err := Verify(value, data, nil)
			if err != nil || proof.Algorithm != tc.method || proof.Format != "CMS SignedData" {
				t.Fatalf("CMS 往返失败: %+v %v", proof, err)
			}
			if tc.curve == nil {
				// 组合 RSA OID 与 rsaEncryption 等价，但必须与独立摘要声明一致。
				var envelope contentInfo
				if err := decode(value, &envelope); err != nil {
					t.Fatal(err)
				}
				sd, err := parse(value)
				if err != nil {
					t.Fatal(err)
				}
				var composite asn1.ObjectIdentifier
				for _, oid := range []asn1.ObjectIdentifier{{1, 2, 840, 113549, 1, 1, 11}, {1, 2, 840, 113549, 1, 1, 12}, {1, 2, 840, 113549, 1, 1, 13}} {
					if oid.String() == tc.method {
						composite = oid
					}
				}
				sd.Signers[0].Algorithm.Algorithm = composite
				inner, err := asn1.Marshal(*sd)
				if err != nil {
					t.Fatal(err)
				}
				envelope.Content = asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: inner}
				combined, err := asn1.Marshal(envelope)
				if err != nil {
					t.Fatal(err)
				}
				if proof, err := Verify(combined, data, nil); err != nil || proof.Algorithm != tc.method {
					t.Fatalf("组合 RSA OID 未正确验证: %+v %v", proof, err)
				}
			}
			if _, err := Verify(value, append(bytes.Clone(data), ' '), nil); !errors.Is(err, ErrInvalid) {
				t.Fatalf("篡改原文被接受: %v", err)
			}
			bad := bytes.Clone(value)
			bad[len(bad)-1] ^= 1
			if _, err := Verify(bad, data, nil); !errors.Is(err, ErrInvalid) {
				t.Fatalf("篡改数学签名被接受: %v", err)
			}
			t.Run("OpenSSL", func(t *testing.T) {
				tool, err := exec.LookPath("openssl")
				if err != nil {
					t.Skip("缺少 OpenSSL，跳过独立 CMS 互操作检查")
				}
				dir := t.TempDir()
				keyDER, err := x509.MarshalPKCS8PrivateKey(signer)
				if err != nil {
					t.Fatal(err)
				}
				for name, content := range map[string][]byte{
					"cert.pem": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert}),
					"key.pem":  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
					"data.xml": data, "ours.der": value,
				} {
					if err := os.WriteFile(filepath.Join(dir, name), content, 0600); err != nil {
						t.Fatal(err)
					}
				}
				run := func(args ...string) {
					t.Helper()
					cmd := exec.CommandContext(t.Context(), tool, args...)
					cmd.Dir = dir
					if output, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("OpenSSL %v: %v\n%s", args, err, output)
					}
				}
				run("cms", "-verify", "-binary", "-inform", "DER", "-in", "ours.der", "-content", "data.xml", "-noverify", "-out", "verified.xml")
				verified, err := os.ReadFile(filepath.Join(dir, "verified.xml"))
				if err != nil || !bytes.Equal(verified, data) {
					t.Fatalf("OpenSSL 未还原完整原文字节: %v", err)
				}
				a, _ := lookupAlgorithm(tc.method)
				for _, mode := range []string{"detached", "attached", "noattrs"} {
					args := []string{"cms", "-sign", "-binary", "-in", "data.xml", "-signer", "cert.pem", "-inkey", "key.pem", "-md", a.hash.String(), "-nosmimecap", "-outform", "DER", "-out", "openssl.der"}
					switch mode {
					case "attached":
						args = append(args, "-nodetach")
					case "noattrs":
						args = append(args, "-noattr")
					}
					run(args...)
					encoded, err := os.ReadFile(filepath.Join(dir, "openssl.der"))
					if err != nil {
						t.Fatal(err)
					}
					proof, err := Verify(encoded, data, nil)
					if err != nil || proof.Algorithm != tc.method {
						t.Fatalf("不能验证 OpenSSL %s CMS: %+v %v", mode, proof, err)
					}
				}
			})
		})
	}
}

func TestCMSRejectsKeyMismatchAndAlgorithmParameters(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := cmsCertificate(t, key)
	for _, method := range []string{SM2WithSM3, SHA256WithRSA, "1.2.840.113549.1.1.10"} {
		if _, err := Create([]byte("data"), cert, key, Options{Algorithm: method}); err == nil {
			t.Fatalf("不匹配或未支持的签名算法被接受: %s", method)
		}
	}
	value, err := Create([]byte("data"), cert, key, Options{Algorithm: ECDSAWithSHA256})
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*signedData){
		func(sd *signedData) { sd.Signers[0].Algorithm.Parameters = asn1.NullRawValue },
		func(sd *signedData) { sd.Signers[0].Digest.Parameters = asn1.RawValue{FullBytes: []byte{4, 1, 0}} },
		func(sd *signedData) {
			sd.Signers[0].Algorithm.Algorithm = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 3}
		},
	} {
		var envelope contentInfo
		if err := decode(value, &envelope); err != nil {
			t.Fatal(err)
		}
		sd, err := parse(value)
		if err != nil {
			t.Fatal(err)
		}
		mutate(sd)
		inner, err := asn1.Marshal(*sd)
		if err != nil {
			t.Fatal(err)
		}
		envelope.Content.Bytes = inner
		envelope.Content.FullBytes = nil
		bad, err := asn1.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Verify(bad, []byte("data"), nil); err == nil {
			t.Fatal("非法算法参数或摘要与签名算法不一致被接受")
		}
	}
}
