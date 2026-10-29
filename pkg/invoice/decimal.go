package invoice

import (
	"math/big"
	"regexp"
	"strings"

	"github.com/goccy/go-json/v2"
)

// Decimal 表示精确的十进制数值（金额、数量、税率），基于任意精度的有理数。
// JSON 序列化为十进制字符串（如 "123.45"），避免浮点误差。
//
// 零值和 nil 的含义不同：nil 表示「票面上没有这个数」（序列化时省略该字段），
// 而 &Decimal{} 的零值是合法数字 0。金额相加时 nil 一律按「没有参与」处理，
// 不当成 0 累加。
type Decimal struct {
	rat *big.Rat
}

// MoneyScale 是金额保留的小数位数，即最小货币单位「分」。
const MoneyScale = 2

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

// Sign 返回数值的符号：负数返回 -1，零返回 0，正数返回 1。nil 视为零。
func (d *Decimal) Sign() int {
	if d == nil || d.rat == nil {
		return 0
	}
	return d.rat.Sign()
}

// IsZero 判断数值是否为零；nil 与零值都返回 true。
func (d *Decimal) IsZero() bool {
	return d.Sign() == 0
}

// IsNegative 判断金额是否为负。红字（红冲）发票的金额为负，是正常业务数据，
// 汇总时必须参与求和，不能当异常丢弃。
func (d *Decimal) IsNegative() bool {
	return d.Sign() < 0
}

// Cmp 比较两个数值，返回 -1、0 或 1。nil 视为零，因此 nil 与 0 比较结果为 0。
func (d *Decimal) Cmp(other *Decimal) int {
	return d.ratOrZero().Cmp(other.ratOrZero())
}

// Add 返回两数之和。任一操作数为 nil 时返回 nil：这个数在票面上不存在，
// 把它当成 0 参与求和会让「缺字段」与「金额为零」无法区分。
func (d *Decimal) Add(other *Decimal) *Decimal {
	if d == nil || other == nil {
		return nil
	}
	return &Decimal{rat: new(big.Rat).Add(d.ratOrZero(), other.ratOrZero())}
}

// Sub 返回 d-other。任一操作数为 nil 时返回 nil。
func (d *Decimal) Sub(other *Decimal) *Decimal {
	if d == nil || other == nil {
		return nil
	}
	return &Decimal{rat: new(big.Rat).Sub(d.ratOrZero(), other.ratOrZero())}
}

// Mul 返回两数之积。任一操作数为 nil 时返回 nil。
func (d *Decimal) Mul(other *Decimal) *Decimal {
	if d == nil || other == nil {
		return nil
	}
	return &Decimal{rat: new(big.Rat).Mul(d.ratOrZero(), other.ratOrZero())}
}

// Neg 返回相反数，nil 返回 nil。
func (d *Decimal) Neg() *Decimal {
	if d == nil {
		return nil
	}
	return &Decimal{rat: new(big.Rat).Neg(d.ratOrZero())}
}

// Abs 返回绝对值，nil 返回 nil。
func (d *Decimal) Abs() *Decimal {
	if d == nil {
		return nil
	}
	return &Decimal{rat: new(big.Rat).Abs(d.ratOrZero())}
}

// Sum 返回一组数值之和，逐项跳过 nil。全部为 nil 时返回 nil（区别于合计为零），
// 以便调用方判断「这批票根本没有金额」与「这批票金额都是 0」。
func Sum(values []*Decimal) *Decimal {
	var total *big.Rat
	for _, value := range values {
		if value == nil || value.rat == nil {
			continue
		}
		if total == nil {
			total = new(big.Rat)
		}
		total.Add(total, value.rat)
	}
	if total == nil {
		return nil
	}
	return &Decimal{rat: total}
}

// Round 返回按 places 位小数四舍五入的结果，恰好在中点时远离零（half away
// from zero），与财务票据的进位习惯一致。places 为负时按 0 处理，nil 返回 nil。
//
// 不做舍入的求和会输出 64 位小数（例如 1/3 展开成 0.333…），这种数字无法对账，
// 金额合计必须先过 Round(MoneyScale)。
func (d *Decimal) Round(places int) *Decimal {
	if d == nil || d.rat == nil {
		return nil
	}
	if places < 0 {
		places = 0
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(places)), nil)
	scaled := new(big.Rat).Mul(d.rat, new(big.Rat).SetInt(scale))
	return &Decimal{rat: new(big.Rat).SetFrac(roundRatInt(scaled), scale)}
}

// Money 返回按「分」四舍五入后的金额。
func (d *Decimal) Money() *Decimal {
	return d.Round(MoneyScale)
}

// Fixed 返回固定 places 位小数的字符串表示，用于报表定宽输出。
// 金额字段用 String() 即可（最少十进制形式），需要「1280.00」这种定宽形态时
// 用 Fixed(MoneyScale)。
func (d *Decimal) Fixed(places int) string {
	if d == nil || d.rat == nil {
		return ""
	}
	if places < 0 {
		places = 0
	}
	rounded := d.Round(places)
	integer, fraction := rounded.isolateDecimal(places)
	if places == 0 {
		return integer
	}
	return integer + "." + fraction
}

// ratOrZero 返回底层的 rational，nil 视作 0。
func (d *Decimal) ratOrZero() *big.Rat {
	if d == nil || d.rat == nil {
		return new(big.Rat)
	}
	return d.rat
}

// isolateDecimal 把已舍入的值拆成整数部分与 places 位定长小数部分。
func (d *Decimal) isolateDecimal(places int) (string, string) {
	sign := ""
	value := new(big.Rat).Set(d.rat)
	if value.Sign() < 0 {
		sign = "-"
		value.Neg(value)
	}
	integer, remainder := new(big.Int).QuoRem(value.Num(), value.Denom(), new(big.Int))
	fraction := new(big.Int).Mul(remainder, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(places)), nil))
	fraction.Quo(fraction, value.Denom())
	text := fraction.String()
	for len(text) < places {
		text = "0" + text
	}
	return sign + integer.String(), text
}

// NewDecimal 解析十进制文本（如 "1280.00"、"-13.5"、"1280.00/3"）返回精确数值，
// 文本不含合法数值时返回 nil。这是构造 Decimal 的公开入口：解析文本用 NewDecimal，
// 金额求和用 Add 或 Sum。
//
// 允许分数形式（"1/3"）是 big.Rat 的原生语法，Decimal 本就基于有理数，
// 明细行数量/单价的异常数据会用到。
func NewDecimal(text string) (*Decimal, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, false
	}
	rat, ok := new(big.Rat).SetString(trimmed)
	if !ok {
		return nil, false
	}
	return &Decimal{rat: rat}, true
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
// 未做舍入，因此逐位相除得到的 1/3 会展开成长小数——需要定长金额时用 Fixed。
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

// roundRatInt 把有理数四舍五入为整数，恰好在中点时远离零。
//
// 不能用 big.Int.Quo + 小数比较：票据金额的中点进位习惯是「四舍五入、逢五进一」，
// 而 Go 与 big.Int 的默认取整是向零截断，1.005 会被截成 1。改用 QuoRem 拿到余数
// 后与分母的一半比较，余数恰好等于分母一半时进位。
func roundRatInt(value *big.Rat) *big.Int {
	den := value.Denom()
	negative := value.Sign() < 0
	abs := new(big.Int).Abs(new(big.Int).Set(value.Num()))
	quotient, remainder := new(big.Int).QuoRem(abs, den, new(big.Int))
	if new(big.Int).Lsh(remainder, 1).Cmp(den) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if negative {
		quotient.Neg(quotient)
	}
	return quotient
}
