package invoice_test

import (
	"bytes"
	"fmt"

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
