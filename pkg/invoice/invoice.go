// Package invoice 从 OFD 电子发票中抽取结构化发票信息，并支持多票汇总、对账
// 报表与合并打印。
//
// 抽取以 OFD 包内嵌的发票结构化附件（通常为 Doc_0/Attachs/original_invoice.xml）
// 为主；没有有效附件时，按文档自定义标引的 ObjectRef 引用读取页面中的发票字段。
// 两种结构化数据来源均不可用时返回错误。页面文字层另用于补齐票面视觉字段（发票标题、
// 价税合计大写金额），并据此推断发票类型。不涉及 PDF 等非 OFD 输入。
//
// 单张发票用 Extract；多张发票用 ExtractBatch 批量抽取并汇总金额（按销售方、
// 发票类型和开票月份分组），用 WriteCSV 与 WriteXLSX 导出对账报表，用 MergePDF
// 合并成一个便于打印的 PDF。金额基于任意精度有理数，求和后按最小货币单位舍入，
// 红字（红冲）发票的负金额照常参与合计。
//
// 批量处理默认严格：任一输入抽不出发票结构化数据即整批报错，避免金额在无人
// 察觉时少算；BatchOptions.SkipBroken 与 BatchOptions.Dedup 分别放宽失败输入
// 和排除重复发票，两者都会改变参与合计的口径。
package invoice

// Invoice 是一张电子发票的抽取结果。
type Invoice struct {
	// Title 是票面标题，例如「增值税电子普通发票」。
	Title string `json:"title,omitempty"`
	// Type 是由标题推断的发票类型：普通发票、专用发票、通行费或铁路电子客票。
	Type string `json:"type,omitempty"`
	// MachineNumber 是机器编号。
	MachineNumber string `json:"machine_number,omitempty"`
	// Code 是发票代码。
	Code string `json:"code,omitempty"`
	// Number 是发票号码。
	Number string `json:"number,omitempty"`
	// Date 是开票日期（通常为 YYYY-MM-DD）。
	Date string `json:"date,omitempty"`
	// Checksum 是发票校验码。
	Checksum string `json:"checksum,omitempty"`
	// TotalAmountString 是价税合计的大写金额文本，来自页面文字层。
	TotalAmountString string `json:"total_amount_string,omitempty"`
	// Amount 是不含税金额。
	Amount *Decimal `json:"amount,omitempty"`
	// TaxAmount 是税额。
	TaxAmount *Decimal `json:"tax_amount,omitempty"`
	// TotalAmount 是价税合计（小写）。
	TotalAmount *Decimal `json:"total_amount,omitempty"`
	// TaxControlCode 是税控码。
	TaxControlCode string `json:"tax_control_code,omitempty"`
	// Payee 是收款人。
	Payee string `json:"payee,omitempty"`
	// Reviewer 是复核人。
	Reviewer string `json:"reviewer,omitempty"`
	// Drawer 是开票人。
	Drawer string `json:"drawer,omitempty"`
	// Buyer 是购买方信息。
	Buyer Party `json:"buyer"`
	// Seller 是销售方信息。
	Seller Party `json:"seller"`
	// Details 是商品或劳务明细行。
	Details []Detail `json:"details"`
	// Attachment 是实际使用到的结构化附件或自定义标引数据文件的包内路径。
	Attachment string `json:"attachment"`
	// Warnings 是抽取过程中的非致命提示。
	Warnings []string `json:"warnings,omitempty"`
}

// Party 是发票一方的资料（购买方或销售方）。
type Party struct {
	// Name 是名称。
	Name string `json:"name,omitempty"`
	// Code 是纳税人识别号。
	Code string `json:"code,omitempty"`
	// Address 是地址电话。
	Address string `json:"address,omitempty"`
	// Account 是开户行及账号。
	Account string `json:"account,omitempty"`
}

// Detail 是一行价税明细。
type Detail struct {
	// Name 是项目名称。
	Name string `json:"name,omitempty"`
	// Model 是规格型号。
	Model string `json:"model,omitempty"`
	// Unit 是单位。
	Unit string `json:"unit,omitempty"`
	// Count 是数量。
	Count *Decimal `json:"count,omitempty"`
	// Price 是单价。
	Price *Decimal `json:"price,omitempty"`
	// Amount 是金额。
	Amount *Decimal `json:"amount,omitempty"`
	// TaxRate 是税率（小数形式，如 0.13）。
	TaxRate *Decimal `json:"tax_rate,omitempty"`
	// TaxAmount 是税额。
	TaxAmount *Decimal `json:"tax_amount,omitempty"`
}
