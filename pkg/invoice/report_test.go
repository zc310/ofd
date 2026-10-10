package invoice

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/xuri/excelize/v2"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
)

// TestWriteCSVIncludesFailedAndTotal 确认 CSV 明细里失败项占一行、末行是合计，
// 且失败原因不会被表格吞掉。
func TestWriteCSVIncludesFailedAndTotal(t *testing.T) {
	broken := invoiceFixture{name: "broken.ofd", title: "增值税电子普通发票", missingAttachment: true}
	good := plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")
	batch := mustExtractBatch(t, batchSources(good, broken), BatchOptions{SkipBroken: true})

	var buffer bytes.Buffer
	if err := WriteCSV(&buffer, batch); err != nil {
		t.Fatalf("写 CSV 失败: %v", err)
	}
	text := buffer.String()
	assertContains(t, text, "发票代码")
	assertContains(t, text, "broken.ofd")
	assertContains(t, text, "合计 1 张")
	assertContains(t, text, "113.00")
	if !strings.HasPrefix(text, "\ufeff") {
		t.Error("CSV 应以 UTF-8 BOM 开头，否则 Excel 会把中文读成乱码")
	}
}

// TestWriteCSVMarksRedAndDuplicate 确认红字与重复在 CSV 里可辨识。
func TestWriteCSVMarksRedAndDuplicate(t *testing.T) {
	first := plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")
	red := plainInvoice("b.ofd", "033001900111", "2", "2026-09-20")
	red.amount, red.tax, red.total = "-100.00", "-13.00", "-113.00"
	again := plainInvoice("a-copy.ofd", "033001900111", "1", "2026-09-01")
	batch := mustExtractBatch(t, batchSources(first, red, again), BatchOptions{})

	var buffer bytes.Buffer
	if err := WriteCSV(&buffer, batch); err != nil {
		t.Fatalf("写 CSV 失败: %v", err)
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(buffer.String(), "\ufeff"))).ReadAll()
	if err != nil {
		t.Fatalf("解析 CSV 失败: %v", err)
	}
	if len(rows) != 5 {
		t.Fatalf("应为表头 + 3 明细 + 1 合计，实际 %d 行", len(rows))
	}
	if rows[2][5] != "红字" {
		t.Errorf("红字发票应在红字列标记，实际 %q", rows[2][5])
	}
	assertContains(t, rows[3][13], "重复")
	if rows[3][14] != "" {
		t.Errorf("重复已由「重复」列表达，异常列不应重复一遍，实际 %q", rows[3][14])
	}
	// 合计行必须与 Summary 同口径，含红字负数。
	assertContains(t, rows[len(rows)-1][0], "合计 3 张")
	assertContains(t, rows[len(rows)-1][6], "113.00")
}

// TestWriteCSVRejectsNilOutput 确认写出器为空时立即报错。
func TestWriteCSVRejectsNilOutput(t *testing.T) {
	if err := WriteCSV(nil, &Batch{}); err == nil {
		t.Error("空写入器应报错")
	}
}

