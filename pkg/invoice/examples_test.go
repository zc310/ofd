package invoice_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/pkg/invoice"
)

// ExampleExtract 演示从包含发票结构化附件的 OFD 字节中抽取发票信息。
func ExampleExtract() {
	data := sampleInvoiceOFD()
	got, err := invoice.Extract(data)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(got.Type)
	fmt.Println(got.Code, got.Number)
	fmt.Println(got.TotalAmountString)
	fmt.Println(got.Buyer.Name)
	fmt.Println(got.Details[0].Name, got.Details[0].TaxRate)
	// Output:
	// 普通发票
	// 033001900111 12345678
	// 壹佰壹拾叁元整
	// 买方公司
	// 办公用品*中性笔 0.13
}

// ExampleExtractBatch 演示汇总多张发票的金额，并按销售方分组。
func ExampleExtractBatch() {
	batch, err := invoice.ExtractBatch([]invoice.Source{
		{Name: "九月", Input: sampleInvoiceOFD()},
		{Name: "十月", Input: sampleInvoiceWithAmount("12345679", "2026-10-08", "200.00", "26.00", "226.00")},
	}, invoice.BatchOptions{})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(batch.Summary.Count, batch.Summary.TotalAmount.Fixed(invoice.MoneyScale))
	for _, group := range batch.Summary.ByMonth {
		fmt.Println(group.Name, group.Count, group.TotalAmount.Fixed(invoice.MoneyScale))
	}
	// 分组按价税合计降序排列，因此十月在九月之前。
	// Output:
	// 2 339.00
	// 2026-10 1 226.00
	// 2026-09 1 113.00
}

// ExampleMergePDF 演示把多张发票合并成一个便于打印的 PDF，并在首页放汇总对账表。
func ExampleMergePDF() {
	sources := []invoice.Source{
		{Name: "a", Input: sampleInvoiceOFD()},
		{Name: "b", Input: sampleInvoiceWithAmount("12345679", "2026-10-08", "200.00", "26.00", "226.00")},
	}
	var out bytes.Buffer
	if _, err := invoice.MergePDF(context.Background(), sources, &out, invoice.MergeOptions{
		Cover: true,
		Sort:  invoice.SortDate,
	}); err != nil {
		fmt.Println(err)
		return
	}
	data := out.Bytes()
	fmt.Println(strings.HasPrefix(string(data), "%PDF-"))
	// Output:
	// true
}

// ExampleWriteCSV 演示导出对账 CSV：明细逐票一行，末行是合计。
func ExampleWriteCSV() {
	batch, err := invoice.ExtractBatch([]invoice.Source{
		{Name: "九月", Input: sampleInvoiceOFD()},
	}, invoice.BatchOptions{})
	if err != nil {
		fmt.Println(err)
		return
	}
	var out bytes.Buffer
	if err := invoice.WriteCSV(&out, batch); err != nil {
		fmt.Println(err)
		return
	}
	// CSV 以 CRLF 结尾，切分后最后一个元素是空串，合计行是倒数第二个。
	lines := strings.Split(strings.TrimPrefix(out.String(), "\ufeff"), "\r\n")
	fmt.Println(lines[0])
	fmt.Println(lines[len(lines)-2])
	// Output:
	// 来源,发票代码,发票号码,开票日期,发票类型,红字,购买方名称,购买方税号,销售方名称,销售方税号,不含税金额,税额,价税合计,重复,异常
	// 合计 1 张,不含税金额,100.00,税额,13.00,价税合计,113.00,,,,,,,,
}

// sampleInvoiceOFD 构造一个最小 OFD，包含发票结构化附件与票面文字层。
func sampleInvoiceOFD() []byte {
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
		"Doc_0/Pages/Page_0/Content.xml": `<?xml version="1.0" encoding="UTF-8"?>
<Page xmlns="http://www.ofdspec.org/2016">
  <Area><PhysicalBox>0 0 210 297</PhysicalBox><ApplicationBox>0 0 210 297</ApplicationBox></Area>
  <Content>
    <Layer ID="1" Type="Body">
      <TextObject ID="1" Boundary="70 10 70 8" Font="1" Size="4"><TextCode X="30" Y="20">增值税电子普通发票</TextCode></TextObject>
      <TextObject ID="2" Boundary="110 260 60 6" Font="1" Size="3"><TextCode X="100" Y="262">价税合计（大写）壹佰壹拾叁元整</TextCode></TextObject>
    </Layer>
  </Content>
</Page>`,
		"Doc_0/Attachs/original_invoice.xml": `<?xml version="1.0" encoding="UTF-8"?>
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
  <Buyer><BuyerName>买方公司</BuyerName><BuyerTaxID>911100000000000000</BuyerTaxID></Buyer>
  <Seller><SellerName>卖方公司</SellerName><SellerTaxID>911100001111000000</SellerTaxID></Seller>
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
</Invoice>`,
	}
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	for name, value := range files {
		writer, err := archive.Create(name)
		if err != nil {
			panic(err)
		}
		if _, err := writer.Write([]byte(value)); err != nil {
			panic(err)
		}
	}
	if err := archive.Close(); err != nil {
		panic(err)
	}
	return data.Bytes()
}

// sampleInvoiceWithAmount 构造一张指定号码、日期与金额的发票 OFD，
// 用于演示多票汇总。号码不同，避免被当成重复票。
func sampleInvoiceWithAmount(number, date, amount, tax, total string) []byte {
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
		"Doc_0/Pages/Page_0/Content.xml": `<?xml version="1.0" encoding="UTF-8"?>
<Page xmlns="http://www.ofdspec.org/2016">
  <Area><PhysicalBox>0 0 210 297</PhysicalBox><ApplicationBox>0 0 210 297</ApplicationBox></Area>
  <Content>
    <Layer ID="1" Type="Body">
      <TextObject ID="1" Boundary="70 10 70 8" Font="1" Size="4"><TextCode X="30" Y="20">增值税电子普通发票</TextCode></TextObject>
    </Layer>
  </Content>
</Page>`,
		"Doc_0/Attachs/original_invoice.xml": fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Invoice xmlns="urn:cn:gov:tax:invoice">
  <InvoiceCode>033001900111</InvoiceCode>
  <InvoiceNo>%s</InvoiceNo>
  <IssueDate>%s</IssueDate>
  <TaxExclusiveTotalAmount>%s</TaxExclusiveTotalAmount>
  <TaxTotalAmount>%s</TaxTotalAmount>
  <TaxInclusiveTotalAmount>%s</TaxInclusiveTotalAmount>
  <Buyer><BuyerName>买方公司</BuyerName><BuyerTaxID>911100000000000000</BuyerTaxID></Buyer>
  <Seller><SellerName>卖方公司</SellerName><SellerTaxID>911100001111000000</SellerTaxID></Seller>
  <GoodsInfos>
    <G1>
      <Item>技术服务</Item>
      <Quantity>1.00</Quantity>
      <Price>%s</Price>
      <Amount>%s</Amount>
      <TaxScheme>13%%</TaxScheme>
      <TaxAmount>%s</TaxAmount>
    </G1>
  </GoodsInfos>
</Invoice>`, number, date, amount, tax, total, amount, amount, tax),
	}
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	for name, value := range files {
		writer, err := archive.Create(name)
		if err != nil {
			panic(err)
		}
		if _, err := writer.Write([]byte(value)); err != nil {
			panic(err)
		}
	}
	if err := archive.Close(); err != nil {
		panic(err)
	}
	return data.Bytes()
}
