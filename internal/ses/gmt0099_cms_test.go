package ses_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	"github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/pkg/integrity"
	"github.com/zc310/ofd/pkg/sign"
	"github.com/zc310/ofd/pkg/validator"
)

func cmsAppendResult(t *testing.T, results []sign.Verification) sign.Verification {
	t.Helper()
	for _, result := range results {
		if result.ID == "sign-2" && result.Result != nil {
			return result
		}
	}
	t.Fatalf("缺少追加的 CMS 签名结果: %+v", results)
	return sign.Verification{}
}

// 同时覆盖国际算法 CA、CRL、混合算法追加和带时间戳的完整验证。
func TestGMT0099InternationalCMSFullEvidenceAndMixedAppend(t *testing.T) {
	for _, method := range []string{sign.SHA256WithRSA, sign.ECDSAWithSHA384} {
		t.Run(method, func(t *testing.T) {
			f := newSigningFixture(t, false)
			f.options.CurrentTime = f.now.Add(2 * time.Second)
			var key crypto.Signer
			var err error
			if method == sign.SHA256WithRSA {
				key, err = rsa.GenerateKey(rand.Reader, 2048)
			} else {
				key, err = ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
			}
			if err != nil {
				t.Fatal(err)
			}
			rootTemplate := &x509.Certificate{SerialNumber: big.NewInt(100), Subject: pkix.Name{CommonName: "CMS local CA"},
				NotBefore: f.now.Add(-time.Hour), NotAfter: f.now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
				KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
			rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, key.Public(), key)
			if err != nil {
				t.Fatal(err)
			}
			root, err := smx509.ParseCertificate(rootDER)
			if err != nil {
				t.Fatal(err)
			}
			leafTemplate := &x509.Certificate{SerialNumber: big.NewInt(101), Subject: pkix.Name{CommonName: "CMS local signer"},
				NotBefore: f.now.Add(-time.Hour), NotAfter: f.now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
			leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, rootTemplate, key.Public(), key)
			if err != nil {
				t.Fatal(err)
			}
			crl, err := smx509.CreateRevocationList(rand.Reader, &smx509.RevocationList{Number: big.NewInt(1), ThisUpdate: f.now.Add(-time.Hour), NextUpdate: f.now.Add(time.Hour)}, root, key)
			if err != nil {
				t.Fatal(err)
			}
			f.options.Trust.Roots.AddCert(root)
			f.options.Revocation.CRLs = append(f.options.Revocation.CRLs, crl)
			f.options.Revocation.Issuers = append(f.options.Revocation.Issuers, rootDER)
			callback := func(ctx context.Context, data []byte) ([]byte, error) {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				return sign.CreateDigitalSignedValue(data, leafDER, key, sign.DigitalSigningOptions{
					Algorithm: method, Time: f.now, Intermediates: [][]byte{rootDER},
					Timestamp: func(signature []byte) ([]byte, error) { return makeTimestamp(t, f, signature, false), nil },
				})
			}
			var first, final bytes.Buffer
			if err := sign.Sign(t.Context(), gmtArchive(t, gmtFiles()), &first, sign.Options{
				Type: "Sign", Append: true, Date: f.now, Sign: digitalSigner(t, f, false),
			}); err != nil {
				t.Fatal(err)
			}
			if err := sign.Sign(t.Context(), first.Bytes(), &final, sign.Options{
				Type: "Sign", Append: true, Relative: "sign-1", LockSignatures: true,
				SignatureMethod: method, CheckMethod: "SHA384", Date: f.now, Sign: callback,
			}); err != nil {
				t.Fatal(err)
			}
			results, err := sign.Verify(t.Context(), final.Bytes(), *f.options)
			if err != nil || len(results) != 2 {
				t.Fatalf("混合算法验签失败: %+v %v", results, err)
			}
			for _, item := range results {
				if item.Error != "" || !item.DigestValid || !item.Result.FullyValid() {
					t.Fatalf("证书、吊销、时间戳或基础签名未完整通过: %+v", item)
				}
			}
			cms := cmsAppendResult(t, results)
			if cms.Result.Outer.Algorithm != method || cms.Result.Outer.SignatureFormat != "CMS SignedData" {
				t.Fatal("报告未保留实际签名算法和格式")
			}
			instance, err := validator.New(validator.WithGMT0099(true))
			if err != nil {
				t.Fatal(err)
			}
			if report := instance.ValidateReader(t.Context(), bytes.NewReader(final.Bytes()), "cms.ofd"); report.HasErrors() || report.Summary.Warnings != 0 {
				t.Fatalf("CMS OFD 结构/引用摘要校验失败: %+v", report)
			}
			var protected bytes.Buffer
			if _, err := integrity.Protect(t.Context(), final.Bytes(), &protected, integrity.ProtectOptions{
				Options: integrity.Options{Verification: *f.options}, Date: f.now, Sign: callback,
			}); err != nil {
				t.Fatal(err)
			}
			if report, err := integrity.Verify(t.Context(), protected.Bytes(), integrity.Options{Verification: *f.options}); err != nil || report.Status != "valid" {
				t.Fatalf("国际算法完整性清单验证失败: %+v %v", report, err)
			}
			files := gmtUnpack(t, final.Bytes())
			files["Doc_0/Document.xml"] = append(files["Doc_0/Document.xml"], '\n')
			results, err = sign.Verify(t.Context(), gmtArchive(t, files), *f.options)
			if err != nil || len(results) != 2 {
				t.Fatalf("篡改正文未被拒绝: %+v %v", results, err)
			}
			cms = cmsAppendResult(t, results)
			if cms.DigestValid || cms.Result.Validation.Status != "invalid" {
				t.Fatalf("篡改正文未被拒绝: %+v", cms)
			}
			// 签名器算法与 XML 声明不一致时，在写出 OFD 前拒绝。
			var rejected bytes.Buffer
			if err := sign.Sign(t.Context(), gmtArchive(t, gmtFiles()), &rejected, sign.Options{
				Type: "Sign", SignatureMethod: sign.SM2WithSM3, Date: f.now, Sign: callback,
			}); err == nil || rejected.Len() != 0 {
				t.Fatalf("算法声明不一致被接受: %v", err)
			}
			// 重签错误算法声明的 XML，确保验签器也独立检查它与 CMS 的一致性。
			files = gmtUnpack(t, final.Bytes())
			xmlPath := "Doc_0/Signatures/sign-2/Signature_sign-2.xml"
			files[xmlPath] = bytes.ReplaceAll(files[xmlPath], []byte(method), []byte(sign.SM2WithSM3))
			value, err := callback(t.Context(), files[xmlPath])
			if err != nil {
				t.Fatal(err)
			}
			files["Doc_0/Signatures/sign-2/Data/sign-2.dat"] = value
			results, err = sign.Verify(t.Context(), gmtArchive(t, files), *f.options)
			if err != nil || len(results) != 2 {
				t.Fatalf("XML/CMS 算法声明不一致被接受: %+v %v", results, err)
			}
			cms = cmsAppendResult(t, results)
			if !cms.Result.Valid || cms.Result.Validation.Status != "invalid" {
				t.Fatalf("XML/CMS 算法声明不一致被接受: %+v", cms)
			}
		})
	}
}