// TestWriteXLSXProducesWorkbook 确认 XLSX 报表能写出并读回三个工作表，
// 且金额是数值单元格（写文本会让 Excel 的求和失效）。
func TestWriteXLSXProducesWorkbook(t *testing.T) {
	first := plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")
	second := plainInvoice("b.ofd", "033001900111", "2", "2026-10-01")
	second.sellerName = "另一家"
	second.sellerCode = "911100002222000000"
	batch := mustExtractBatch(t, batchSources(first, second), BatchOptions{})

	var buffer bytes.Buffer
	if err := WriteXLSX(&buffer, batch); err != nil {
		t.Fatalf("写 XLSX 失败: %v", err)
	}
	file, err := excelize.OpenReader(bytes.NewReader(buffer.Bytes()))
	if err != nil {
		t.Fatalf("读回 XLSX 失败: %v", err)
	}
	defer func() { _ = file.Close() }()

	for _, sheet := range []string{xlsxSheetDetail, xlsxSheetGroup, xlsxSheetIssue} {
		if index, _ := file.GetSheetIndex(sheet); index < 0 {
			t.Errorf("缺少工作表 %s", sheet)
		}
	}
	// 明细第 5 行第 11 列是不含税金额：显示为两位小数，且套用了数字格式
	// （显示值相同但没格式的文本单元格会被 Excel 的 SUM 跳过）。
	value, err := file.GetCellValue(xlsxSheetDetail, "K5")
	if err != nil {
		t.Fatalf("读取明细金额失败: %v", err)
	}
	if value != "100.00" {
		t.Errorf("明细金额 = %q，期望 100.00", value)
	}
	assertNumericFormat(t, file, xlsxSheetDetail, "K5")
	// 分组汇总第 5 行是合计，价税合计应为 226。
	rows, err := file.GetRows(xlsxSheetGroup)
	if err != nil {
		t.Fatalf("读取分组汇总失败: %v", err)
	}
	if len(rows) < 8 {
		t.Fatalf("分组汇总应有合计行与三个维度分组，实际 %d 行", len(rows))
	}
	joined := strings.Join(rows[4], ",")
	assertContains(t, joined, "合计")
	assertContains(t, joined, "226")
}

// TestWriteXLSXListsIssues 确认自校验不通过的发票出现在异常表里。
func TestWriteXLSXListsIssues(t *testing.T) {
	broken := plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")
	broken.total = "999.00"
	batch := mustExtractBatch(t, batchSources(broken), BatchOptions{})

	var buffer bytes.Buffer
	if err := WriteXLSX(&buffer, batch); err != nil {
		t.Fatalf("写 XLSX 失败: %v", err)
	}
	file, err := excelize.OpenReader(bytes.NewReader(buffer.Bytes()))
	if err != nil {
		t.Fatalf("读回 XLSX 失败: %v", err)
	}
	defer func() { _ = file.Close() }()

	rows, err := file.GetRows(xlsxSheetIssue)
	if err != nil {
		t.Fatalf("读取异常表失败: %v", err)
	}
	if len(rows) < 5 {
		t.Fatalf("异常表应有标题、副标题、表头与至少一行说明，实际 %d 行", len(rows))
	}
	joined := strings.Join(rows[4], ",")
	assertContains(t, joined, "a.ofd")
	assertContains(t, joined, "价税合计不符")
}

// TestMergePDFWritesEveryInvoice 确认合并 PDF 包含每张发票的页面。
//
// 用页面对象数量核对页数：N 张发票各一页，合并后不应少页，也不应把汇总页混进来。
func TestMergePDFWritesEveryInvoice(t *testing.T) {
	first := plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")
	second := plainInvoice("b.ofd", "033001900111", "2", "2026-09-02")
	third := plainInvoice("c.ofd", "033001900111", "3", "2026-09-03")

	var buffer bytes.Buffer
	batch, err := MergePDF(context.Background(), batchSources(first, second, third),
		&buffer, MergeOptions{})
	if err != nil {
		t.Fatalf("合并 PDF 失败: %v", err)
	}
	if batch.Summary.Count != 3 {
		t.Errorf("汇总张数 = %d，期望 3", batch.Summary.Count)
	}
	data := buffer.Bytes()
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatalf("输出不是 PDF：%q", data[:min(len(data), 16)])
	}
	if got := countPDFPages(data); got != 3 {
		t.Errorf("合并后应有 3 页，实际 %d", got)
	}
}

// TestMergePDFAddsCoverPage 确认开启汇总页时首页是对账表，且票页一页不少。
func TestMergePDFAddsCoverPage(t *testing.T) {
	first := plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")
	second := plainInvoice("b.ofd", "033001900111", "2", "2026-10-01")

	var buffer bytes.Buffer
	if _, err := MergePDF(context.Background(), batchSources(first, second),
		&buffer, MergeOptions{Cover: true, CoverTitle: "九月对账"}); err != nil {
		t.Fatalf("合并 PDF 失败: %v", err)
	}
	data := buffer.Bytes()
	if got := countPDFPages(data); got != 3 {
		t.Errorf("汇总页 + 2 张发票应有 3 页，实际 %d", got)
	}
}

