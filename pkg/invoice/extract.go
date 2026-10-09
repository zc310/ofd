package invoice

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/textdoc"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const maxAttachmentBytes = 16 << 20

// ErrNoAttachment 表示 OFD 包内未找到发票结构化附件。
var ErrNoAttachment = errors.New("未找到 OFD 发票结构化附件（Attachs 目录下的 XML）")

// ErrNotInvoice 表示附件存在，但里面不是发票数据：既没有发票代码，也没有价税合计。
//
// 与 ErrNoAttachment 分开是因为两者的后续动作不同：找不到附件时厂商可能把附件放在了
// 别处（值得确认），而附件里没有发票数据就是这份文档本身不是发票（没什么可查）。
var ErrNotInvoice = errors.New("附件不是发票数据：既无发票代码，也无价税合计")

// 常见发票附加 XML 的文件名首选项，优先匹配规范的 original_invoice.xml。
var preferredAttachment = "Doc_0/Attachs/original_invoice.xml"

// Extract 从 OFD 电子发票中抽取发票信息。input 支持文件路径（string）、
// 文件数据（[]byte）或 io.Reader。
//
// 抽取以 OFD 包内嵌的结构化附件为主：先尝试 Doc_0/Attachs/original_invoice.xml，
// 不存在时扫描所有包含 attach 的 XML 条目，取第一个带发票代码或价税合计的文件。
// 之后读取页面文字层补齐票面标题与价税合计大写金额，并按标题推断发票类型。
//
// 附件存在但没有发票代码也没有价税合计时返回 ErrNotInvoice，不返回空壳：这种附件
// 多半是版式说明或签章属性，兜一个几乎全空的 Invoice 会让调用方看不出出错。
func Extract(input any) (*Invoice, error) {
	data, err := readInput(input)
	if err != nil {
		return nil, err
	}
	attachment, raw, err := loadAttachment(data)
	if err != nil {
		return nil, err
	}
	invoice, err := parseAttachment(raw)
	if err != nil {
		return nil, fmt.Errorf("解析发票附件 %s 失败: %w", attachment, err)
	}
	invoice.Attachment = attachment
	supplementFromText(invoice, data)
	return invoice, nil
}

func readInput(input any) ([]byte, error) {
	switch value := input.(type) {
	case string:
		data, err := os.ReadFile(value)
		if err != nil {
			return nil, fmt.Errorf("读取 OFD 文件失败: %w", err)
		}
		return data, nil
	case []byte:
		return value, nil
	case io.Reader:
		data, err := io.ReadAll(value)
		if err != nil {
			return nil, fmt.Errorf("读取 OFD 内容失败: %w", err)
		}
		return data, nil
	default:
		return nil, fmt.Errorf("不支持的类型: %T, 请提供文件路径(string)、文件数据([]byte)或 io.Reader", input)
	}
}

// loadAttachment 在 OFD 包内定位并读取发票结构化附件。
func loadAttachment(data []byte) (string, []byte, error) {
	pkg, err := core.OpenBytes(data)
	if err != nil {
		return "", nil, fmt.Errorf("打开 OFD 包失败: %w", err)
	}
	defer pkg.Close()
	path, err := findAttachment(pkg)
	if err != nil {
		return "", nil, err
	}
	raw, err := pkg.ReadLimit(path, maxAttachmentBytes)
	if err != nil {
		return "", nil, fmt.Errorf("读取发票附件 %s 失败: %w", path, err)
	}
	return path, raw, nil
}

// findAttachment 返回实际使用的发票附件包内路径。
func findAttachment(pkg *core.Package) (string, error) {
	if pkg.Has(preferredAttachment) {
		return preferredAttachment, nil
	}
	candidates := make([]string, 0, 4)
	for _, entry := range pkg.Entries() {
		if entry.IsDir || !strings.HasSuffix(strings.ToLower(entry.Path), ".xml") {
			continue
		}
		if strings.Contains(strings.ToLower(entry.Path), "attach") {
			candidates = append(candidates, entry.Path)
		}
	}
	sort.Strings(candidates)
	// 记下"读到过能解析但不是发票"的候选：这时报"不是发票"比报"没有附件"准确，
	// 调用方能区分"附件放错地方"与"这份文档根本不是发票"。
	sawNonInvoice := false
	for _, path := range candidates {
		raw, err := pkg.ReadLimit(path, maxAttachmentBytes)
		if err != nil {
			continue
		}
		var document invoiceAttachment
		if err := decodeXML(raw, &document); err != nil {
			continue
		}
		if hasInvoiceData(document) {
			return path, nil
		}
		sawNonInvoice = true
	}
	if sawNonInvoice {
		return "", ErrNotInvoice
	}
	return "", ErrNoAttachment
}

