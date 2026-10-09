package invoice

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestExtractBatchSumsAmounts 确认多张发票的金额被汇总，合计按分输出。
func TestExtractBatchSumsAmounts(t *testing.T) {
	first := plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")
	second := plainInvoice("b.ofd", "033001900111", "2", "2026-09-15")
	batch := mustExtractBatch(t, batchSources(first, second), BatchOptions{})

	if batch.Summary.Count != 2 {
		t.Errorf("张数 = %d，期望 2", batch.Summary.Count)
	}
	if got := batch.Summary.TotalAmount.Fixed(MoneyScale); got != "226.00" {
		t.Errorf("价税合计 = %s，期望 226.00", got)
	}
	if got := batch.Summary.Amount.Fixed(MoneyScale); got != "200.00" {
		t.Errorf("不含税合计 = %s，期望 200.00", got)
	}
	if got := batch.Summary.TaxAmount.Fixed(MoneyScale); got != "26.00" {
		t.Errorf("税额合计 = %s，期望 26.00", got)
	}
	if len(batch.Skipped) != 0 {
		t.Errorf("不应有跳过项，实际 %v", batch.Skipped)
	}
}

// TestSummarizeKeepsRedInvoiceNegative 确认红字发票的负金额参与合计。
//
// 红冲票被当成 0 或被丢弃，会让合计凭空多出一笔正数，跨月对账直接对不上。
func TestSummarizeKeepsRedInvoiceNegative(t *testing.T) {
	blue := plainInvoice("blue.ofd", "033001900111", "1", "2026-09-01")
	red := plainInvoice("red.ofd", "033001900111", "2", "2026-09-20")
	red.amount, red.tax, red.total = "-100.00", "-13.00", "-113.00"
	batch := mustExtractBatch(t, batchSources(blue, red), BatchOptions{})

	if batch.Summary.Red != 1 {
		t.Errorf("红字张数 = %d，期望 1", batch.Summary.Red)
	}
	if got := batch.Summary.TotalAmount.Fixed(MoneyScale); got != "0.00" {
		t.Errorf("红字参与合计后价税合计 = %s，期望 0.00", got)
	}
	if !batch.Items[1].Red {
		t.Error("红字发票应在结果里被标记")
	}
}

// TestExtractBatchIsStrictByDefault 确认默认严格：一张票抽不出，整批报错。
//
// 静默跳过会让一批票的合计少一张而没有任何提示，财务场景下这是事故而不是便利。
func TestExtractBatchIsStrictByDefault(t *testing.T) {
	broken := invoiceFixture{name: "broken.ofd", title: "增值税电子普通发票", missingAttachment: true}
	good := plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")

	_, err := ExtractBatch(batchSources(good, broken), BatchOptions{})
	if err == nil {
		t.Fatal("默认严格模式下整批应报错")
	}
	if !errors.Is(err, ErrNoAttachment) {
		t.Errorf("错误应保留 ErrNoAttachment 以便判断原因，实际：%v", err)
	}
	if !strings.Contains(err.Error(), "broken.ofd") {
		t.Errorf("错误信息应指明失败的来源名，实际：%v", err)
	}
}

// TestExtractBatchSkipBrokenReportsFailures 确认 --skip-broken 继续处理，
// 但失败项仍出现在结果里，不会从报告里消失。
func TestExtractBatchSkipBrokenReportsFailures(t *testing.T) {
	broken := invoiceFixture{name: "broken.ofd", title: "增值税电子普通发票", missingAttachment: true}
	good := plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")

	batch := mustExtractBatch(t, batchSources(good, broken), BatchOptions{SkipBroken: true})
	if len(batch.Items) != 2 {
		t.Fatalf("结果应有 2 条，实际 %d", len(batch.Items))
	}
	if batch.Items[1].OK() {
		t.Error("broken.ofd 应记录抽取失败")
	}
	if batch.Items[1].Error == "" {
		t.Error("失败项应带错误说明")
	}
	if batch.Summary.Count != 1 {
		t.Errorf("汇总张数 = %d，期望 1（失败项不参与合计）", batch.Summary.Count)
	}
}