// TestMergePDFSortsPages 确认按日期排序改变页序，且排序作用于合并结果。
func TestMergePDFSortsPages(t *testing.T) {
	late := plainInvoice("late.ofd", "033001900111", "2", "2026-09-20")
	early := plainInvoice("early.ofd", "033001900111", "1", "2026-09-01")

	sources := batchSources(late, early)
	var buffer bytes.Buffer
	batch, err := MergePDF(context.Background(), sources, &buffer, MergeOptions{Sort: SortDate})
	if err != nil {
		t.Fatalf("合并 PDF 失败: %v", err)
	}
	names := make([]string, 0, len(batch.Items))
	for _, item := range batch.Items {
		names = append(names, item.Name)
	}
	if len(names) != 2 || names[0] != "early.ofd" {
		t.Errorf("日期排序后的结果顺序 = %v，期望 early.ofd 在前", names)
	}
}

// TestMergePDFHonorsContextCancel 确认取消信号能中断合并，且错误原样是 ctx.Err()。
//
// 包一层"合并失败"的话，调用方无法用 errors.Is 区分取消与超时。
func TestMergePDFHonorsContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sources := batchSources(plainInvoice("a.ofd", "033001900111", "1", "2026-09-01"))

	var buffer bytes.Buffer
	_, err := MergePDF(ctx, sources, &buffer, MergeOptions{})
	if err == nil {
		t.Fatal("已取消的上下文应返回错误")
	}
	if err != context.Canceled {
		t.Errorf("错误应原样返回 context.Canceled，实际 %v", err)
	}
}

// TestMergePDFSkipsDuplicates 确认去重后的重复票不会合并进 PDF。
func TestMergePDFSkipsDuplicates(t *testing.T) {
	first := plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")
	again := plainInvoice("a-copy.ofd", "033001900111", "1", "2026-09-01")

	var buffer bytes.Buffer
	batch, err := MergePDF(context.Background(), batchSources(first, again), &buffer,
		MergeOptions{BatchOptions: BatchOptions{Dedup: true}})
	if err != nil {
		t.Fatalf("合并 PDF 失败: %v", err)
	}
	if got := countPDFPages(buffer.Bytes()); got != 1 {
		t.Errorf("去重后应只有 1 页，实际 %d", got)
	}
	if len(batch.Skipped) != 1 {
		t.Errorf("被跳过的重复票应记录在 Skipped，实际 %v", batch.Skipped)
	}
}

// TestMergePDFRejectsOversizedBatch 确认超过张数上限时报错而不是继续。
func TestMergePDFRejectsOversizedBatch(t *testing.T) {
	sources := batchSources(
		plainInvoice("a.ofd", "033001900111", "1", "2026-09-01"),
		plainInvoice("b.ofd", "033001900111", "2", "2026-09-02"))
	var buffer bytes.Buffer
	_, err := MergePDF(context.Background(), sources, &buffer,
		MergeOptions{Limits: MergeLimits{MaxInvoices: 1}})
	if err == nil {
		t.Fatal("超过张数上限应报错")
	}
	if !strings.Contains(err.Error(), "上限") {
		t.Errorf("错误信息应说明超限，实际：%v", err)
	}
}