// invoiceAttachment 是发票结构化附件的 XML 解析模型。字段按元素名匹配，
// 与命名空间无关；命名以税额控开具的 original_invoice.xml 字段为准。
type invoiceAttachment struct {
	MachineNo               string           `xml:"MachineNo"`
	InvoiceCode             string           `xml:"InvoiceCode"`
	InvoiceNo               string           `xml:"InvoiceNo"`
	IssueDate               string           `xml:"IssueDate"`
	InvoiceCheckCode        string           `xml:"InvoiceCheckCode"`
	TaxExclusiveTotalAmount string           `xml:"TaxExclusiveTotalAmount"`
	TaxTotalAmount          string           `xml:"TaxTotalAmount"`
	TaxInclusiveTotalAmount string           `xml:"TaxInclusiveTotalAmount"`
	Payee                   string           `xml:"Payee"`
	Checker                 string           `xml:"Checker"`
	InvoiceClerk            string           `xml:"InvoiceClerk"`
	TaxControlCode          string           `xml:"TaxControlCode"`
	Buyer                   buyerAttachment  `xml:"Buyer"`
	Seller                  sellerAttachment `xml:"Seller"`
	GoodsInfos              goodsAttachment  `xml:"GoodsInfos"`
}

type buyerAttachment struct {
	BuyerName             string `xml:"BuyerName"`
	BuyerTaxID            string `xml:"BuyerTaxID"`
	BuyerAddrTel          string `xml:"BuyerAddrTel"`
	BuyerFinancialAccount string `xml:"BuyerFinancialAccount"`
}

type sellerAttachment struct {
	SellerName             string `xml:"SellerName"`
	SellerTaxID            string `xml:"SellerTaxID"`
	SellerAddrTel          string `xml:"SellerAddrTel"`
	SellerFinancialAccount string `xml:"SellerFinancialAccount"`
}

type goodsAttachment struct {
	Items []goodsItem `xml:",any"`
}

type goodsItem struct {
	Item                 string `xml:"Item"`
	Specification        string `xml:"Specification"`
	MeasurementDimension string `xml:"MeasurementDimension"`
	Quantity             string `xml:"Quantity"`
	Price                string `xml:"Price"`
	Amount               string `xml:"Amount"`
	TaxScheme            string `xml:"TaxScheme"`
	TaxRate              string `xml:"TaxRate"`
	TaxAmount            string `xml:"TaxAmount"`
}

// hasInvoiceData 判断附件里是否带发票的标志性数据：发票代码或价税合计。
//
// 只认这两个字段，而不是"有没有填任何内容"。OFD 的 Attachs 目录里不只有发票数据，
// 还可能有版式说明、签章属性之类的 XML，甚至只填一个购方名称。用"非空即有效"判定
// 会给这类文档兜一个几乎全空的 Invoice——调用方拿到的是空壳，看不出哪里不对，
// 后面汇总出 0 元也只会被当成"这批票没金额"。
//
// 两个字段取或：定额发票、卷式发票可能没有发票代码但有金额，代码齐全的票也可能缺
// 金额字段。两者皆无才认定不是发票。
func hasInvoiceData(document invoiceAttachment) bool {
	return strings.TrimSpace(document.InvoiceCode) != "" ||
		strings.TrimSpace(document.TaxInclusiveTotalAmount) != ""
}

// parseAttachment 把附件 XML 映射到 Invoice 模型。
func parseAttachment(raw []byte) (*Invoice, error) {
	var document invoiceAttachment
	if err := decodeXML(raw, &document); err != nil {
		return nil, err
	}
	if !hasInvoiceData(document) {
		return nil, ErrNotInvoice
	}
	invoice := &Invoice{}
	invoice.MachineNumber = strings.TrimSpace(document.MachineNo)
	invoice.Code = strings.TrimSpace(document.InvoiceCode)
	invoice.Number = strings.TrimSpace(document.InvoiceNo)
	invoice.Date = strings.TrimSpace(document.IssueDate)
	invoice.Checksum = strings.TrimSpace(document.InvoiceCheckCode)
	invoice.Amount = newDecimal(document.TaxExclusiveTotalAmount)
	invoice.TaxAmount = newDecimal(document.TaxTotalAmount)
	invoice.TotalAmount = newDecimal(document.TaxInclusiveTotalAmount)
	invoice.Payee = strings.TrimSpace(document.Payee)
	invoice.Reviewer = strings.TrimSpace(document.Checker)
	invoice.Drawer = strings.TrimSpace(document.InvoiceClerk)
	invoice.TaxControlCode = strings.TrimSpace(document.TaxControlCode)
	invoice.Buyer = Party{
		Name:    strings.TrimSpace(document.Buyer.BuyerName),
		Code:    strings.TrimSpace(document.Buyer.BuyerTaxID),
		Address: strings.TrimSpace(document.Buyer.BuyerAddrTel),
		Account: strings.TrimSpace(document.Buyer.BuyerFinancialAccount),
	}
	invoice.Seller = Party{
		Name:    strings.TrimSpace(document.Seller.SellerName),
		Code:    strings.TrimSpace(document.Seller.SellerTaxID),
		Address: strings.TrimSpace(document.Seller.SellerAddrTel),
		Account: strings.TrimSpace(document.Seller.SellerFinancialAccount),
	}
	for _, item := range document.GoodsInfos.Items {
		invoice.Details = append(invoice.Details, parseGoodsItem(item))
	}
	return invoice, nil
}

