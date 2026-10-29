package split

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/crypt"
	"github.com/zc310/ofd/pkg/merge"
	"github.com/zc310/ofd/pkg/sign"
)

func signedFixture(t *testing.T, protectRoot bool) []byte {
	t.Helper()
	return signSourceFixture(t, splitFixture(t), protectRoot)
}

func signSourceFixture(t *testing.T, source []byte, protectRoot bool) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "split-test"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = sign.Sign(t.Context(), source, &out, sign.Options{Type: "Sign", ID: "split-signature", SignatureMethod: sign.SHA256WithRSA, CheckMethod: "SHA256", Date: now, Deterministic: true,
		References: &sign.ReferenceOptions{RootDocument: protectRoot}, Sign: func(_ context.Context, data []byte) ([]byte, error) {
			return sign.CreateDigitalSignedValue(data, der, key, sign.DigitalSigningOptions{Algorithm: sign.SHA256WithRSA})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestSplitPreservesSignatureBytesAndMathematicalVerification(t *testing.T) {
	signed := signedFixture(t, false)
	var combined bytes.Buffer
	if err := merge.Bytes([][]byte{signed, splitFixture(t)}, &combined, merge.Options{}); err != nil {
		t.Fatal(err)
	}
	s := openTest(t, combined.Bytes(), Options{})
	var out bytes.Buffer
	report, err := s.Write(t.Context(), Part{Document: 1, WholeDocument: true}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Signatures) != 1 {
		t.Fatalf("签名保留报告错误: %+v", report)
	}
	statuses, err := merge.VerifySignatures(out.Bytes())
	if err != nil || len(statuses) != 1 || !statuses[0].DigestValid || !statuses[0].Verified {
		t.Fatalf("拆分后数学签名/摘要无效: %+v %v", statuses, err)
	}
	original, result := readFiles(t, combined.Bytes()), readFiles(t, out.Bytes())
	for name, value := range result {
		if name != "OFD.xml" && !bytes.Equal(value, original[name]) {
			t.Fatalf("改变了签名保护文件: %s", name)
		}
	}
}

func TestSplitRejectsProtectedRootChangesAndDropsPageSignatures(t *testing.T) {
	signed := signedFixture(t, true)
	s := openTest(t, signed, Options{})
	// 单文档体完整提取无需修改入口，保护 OFD.xml 的签名也应保持有效。
	var out bytes.Buffer
	if _, err := s.Write(t.Context(), Part{Document: 1, WholeDocument: true}, &out); err != nil {
		t.Fatal(err)
	}
	statuses, err := merge.VerifySignatures(out.Bytes())
	if err != nil || len(statuses) != 1 || !statuses[0].Verified || !statuses[0].DigestValid {
		t.Fatalf("单文档体签名丢失: %+v %v", statuses, err)
	}
	// 对最终多文档入口签署有效签名，再确认拆出单个文档体会被保护范围拦住。
	var combined bytes.Buffer
	if err := merge.Bytes([][]byte{splitFixture(t), splitFixture(t)}, &combined, merge.Options{}); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, signSourceFixture(t, combined.Bytes(), true), Options{})
	out.Reset()
	if _, err := s.Write(t.Context(), Part{Document: 1, WholeDocument: true}, &out); err == nil || !strings.Contains(err.Error(), "OFD.xml") || out.Len() != 0 {
		t.Fatalf("主入口受保护时应拒绝改写: %v", err)
	}
	s = openTest(t, signed, Options{})
	data, report := writeTest(t, s, Part{Document: 1, Pages: []int{1}})
	for name := range readFiles(t, data) {
		if strings.Contains(name, "Signatures") || strings.Contains(name, "Signature.xml") {
			t.Fatalf("页面拆分遗留签名: %s", name)
		}
	}
	if len(report.Dropped) == 0 {
		t.Fatal("未报告签名移除")
	}
	s = openTest(t, signed, Options{Signatures: creator.SignaturePreserve})
	if _, err := s.Write(t.Context(), Part{Document: 1, Pages: []int{1}}, &out); err == nil {
		t.Fatal("页面拆分不应允许保留旧签名")
	}
}

func TestSplitEncryptedAndIntegrityInputsRequireExplicitHandling(t *testing.T) {
	source := splitFixture(t)
	var encrypted bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), source, &encrypted, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "test", Password: []byte("password")}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), encrypted.Bytes(), Options{}); err == nil {
		t.Fatal("加密输入未拒绝")
	}
	files := readFiles(t, source)
	files["OFDEntries.xml"] = []byte("protected marker")
	protected := repack(t, files)
	if _, err := Open(t.Context(), protected, Options{}); err == nil {
		t.Fatal("完整性清单未要求显式处理")
	}
	s := openTest(t, protected, Options{DropIntegrity: true})
	data, report := writeTest(t, s, Part{Document: 1, Pages: []int{1}})
	if _, ok := readFiles(t, data)["OFDEntries.xml"]; ok || len(report.Dropped) == 0 {
		t.Fatal("清单未移除或未报告")
	}
}
