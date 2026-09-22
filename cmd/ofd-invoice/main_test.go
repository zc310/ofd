package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
)

const invoiceAttachment = `<?xml version="1.0" encoding="UTF-8"?>
<Invoice>
  <MachineNo>444000224046</MachineNo>
  <InvoiceCode>033001900111</InvoiceCode>
  <InvoiceNo>12345678</InvoiceNo>
  <IssueDate>2026-09-18</IssueDate>
  <InvoiceCheckCode>6A5B2C3D1E4F</InvoiceCheckCode>
  <TaxExclusiveTotalAmount>100.00</TaxExclusiveTotalAmount>
  <TaxTotalAmount>13.00</TaxTotalAmount>
  <TaxInclusiveTotalAmount>113.00</TaxInclusiveTotalAmount>
  <Payee>王五</Payee>
  <Checker>赵六</Checker>
  <InvoiceClerk>张三</InvoiceClerk>
  <Buyer><BuyerName>买方公司</BuyerName><BuyerTaxID>911100000000000000</BuyerTaxID></Buyer>
  <Seller><SellerName>卖方公司</SellerName><SellerTaxID>911100001111000000</SellerTaxID></Seller>
  <GoodsInfos>
    <G1><Item>办公用品*中性笔</Item><Quantity>10.00</Quantity><Price>2.00</Price>
      <Amount>20.00</Amount><TaxScheme>13%</TaxScheme><TaxAmount>2.60</TaxAmount></G1>
  </GoodsInfos>
</Invoice>`

// buildInvoiceOFD 生成带发票附件的最小 OFD 包。
func buildInvoiceOFD(t *testing.T) []byte {
	t.Helper()
	files := map[string]string{
		"OFD.xml": `<?xml version="1.0" encoding="UTF-8"?>
<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD">
  <DocBody><DocInfo><DocID>invoice</DocID><Title>发票</Title></DocInfo>
  <DocRoot>Doc_0/Document.xml</DocRoot></DocBody>
</OFD>`,
		"Doc_0/Document.xml": `<?xml version="1.0" encoding="UTF-8"?>
<Document xmlns="http://www.ofdspec.org/2016">
  <CommonData><MaxUnitID>10</MaxUnitID>
    <PageArea><PhysicalBox>0 0 210 297</PhysicalBox><ApplicationBox>0 0 210 297</ApplicationBox></PageArea>
  </CommonData>
  <Pages><Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></Pages>
</Document>`,
		"Doc_0/Pages/Page_0/Content.xml": `<?xml version="1.0" encoding="UTF-8"?>
<Page xmlns="http://www.ofdspec.org/2016">
  <Area><PhysicalBox>0 0 210 297</PhysicalBox><ApplicationBox>0 0 210 297</ApplicationBox></Area>
  <Content>
    <Layer ID="1" Type="Body">
      <TextObject ID="1" Boundary="70 10 70 8" Font="1" Size="4"><TextCode X="30" Y="20">增值税电子普通发票</TextCode></TextObject>
    </Layer>
  </Content>
</Page>`,
		"Doc_0/Attachs/original_invoice.xml": invoiceAttachment,
	}
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	for name, value := range files {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func writeTempOFD(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "invoice.ofd")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunExtractsInvoiceToStdout(t *testing.T) {
	path := writeTempOFD(t, buildInvoiceOFD(t))
	var stdout, stderr bytes.Buffer
	code := run([]string{path}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	text := stdout.String()
	for _, want := range []string{`"number":"12345678"`, `"code":"033001900111"`, `"type":"普通发票"`, `"title":"增值税电子普通发票"`} {
		if !strings.Contains(text, want) {
			t.Errorf("stdout 缺少 %s\n%s", want, text)
		}
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want 空", stderr.String())
	}
}

func TestRunPrettyOutputIndents(t *testing.T) {
	path := writeTempOFD(t, buildInvoiceOFD(t))
	var stdout, stderr bytes.Buffer
	code := run([]string{"--pretty", path}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "\n  \"") {
		t.Errorf("--pretty 应输出缩进 JSON")
	}
}

func TestRunWritesOutputFile(t *testing.T) {
	path := writeTempOFD(t, buildInvoiceOFD(t))
	output := filepath.Join(t.TempDir(), "invoice.json")
	var stdout, stderr bytes.Buffer
	code := run([]string{"-o", output, path}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want 空", stdout.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"number":"12345678"`) {
		t.Errorf("输出文件缺少发票号码字段")
	}
}

func TestRunMissingInputIsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "缺少输入") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRunTooManyArgsIsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"a.ofd", "b.ofd"}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
}

func TestRunMissingInputFileIsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{filepath.Join(t.TempDir(), "missing.ofd")}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "输入文件") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRunNonInvoiceIsExitError(t *testing.T) {
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	writer, err := archive.Create("OFD.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("<OFD/>")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	path := writeTempOFD(t, data.Bytes())
	var stdout, stderr bytes.Buffer
	code := run([]string{path}, &stdout, &stderr)
	if code != exitError {
		t.Fatalf("exit = %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr.String(), "附件") {
		t.Errorf("stderr = %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want 空", stdout.String())
	}
}

func TestRunOutputOverwritingInputIsUsageError(t *testing.T) {
	path := writeTempOFD(t, buildInvoiceOFD(t))
	var stdout, stderr bytes.Buffer
	code := run([]string{"-o", path, path}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
}

func TestRunHelpExitsOK(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--help"}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit = %d, want %d", code, exitOK)
	}
	if !strings.Contains(stdout.String(), "用法") {
		t.Errorf("stdout = %q", stdout.String())
	}
}