func parseGoodsItem(item goodsItem) Detail {
	taxRate := percentDecimal(item.TaxScheme)
	if taxRate == nil {
		taxRate = percentDecimal(item.TaxRate)
	}
	return Detail{
		Name:      strings.TrimSpace(item.Item),
		Model:     strings.TrimSpace(item.Specification),
		Unit:      strings.TrimSpace(item.MeasurementDimension),
		Count:     newDecimal(item.Quantity),
		Price:     newDecimal(item.Price),
		Amount:    newDecimal(item.Amount),
		TaxRate:   taxRate,
		TaxAmount: newDecimal(item.TaxAmount),
	}
}

// decodeXML 解析发票附件 XML，并兼容部分第三方按 GBK 等本地编码写出的数据。
func decodeXML(data []byte, target any) error {
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	decoder.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		var reader io.Reader
		var err error
		switch strings.ToLower(charset) {
		case "gbk", "gb2312", "gb18030":
			reader, err = simplifiedchinese.GBK.NewDecoder().Reader(input), nil
		default:
			err = fmt.Errorf("不支持的字符集 %q", charset)
		}
		return reader, err
	}
	return decoder.Decode(target)
}

// supplementFromText 从页面文字层补齐票面标题与价税合计大写金额，并推断类型。
// 页面解析失败等非致命问题只记入 Warnings，不中断抽取。
func supplementFromText(invoice *Invoice, data []byte) {
	ofd, err := parser.NewOFD(data)
	if err != nil {
		invoice.Warnings = append(invoice.Warnings, "页面文字层读取失败: "+err.Error())
		return
	}
	defer ofd.Close()
	pages := textdoc.Collect(ofd.Documents, 0, textdoc.Count(ofd.Documents))
	title := pageTitle(pages)
	invoice.Title = title
	if upper := upperAmount(pages); upper != "" {
		invoice.TotalAmountString = upper
	}
	invoice.Type = invoiceType(title)
}

// pageTitle 返回票面名称。票面标题通常是含「发票/通行费」字样的文字条目中字号最大的
// 一条（可能位于模板文字层），而不是几何位置最左上角；一组候选没有命中时退回左上角条目。
func pageTitle(pages []textdoc.Page) string {
	var candidates []textdoc.Entry
	for _, page := range pages {
		for _, entry := range page.Entries {
			text := strings.TrimSpace(entry.Text)
			if text == "" {
				continue
			}
			if strings.Contains(text, "发票") || strings.Contains(text, "通行费") {
				candidates = append(candidates, entry)
			}
		}
	}
	if len(candidates) == 0 {
		return topLeftPageText(pages)
	}
	best := candidates[0]
	for _, entry := range candidates[1:] {
		higher := entry.Size > best.Size ||
			(entry.Size == best.Size && (entry.Y < best.Y || (entry.Y == best.Y && entry.X < best.X)))
		if higher {
			best = entry
		}
	}
	return strings.TrimSpace(best.Text)
}

// topLeftPageText 返回所有页面中最靠左上角的文字条目，作为标题回退项。
func topLeftPageText(pages []textdoc.Page) string {
	best := ""
	bestY := math.MaxFloat64
	bestX := math.MaxFloat64
	for _, page := range pages {
		for _, entry := range page.Entries {
			text := strings.TrimSpace(entry.Text)
			if text == "" {
				continue
			}
			if entry.Y < bestY || (entry.Y == bestY && entry.X < bestX) {
				best, bestY, bestX = text, entry.Y, entry.X
			}
		}
	}
	return best
}

// upperAmount 返回页面文字层中的价税合计大写金额；多个候选时取最后一个。
func upperAmount(pages []textdoc.Page) string {
	amount := ""
	for _, page := range pages {
		for _, entry := range page.Entries {
			if candidate := amountFromText(entry.Text); candidate != "" {
				amount = candidate
			}
		}
	}
	return amount
}

// amountFromText 从文字条目中截取中文大写金额子串：自首个大写数字字形起，
// 到最后一个「整」为止。找不到大写金额字形时返回空字符串。
func amountFromText(text string) string {
	start := strings.IndexFunc(text, func(r rune) bool {
		return strings.ContainsRune("零壹贰叁肆伍陆柒捌玖拾佰仟万亿圆元整角分", r)
	})
	if start < 0 {
		return ""
	}
	if end := strings.LastIndex(text[start:], "整"); end >= 0 {
		return text[start : start+end+len("整")]
	}
	return text[start:]
}

// invoiceType 按票面标题推断发票类型，与常见发票版式规则保持一致。
func invoiceType(title string) string {
	switch {
	case title == "":
		return ""
	case strings.Contains(title, "专用"):
		return "专用发票"
	case strings.Contains(title, "通行费"):
		return "通行费"
	default:
		return "普通发票"
	}
}
