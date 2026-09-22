// Package invoice 从 OFD 电子发票中抽取结构化发票信息。
//
// 抽取以 OFD 包内嵌的发票结构化附件（通常为 Doc_0/Attachs/original_invoice.xml）
// 为主，附件缺失不可用时返回错误；页面文字层只用于补齐票面视觉字段（发票标题、
// 价税合计大写金额），并据此推断发票类型。不涉及 PDF 等非 OFD 输入。
package invoice

import (
	"encoding/json"
	"math/big"
	"regexp"
	"strings"
)

// Invoice 是一张电子发票的抽取结果。
type Invoice struct {
	// Title 是票面标题，例如「增值税电子普通发票」。
	Title string `json:"title,omitempty"`
	// Type 是由标题推断的发票类型：普通发票、专用发票或通行费。
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
	// Attachment 是实际使用到的附件包内路径。
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

// Decimal 表示精确的十进制数值（金额、数量、税率），基于任意精度的有理数。
// JSON 序列化为十进制字符串（如 "123.45"），避免浮点误差。
type Decimal struct {
	rat *big.Rat
}

// MarshalJSON 把 Decimal 输出为十进制字符串。
func (d *Decimal) MarshalJSON() ([]byte, error) {
	if d == nil || d.rat == nil {
		return []byte("null"), nil
	}
	return json.Marshal(decimalString(d.rat))
}

// String 返回 Decimal 的十进制字符串表示。
func (d *Decimal) String() string {
	return decimalString(d.rat)
}

var numberPattern = regexp.MustCompile(`[+-]?[0-9]+(?:\.[0-9]+)?`)

// newDecimal 从文本中提取第一个十进制数值；找不到合法数值时返回 nil。
func newDecimal(text string) *Decimal {
	match := numberPattern.FindString(strings.TrimSpace(text))
	if match == "" {
		return nil
	}
	rat, ok := new(big.Rat).SetString(match)
	if !ok {
		return nil
	}
	return &Decimal{rat: rat}
}

// percentDecimal 把「13%」「13」「0.13」等税率文本解析为小数形式（0.13）。
// 带百分号的按百分比处理；不带百分号的，绝对值大于 1 视为百分比，否则视为小数。
func percentDecimal(text string) *Decimal {
	trimmed := strings.TrimSpace(text)
	percent := strings.HasSuffix(trimmed, "%")
	cleaned := strings.TrimSuffix(trimmed, "%")
	decimal := newDecimal(cleaned)
	if decimal == nil || decimal.rat == nil {
		return nil
	}
	if !percent && new(big.Rat).Abs(new(big.Rat).Set(decimal.rat)).Cmp(big.NewRat(1, 1)) <= 0 {
		return decimal
	}
	return &Decimal{rat: new(big.Rat).Quo(decimal.rat, big.NewRat(100, 1))}
}

// decimalString 把有理数输出为十进制字符串；无法整除的小数部分最多展开 64 位。
func decimalString(rat *big.Rat) string {
	if rat == nil {
		return ""
	}
	if rat.Sign() == 0 {
		return "0"
	}
	sign := ""
	value := new(big.Rat).Set(rat)
	if value.Sign() < 0 {
		sign = "-"
		value.Neg(value)
	}
	num := value.Num()
	den := value.Denom()
	integer, remainder := new(big.Int).QuoRem(num, den, new(big.Int))
	if remainder.Sign() == 0 {
		return sign + integer.String()
	}
	var fraction strings.Builder
	scale := new(big.Int).Set(remainder)
	ten := big.NewInt(10)
	for i := 0; i < 64 && scale.Sign() != 0; i++ {
		scale.Mul(scale, ten)
		digit, next := new(big.Int).QuoRem(scale, den, new(big.Int))
		fraction.WriteString(digit.String())
		scale = next
	}
	return sign + integer.String() + "." + fraction.String()
}
