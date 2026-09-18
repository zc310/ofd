package pdfimport_test

import (
	"bytes"
	"testing"

	"github.com/zc310/ofd/internal/testutil"
	"github.com/zc310/ofd/pkg/converter"
	_ "github.com/zc310/ofd/pkg/converter/pdfimport"
)

func TestPDFImporterRegistered(t *testing.T) {
	imp, ok := converter.ImporterByName("pdf")
	if !ok {
		t.Fatal("ImporterByName(pdf) 未注册")
	}
	if imp.MIME() != "application/pdf" {
		t.Fatalf("pdf 导入器 MIME = %q", imp.MIME())
	}
	if _, ok := converter.ImporterByExtension(".pdf"); !ok {
		t.Fatal("ImporterByExtension(.pdf) 未注册")
	}
}

func TestConvertPDFToOFD(t *testing.T) {
	pdf := testutil.MinimalPDF([]byte("BT /F1 12 Tf 72 200 Td (Hello) Tj ET"), 144, 288)
	for _, from := range []string{"pdf", ".pdf", ""} {
		var output bytes.Buffer
		if err := converter.Convert(from, "ofd", pdf, &output); err != nil {
			t.Fatalf("Convert(%q, ofd) 失败: %v", from, err)
		}
		if !bytes.HasPrefix(output.Bytes(), []byte("PK\x03\x04")) {
			t.Fatalf("Convert(%q, ofd) 输出不是 OFD zip 包", from)
		}
	}
	// 未设置输出写入器时必须报错。
	if err := converter.Convert("pdf", "ofd", pdf, nil); err == nil {
		t.Fatal("Convert(pdf, ofd, nil) 期望返回未设置 OFD 输出参数错误")
	}
}

func TestConvertComposesPDFToPDF(t *testing.T) {
	pdf := testutil.MinimalPDF([]byte("BT /F1 12 Tf 72 200 Td (Hello) Tj ET"), 144, 288)
	var output bytes.Buffer
	if err := converter.Convert("pdf", "pdf", pdf, &output); err != nil {
		t.Fatalf("Convert(pdf, pdf) 失败: %v", err)
	}
	if !bytes.HasPrefix(output.Bytes(), []byte("%PDF-")) {
		t.Fatal("Convert(pdf, pdf) 输出不是 PDF")
	}
}

func TestConvertOFDToPDF(t *testing.T) {
	pdf := testutil.MinimalPDF([]byte("BT /F1 12 Tf 72 200 Td (Hello) Tj ET"), 144, 288)
	var ofd bytes.Buffer
	if err := converter.Convert("pdf", "ofd", pdf, &ofd); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := converter.Convert("ofd", "pdf", ofd.Bytes(), &output); err != nil {
		t.Fatalf("Convert(ofd, pdf) 失败: %v", err)
	}
	if !bytes.HasPrefix(output.Bytes(), []byte("%PDF-")) {
		t.Fatal("Convert(ofd, pdf) 输出不是 PDF")
	}
}

func TestConvertUnsupportedInputFormat(t *testing.T) {
	if err := converter.Convert("docx", "ofd", []byte("x"), &bytes.Buffer{}); err == nil {
		t.Fatal("未注册的导入格式应报错")
	}
	if err := converter.Convert("ofd", "ofd", []byte("x"), &bytes.Buffer{}); err == nil {
		t.Fatal("输入输出同为 OFD 应报错")
	}
}