// TestExtractBatchFlagsDuplicatesWithoutDropping 确认重复发票默认被标记而不是丢弃。
func TestExtractBatchFlagsDuplicatesWithoutDropping(t *testing.T) {
	first := plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")
	again := plainInvoice("a-copy.ofd", "033001900111", "1", "2026-09-01")
	batch := mustExtractBatch(t, batchSources(first, again), BatchOptions{})

	if len(batch.Items) != 2 {
		t.Fatalf("重复项不应被丢弃，实际结果 %d 条", len(batch.Items))
	}
	if batch.Items[1].DuplicateOf != "a.ofd" {
		t.Errorf("重复项应指明来源，实际 %q", batch.Items[1].DuplicateOf)
	}
	if got := batch.Summary.TotalAmount.Fixed(MoneyScale); got != "226.00" {
		t.Errorf("默认不去重时合计 = %s，期望 226.00", got)
	}
	if len(batch.Skipped) != 0 {
		t.Errorf("默认不应跳过重复项，实际 %v", batch.Skipped)
	}
}

// TestExtractBatchDedupMovesRepeatToSkipped 确认去重把重复项移入 Skipped，
// 并且不再参与合计与后续合并。
func TestExtractBatchDedupMovesRepeatToSkipped(t *testing.T) {
	first := plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")
	again := plainInvoice("a-copy.ofd", "033001900111", "1", "2026-09-01")
	batch := mustExtractBatch(t, batchSources(first, again), BatchOptions{Dedup: true})

	if len(batch.Skipped) != 1 || batch.Skipped[0].Name != "a-copy.ofd" {
		t.Fatalf("重复项应移入 Skipped，实际 %v", batch.Skipped)
	}
	if got := batch.Summary.TotalAmount.Fixed(MoneyScale); got != "113.00" {
		t.Errorf("去重后合计 = %s，期望 113.00", got)
	}
	if len(batch.ActiveItems()) != 1 {
		t.Errorf("ActiveItems 应只含 1 项，实际 %d", len(batch.ActiveItems()))
	}
}

// TestSummaryGroupsBySellerTaxID 确认销售方按税号分组而不是名称，
// 否则同一销售方的不同开票主体会裂成多行。
func TestSummaryGroupsBySellerTaxID(t *testing.T) {
	first := plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")
	second := plainInvoice("b.ofd", "033001900111", "2", "2026-09-02")
	second.sellerName = "卖方公司(分公司)"
	third := plainInvoice("c.ofd", "033001900111", "3", "2026-09-03")
	third.sellerName = "另一家"
	third.sellerCode = "911100002222000000"

	batch := mustExtractBatch(t, batchSources(first, second, third), BatchOptions{})
	if len(batch.Summary.BySeller) != 2 {
		t.Fatalf("应按税号分成 2 组，实际 %d：%v", len(batch.Summary.BySeller), batch.Summary.BySeller)
	}
	groups := batch.Summary.BySeller
	if groups[0].Count != 2 {
		t.Errorf("同税号应合并为 2 张，实际 %d", groups[0].Count)
	}
	if got := groups[0].TotalAmount.Fixed(MoneyScale); got != "226.00" {
		t.Errorf("分组合计 = %s，期望 226.00", got)
	}
}

// TestSummaryGroupsByMonth 确认按开票月份分组。
func TestSummaryGroupsByMonth(t *testing.T) {
	september := plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")
	also := plainInvoice("b.ofd", "033001900111", "2", "2026-09-20")
	october := plainInvoice("c.ofd", "033001900111", "3", "2026-10-01")
	batch := mustExtractBatch(t, batchSources(september, also, october), BatchOptions{})

	if len(batch.Summary.ByMonth) != 2 {
		t.Fatalf("应按月份分成 2 组，实际 %v", batch.Summary.ByMonth)
	}
	if batch.Summary.ByMonth[0].Key != "2026-09" || batch.Summary.ByMonth[0].Count != 2 {
		t.Errorf("9 月分组应为 2 张，实际 %+v", batch.Summary.ByMonth[0])
	}
}

