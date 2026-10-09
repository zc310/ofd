package invoice

import (
	"strings"
	"testing"
)

// TestDecimalRoundHalfAwayFromZero 守住金额进位方向。
//
// 票据金额的「四舍五入」是逢五进一（half away from zero），不是 Go 默认的
// banker's rounding，也不是向零截断。0.005 进位成 0.01、-0.005 进位成 -0.01
// 才是财务口径；这里错一位，跨月汇总就会和开票系统对不上。
func TestDecimalRoundHalfAwayFromZero(t *testing.T) {
	cases := []struct {
		text   string
		places int
		want   string
	}{
		{"1.005", 2, "1.01"},
		{"-1.005", 2, "-1.01"},
		{"2.675", 2, "2.68"},
		{"-2.675", 2, "-2.68"},
		{"1.004", 2, "1"},
		{"1.5", 0, "2"},
		{"-1.5", 0, "-2"},
		{"1280", 2, "1280"},
		{"0.994", 2, "0.99"},
	}
	for _, c := range cases {
		decimal := mustDecimal(t, c.text)
		got := decimal.Round(c.places).String()
		if got != c.want {
			t.Errorf("Round(%s, %d) = %s，期望 %s", c.text, c.places, got, c.want)
		}
	}
}

// TestDecimalRoundTenthTruncatesBeyondScale 确认超出目标位数的小数位直接舍去，
// 而不是让它们继续参与进位（否则 1.999 按两位会进成 2.00 之外还带着误差）。
func TestDecimalRoundBeyondScale(t *testing.T) {
	if got := mustDecimal(t, "1.005").Round(1).String(); got != "1" {
		t.Errorf("Round(1.005, 1) = %s，期望 1", got)
	}
	if got := mustDecimal(t, "1.015").Round(1).String(); got != "1" {
		t.Errorf("Round(1.015, 1) = %s，期望 1", got)
	}
}

// TestDecimalSumSkipsNil 确认合计跳过缺字段的金额，并区分「合计为零」与
// 「根本没有金额」：后者必须返回 nil，否则 JSON 会输出 "amount": "0"，
// 下游无法判断这批票是否带金额信息。
func TestDecimalSumSkipsNil(t *testing.T) {
	if got := Sum([]*Decimal{nil, nil}); got != nil {
		t.Errorf("全部为 nil 时 Sum 应返回 nil，实际 %v", got)
	}
	if got := Sum(nil); got != nil {
		t.Errorf("空集合 Sum 应返回 nil，实际 %v", got)
	}
	sum := Sum([]*Decimal{mustDecimal(t, "100.00"), nil, mustDecimal(t, "13.00")})
	if got := sum.String(); got != "113" {
		t.Errorf("Sum = %s，期望 113", got)
	}
	if Sum([]*Decimal{mustDecimal(t, "-100"), mustDecimal(t, "100")}) == nil {
		t.Error("正负相抵后合计为零时不应返回 nil")
	}
}

// TestDecimalArithmeticTreatsNilAsAbsent 确认 nil 参与运算的结果仍是 nil。
//
// nil 表示「票面上没有这个数」，把它当 0 累加会让缺字段的发票静默混入合计，
// 对账时无法发现这一张票的金额是抽出来的还是压根没有。
func TestDecimalArithmeticTreatsNilAsAbsent(t *testing.T) {
	value := mustDecimal(t, "10")
	if got := value.Add(nil); got != nil {
		t.Errorf("nil.Add 应为 nil，实际 %v", got)
	}
	if got := (*Decimal)(nil).Add(value); got != nil {
		t.Errorf("Add(nil) 应为 nil，实际 %v", got)
	}
	if got := value.Sub(nil); got != nil {
		t.Errorf("Sub(nil) 应为 nil，实际 %v", got)
	}
	if got := value.Mul(nil); got != nil {
		t.Errorf("Mul(nil) 应为 nil，实际 %v", got)
	}
	if got := value.Neg().String(); got != "-10" {
		t.Errorf("Neg = %s，期望 -10", got)
	}
	if got := value.Neg().Abs().String(); got != "10" {
		t.Errorf("Abs = %s，期望 10", got)
	}
}

