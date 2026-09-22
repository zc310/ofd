package invoice

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
)

const sampleAttachment = `<?xml version="1.0" encoding="UTF-8"?>
<Invoice xmlns="urn:cn:gov:tax:invoice">
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
  <TaxControlCode>4567 8910 1121 3141</TaxControlCode>
  <Buyer>
    <BuyerName>买方公司</BuyerName>
    <BuyerTaxID>911100000000000000</BuyerTaxID>
    <BuyerAddrTel>北京市海淀区</BuyerAddrTel>
    <BuyerFinancialAccount>6222000000000000</BuyerFinancialAccount>
  </Buyer>
  <Seller>
    <SellerName>卖方公司</SellerName>
    <SellerTaxID>911100001111000000</SellerTaxID>
    <SellerAddrTel>上海市浦东新区</SellerAddrTel>
    <SellerFinancialAccount>6222000011110000</SellerFinancialAccount>
  </Seller>
  <GoodsInfos>
    <G1>
      <Item>办公用品*中性笔</Item>
      <Specification>0.5mm</Specification>
      <MeasurementDimension>支</MeasurementDimension>
      <Quantity>10.00</Quantity>
      <Price>2.00</Price>
      <Amount>20.00</Amount>
      <TaxScheme>13%</TaxScheme>
      <TaxAmount>2.60</TaxAmount>
    </G1>
  </GoodsInfos>
</Invoice>`

