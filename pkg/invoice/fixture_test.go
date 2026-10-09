package invoice

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/parser"
)

// invoiceFixture 描述一张用于测试的发票。
type invoiceFixture struct {
	name       string
	code       string
	number     string
	date       string
	kind       string
	sellerName string
	sellerCode string
	buyerName  string
	amount     string
	tax        string
	total      string
	title      string
	// missingAttachment 生成不含发票结构化附件的 OFD。
	missingAttachment bool
	// pageWidth、pageHeight 是页面尺寸（毫米），0 表示 A4 纵向。
	pageWidth, pageHeight float64
}

// pageBox 返回该夹具的页面尺寸。
func (f invoiceFixture) pageBox() (float64, float64) {
	width, height := f.pageWidth, f.pageHeight
	if width <= 0 || height <= 0 {
		return 210, 297
	}
	return width, height
}

// sampleInvoice 构造一张带发票结构化附件与票面文字层的 OFD。
func sampleInvoice(fixture invoiceFixture) []byte {
	width, height := fixture.pageBox()
	files := map[string]string{
		"OFD.xml": `<?xml version="1.0" encoding="UTF-8"?>
<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD">
  <DocBody>
    <DocInfo><DocID>invoice</DocID><Title>发票</Title></DocInfo>
    <DocRoot>Doc_0/Document.xml</DocRoot>
  </DocBody>
</OFD>`,
		"Doc_0/Document.xml": fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Document xmlns="http://www.ofdspec.org/2016">
  <CommonData>
    <MaxUnitID>10</MaxUnitID>
    <PageArea><PhysicalBox>0 0 %[1]g %[2]g</PhysicalBox><ApplicationBox>0 0 %[1]g %[2]g</ApplicationBox></PageArea>
  </CommonData>
  <Pages><Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></Pages>
</Document>`, width, height),
		"Doc_0/Pages/Page_0/Content.xml": fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Page xmlns="http://www.ofdspec.org/2016">
  <Area><PhysicalBox>0 0 %[1]g %[2]g</PhysicalBox><ApplicationBox>0 0 %[1]g %[2]g</ApplicationBox></Area>
  <Content>
    <Layer ID="1" Type="Body">
      <TextObject ID="1" Boundary="20 10 70 8" Font="1" Size="4"><TextCode X="30" Y="20">%[3]s</TextCode></TextObject>
    </Layer>
  </Content>
</Page>`, width, height, fixture.title),
	}
	if !fixture.missingAttachment {
		files["Doc_0/Attachs/original_invoice.xml"] = fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Invoice xmlns="urn:cn:gov:tax:invoice">
  <InvoiceCode>%s</InvoiceCode>
  <InvoiceNo>%s</InvoiceNo>
  <IssueDate>%s</IssueDate>
  <TaxExclusiveTotalAmount>%s</TaxExclusiveTotalAmount>
  <TaxTotalAmount>%s</TaxTotalAmount>
  <TaxInclusiveTotalAmount>%s</TaxInclusiveTotalAmount>
  <Buyer><BuyerName>%s</BuyerName></Buyer>
  <Seller><SellerName>%s</SellerName><SellerTaxID>%s</SellerTaxID></Seller>
  <GoodsInfos>
    <G1>
      <Item>服务费</Item>
      <Quantity>1.00</Quantity>
      <Price>%s</Price>
      <Amount>%s</Amount>
      <TaxScheme>13%%</TaxScheme>
      <TaxAmount>%s</TaxAmount>
    </G1>
  </GoodsInfos>
</Invoice>`,
			fixture.code, fixture.number, fixture.date,
			fixture.amount, fixture.tax, fixture.total,
			fixture.buyerName, fixture.sellerName, fixture.sellerCode,
			fixture.amount, fixture.amount, fixture.tax)
	}
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for _, name := range sortedNames(files) {
		writer, err := archive.Create(name)
		if err != nil {
			panic(err)
		}
		if _, err := writer.Write([]byte(files[name])); err != nil {
			panic(err)
		}
	}
	if err := archive.Close(); err != nil {
		panic(err)
	}
	return buffer.Bytes()
}

// sortedNames 返回包内条目名，排序只为让生成的 ZIP 字节稳定可比对。
func sortedNames(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// plainInvoice 返回一张字段完整、金额自洽的普通发票。
func plainInvoice(name, code, number, date string) invoiceFixture {
	return invoiceFixture{
		name:       name,
		code:       code,
		number:     number,
		date:       date,
		kind:       "普通发票",
		title:      "增值税电子普通发票",
		sellerName: "卖方公司",
		sellerCode: "911100001111000000",
		buyerName:  "买方公司",
		amount:     "100.00",
		tax:        "13.00",
		total:      "113.00",
	}
}

// batchSources 把一组发票夹具转成批量输入。
func batchSources(fixtures ...invoiceFixture) []Source {
	sources := make([]Source, 0, len(fixtures))
	for _, fixture := range fixtures {
		fixture := fixture
		sources = append(sources, Source{Name: fixture.name, Input: sampleInvoice(fixture)})
	}
	return sources
}

// mustExtractBatch 抽取一批发票，失败时终止测试。
func mustExtractBatch(t *testing.T, sources []Source, options BatchOptions) *Batch {
	t.Helper()
	batch, err := ExtractBatch(sources, options)
	if err != nil {
		t.Fatalf("批量抽取失败: %v", err)
	}
	if batch == nil {
		t.Fatal("批量抽取返回空结果")
	}
	return batch
}

// assertContains 断言文本包含子串。
func assertContains(t *testing.T, text, want string) {
	t.Helper()
	if !strings.Contains(text, want) {
		t.Fatalf("输出缺少 %q，实际：\n%s", want, text)
	}
}

// twoPageInvoice 返回一个两页 OFD：两页文字层都不含票面特征词。
//
// 电子普票常见「第 1 页发票清单 + 第 2 页票面」，这里先提供一个反面样本，
// 用来验证票面页识别在认不出时会退回首页，而不是输出零页。
func twoPageInvoice(t *testing.T) (*parser.Document, io.Closer) {
	t.Helper()
	ofd, err := parser.NewOFD(multiPageInvoiceOFD(t, []string{
		"*餐饮服务*餐费 100.00",
		"*住宿服务*住宿费 200.00",
	}))
	if err != nil {
		t.Fatalf("解析两页发票失败: %v", err)
	}
	if len(ofd.Documents) == 0 {
		t.Fatal("两页发票没有文档体")
	}
	return ofd.Documents[0], ofd
}

// invoiceWithFaceOnSecondPage 返回一个两页 OFD：第 1 页是清单，第 2 页是票面。
func invoiceWithFaceOnSecondPage(t *testing.T) (*parser.Document, io.Closer) {
	t.Helper()
	ofd, err := parser.NewOFD(multiPageInvoiceOFD(t, []string{
		"项目名称 规格型号 数量 单价 金额 税率 税额",
		"发票代码：033001900111 发票号码：12345678 价税合计 壹佰壹拾叁元整",
	}))
	if err != nil {
		t.Fatalf("解析票面在第二页的发票失败: %v", err)
	}
	if len(ofd.Documents) == 0 {
		t.Fatal("两页发票没有文档体")
	}
	return ofd.Documents[0], ofd
}

// multiPageInvoiceOFD 生成每页放一行指定文字的发票 OFD。
func multiPageInvoiceOFD(t *testing.T, pageTexts []string) []byte {
	t.Helper()
	files := map[string]string{
		"OFD.xml": `<?xml version="1.0" encoding="UTF-8"?>
<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD">
  <DocBody>
    <DocInfo><DocID>invoice</DocID><Title>发票</Title></DocInfo>
    <DocRoot>Doc_0/Document.xml</DocRoot>
  </DocBody>
</OFD>`,
		"Doc_0/Attachs/original_invoice.xml": `<?xml version="1.0" encoding="UTF-8"?>
<Invoice xmlns="urn:cn:gov:tax:invoice">
  <InvoiceCode>033001900111</InvoiceCode>
  <InvoiceNo>12345678</InvoiceNo>
  <IssueDate>2026-09-18</IssueDate>
  <TaxExclusiveTotalAmount>100.00</TaxExclusiveTotalAmount>
  <TaxTotalAmount>13.00</TaxTotalAmount>
  <TaxInclusiveTotalAmount>113.00</TaxInclusiveTotalAmount>
  <Buyer><BuyerName>买方公司</BuyerName></Buyer>
  <Seller><SellerName>卖方公司</SellerName><SellerTaxID>911100001111000000</SellerTaxID></Seller>
  <GoodsInfos><G1><Item>服务费</Item><Quantity>1.00</Quantity><Price>100.00</Price>
    <Amount>100.00</Amount><TaxScheme>13%</TaxScheme><TaxAmount>13.00</TaxAmount></G1></GoodsInfos>
</Invoice>`,
	}
	pages := ""
	for index, text := range pageTexts {
		id := index + 1
		pages += fmt.Sprintf(`<Page ID="%d" BaseLoc="Pages/Page_%d/Content.xml"/>`, id, index)
		files[fmt.Sprintf("Doc_0/Pages/Page_%d/Content.xml", index)] = fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Page xmlns="http://www.ofdspec.org/2016">
  <Area><PhysicalBox>0 0 210 297</PhysicalBox><ApplicationBox>0 0 210 297</ApplicationBox></Area>
  <Content>
    <Layer ID="1" Type="Body">
      <TextObject ID="1" Boundary="20 20 170 10" Font="1" Size="4"><TextCode X="30" Y="20">%s</TextCode></TextObject>
    </Layer>
  </Content>
</Page>`, text)
	}
	files["Doc_0/Document.xml"] = fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Document xmlns="http://www.ofdspec.org/2016">
  <CommonData>
    <MaxUnitID>10</MaxUnitID>
    <PageArea><PhysicalBox>0 0 210 297</PhysicalBox><ApplicationBox>0 0 210 297</ApplicationBox></PageArea>
  </CommonData>
  <Pages>%s</Pages>
</Document>`, pages)
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for _, name := range sortedNames(files) {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
