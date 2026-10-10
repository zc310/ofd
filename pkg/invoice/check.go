package invoice

import (
	"errors"
	"math/big"
	"strings"
	"time"
)

// errUnparsableDate 表示开票日期无法按已知格式解析。
var errUnparsableDate = errors.New("开票日期格式无法识别")

// 票面上可能出现的开票日期写法。附件里通常是 ISO 日期，部分开票软件写成
// 「2026年09月18日」，两种都要认；认不出来时在 Check 里报告，不静默跳过。
var invoiceDateLayouts = []string{"2006-01-02", "2006年01月02日", "2006-1-2", "20060102"}

// ID 返回发票的稳定标识。发票代码与号码都在时返回「代码-号码」，只有号码时返回
// 号码，两样都缺才返回空字符串。
//
// 全电发票（数电票）自落地起就没有发票代码，只有 20 位全电发票号码，铁路电子客票
// 同理——它的号码在附件的 context/entity/identifier 里就是这张票的标识。原先要求
// 代码与号码同时在场，这些票的 ID 全是空串：Check 逐条报「无法唯一标识」，去重也
// 永远认不出它们。两样皆无才是真的无法定位。
func (i *Invoice) ID() string {
	if i == nil {
		return ""
	}
	code := strings.TrimSpace(i.Code)
	number := strings.TrimSpace(i.Number)
	if number == "" {
		return ""
	}
	if code == "" {
		return number
	}
	return code + "-" + number
}

// IsRed 判断是否为红字（红冲）发票：价税合计为负。
//
// 红字发票是正常业务数据，汇总时金额必须带符号参与求和，把它当异常过滤掉
// 会让合计凭空多出一笔。
func (i *Invoice) IsRed() bool {
	return i != nil && i.TotalAmount.IsNegative()
}

// InvoiceDate 解析开票日期。附件未写日期时返回零值时间。
func (i *Invoice) InvoiceDate() (time.Time, error) {
	if i == nil {
		return time.Time{}, nil
	}
	text := strings.TrimSpace(i.Date)
	if text == "" {
		return time.Time{}, nil
	}
	for _, layout := range invoiceDateLayouts {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, errUnparsableDate
}

// Check 返回发票自身的自洽性问题描述，空切片表示没有发现异常。
//
// 这些检查不依赖外部数据，只比对同一张票内部各处的金额是否对得上：
// 价税合计是否等于不含税金额加税额、逐行明细汇总是否等于票面金额、开票日期
// 是否可识别。附件被替换或损坏时这些关系会先失衡，因此 Check 是入账前最廉价
// 的一道拦截。
func (i *Invoice) Check() []string {
	if i == nil {
		return nil
	}
	var issues []string
	if i.ID() == "" {
		issues = append(issues, "缺少发票号码，无法唯一标识")
	}
	if _, err := i.InvoiceDate(); err != nil {
		issues = append(issues, "开票日期格式无法识别："+i.Date)
	}
	if i.Amount != nil && i.TaxAmount != nil && i.TotalAmount != nil {
		if diff := i.Amount.Add(i.TaxAmount).Sub(i.TotalAmount); !diff.IsZero() {
			issues = append(issues, "不含税金额加税额与价税合计不符，相差 "+diff.Fixed(MoneyScale))
		}
	} else if i.TotalAmount != nil {
		issues = append(issues, "缺少不含税金额或税额，无法校验价税合计")
	}
	issues = append(issues, detailIssues(i)...)
	return issues
}

// detailIssues 比对逐行明细与票面合计。
//
// 明细行按明细各自申报的金额、税额汇总，与票面合计不一致说明附件里的两处数据
// 不同步。金额按分比较：合法数据本身就有 0.01 以内的四舍五入尾差，差一个分之内
// 不算异常，超过一分才报告。
func detailIssues(i *Invoice) []string {
	var issues []string
	if len(i.Details) == 0 {
		return nil
	}
	amounts := make([]*Decimal, 0, len(i.Details))
	taxes := make([]*Decimal, 0, len(i.Details))
	for _, detail := range i.Details {
		amounts = append(amounts, detail.Amount)
		taxes = append(taxes, detail.TaxAmount)
	}
	if i.Amount != nil {
		if diff := Sum(amounts).Sub(i.Amount); diff.Abs().Cmp(moneyUnit()) > 0 {
			issues = append(issues, "明细金额合计与不含税金额不符，相差 "+diff.Abs().Fixed(MoneyScale))
		}
	}
	if i.TaxAmount != nil {
		if diff := Sum(taxes).Sub(i.TaxAmount); diff.Abs().Cmp(moneyUnit()) > 0 {
			issues = append(issues, "明细税额合计与税额不符，相差 "+diff.Abs().Fixed(MoneyScale))
		}
	}
	return issues
}

// moneyUnit 返回一个「分」，用作明细汇总的容差单位。
func moneyUnit() *Decimal {
	return &Decimal{rat: new(big.Rat).SetFrac64(1, 100)}
}