// invoiceContentXML 构造包含票面标题与价税合计大写金额的页面文字层。
func invoiceContentXML(title, upperAmount string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<Page xmlns="http://www.ofdspec.org/2016">
  <Area><PhysicalBox>0 0 210 297</PhysicalBox><ApplicationBox>0 0 210 297</ApplicationBox></Area>
  <Content>
    <Layer ID="1" Type="Body">
      <TextObject ID="1" Boundary="70 10 70 8" Font="1" Size="4"><TextCode X="30" Y="20">` + title + `</TextCode></TextObject>
      <TextObject ID="2" Boundary="110 260 60 6" Font="1" Size="3"><TextCode X="100" Y="262">价税合计（大写）` + upperAmount + `</TextCode></TextObject>
    </Layer>
  </Content>
</Page>`
}

// buildInvoiceOFD 组装一个最小合法 OFD，可包含发票附件与页面文字层。
func buildInvoiceOFD(t testing.TB, attachmentName, attachment string, content string) []byte {
	t.Helper()
	if content == "" {
		content = invoiceContentXML("增值税电子普通发票", "壹佰壹拾叁元整")
	}
	files := map[string]string{
		"OFD.xml": `<?xml version="1.0" encoding="UTF-8"?>
<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD">
  <DocBody>
    <DocInfo><DocID>invoice</DocID><Title>发票</Title></DocInfo>
    <DocRoot>Doc_0/Document.xml</DocRoot>
  </DocBody>
</OFD>`,
		"Doc_0/Document.xml": `<?xml version="1.0" encoding="UTF-8"?>
<Document xmlns="http://www.ofdspec.org/2016">
  <CommonData>
    <MaxUnitID>10</MaxUnitID>
    <PageArea><PhysicalBox>0 0 210 297</PhysicalBox><ApplicationBox>0 0 210 297</ApplicationBox></PageArea>
  </CommonData>
  <Pages><Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></Pages>
</Document>`,
		"Doc_0/Pages/Page_0/Content.xml": content,
	}
	if attachment != "" {
		files[attachmentName] = attachment
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

func TestExtractReadsAttachmentAndSupplementsText(t *testing.T) {
	data := buildInvoiceOFD(t, "Doc_0/Attachs/original_invoice.xml", sampleAttachment, "")
	got, err := Extract(data)
	if err != nil {
		t.Fatal(err)
	}
	assertString := func(field, got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}
	assertString("Title", got.Title, "增值税电子普通发票")
	assertString("Type", got.Type, "普通发票")
	assertString("MachineNumber", got.MachineNumber, "444000224046")
	assertString("Code", got.Code, "033001900111")
	assertString("Number", got.Number, "12345678")
	assertString("Date", got.Date, "2026-09-18")
	assertString("Checksum", got.Checksum, "6A5B2C3D1E4F")
	assertString("TotalAmountString", got.TotalAmountString, "壹佰壹拾叁元整")
	assertDecimal := func(field string, got *Decimal, want string) {
		t.Helper()
		if got == nil {
			t.Errorf("%s = nil, want %s", field, want)
			return
		}
		if value := decimalString(got.rat); value != want {
			t.Errorf("%s = %s, want %s", field, value, want)
		}
	}
	assertDecimal("Amount", got.Amount, "100")
	assertDecimal("TaxAmount", got.TaxAmount, "13")
	assertDecimal("TotalAmount", got.TotalAmount, "113")
	assertString("Attachment", got.Attachment, "Doc_0/Attachs/original_invoice.xml")
	assertString("Payee", got.Payee, "王五")
	assertString("Reviewer", got.Reviewer, "赵六")
	assertString("Drawer", got.Drawer, "张三")
	if got.Buyer.Name != "买方公司" || got.Buyer.Code != "911100000000000000" ||
		got.Buyer.Address != "北京市海淀区" || got.Buyer.Account != "6222000000000000" {
		t.Errorf("Buyer = %+v", got.Buyer)
	}
	if got.Seller.Name != "卖方公司" || got.Seller.Code != "911100001111000000" ||
		got.Seller.Address != "上海市浦东新区" || got.Seller.Account != "6222000011110000" {
		t.Errorf("Seller = %+v", got.Seller)
	}
	if len(got.Details) != 1 {
		t.Fatalf("Details 行数 = %d, want 1", len(got.Details))
	}
	detail := got.Details[0]
	assertString("Detail.Name", detail.Name, "办公用品*中性笔")
	assertString("Detail.Model", detail.Model, "0.5mm")
	assertString("Detail.Unit", detail.Unit, "支")
	assertDecimal("Detail.Count", detail.Count, "10")
	assertDecimal("Detail.Price", detail.Price, "2")
	assertDecimal("Detail.Amount", detail.Amount, "20")
	assertDecimal("Detail.TaxRate", detail.TaxRate, "0.13")
	assertDecimal("Detail.TaxAmount", detail.TaxAmount, "2.6")
	if len(got.Warnings) != 0 {
		t.Errorf("Warnings = %v, want 空", got.Warnings)
	}
}

func TestExtractFindsFallbackAttachment(t *testing.T) {
	data := buildInvoiceOFD(t, "Doc_0/Attach/invoice_data.xml", sampleAttachment,
		invoiceContentXML("增值税电子专用发票", "壹佰元整"))
	got, err := Extract(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Attachment != "Doc_0/Attach/invoice_data.xml" {
		t.Errorf("Attachment = %q, want 兜底附件路径", got.Attachment)
	}
	if got.Code != "033001900111" {
		t.Errorf("Code = %q", got.Code)
	}
	if got.Type != "专用发票" {
		t.Errorf("Type = %q, want 专用发票", got.Type)
	}
}

func TestExtractInfersPassageType(t *testing.T) {
	data := buildInvoiceOFD(t, "Doc_0/Attachs/original_invoice.xml", sampleAttachment,
		invoiceContentXML("电子通行费增值税普通发票", "壹佰元整"))
	got, err := Extract(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != "通行费" {
		t.Errorf("Type = %q, want 通行费", got.Type)
	}
}

func TestExtractPrefersLargeTitleOverTopLeftLabel(t *testing.T) {
	// 模板/页面中「发票代码：」标签虽更靠左上，但标题字号更大，应取标题。
	content := `<?xml version="1.0" encoding="UTF-8"?>
<Page xmlns="http://www.ofdspec.org/2016">
  <Area><PhysicalBox>0 0 210 297</PhysicalBox><ApplicationBox>0 0 210 297</ApplicationBox></Area>
  <Content>
    <Layer ID="1" Type="Body">
      <TextObject ID="1" Boundary="8 3 30 4" Font="1" Size="3.175"><TextCode X="3" Y="5">发票代码：</TextCode></TextObject>
      <TextObject ID="2" Boundary="68 7 80 6.35" Font="2" Size="6.7"><TextCode X="30" Y="9">重庆增值税电子普通发票</TextCode></TextObject>
      <TextObject ID="3" Boundary="110 260 60 6" Font="1" Size="3"><TextCode X="100" Y="262">价税合计（大写）随机码壹佰壹拾叁元整</TextCode></TextObject>
    </Layer>
  </Content>
</Page>`
	data := buildInvoiceOFD(t, "Doc_0/Attachs/original_invoice.xml", sampleAttachment, content)
	got, err := Extract(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "重庆增值税电子普通发票" {
		t.Errorf("Title = %q, want 字号最大的标题而非左上角标签", got.Title)
	}
	if got.Type != "普通发票" {
		t.Errorf("Type = %q, want 普通发票", got.Type)
	}
	if got.TotalAmountString != "壹佰壹拾叁元整" {
		t.Errorf("TotalAmountString = %q, want 壹佰壹拾叁元整", got.TotalAmountString)
	}
}

func TestExtractMissingAttachmentErrors(t *testing.T) {
	data := buildInvoiceOFD(t, "", "", "")
	if _, err := Extract(data); !errors.Is(err, ErrNoAttachment) {
		t.Fatalf("错误 = %v, want ErrNoAttachment", err)
	}
}

func TestExtractEmptyPreferredAttachmentRejected(t *testing.T) {
	data := buildInvoiceOFD(t, "Doc_0/Attachs/original_invoice.xml",
		`<?xml version="1.0"?><Root/>`, "")
	if _, err := Extract(data); !errors.Is(err, ErrNoAttachment) {
		t.Fatalf("错误 = %v, want ErrNoAttachment", err)
	}
}

func TestExtractFallbackSkipsNonInvoiceAttachment(t *testing.T) {
	files := map[string]string{
		"Doc_0/Attachs/readme.xml": `<?xml version="1.0"?><Root><Note>不是发票</Note></Root>`,
		"Doc_0/Attachs/data.xml":   sampleAttachment,
	}
	base := buildInvoiceOFD(t, "", "", "")
	pkg := buildInvoiceOFDWithExtra(t, base, files)
	got, err := Extract(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if got.Attachment != "Doc_0/Attachs/data.xml" {
		t.Errorf("Attachment = %q, want 跳过非发票附件", got.Attachment)
	}
	if got.Number != "12345678" {
		t.Errorf("Number = %q", got.Number)
	}
}

func buildInvoiceOFDWithExtra(t testing.TB, base []byte, extra map[string]string) []byte {
	t.Helper()
	archiveRef, err := zip.NewReader(bytes.NewReader(base), int64(len(base)))
	if err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	output := zip.NewWriter(&data)
	for _, file := range archiveRef.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		writer, err := output.Create(file.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(writer, reader); err != nil {
			t.Fatal(err)
		}
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for name, value := range extra {
		writer, err := output.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestExtractTaxTotalWithTrailingText(t *testing.T) {
	patch := strings.Replace(sampleAttachment,
		"<TaxTotalAmount>13.00</TaxTotalAmount>",
		"<TaxTotalAmount>13.00（含税，由发票平台生成）</TaxTotalAmount>", 1)
	data := buildInvoiceOFD(t, "Doc_0/Attachs/original_invoice.xml", patch, "")
	got, err := Extract(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.TaxAmount == nil || decimalString(got.TaxAmount.rat) != "13" {
		t.Errorf("TaxAmount = %v, want 13", got.TaxAmount)
	}
}

func TestExtractTaxRateFromDecimalText(t *testing.T) {
	patch := strings.Replace(sampleAttachment, "<TaxScheme>13%</TaxScheme>", "<TaxScheme>0.13</TaxScheme>", 1)
	data := buildInvoiceOFD(t, "Doc_0/Attachs/original_invoice.xml", patch, "")
	got, err := Extract(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Details[0].TaxRate == nil || decimalString(got.Details[0].TaxRate.rat) != "0.13" {
		t.Errorf("TaxRate = %v, want 0.13", got.Details[0].TaxRate)
	}
}

func TestExtractBrokenDocumentDegradesToAttachment(t *testing.T) {
	files := map[string]string{
		"OFD.xml":                            `<?xml version="1.0"?><OFD><DocBody><unclosed`,
		"Doc_0/Attachs/original_invoice.xml": sampleAttachment,
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
	got, err := Extract(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got.Code != "033001900111" {
		t.Errorf("Code = %q, 文档损坏不应影响附件字段", got.Code)
	}
	if got.Type != "" {
		t.Errorf("Type = %q, 页面不可用时应为空", got.Type)
	}
	if len(got.Warnings) == 0 {
		t.Error("Warnings 应为文档降级提示")
	}
}

func TestExtractAcceptsFilePathAndReader(t *testing.T) {
	data := buildInvoiceOFD(t, "Doc_0/Attachs/original_invoice.xml", sampleAttachment, "")
	path := t.TempDir() + "/invoice.ofd"
	if err := writeFile(path, data); err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string]any{
		"路径":     path,
		"字节":     data,
		"reader": bytes.NewReader(data),
	} {
		got, err := Extract(input)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got.Number != "12345678" {
			t.Errorf("%s: Number = %q", name, got.Number)
		}
	}
}

func TestGarbageInputErrors(t *testing.T) {
	if _, err := Extract([]byte("not a zip")); err == nil {
		t.Fatal("非法输入应返回错误")
	}
	if _, err := Extract(42); err == nil {
		t.Fatal("不支持的类型应返回错误")
	}
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