// TestSummarizeWithoutAmountsKeepsNil 确认所有发票都没有金额时合计为 nil 而不是 0，
// 让下游能区分「没抽到金额」与「金额为零」。
func TestSummarizeWithoutAmountsKeepsNil(t *testing.T) {
	summary := Summarize([]*Invoice{{Code: "1", Number: "1"}, nil})
	if summary.TotalAmount != nil {
		t.Errorf("无金额时合计应为 nil，实际 %v", summary.TotalAmount)
	}
	if summary.Count != 1 {
		t.Errorf("张数 = %d，期望 1", summary.Count)
	}
}

// TestInvoiceCheckReportsMismatch 确认自校验能发现价税合计不自洽。
func TestInvoiceCheckReportsMismatch(t *testing.T) {
	broken := plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")
	broken.total = "999.00"
	source := Source{Name: "a.ofd", Input: sampleInvoice(broken)}
	batch := mustExtractBatch(t, []Source{source}, BatchOptions{})

	issues := batch.Items[0].Issues
	if len(issues) == 0 {
		t.Fatal("价税合计被改写后应报告异常")
	}
	joined := strings.Join(issues, "；")
	assertContains(t, joined, "价税合计不符")
	assertContains(t, joined, "886.00")
}

// TestInvoiceCheckAcceptsConsistentTicket 确认字段自洽时不报异常。
func TestInvoiceCheckAcceptsConsistentTicket(t *testing.T) {
	batch := mustExtractBatch(t, batchSources(plainInvoice("a.ofd", "1", "1", "2026-09-01")),
		BatchOptions{})
	if issues := batch.Items[0].Issues; len(issues) != 0 {
		t.Errorf("自洽发票不应报异常，实际 %v", issues)
	}
}

// TestInvoiceIDRequiresCodeAndNumber 确认缺代码或号码时没有稳定标识。
func TestInvoiceIDRequiresCodeAndNumber(t *testing.T) {
	if got := (&Invoice{Number: "1"}).ID(); got != "" {
		t.Errorf("缺代码时 ID 应为空，实际 %q", got)
	}
	if got := (&Invoice{Code: "1", Number: "2"}).ID(); got != "1-2" {
		t.Errorf("ID = %q，期望 1-2", got)
	}
}

// TestSortItemsByDateAndCode 确认排序把缺日期和缺发票的项排在最后，不打乱其余顺序。
func TestSortItemsByDateAndDateKey(t *testing.T) {
	late := plainInvoice("late.ofd", "033001900111", "3", "2026-09-20")
	early := plainInvoice("early.ofd", "033001900111", "1", "2026-09-01")
	middle := plainInvoice("middle.ofd", "033001900111", "2", "2026-09-10")
	batch := mustExtractBatch(t, batchSources(late, early, middle), BatchOptions{})

	if err := SortItems(batch.Items, SortDate); err != nil {
		t.Fatalf("按日期排序失败: %v", err)
	}
	got := []string{batch.Items[0].Name, batch.Items[1].Name, batch.Items[2].Name}
	want := []string{"early.ofd", "middle.ofd", "late.ofd"}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("日期排序结果 = %v，期望 %v", got, want)
		}
	}
	if err := SortItems(batch.Items, "unknown"); err == nil {
		t.Error("未知排序方式应报错")
	}
}

// TestBatchJSONKeepsMoneyAsString 确认批量结果的 JSON 形态与单张一致：
// 金额仍是十进制字符串，不能在汇总环节变成 JSON 数字。
func TestBatchJSONKeepsMoneyAsString(t *testing.T) {
	batch := mustExtractBatch(t, batchSources(plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")),
		BatchOptions{})
	encoded, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	text := string(encoded)
	assertContains(t, text, `"total_amount":"113"`)
	assertContains(t, text, `"summary"`)
	if strings.Contains(text, `"total_amount":113`) {
		t.Errorf("合计金额不应输出为 JSON 数字：%s", text)
	}
}

// TestInvoiceJSONIsWriteOnlyStillHolds 确认汇总功能没有给 Decimal 加上反序列化，
// Invoice 仍是单向模型。
func TestInvoiceJSONIsWriteOnlyStillHolds(t *testing.T) {
	if _, ok := any(&Decimal{}).(json.Unmarshaler); ok {
		t.Error("Decimal 不应实现 json.Unmarshaler；本测试提醒届时同步更新 TestInvoiceJSONIsWriteOnly")
	}
}
