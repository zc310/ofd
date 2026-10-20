package invoice

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestExtractCustomTagInvoiceFixture(t *testing.T) {
	data, err := os.ReadFile("../../testdata/ofdrw/20240531141733.ofd")
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("缺少 OFD 测试文件：20240531141733.ofd")
	}
	if err != nil {
		t.Fatal(err)
	}
	got, err := Extract(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "电子发票（增值税专用发票）" || got.Type != "专用发票" {
		t.Errorf("标题/类型 = %q/%q", got.Title, got.Type)
	}
	if got.Number != "24112000000048542163" || got.Code != "" || got.Date != "2024年05月28日" {
		t.Errorf("号码/代码/日期 = %q/%q/%q", got.Number, got.Code, got.Date)
	}
	if got.Buyer.Name != "海南麒麟云服科技有限公司" || got.Buyer.Code != "91460300MAA92DJN95" {
		t.Errorf("购买方 = %+v", got.Buyer)
	}
	if got.Seller.Name != "北京互联三六五科技有限公司" || got.Seller.Code != "91110108690017781B" {
		t.Errorf("销售方 = %+v", got.Seller)
	}
	for field, value := range map[string]struct {
		got  *Decimal
		want string
	}{
		"Amount": {got.Amount, "4197.17"}, "TaxAmount": {got.TaxAmount, "251.83"},
		"TotalAmount": {got.TotalAmount, "4449"},
	} {
		if value.got == nil || value.got.String() != value.want {
			t.Errorf("%s = %v, want %s", field, value.got, value.want)
		}
	}
	if got.TotalAmountString != "肆仟肆佰肆拾玖圆整" || got.Drawer != "于俪" {
		t.Errorf("大写金额/开票人 = %q/%q", got.TotalAmountString, got.Drawer)
	}
	if got.Attachment != "Doc_0/Tags/CustomTag.xml" {
		t.Errorf("Attachment = %q", got.Attachment)
	}
	if len(got.Details) != 1 {
		t.Fatalf("明细 = %+v", got.Details)
	}
	detail := got.Details[0]
	if detail.Name != "*信息技术服务*技术服务费" || detail.Count == nil || detail.Count.String() != "1" ||
		detail.Price == nil || detail.Price.String() != "4197.16981132075" ||
		detail.Amount == nil || detail.Amount.String() != "4197.17" ||
		detail.TaxRate == nil || detail.TaxRate.String() != "0.06" ||
		detail.TaxAmount == nil || detail.TaxAmount.String() != "251.83" {
		t.Errorf("明细 = %+v", detail)
	}
	if issues := got.Check(); len(issues) != 0 {
		t.Errorf("自校验 = %v", issues)
	}
	if len(got.Warnings) != 0 {
		t.Errorf("Warnings = %v", got.Warnings)
	}
}

// 两页故意复用文字对象 ID，验证按 PageRef 定位；同一字段引用多个对象时按引用顺序拼接。
func customTagInvoiceFiles() map[string]string {
	return map[string]string{
		"Doc_0/Document.xml": `<Document xmlns="http://www.ofdspec.org/2016">
<CommonData><MaxUnitID>100</MaxUnitID><PageArea><PhysicalBox>0 0 210 140</PhysicalBox></PageArea></CommonData>
<Pages><Page ID="10" BaseLoc="Pages/Page_0/Content.xml"/><Page ID="20" BaseLoc="Pages/Page_1/Content.xml"/></Pages>
<CustomTags>Metadata/tags.xml</CustomTags></Document>`,
		"Doc_0/Metadata/tags.xml": `<CustomTags xmlns="http://www.ofdspec.org/2016">
<CustomTag TypeID="invoice"><FileLoc>data/invoice.xml</FileLoc></CustomTag></CustomTags>`,
		"Doc_0/Metadata/data/invoice.xml": `<root>
<InvoiceNo><ObjectRef PageRef="10">1</ObjectRef></InvoiceNo>
<TaxInclusiveTotalAmount><ObjectRef PageRef="20">2</ObjectRef><ObjectRef PageRef="20">1</ObjectRef></TaxInclusiveTotalAmount>
<Item><ObjectRef PageRef="10">3</ObjectRef></Item><Amount><ObjectRef PageRef="10">4</ObjectRef></Amount>
<TaxScheme><ObjectRef PageRef="10">5</ObjectRef></TaxScheme>
<Item><ObjectRef PageRef="20">3</ObjectRef></Item><Amount><ObjectRef PageRef="20">4</ObjectRef></Amount>
<TaxScheme><ObjectRef PageRef="20">5</ObjectRef></TaxScheme></root>`,
		"Doc_0/Pages/Page_0/Content.xml": `<Page xmlns="http://www.ofdspec.org/2016"><Content><Layer ID="50">
<TextObject ID="1" Boundary="0 0 20 5" Font="1" Size="3"><TextCode>1234567890</TextCode></TextObject>
<PageBlock ID="60"><TextObject ID="3" Boundary="0 10 20 5" Font="1" Size="3"><TextCode>服务</TextCode><TextCode>甲</TextCode></TextObject></PageBlock>
<TextObject ID="4" Boundary="20 10 20 5" Font="1" Size="3"><TextCode>100</TextCode></TextObject>
<TextObject ID="5" Boundary="40 10 20 5" Font="1" Size="3"><TextCode>6%</TextCode></TextObject>
</Layer></Content></Page>`,
		"Doc_0/Pages/Page_1/Content.xml": `<Page xmlns="http://www.ofdspec.org/2016"><Content><Layer ID="51">
<TextObject ID="1" Boundary="0 0 20 5" Font="1" Size="3"><TextCode>318.00</TextCode></TextObject>
<TextObject ID="2" Boundary="20 0 5 5" Font="1" Size="3"><TextCode>¥</TextCode></TextObject>
<TextObject ID="3" Boundary="0 10 20 5" Font="1" Size="3"><TextCode>服务乙</TextCode></TextObject>
<TextObject ID="4" Boundary="20 10 20 5" Font="1" Size="3"><TextCode>200</TextCode></TextObject>
<TextObject ID="5" Boundary="40 10 20 5" Font="1" Size="3"><TextCode>0.06</TextCode></TextObject>
</Layer></Content></Page>`,
	}
}