// TestCoverPaginatesLongGroups 确认分组很多时汇总页会分页，且不丢行。
//
// coverPages 只负责把表分配到页上，单表超出一页时由创建器切成多页，所以页数要从
// 真正生成的 OFD 里数。
func TestCoverPaginatesLongGroups(t *testing.T) {
	groups := make([]Group, 0, 120)
	for index := 0; index < 120; index++ {
		groups = append(groups, Group{
			Key:   "TAX" + strings.Repeat("0", 3),
			Name:  "销售方甲" + strconv.Itoa(index),
			Count: 1,
		})
	}
	summary := Summary{Count: 120, BySeller: groups}
	pages := coverPageCount(t, summary, "分页测试")
	if pages < 3 {
		t.Fatalf("120 个销售方应分多页，实际 %d 页", pages)
	}

	// 行数不因分页丢失：按销售方汇总表应是表头加 120 行。
	rows := 0
	for _, table := range coverTables(summary, "分页测试") {
		if table.table.Caption == "按销售方汇总" {
			rows += len(table.table.Rows)
		}
	}
	if rows != len(groups)+1 {
		t.Errorf("按销售方汇总表应有表头加 %d 行，实际 %d 行", len(groups), rows-1)
	}
}

// TestCoverTablesSplitAcrossPages 确认每一张表都真的排进了 OFD，行一个不少。
func TestCoverTablesSplitAcrossPages(t *testing.T) {
	groups := make([]Group, 0, 120)
	for index := 0; index < 120; index++ {
		groups = append(groups, Group{
			Key:   "TAX000",
			Name:  "销售方甲" + strconv.Itoa(index),
			Count: 1,
		})
	}
	summary := Summary{Count: 120, BySeller: groups}
	ofd := coverOFD(t, summary, "")
	text := coverTextOf(t, ofd)
	for _, group := range groups {
		if !strings.Contains(text, group.Name) {
			t.Fatalf("销售方 %q 在生成结果里缺失", group.Name)
		}
	}
}

// coverOFD 生成汇总页并解析回 OFD。
func coverOFD(t *testing.T, summary Summary, title string) *parser.OFD {
	t.Helper()
	pages := coverPages(summary, title)
	document := creator.Document{
		ID:       "cover-test",
		PageSize: creator.PageSize{Width: coverPageWidth, Height: coverPageHeight},
		Fonts:    []creator.Font{{Name: coverFont, FamilyName: coverFont, Charset: "prc"}},
	}
	encoded, err := creator.MarshalWithPages(document, coverPageProvider{pages: pages}, creator.CreateOptions{})
	if err != nil {
		t.Fatalf("生成汇总页失败: %v", err)
	}
	ofd, err := parser.NewOFD(encoded)
	if err != nil {
		t.Fatalf("解析汇总页失败: %v", err)
	}
	t.Cleanup(func() { _ = ofd.Close() })
	return ofd
}

func coverPageCount(t *testing.T, summary Summary, title string) int {
	t.Helper()
	ofd := coverOFD(t, summary, title)
	if len(ofd.Documents) == 0 {
		t.Fatal("生成的 OFD 没有文档体")
	}
	return len(ofd.Documents[0].Pages)
}

// coverTextOf 提取生成结果里的全部文字。
func coverTextOf(t *testing.T, ofd *parser.OFD) string {
	t.Helper()
	var builder strings.Builder
	for _, document := range ofd.Documents {
		for _, page := range document.Pages {
			var content models.PageContent
			if err := page.WithPageContent(func(c *models.PageContent) error {
				content = *c
				return nil
			}); err != nil {
				t.Fatalf("读取页面内容失败: %v", err)
			}
			if content.Content == nil {
				continue
			}
			for _, layer := range content.Content.Layer {
				for _, item := range layer.Items {
					if item.Text == nil {
						continue
					}
					for _, code := range item.Text.TextCode {
						builder.WriteString(code.Value)
					}
					builder.WriteByte('\n')
				}
			}
		}
	}
	return builder.String()
}

