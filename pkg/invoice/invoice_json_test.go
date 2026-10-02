package invoice

import (
	"strings"
	"testing"

	"github.com/goccy/go-json"
)

// 本文件锁定发票抽取结果的 JSON 形态。
//
// 本包用 github.com/goccy/go-json，与 internal/jobstore、internal/runner、
// cmd/ofd-server 一致。go-json v0.11.2 起（#675）改为信任标准库实现的
// Marshaler/Unmarshaler，而 Decimal 恰好实现了 json.Marshaler，所以这条路径的
// 实际输出由那次变更决定，值得单独锁住。
//
// Decimal 只实现了 MarshalJSON、没实现 UnmarshalJSON，因此 Invoice 是单向的：
// 能序列化，不能反序列化。TestInvoiceJSONIsWriteOnly 记录这一事实，
// TestInvoiceJSONShape 因此只断言序列化形态。

// TestInvoiceJSONShape 断言金额、数量、税率的序列化形态。
//
// 金额走 Decimal.MarshalJSON，输出十进制字符串而不是 JSON 数字——金额用浮点数
// 表示会有精度丢失。这里守住三点：输出是字符串、取最小十进制形式、税率归一化
// 后的形态不被改写。
func TestInvoiceJSONShape(t *testing.T) {
	value := Invoice{
		Title:       "增值税电子普通发票",
		Amount:      mustDecimal(t, "1280.00"),
		TaxAmount:   mustDecimal(t, "166.40"),
		TotalAmount: mustDecimal(t, "1446.40"),
		Details: []Detail{{
			Name:    "软件服务",
			Count:   mustDecimal(t, "2"),
			Price:   mustDecimal(t, "640.00"),
			Amount:  mustDecimal(t, "1280.00"),
			TaxRate: mustDecimal(t, "0.13"),
		}},
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	text := string(encoded)

	// 金额必须是带引号的字符串，不能是裸数字。
	for _, field := range []string{"amount", "tax_amount", "total_amount"} {
		if !strings.Contains(text, `"`+field+`":"`) {
			t.Errorf("%s 应输出为字符串，实际 JSON：%s", field, text)
		}
	}
	if strings.Contains(text, `"amount":1`) {
		t.Errorf("amount 不应输出为裸数字，实际 JSON：%s", text)
	}
	// decimalString 输出的是最小十进制形式，不保留尾随零：1280.00 → "1280"、
	// 166.40 → "166.4"。这与金额的语义一致（两者是同一笔钱），也避免了
	// 输出宽度随来源文档的书写习惯变化。
	for _, want := range []string{`"amount":"1280"`, `"tax_amount":"166.4"`,
		`"total_amount":"1446.4"`, `"price":"640"`} {
		if !strings.Contains(text, want) {
			t.Errorf("应输出最小十进制形式 %s，实际 JSON：%s", want, text)
		}
	}
	// 税率保留归一化后的小数形态。
	if !strings.Contains(text, `"tax_rate":"0.13"`) {
		t.Errorf("tax_rate 应输出 0.13，实际 JSON：%s", text)
	}
	// 明细行也要带上金额字段。
	if !strings.Contains(text, `"details":[{`) {
		t.Errorf("details 应输出为数组，实际 JSON：%s", text)
	}
}

// TestInvoiceJSONOmitsEmptyAmounts 确认 nil 金额的字段被省略，避免下游把
// 「没有这个字段」当成「金额是零」。
func TestInvoiceJSONOmitsEmptyAmounts(t *testing.T) {
	encoded, err := json.Marshal(Invoice{Title: "无金额"})
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	for _, field := range []string{"amount", "tax_amount", "total_amount", "tax_rate"} {
		if strings.Contains(string(encoded), `"`+field+`"`) {
			t.Errorf("零值字段 %s 应省略，实际 JSON：%s", field, encoded)
		}
	}
}

// TestInvoiceJSONRejectsNonStringAmount 确认金额字段只接受字符串形式：写成
// JSON 数字会报错，而不是被静默截断成整数。
func TestInvoiceJSONRejectsNonStringAmount(t *testing.T) {
	for _, raw := range []string{`{"amount":1280}`, `{"amount":1280.00}`, `{"amount":true}`} {
		var value Invoice
		if err := json.Unmarshal([]byte(raw), &value); err == nil {
			t.Errorf("%s 应报错，实际解析为 %v", raw, value.Amount)
		}
	}
}

// TestInvoiceJSONIsWriteOnly 记录 Invoice 是单向的：Decimal 实现了
// MarshalJSON 但没有 UnmarshalJSON，所以序列化产物无法再读回 Invoice。
//
// 这是当前设计如此——抽取结果只往标准输出写，没有读回 Invoice 的调用方
// （cmd/ofd-invoice 的 writeJSON 是唯一出口）。若将来需要读回，
// 必须先给 Decimal 补 UnmarshalJSON，本测试会提醒那时它该变成什么行为。
func TestInvoiceJSONIsWriteOnly(t *testing.T) {
	encoded, err := json.Marshal(Invoice{Title: "增值税电子普通发票", Amount: mustDecimal(t, "1280.00")})
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var decoded Invoice
	err = json.Unmarshal(encoded, &decoded)
	if err == nil {
		t.Fatalf("Invoice 目前不可反序列化；若已补上 Decimal.UnmarshalJSON，请更新本测试与 TestInvoiceJSONShape 的往返断言")
	}
	if !strings.Contains(err.Error(), "invoice.Decimal") {
		t.Errorf("错误信息应指向 Decimal 缺少 UnmarshalJSON，实际：%v", err)
	}
}

// mustDecimal 解析十进制文本，取不到数值时终止测试。
func mustDecimal(t *testing.T, text string) *Decimal {
	t.Helper()
	value := newDecimal(text)
	if value == nil {
		t.Fatalf("解析金额 %q 失败", text)
	}
	return value
}