func TestExtractCustomTagsResolvesPagesAndGroupsDetails(t *testing.T) {
	got, err := Extract(buildInvoiceOFDFiles(t, customTagInvoiceFiles(), ""))
	if err != nil {
		t.Fatal(err)
	}
	if got.Number != "1234567890" || got.TotalAmount == nil || got.TotalAmount.String() != "318" {
		t.Errorf("号码/总额 = %q/%v", got.Number, got.TotalAmount)
	}
	if len(got.Details) != 2 {
		t.Fatalf("明细 = %+v", got.Details)
	}
	for index, want := range []string{"服务甲", "服务乙"} {
		detail := got.Details[index]
		if detail.Name != want || detail.TaxRate == nil || detail.TaxRate.String() != "0.06" {
			t.Errorf("明细 %d = %+v", index, detail)
		}
	}
	if got.Details[0].Amount.String() != "100" || got.Details[1].Amount.String() != "200" {
		t.Errorf("明细金额 = %v/%v", got.Details[0].Amount, got.Details[1].Amount)
	}
}

func TestExtractCustomTagsRejectsMissingReferences(t *testing.T) {
	for _, tc := range []struct {
		name, old, replacement, want string
	}{
		{"缺少页面", `PageRef="10">1`, `PageRef="99">1`, "PageRef 99"},
		{"缺少对象", `PageRef="10">1`, `PageRef="10">99`, "ObjectRef 99"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := customTagInvoiceFiles()
			files["Doc_0/Metadata/data/invoice.xml"] = strings.Replace(files["Doc_0/Metadata/data/invoice.xml"], tc.old, tc.replacement, 1)
			_, err := Extract(buildInvoiceOFDFiles(t, files, ""))
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "Doc_0/Metadata/data/invoice.xml") {
				t.Fatalf("错误 = %v, want 指明标引路径及 %s", err, tc.want)
			}
		})
	}
}

func TestExtractCustomTagsRejectsNonInvoiceData(t *testing.T) {
	files := customTagInvoiceFiles()
	for _, raw := range []string{
		`<root><Buyer><BuyerName>只有名称</BuyerName></Buyer></root>`,
		`<root><InvoiceNo><ObjectRef PageRef="10">1</ObjectRef></InvoiceNo></root>`,
	} {
		files["Doc_0/Metadata/data/invoice.xml"] = raw
		if _, err := Extract(buildInvoiceOFDFiles(t, files, "")); !errors.Is(err, ErrNotInvoice) {
			t.Errorf("错误 = %v, want ErrNotInvoice", err)
		}
	}
}

func TestExtractPrefersAttachmentOverCustomTags(t *testing.T) {
	files := customTagInvoiceFiles()
	files[preferredAttachment] = sampleAttachment
	files["Doc_0/Metadata/data/invoice.xml"] = `<broken`
	got, err := Extract(buildInvoiceOFDFiles(t, files, ""))
	if err != nil {
		t.Fatal(err)
	}
	if got.Attachment != preferredAttachment || got.Number != "12345678" || got.TotalAmount.String() != "113" {
		t.Errorf("附件优先结果 = %+v", got)
	}
}

func TestExtractCustomTagsAfterNonInvoiceAttachments(t *testing.T) {
	for _, attachment := range []string{preferredAttachment, "Doc_0/Attachs/notes.xml"} {
		t.Run(attachment, func(t *testing.T) {
			files := customTagInvoiceFiles()
			files[attachment] = `<root><Note>不是发票</Note></root>`
			got, err := Extract(buildInvoiceOFDFiles(t, files, ""))
			if err != nil {
				t.Fatal(err)
			}
			if got.Number != "1234567890" || got.Attachment != "Doc_0/Metadata/data/invoice.xml" {
				t.Errorf("自定义标引结果 = %+v", got)
			}
		})
	}
}