// TestCoverKeepsSinglePageWhenShort 确认少量发票只生成一页汇总。
func TestCoverKeepsSinglePageWhenShort(t *testing.T) {
	batch := mustExtractBatch(t, batchSources(plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")),
		BatchOptions{})
	pages := coverPages(batch.Summary, "")
	if len(pages) != 1 {
		t.Fatalf("单张发票的汇总页应为 1 页，实际 %d", len(pages))
	}
	title, ok := coverPageText(pages[0])
	if !ok {
		t.Fatal("首页应含标题文字")
	}
	if title != defaultCoverTitle {
		t.Errorf("默认标题 = %q，期望 %q", title, defaultCoverTitle)
	}
}

// TestCoverWithRedInvoicesIsCreatable 确认含红字发票的汇总页能真正生成 OFD。
//
// 红字发票那行只有项目列有文字，金额列是占位空串。此前 coverRow 对每个值都生成
// 文字图元，空串被 creator 校验拦下（"文字内容不能为空"），整份汇总页创建失败——
// 只要这批发票里有红字发票，--cover 就完全不可用。
func TestCoverWithRedInvoicesIsCreatable(t *testing.T) {
	first := plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")
	red := plainInvoice("b.ofd", "033001900111", "2", "2026-09-20")
	red.amount, red.tax, red.total = "-100.00", "-13.00", "-113.00"
	batch := mustExtractBatch(t, batchSources(first, red), BatchOptions{})
	if batch.Summary.Red != 1 {
		t.Fatalf("应识别出 1 张红字发票，实际 %d", batch.Summary.Red)
	}

	pages := coverPages(batch.Summary, "")
	provider := coverPageProvider{pages: pages}
	meta := creator.Document{
		ID:       "cover-red",
		PageSize: creator.PageSize{Width: coverPageWidth, Height: coverPageHeight},
		Fonts:    []creator.Font{{Name: coverFont, FamilyName: coverFont, Charset: "prc"}},
	}
	var buffer bytes.Buffer
	if err := creator.CreateWithPages(meta, provider, &buffer); err != nil {
		t.Fatalf("含红字发票的汇总页应能生成: %v", err)
	}
	if buffer.Len() == 0 {
		t.Fatal("生成的汇总页为空")
	}
}

// TestCoverTableSkipsEmptyCells 确认占位空串不生成空单元格。
//
// 红字发票那行只有项目列有文字，金额列是占位空串。空单元格交给创建器会被
// "文字内容不能为空"校验拦下，整份汇总页都生成不出来。
func TestCoverTableSkipsEmptyCells(t *testing.T) {
	table := coverTableOf("", [][]string{
		{"甲", "3", "1.00", "0.13", "1.13"},
		{"其中红字发票 3 张", "", "", "", ""},
	}).table
	if len(table.Rows) != 3 {
		t.Fatalf("表头加两行应共 3 行，实际 %d 行", len(table.Rows))
	}
	full := table.Rows[1].Cells
	if len(full) != 5 {
		t.Errorf("完整行应生成 5 个单元格，实际 %d 个", len(full))
	}
	spare := table.Rows[2].Cells
	if len(spare) != 1 {
		t.Fatalf("只有项目列有文字，应只生成 1 个单元格，实际 %d 个", len(spare))
	}
	if !strings.Contains(spare[0].Value, "红字发票") {
		t.Errorf("单元格文字 = %q，应含红字发票说明", spare[0].Value)
	}
}

// TestSummaryStringIsStable 确认合计描述稳定可读，供日志与提示使用。
func TestSummaryStringIsStable(t *testing.T) {
	batch := mustExtractBatch(t, batchSources(plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")),
		BatchOptions{})
	assertContains(t, batch.Summary.String(), "1 张")
	assertContains(t, batch.Summary.String(), "价税合计 113.00")
}

// assertNumericFormat 断言单元格套用了带两位小数的数字格式。
//
// 金额写成文本同样能在单元格里显示成「113.00」，但 Excel 的 SUM 会跳过文本，
// 整列求和直接得 0——所以要断言格式，而不只是断言显示值。
func assertNumericFormat(t *testing.T, file *excelize.File, sheet, cell string) {
	t.Helper()
	styleID, err := file.GetCellStyle(sheet, cell)
	if err != nil {
		t.Fatalf("读取 %s!%s 样式失败: %v", sheet, cell, err)
	}
	style, err := file.GetStyle(styleID)
	if err != nil {
		t.Fatalf("解析 %s!%s 样式失败: %v", sheet, cell, err)
	}
	if style.CustomNumFmt == nil || !strings.Contains(*style.CustomNumFmt, "0.00") {
		t.Errorf("%s!%s 应套用两位小数数字格式，实际 %v", sheet, cell, style.CustomNumFmt)
	}
}

// pdfPagePattern 匹配页对象字典，Pages 是页树根节点，不算页。
var pdfPagePattern = regexp.MustCompile(`/Type\s*/Page[^s]`)

// countPDFPages 统计 PDF 的页数。
func countPDFPages(data []byte) int {
	return len(pdfPagePattern.FindAll(data, -1))
}

// TestWriteXLSXUsesReportLayout 锁定三张表的版式：标题、副标题、第 4 行表头、
// 第 5 行起数据，并冻结表头。
//
// 版式与冻结窗格、筛选范围是绑在一起的：数据一旦从第 4 行挪到第 5 行而忘了改
// TopLeftCell，冻结位置就会错位一行；表头行号写死在别处而不是常量，则筛选范围
// 会把标题圈进去。
func TestWriteXLSXUsesReportLayout(t *testing.T) {
	batch := mustExtractBatch(t, batchSources(plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")),
		BatchOptions{})
	var buffer bytes.Buffer
	if err := WriteXLSX(&buffer, batch); err != nil {
		t.Fatalf("写 XLSX 失败: %v", err)
	}
	file, err := excelize.OpenReader(bytes.NewReader(buffer.Bytes()))
	if err != nil {
		t.Fatalf("读回 XLSX 失败: %v", err)
	}
	defer func() { _ = file.Close() }()

	for _, sheet := range []string{xlsxSheetDetail, xlsxSheetGroup, xlsxSheetIssue} {
		title, err := file.GetCellValue(sheet, "A1")
		if err != nil {
			t.Fatalf("读取 %s 标题失败: %v", sheet, err)
		}
		if strings.TrimSpace(title) == "" {
			t.Errorf("%s 第 1 行应是标题", sheet)
		}
		subtitle, err := file.GetCellValue(sheet, "A2")
		if err != nil {
			t.Fatalf("读取 %s 副标题失败: %v", sheet, err)
		}
		if strings.TrimSpace(subtitle) == "" {
			t.Errorf("%s 第 2 行应是副标题", sheet)
		}
		panes, err := file.GetPanes(sheet)
		if err != nil {
			t.Fatalf("读取 %s 冻结窗格失败: %v", sheet, err)
		}
		if !panes.Freeze || panes.YSplit != xlsxHeaderRow {
			t.Errorf("%s 应冻结到第 %d 行，实际 Freeze=%v YSplit=%d",
				sheet, xlsxHeaderRow, panes.Freeze, panes.YSplit)
		}
	}
	header, err := file.GetCellValue(xlsxSheetDetail, cellName(1, xlsxHeaderRow))
	if err != nil {
		t.Fatalf("读取明细表头失败: %v", err)
	}
	if header != csvHeader[0] {
		t.Errorf("明细表头第 1 列 = %q，期望 %q", header, csvHeader[0])
	}
	// 明细第 5 行才是第一张票，第 4 行是表头。
	first, err := file.GetCellValue(xlsxSheetDetail, cellName(1, xlsxFirstRow))
	if err != nil {
		t.Fatalf("读取明细首行失败: %v", err)
	}
	if first != "a.ofd" {
		t.Errorf("明细第 %d 行 = %q，期望 a.ofd", xlsxFirstRow, first)
	}
}

// TestWriteXLSXAutofilterExcludesTotalRow 确认自动筛选只覆盖表头与明细。
//
// 把合计行圈进筛选范围后，用户按金额筛选时合计会跟着变，对账时容易被读成
// "筛选后的合计"，那是错的。范围只能从工作表 XML 里读，excelize 没有对应的
// getter——顺带守住 "A4:O7" 这类范围里的列名不要被写成带行号的单元格名。
func TestWriteXLSXAutofilterExcludesTotalRow(t *testing.T) {
	batch := mustExtractBatch(t, batchSources(
		plainInvoice("a.ofd", "033001900111", "1", "2026-09-01"),
		plainInvoice("b.ofd", "033001900111", "2", "2026-09-02")), BatchOptions{})
	var buffer bytes.Buffer
	if err := WriteXLSX(&buffer, batch); err != nil {
		t.Fatalf("写 XLSX 失败: %v", err)
	}

	lastData := xlsxFirstRow + len(batch.Items) - 1
	want := fmt.Sprintf("$A$%d:$%s$%d", xlsxHeaderRow, columnName(len(csvHeader)), lastData)
	// 合计行在明细之后，筛选范围不能越过它。
	total, err := cellText(buffer.Bytes(), xlsxSheetDetail, cellName(1, lastData+1))
	if err != nil {
		t.Fatalf("读取合计行失败: %v", err)
	}
	if !strings.HasPrefix(total, "合计") {
		t.Errorf("第 %d 行应是合计行，实际 %q", lastData+1, total)
	}
	marker := fmt.Sprintf("A1:%s1", columnName(len(csvHeader)))
	if got := autoFilterRef(t, buffer.Bytes(), marker); got != want {
		t.Errorf("明细筛选范围 = %q，期望 %q（不得包含合计行）", got, want)
	}
}

// cellText 从 XLSX 里读一个工作表的单元格值。
func cellText(data []byte, sheet, cell string) (string, error) {
	file, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	return file.GetCellValue(sheet, cell)
}

// autoFilterRef 读出工作表 XML 里的自动筛选范围；没有筛选时返回空串。
//
// marker 是该表标题的合并区域（如 "A1:O1"），用它定位目标 XML：三张表的
// sheetN.xml 编号与建表顺序耦合，副标题里又有中文与全角标点（写进 XML 会被
// 转义），都不适合当锚点。
func autoFilterRef(t *testing.T, data []byte, marker string) string {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("读取 XLSX 包失败: %v", err)
	}
	for _, entry := range archive.File {
		if !strings.HasPrefix(entry.Name, "xl/worksheets/") || !strings.HasSuffix(entry.Name, ".xml") {
			continue
		}
		raw, err := entry.Open()
		if err != nil {
			t.Fatalf("打开 %s 失败: %v", entry.Name, err)
		}
		content, err := io.ReadAll(raw)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", entry.Name, err)
		}
		_ = raw.Close()
		text := string(content)
		if !strings.Contains(text, `<mergeCell ref="`+marker+`">`) {
			continue
		}
		start := strings.Index(text, `<autoFilter ref="`)
		if start < 0 {
			return ""
		}
		start += len(`<autoFilter ref="`)
		end := strings.Index(text[start:], `"`)
		if end < 0 {
			return ""
		}
		return text[start : start+end]
	}
	t.Fatalf("XLSX 里找不到标题合并区域为 %s 的工作表", marker)
	return ""
}

// TestWriteXLSXSubtitleCarriesTotals 确认副标题带合计，便于不开公式就能核对。
func TestWriteXLSXSubtitleCarriesTotals(t *testing.T) {
	batch := mustExtractBatch(t, batchSources(
		plainInvoice("a.ofd", "033001900111", "1", "2026-09-01"),
		plainInvoice("b.ofd", "033001900111", "2", "2026-09-02")), BatchOptions{})
	var buffer bytes.Buffer
	if err := WriteXLSX(&buffer, batch); err != nil {
		t.Fatalf("写 XLSX 失败: %v", err)
	}
	file, err := excelize.OpenReader(bytes.NewReader(buffer.Bytes()))
	if err != nil {
		t.Fatalf("读回 XLSX 失败: %v", err)
	}
	defer func() { _ = file.Close() }()

	subtitle, err := file.GetCellValue(xlsxSheetDetail, "A2")
	if err != nil {
		t.Fatalf("读取副标题失败: %v", err)
	}
	for _, want := range []string{"共 2 张", "不含税合计 200.00", "价税合计 226.00"} {
		assertContains(t, subtitle, want)
	}
}

// TestWriteXLSXReportsEmptyIssues 确认没有异常时异常表给出明确提示，
// 而不是留一张空表让人以为没生成出来。
func TestWriteXLSXReportsEmptyIssues(t *testing.T) {
	batch := mustExtractBatch(t, batchSources(plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")),
		BatchOptions{})
	var buffer bytes.Buffer
	if err := WriteXLSX(&buffer, batch); err != nil {
		t.Fatalf("写 XLSX 失败: %v", err)
	}
	file, err := excelize.OpenReader(bytes.NewReader(buffer.Bytes()))
	if err != nil {
		t.Fatalf("读回 XLSX 失败: %v", err)
	}
	defer func() { _ = file.Close() }()

	value, err := file.GetCellValue(xlsxSheetIssue, cellName(2, xlsxFirstRow))
	if err != nil {
		t.Fatalf("读取异常表提示失败: %v", err)
	}
	if value != "无异常" {
		t.Errorf("无异常时应给出提示，实际 %q", value)
	}
}

// TestWriteXLSXColorsMoneyNegatives 确认金额格式带 [Red]，红字发票在表里能一眼挑出。
func TestWriteXLSXColorsMoneyNegatives(t *testing.T) {
	blue := plainInvoice("blue.ofd", "033001900111", "1", "2026-09-01")
	red := plainInvoice("red.ofd", "033001900111", "2", "2026-09-20")
	red.amount, red.tax, red.total = "-100.00", "-13.00", "-113.00"
	batch := mustExtractBatch(t, batchSources(blue, red), BatchOptions{})

	var buffer bytes.Buffer
	if err := WriteXLSX(&buffer, batch); err != nil {
		t.Fatalf("写 XLSX 失败: %v", err)
	}
	file, err := excelize.OpenReader(bytes.NewReader(buffer.Bytes()))
	if err != nil {
		t.Fatalf("读回 XLSX 失败: %v", err)
	}
	defer func() { _ = file.Close() }()

	styleID, err := file.GetCellStyle(xlsxSheetDetail, cellName(detailAmountCol, xlsxFirstRow))
	if err != nil {
		t.Fatalf("读取金额样式失败: %v", err)
	}
	style, err := file.GetStyle(styleID)
	if err != nil {
		t.Fatalf("解析金额样式失败: %v", err)
	}
	if style.CustomNumFmt == nil || !strings.Contains(*style.CustomNumFmt, "[Red]") {
		t.Errorf("金额格式应把负数标红，实际 %v", style.CustomNumFmt)
	}
	// 红字票在「红字」列有标记，金额单元格仍是 -113。
	value, err := file.GetCellValue(xlsxSheetDetail, cellName(detailAmountEnd, xlsxFirstRow+1))
	if err != nil {
		t.Fatalf("读取红字金额失败: %v", err)
	}
	if !strings.HasPrefix(value, "-") {
		t.Errorf("红字发票金额应为负，实际 %q", value)
	}
	mark, err := file.GetCellValue(xlsxSheetDetail, cellName(detailRedColumn, xlsxFirstRow+1))
	if err != nil {
		t.Fatalf("读取红字标记失败: %v", err)
	}
	if mark != "红字" {
		t.Errorf("红字列 = %q，期望 红字", mark)
	}
}

// coverPageText 取汇总页上的标题文字。
func coverPageText(page creator.Page) (string, bool) {
	for _, item := range page.Items {
		if text, ok := item.(creator.Text); ok {
			return text.Value, true
		}
	}
	return "", false
}
