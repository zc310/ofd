package pdf2ofd

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// makeEncryptedPDF 生成一个口令为 userpw 的加密 PDF。
func makeEncryptedPDF(t *testing.T) []byte {
	t.Helper()
	dir := t.TempDir()
	plain := dir + "/plain.pdf"
	body := []byte("%PDF-1.4\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] " +
		"/Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>\nendobj\n" +
		"4 0 obj\n<< /Length 33 >>\nstream\nBT /F1 12 Tf 20 100 Td (hi) Tj ET\nendstream\nendobj\n" +
		"5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"trailer\n<< /Size 6 /Root 1 0 R >>\n%%EOF\n")
	if err := os.WriteFile(plain, body, 0o600); err != nil {
		t.Fatal(err)
	}
	enc := dir + "/enc.pdf"
	conf := model.NewDefaultConfiguration()
	conf.UserPW = "userpw"
	conf.OwnerPW = "ownerpw"
	if err := api.EncryptFile(context.Background(), plain, enc, conf); err != nil {
		t.Skipf("无法构造加密 PDF: %v", err)
	}
	data, err := os.ReadFile(enc)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestZZEncryptedPDFNeedsPassword(t *testing.T) {
	data := makeEncryptedPDF(t)
	var out bytes.Buffer
	err := Convert(context.Background(), data, &out, "")
	if err == nil {
		t.Fatal("加密 PDF 未给口令却转换成功")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("已加密")) {
		t.Errorf("应报告需要口令，实际: %v", err)
	}
}

func TestZZEncryptedPDFWrongPassword(t *testing.T) {
	data := makeEncryptedPDF(t)
	var out bytes.Buffer
	err := Convert(context.Background(), data, &out, "nope")
	if err == nil {
		t.Fatal("错误口令却转换成功")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("口令不正确")) {
		t.Errorf("应报告口令不正确，实际: %v", err)
	}
}

func TestZZEncryptedPDFCorrectPassword(t *testing.T) {
	data := makeEncryptedPDF(t)
	var out bytes.Buffer
	if err := Convert(context.Background(), data, &out, "userpw"); err != nil {
		t.Fatalf("正确口令应能转换: %v", err)
	}
	if out.Len() == 0 {
		t.Error("产物为空")
	}
}