// TestDecimalSubReportsMismatch 确认相减结果保留负号，便于「合计 - 已报销」
// 这类场景直接看出超额。
func TestDecimalSubReportsMismatch(t *testing.T) {
	got := mustDecimal(t, "100.00").Sub(mustDecimal(t, "113.00")).Money()
	if got.String() != "-13" {
		t.Errorf("100.00-113.00 = %s，期望 -13", got)
	}
	if !got.IsNegative() {
		t.Error("相减为负时应报告 IsNegative")
	}
}

// TestDecimalMoneyRoundsSumToCents 确认求和后按分量化。
//
// 1/3 这类除不尽的数直接字符串相加会展开成 64 位小数，财务不可对账；
// 合计必须先 Money() 再输出。
func TestDecimalMoneyRoundsSumToCents(t *testing.T) {
	third, ok := NewDecimal("1/3")
	if !ok {
		t.Fatal("解析分数 1/3 失败")
	}
	total := Sum([]*Decimal{third, third, third})
	if total.IsZero() {
		t.Fatal("1/3 三次求和不应为零")
	}
	money := total.Money()
	if money.String() != "1" {
		t.Errorf("Money() = %s，期望 1", money)
	}
	if strings.Contains(money.Fixed(4), "3333") {
		t.Errorf("Fixed(4) 不应重复展开余数，实际 %s", money.Fixed(4))
	}
}

// TestDecimalFixedKeepsTwoDecimals 确认 Fixed 输出定长两位，供 CSV/XLSX 报表对齐。
func TestDecimalFixedKeepsTwoDecimals(t *testing.T) {
	cases := []struct {
		text  string
		want  string
		empty bool
	}{
		{text: "1280", want: "1280.00"},
		{text: "1280.5", want: "1280.50"},
		{text: "-13.456", want: "-13.46"},
		{text: "0", want: "0.00"},
	}
	for _, c := range cases {
		if got := mustDecimal(t, c.text).Fixed(MoneyScale); got != c.want {
			t.Errorf("Fixed(%s) = %s，期望 %s", c.text, got, c.want)
		}
	}
	var missing *Decimal
	if got := missing.Fixed(MoneyScale); got != "" {
		t.Errorf("nil.Fixed 应为空字符串，实际 %q", got)
	}
}

// TestNewDecimalRejectsNonNumeric 确认公开构造入口对垃圾文本报错而不是返回 0。
func TestNewDecimalRejectsNonNumeric(t *testing.T) {
	for _, text := range []string{"", "   ", "元", "12,80", "1.2.3", "--5"} {
		if value, ok := NewDecimal(text); ok {
			t.Errorf("NewDecimal(%q) 应失败，实际 %v", text, value)
		}
	}
	value, ok := NewDecimal("  -13.50 ")
	if !ok || value.String() != "-13.5" {
		t.Errorf("NewDecimal(\"  -13.50 \") = %v, %v", value, ok)
	}
}

// TestDecimalCmpTreatsNilAsZero 确认比较时 nil 视作零，让缺失金额不会打乱排序。
func TestDecimalCmpTreatsNilAsZero(t *testing.T) {
	var missing *Decimal
	if got := missing.Cmp(mustDecimal(t, "0")); got != 0 {
		t.Errorf("nil 与 0 比较应相等，实际 %d", got)
	}
	if got := missing.Cmp(mustDecimal(t, "1")); got != -1 {
		t.Errorf("nil 与 1 比较应为 -1，实际 %d", got)
	}
	if !missing.IsZero() {
		t.Error("nil 应视为零")
	}
}

// TestDecimalZeroValueIsNumber 确认零值 &Decimal{} 是合法的 0，不是「缺字段」。
func TestDecimalZeroValueIsNumber(t *testing.T) {
	zero := &Decimal{}
	if !zero.IsZero() {
		t.Error("零值 Decimal 应为零")
	}
	if got := zero.Add(mustDecimal(t, "1")).String(); got != "1" {
		t.Errorf("零值相加 = %s，期望 1", got)
	}
}
