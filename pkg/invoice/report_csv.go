package invoice

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

// csvHeader 是明细表的列名，顺序与 detailRow 一致。
var csvHeader = []string{
	"来源", "发票代码", "发票号码", "开票日期", "发票类型", "红字",
	"购买方名称", "购买方税号", "销售方名称", "销售方税号",
	"不含税金额", "税额", "价税合计", "重复", "异常",
}

// WriteCSV 写出发票明细与合计的 CSV 报表。
//
// 明细按输入顺序输出，每行一张票，抽取失败的输入也占一行并在「异常」列写明
// 原因——对账时要能看出哪些票没进来，而不是表格里凭空少了行。末行是合计行，
// 与 Batch.Summary 的口径一致（已按分四舍五入、含红字负数）。
//
// 输出写 UTF-8 BOM 并使用 CRLF 行尾：Excel 在中文 Windows 上按本地代码页读取
// 无 BOM 的 UTF-8 CSV，中文列名会变成乱码。
func WriteCSV(w io.Writer, batch *Batch) error {
	if w == nil {
		return fmt.Errorf("CSV 输出为空")
	}
	if batch == nil {
		return fmt.Errorf("CSV 报表缺少批量抽取结果")
	}
	_, err := w.Write([]byte{0xEF, 0xBB, 0xBF})
	if err != nil {
		return fmt.Errorf("写入 CSV BOM 失败：%w", err)
	}
	writer := csv.NewWriter(w)
	writer.UseCRLF = true
	if err := writer.Write(csvHeader); err != nil {
		return fmt.Errorf("写入 CSV 表头失败：%w", err)
	}
	for _, item := range batch.Items {
		if err := writer.Write(detailRow(item)); err != nil {
			return fmt.Errorf("写入 CSV 明细失败：%w", err)
		}
	}
	if err := writer.Write(totalRow(batch)); err != nil {
		return fmt.Errorf("写入 CSV 合计行失败：%w", err)
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("写入 CSV 失败：%w", err)
	}
	return nil
}

// detailRow 把一条处理结果转成 CSV 行。金额列写成两位小数的定长文本，与
// Fixed 一致；缺金额时留空而不是写 0，「未提供」和「为零」在报表里要能区分。
func detailRow(item Item) []string {
	row := make([]string, len(csvHeader))
	row[0] = item.Name
	if inv := item.Invoice; inv != nil {
		row[1] = inv.Code
		row[2] = inv.Number
		row[3] = inv.Date
		row[4] = inv.Type
		row[6] = inv.Buyer.Name
		row[7] = inv.Buyer.Code
		row[8] = inv.Seller.Name
		row[9] = inv.Seller.Code
		row[10] = inv.Amount.Fixed(MoneyScale)
		row[11] = inv.TaxAmount.Fixed(MoneyScale)
		row[12] = inv.TotalAmount.Fixed(MoneyScale)
	}
	if item.Red {
		row[5] = "红字"
	}
	if item.DuplicateOf != "" {
		row[13] = "与 " + item.DuplicateOf + " 重复"
	}
	row[14] = strings.Join(itemIssues(item), "；")
	return row
}

// totalRow 返回合计行，前两列留空以示与明细区分。
func totalRow(batch *Batch) []string {
	summary := batch.Summary
	row := make([]string, len(csvHeader))
	row[0] = fmt.Sprintf("合计 %d 张", summary.Count)
	row[1] = "不含税金额"
	row[2] = moneyText(summary.Amount)
	row[3] = "税额"
	row[4] = moneyText(summary.TaxAmount)
	row[5] = "价税合计"
	row[6] = moneyText(summary.TotalAmount)
	if summary.Red > 0 {
		row[7] = fmt.Sprintf("含红字 %d 张", summary.Red)
	}
	return row
}

func moneyText(value *Decimal) string {
	if value == nil {
		return ""
	}
	return value.Fixed(MoneyScale)
}

// itemIssues 汇总一条结果的异常说明：抽取错误优先，其次是自校验问题。
//
// 重复不在这里返回：报表有独立的「重复」列，再在这里重复一遍只会让同一句话说
// 两遍。需要把重复写进异常表时由调用方按 DuplicateOf 单独成行。
func itemIssues(item Item) []string {
	if item.Error != "" {
		return []string{item.Error}
	}
	return item.Issues
}

// issueTexts 返回某条结果在异常表里要写的说明，重复也单列一行。
func issueTexts(item Item) []string {
	if issues := itemIssues(item); len(issues) > 0 {
		return issues
	}
	if item.DuplicateOf != "" {
		return []string{"发票代码号码与 " + item.DuplicateOf + " 重复"}
	}
	return nil
}
