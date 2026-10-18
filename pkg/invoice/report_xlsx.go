//go:build !js || !wasm

package invoice

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// 工作表名。
const (
	xlsxSheetDetail = "发票明细"
	xlsxSheetGroup  = "分组汇总"
	xlsxSheetIssue  = "异常与跳过"
)

// 明细表的列序与 csvHeader 一致：前 10 列是文本，其中开票日期、发票类型、红字三列
// 居中；第 11 至 13 列是三个金额列；末两列是重复与异常说明。
const (
	detailDateColumn  = 4
	detailTypeColumn  = 5
	detailRedColumn   = 6
	detailTextTail    = 10
	detailAmountCol   = 11
	detailAmountEnd   = 13
	detailDuplicateCo = 14
	detailIssueColumn = 15
)

// xlsxLayout 固定三张表的版式：第 1 行标题、第 2 行副标题、第 4 行表头、第 5 行起
// 数据。冻结窗格与自动筛选都按这个版式定位，改动时三处要一起改。
const (
	xlsxHeaderRow   = 4
	xlsxFirstRow    = 5
	xlsxTitleRow    = 1
	xlsxSubtitleRow = 2
)

// 明细表各列宽度。
var detailWidths = []float64{30, 16, 13, 14, 13, 8, 24, 22, 26, 22, 15, 13, 15, 22, 44}

// 分组汇总表各列宽度。
var groupWidths = []float64{14, 24, 28, 10, 10, 17, 15, 17}

// 异常表各列宽度。
var issueWidths = []float64{34, 14, 76}

// xlsxStyles 是报表用的样式集合。配色沿用校验器与分析器报表的那一套（深蓝标题栏、
// 浅蓝副标题、隔行浅灰），这样几个工具产出的报表放在一起看是一致的。
type xlsxStyles struct {
	title    int
	subtitle int
	header   int

	body      int
	bodyAlt   int
	center    int
	centerAlt int
	money     int
	moneyAlt  int

	badText      int
	badTextAlt   int
	redMark      int
	redMarkAlt   int
	duplicate    int
	duplicateAlt int
	issueText    int
	issueTextAlt int
	note         int

	totalLabel int
	totalMoney int
}

// WriteXLSX 写出对账用的 XLSX 报表，包含明细、分组汇总与异常三个工作表。
//
// 明细与 CSV 同源同口径：抽取失败的输入也占一行，末行是合计。分组汇总按
// 「维度 + 键 + 名称」的长表组织，销售方、发票类型和开票月份都放进去，方便在
// Excel 里直接做数据透视。异常表列出自校验不通过和被去重跳过的输入。
//
// 三张表统一版式：第 1 行标题、第 2 行副标题（带张数与合计）、第 4 行表头、
// 第 5 行起数据，冻结窗格固定在表头下方。配色沿用校验器与分析器报表那一套。
//
// 金额写成带两位小数的**数值**单元格，负数由数字格式标红：写成文本会让 Excel 的
// SUM 整列跳过，对账表就失去了意义。自动筛选只覆盖表头与明细，不含合计行——
// 合计跟着筛选变动会被读成「筛选后的合计」。
func WriteXLSX(w io.Writer, batch *Batch) error {
	if w == nil {
		return fmt.Errorf("XLSX 输出为空")
	}
	if batch == nil {
		return fmt.Errorf("XLSX 报表缺少批量抽取结果")
	}
	file := excelize.NewFile()
	defer func() { _ = file.Close() }()

	if err := file.SetSheetName("Sheet1", xlsxSheetDetail); err != nil {
		return fmt.Errorf("重命名 XLSX 明细页失败：%w", err)
	}
	for _, name := range []string{xlsxSheetGroup, xlsxSheetIssue} {
		if _, err := file.NewSheet(name); err != nil {
			return fmt.Errorf("创建 XLSX %s 页失败：%w", name, err)
		}
	}
	styles, err := newXLSXStyles(file)
	if err != nil {
		return err
	}
	if err := writeXLSXDetail(file, batch, styles); err != nil {
		return err
	}
	if err := writeXLSXGroups(file, batch, styles); err != nil {
		return err
	}
	if err := writeXLSXIssues(file, batch, styles); err != nil {
		return err
	}
	index, err := file.GetSheetIndex(xlsxSheetDetail)
	if err != nil {
		return fmt.Errorf("定位 XLSX 明细页失败：%w", err)
	}
	file.SetActiveSheet(index)
	if err := file.Write(w); err != nil {
		return fmt.Errorf("写入 XLSX 工作簿失败：%w", err)
	}
	return nil
}

// newXLSXStyles 建立报表样式。字体统一用 Microsoft YaHei：Excel 在中文 Windows 上
// 一定有它，其它平台缺失时按无衬线回退，仍比默认的 Calibri 适合中文票面。
func newXLSXStyles(file *excelize.File) (xlsxStyles, error) {
	var styles xlsxStyles
	var err error
	if styles.title, err = file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Family: xlsxFont, Size: 16, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"17365D"}},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center"},
	}); err != nil {
		return styles, xlsxStyleError("标题", err)
	}
	if styles.subtitle, err = file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: xlsxFont, Size: 10, Color: "5B6573"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"EAF2F8"}},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
	}); err != nil {
		return styles, xlsxStyleError("副标题", err)
	}
	if styles.header, err = file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Family: xlsxFont, Size: 10, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"1F4E78"}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border:    xlsxBorders("9FBAD0"),
	}); err != nil {
		return styles, xlsxStyleError("表头", err)
	}
	for _, pair := range []struct {
		target *int
		fill   string
		format *string
		align  string
	}{
		{&styles.body, "FFFFFF", nil, "left"},
		{&styles.bodyAlt, "F7FAFC", nil, "left"},
		{&styles.center, "FFFFFF", nil, "center"},
		{&styles.centerAlt, "F7FAFC", nil, "center"},
		{&styles.money, "FFFFFF", strPtr(xlsxMoneyFormat), "right"},
		{&styles.moneyAlt, "F7FAFC", strPtr(xlsxMoneyFormat), "right"},
		{&styles.badText, xlsxBadFill, nil, "left"},
		{&styles.badTextAlt, xlsxBadFillAlt, nil, "left"},
		{&styles.redMark, xlsxRedMarkFill, nil, "center"},
		{&styles.redMarkAlt, xlsxRedMarkFillAlt, nil, "center"},
	} {
		*pair.target, err = xlsxCellStyle(file, pair.fill, pair.format, pair.align, false)
		if err != nil {
			return styles, xlsxStyleError("正文", err)
		}
	}
	if styles.duplicate, err = xlsxCellStyle(file, xlsxWarnFill, nil, "center", true); err != nil {
		return styles, xlsxStyleError("重复标记", err)
	}
	if styles.duplicateAlt, err = xlsxCellStyle(file, xlsxWarnFillAlt, nil, "center", true); err != nil {
		return styles, xlsxStyleError("重复标记", err)
	}
	if styles.issueText, err = xlsxCellStyle(file, xlsxBadFill, nil, "left", true); err != nil {
		return styles, xlsxStyleError("异常说明", err)
	}
	if styles.issueTextAlt, err = xlsxCellStyle(file, xlsxBadFillAlt, nil, "left", true); err != nil {
		return styles, xlsxStyleError("异常说明", err)
	}
	if styles.note, err = file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: xlsxFont, Size: 10, Color: "5B6573", Italic: true},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	}); err != nil {
		return styles, xlsxStyleError("空结果提示", err)
	}
	if styles.totalLabel, err = file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Family: xlsxFont, Size: 10, Color: "17365D"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"DDEBF7"}},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center"},
		Border:    xlsxTotalBorders(),
	}); err != nil {
		return styles, xlsxStyleError("合计标签", err)
	}
	if styles.totalMoney, err = xlsxCellStyle(file, "DDEBF7", strPtr(xlsxMoneyFormat), "right", true); err != nil {
		return styles, xlsxStyleError("合计金额", err)
	}
	return styles, nil
}

// xlsxFont 是报表统一字体名。
const xlsxFont = "Microsoft YaHei"

// xlsxMoneyFormat 是金额的数字格式：千位分隔、两位小数，负数显示为红色——
// 红字发票一眼能从表里挑出来。
const xlsxMoneyFormat = "#,##0.00;[Red]-#,##0.00"

// 异常行的底色。
const (
	xlsxBadFill    = "FDF0F0"
	xlsxBadFillAlt = "FAE4E4"
)

// 红字标记的底色：比异常行的浅一档，别把"红字发票"和"抽取失败"混成同一种颜色。
const (
	xlsxRedMarkFill    = "FFF0F0"
	xlsxRedMarkFillAlt = "FBE2E2"
)

// 重复标记的底色。
const (
	xlsxWarnFill    = "FFF2CC"
	xlsxWarnFillAlt = "FBE9B5"
)

// xlsxCellStyle 建立一个正文单元格样式：底色、数字格式、对齐方式与是否加粗。
func xlsxCellStyle(file *excelize.File, fill string, format *string, align string, bold bool) (int, error) {
	return file.NewStyle(&excelize.Style{
		Font:         &excelize.Font{Bold: bold, Family: xlsxFont, Size: 10, Color: xlsxFontColor(fill)},
		Fill:         excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{fill}},
		Alignment:    &excelize.Alignment{Horizontal: align, Vertical: "center", WrapText: align == "left"},
		Border:       xlsxBorders("E3EAF2"),
		CustomNumFmt: format,
	})
}

// xlsxFontColor 按底色选字色：正文深灰、异常红字、重复棕字。
func xlsxFontColor(fill string) string {
	switch fill {
	case xlsxBadFill, xlsxBadFillAlt:
		return "9C0006"
	case xlsxWarnFill, xlsxWarnFillAlt:
		return "7F6000"
	case xlsxRedMarkFill, xlsxRedMarkFillAlt:
		return "9C0006"
	default:
		return "1F2937"
	}
}

func xlsxBorders(color string) []excelize.Border {
	return []excelize.Border{
		{Type: "left", Style: 1, Color: color},
		{Type: "right", Style: 1, Color: color},
		{Type: "top", Style: 1, Color: color},
		{Type: "bottom", Style: 1, Color: color},
	}
}

// xlsxTotalBorders 给出合计行的边框：上方一条粗线，把合计与明细分开。
func xlsxTotalBorders() []excelize.Border {
	return []excelize.Border{
		{Type: "top", Style: 2, Color: "808080"},
		{Type: "bottom", Style: 1, Color: "D0D7E5"},
		{Type: "left", Style: 1, Color: "D0D7E5"},
		{Type: "right", Style: 1, Color: "D0D7E5"},
	}
}

func xlsxStyleError(part string, err error) error {
	return fmt.Errorf("创建 XLSX %s样式失败：%w", part, err)
}

// xlsxHeader 写入标题、副标题与表头三行，并返回表头所在行号。
func xlsxHeader(file *excelize.File, sheet string, title, subtitle string, headers []string,
	styles xlsxStyles) error {
	last := cellName(len(headers), 1)
	if err := file.MergeCell(sheet, "A1", last); err != nil {
		return fmt.Errorf("合并 XLSX %s 标题失败：%w", sheet, err)
	}
	if err := file.SetCellValue(sheet, "A1", title); err != nil {
		return fmt.Errorf("写入 XLSX %s 标题失败：%w", sheet, err)
	}
	if err := file.SetCellStyle(sheet, "A1", last, styles.title); err != nil {
		return fmt.Errorf("设置 XLSX %s 标题样式失败：%w", sheet, err)
	}
	subtitleLast := cellName(len(headers), 2)
	if err := file.MergeCell(sheet, "A2", subtitleLast); err != nil {
		return fmt.Errorf("合并 XLSX %s 副标题失败：%w", sheet, err)
	}
	if err := file.SetCellValue(sheet, "A2", subtitle); err != nil {
		return fmt.Errorf("写入 XLSX %s 副标题失败：%w", sheet, err)
	}
	if err := file.SetCellStyle(sheet, "A2", subtitleLast, styles.subtitle); err != nil {
		return fmt.Errorf("设置 XLSX %s 副标题样式失败：%w", sheet, err)
	}
	if err := writeXLSXHeaderRow(file, sheet, headers, styles); err != nil {
		return err
	}
	for row, height := range map[int]float64{
		xlsxTitleRow: 30, xlsxSubtitleRow: 24, xlsxHeaderRow: 26,
	} {
		if err := file.SetRowHeight(sheet, row, height); err != nil {
			return fmt.Errorf("设置 XLSX %s 行高失败：%w", sheet, err)
		}
	}
	return nil
}

func writeXLSXHeaderRow(file *excelize.File, sheet string, headers []string, styles xlsxStyles) error {
	row := make([]any, 0, len(headers))
	for _, name := range headers {
		row = append(row, name)
	}
	if err := file.SetSheetRow(sheet, cellName(1, xlsxHeaderRow), &row); err != nil {
		return fmt.Errorf("写入 XLSX %s 表头失败：%w", sheet, err)
	}
	if err := file.SetCellStyle(sheet, cellName(1, xlsxHeaderRow),
		cellName(len(headers), xlsxHeaderRow), styles.header); err != nil {
		return fmt.Errorf("设置 XLSX %s 表头样式失败：%w", sheet, err)
	}
	return nil
}

// xlsxFinish 冻结表头、按内容设列宽，并返回最后一行行号。
func xlsxFinish(file *excelize.File, sheet string, widths []float64, lastRow int) error {
	for index, width := range widths {
		name, err := excelize.ColumnNumberToName(index + 1)
		if err != nil {
			return fmt.Errorf("定位 XLSX %s 列失败：%w", sheet, err)
		}
		if err := file.SetColWidth(sheet, name, name, width); err != nil {
			return fmt.Errorf("设置 XLSX %s 列宽失败：%w", sheet, err)
		}
	}
	panes := &excelize.Panes{
		Freeze:      true,
		YSplit:      xlsxHeaderRow,
		TopLeftCell: cellName(1, xlsxFirstRow),
		ActivePane:  "bottomLeft",
	}
	if err := file.SetPanes(sheet, panes); err != nil {
		return fmt.Errorf("冻结 XLSX %s 表头失败：%w", sheet, err)
	}
	if lastRow <= xlsxHeaderRow {
		return nil
	}
	return nil
}

// writeXLSXDetail 写出发票明细表。
func writeXLSXDetail(file *excelize.File, batch *Batch, styles xlsxStyles) error {
	sheet := xlsxSheetDetail
	if err := xlsxHeader(file, sheet, "发票明细", detailSubtitle(batch), csvHeader, styles); err != nil {
		return err
	}
	for index, item := range batch.Items {
		row := xlsxFirstRow + index
		values := detailCells(item)
		if err := file.SetSheetRow(sheet, cellName(1, row), &values); err != nil {
			return fmt.Errorf("写入 XLSX 明细行失败：%w", err)
		}
		if err := styleXLSXDetailRow(file, sheet, row, index, item, styles); err != nil {
			return err
		}
	}
	lastRow := xlsxFirstRow + len(batch.Items) - 1
	totalRow := lastRow + 1
	if err := writeXLSXDetailTotal(file, sheet, totalRow, batch, styles); err != nil {
		return err
	}
	if err := xlsxFinish(file, sheet, detailWidths, totalRow); err != nil {
		return err
	}
	// 自动筛选只覆盖表头与明细：把合计行圈进去，用户按金额筛选时总计会跟着动，
	// 对账时容易被误读成"筛选后的合计"。
	if len(batch.Items) > 0 {
		ref := fmt.Sprintf("A%d:%s%d", xlsxHeaderRow, columnName(len(csvHeader)), lastRow)
		if err := file.AutoFilter(sheet, ref, nil); err != nil {
			return fmt.Errorf("设置 XLSX 明细自动筛选失败：%w", err)
		}
	}
	return nil
}

// detailSubtitle 是明细表的副标题：一眼看到张数、失败数与三项合计。
func detailSubtitle(batch *Batch) string {
	summary := batch.Summary
	failed := 0
	for _, item := range batch.Items {
		if !item.OK() {
			failed++
		}
	}
	parts := []string{fmt.Sprintf("共 %d 张", len(batch.Items))}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("抽取失败 %d 张", failed))
	}
	if len(batch.Skipped) > 0 {
		parts = append(parts, fmt.Sprintf("重复跳过 %d 张", len(batch.Skipped)))
	}
	if value := moneyText(summary.Amount); value != "" {
		parts = append(parts, "不含税合计 "+value)
	}
	if value := moneyText(summary.TaxAmount); value != "" {
		parts = append(parts, "税额合计 "+value)
	}
	if value := moneyText(summary.TotalAmount); value != "" {
		parts = append(parts, "价税合计 "+value)
	}
	if summary.Red > 0 {
		parts = append(parts, fmt.Sprintf("含红字发票 %d 张", summary.Red))
	}
	return strings.Join(parts, "　·　")
}

// detailCells 把一条处理结果转成明细行的单元格值。金额写数字，其余写文本：
// 文本型金额在 Excel 的 SUM 里会被跳过，整列求和直接得 0。
func detailCells(item Item) []any {
	inv := item.Invoice
	row := []any{
		item.Name, "", "", "", "", "",
		"", "", "", "",
		nil, nil, nil,
		duplicateText(item), strings.Join(itemIssues(item), "；"),
	}
	if inv != nil {
		row[1] = inv.Code
		row[2] = inv.Number
		row[3] = inv.Date
		row[4] = inv.Type
		row[6] = inv.Buyer.Name
		row[7] = inv.Buyer.Code
		row[8] = inv.Seller.Name
		row[9] = inv.Seller.Code
		row[10] = decimalCell(inv.Amount)
		row[11] = decimalCell(inv.TaxAmount)
		row[12] = decimalCell(inv.TotalAmount)
	}
	row[5] = redText(item)
	return row
}

// styleXLSXDetailRow 给明细行上样式。
//
// 不能整行套一个样式：金额列的数字格式、居中列的对齐方式、异常行的底色各是一套，
// 整行一把套上去会把金额格式抹掉，Excel 求和随之失效。
func styleXLSXDetailRow(file *excelize.File, sheet string, row, index int, item Item,
	styles xlsxStyles) error {
	alt := index%2 == 1
	textStyle, centerStyle, moneyStyle := styles.body, styles.center, styles.money
	if !item.OK() {
		// 抽取失败的行整行浅红：金额列本来是空的，用同一套底色才不会在红行里
		// 夹着几格白底，看着像漏填。
		if alt {
			textStyle, centerStyle, moneyStyle = styles.badTextAlt, styles.badTextAlt, styles.badTextAlt
		} else {
			textStyle, centerStyle, moneyStyle = styles.badText, styles.badText, styles.badText
		}
	} else {
		if alt {
			textStyle, centerStyle, moneyStyle = styles.bodyAlt, styles.centerAlt, styles.moneyAlt
		}
		if item.Red {
			// 「红字」列用加粗红字标记；金额负数由数字格式里的 [Red] 负责。
			centerStyle = pick(alt, styles.redMarkAlt, styles.redMark)
		}
	}
	segments := []struct {
		from, to int
		style    int
	}{
		{1, detailDateColumn - 1, textStyle},
		{detailDateColumn, detailTypeColumn, centerStyle},
		{detailTypeColumn + 1, detailRedColumn, centerStyle},
		{detailRedColumn + 1, detailTextTail, textStyle},
		{detailAmountCol, detailAmountEnd, moneyStyle},
	}
	for _, segment := range segments {
		if err := file.SetCellStyle(sheet, cellName(segment.from, row),
			cellName(segment.to, row), segment.style); err != nil {
			return fmt.Errorf("设置 XLSX 明细行样式失败：%w", err)
		}
	}
	noteStyle, issueStyle := textStyle, textStyle
	if item.DuplicateOf != "" {
		noteStyle = pick(alt, styles.duplicateAlt, styles.duplicate)
	}
	if len(itemIssues(item)) > 0 {
		issueStyle = pick(alt, styles.issueTextAlt, styles.issueText)
	}
	for column, style := range map[int]int{
		detailDuplicateCo: noteStyle,
		detailIssueColumn: issueStyle,
	} {
		if err := file.SetCellStyle(sheet, cellName(column, row),
			cellName(column, row), style); err != nil {
			return fmt.Errorf("设置 XLSX 明细备注样式失败：%w", err)
		}
	}
	return nil
}

// writeXLSXDetailTotal 写明细表的合计行。
//
// 合计行的标签放在各金额列左边的空列里，金额与明细列位对齐；整行先套标签样式，
// 再单独把三个金额列改成数字格式，否则合计金额会退化成无法求和的文本。
func writeXLSXDetailTotal(file *excelize.File, sheet string, row int, batch *Batch, styles xlsxStyles) error {
	summary := batch.Summary
	values := []any{
		fmt.Sprintf("合计 %d 张", summary.Count), "", "", "", "", "",
		"不含税金额", decimalCell(summary.Amount),
		"税额", decimalCell(summary.TaxAmount),
		"价税合计", decimalCell(summary.TotalAmount),
		"", "",
	}
	if summary.Red > 0 {
		values[detailDuplicateCo-1] = fmt.Sprintf("含红字 %d 张", summary.Red)
	}
	if err := file.SetSheetRow(sheet, cellName(1, row), &values); err != nil {
		return fmt.Errorf("写入 XLSX 合计行失败：%w", err)
	}
	if err := file.SetRowHeight(sheet, row, 22); err != nil {
		return fmt.Errorf("设置 XLSX 合计行高失败：%w", err)
	}
	if err := file.SetCellStyle(sheet, cellName(1, row), cellName(len(csvHeader), row),
		styles.totalLabel); err != nil {
		return fmt.Errorf("设置 XLSX 合计行样式失败：%w", err)
	}
	for _, column := range []int{detailAmountCol - 2, detailAmountCol, detailAmountEnd} {
		if err := file.SetCellStyle(sheet, cellName(column, row), cellName(column, row),
			styles.totalMoney); err != nil {
			return fmt.Errorf("设置 XLSX 合计金额样式失败：%w", err)
		}
	}
	return nil
}

// writeXLSXGroups 写分组汇总表。
func writeXLSXGroups(file *excelize.File, batch *Batch, styles xlsxStyles) error {
	sheet := xlsxSheetGroup
	headers := []string{"维度", "键", "名称", "张数", "红字", "不含税金额", "税额", "价税合计"}
	if err := xlsxHeader(file, sheet, "分组汇总", groupSubtitle(batch), headers, styles); err != nil {
		return err
	}
	summary := batch.Summary
	values := []any{"合计", "", "", summary.Count, summary.Red,
		decimalCell(summary.Amount), decimalCell(summary.TaxAmount), decimalCell(summary.TotalAmount)}
	if err := file.SetSheetRow(sheet, cellName(1, xlsxFirstRow), &values); err != nil {
		return fmt.Errorf("写入 XLSX 分组合计行失败：%w", err)
	}
	row := xlsxFirstRow + 1
	for _, group := range []struct {
		name   string
		groups []Group
	}{
		{"销售方", summary.BySeller},
		{"发票类型", summary.ByType},
		{"开票月份", summary.ByMonth},
	} {
		for _, entry := range group.groups {
			values := []any{group.name, entry.Key, entry.Name, entry.Count, entry.Red,
				decimalCell(entry.Amount), decimalCell(entry.TaxAmount), decimalCell(entry.TotalAmount)}
			if err := file.SetSheetRow(sheet, cellName(1, row), &values); err != nil {
				return fmt.Errorf("写入 XLSX 分组行失败：%w", err)
			}
			if err := styleXLSXGroupRow(file, sheet, row, row-xlsxFirstRow-1, entry, styles); err != nil {
				return err
			}
			row++
		}
	}
	if err := file.SetCellStyle(sheet, cellName(1, xlsxFirstRow), cellName(len(headers), xlsxFirstRow),
		styles.totalLabel); err != nil {
		return fmt.Errorf("设置 XLSX 分组合计行样式失败：%w", err)
	}
	for _, column := range []int{6, 7, 8} {
		if err := file.SetCellStyle(sheet, cellName(column, xlsxFirstRow),
			cellName(column, xlsxFirstRow), styles.totalMoney); err != nil {
			return fmt.Errorf("设置 XLSX 分组合计金额样式失败：%w", err)
		}
	}
	return xlsxFinish(file, sheet, groupWidths, row)
}

// groupSubtitle 是分组汇总表的副标题，说明分组口径与合计口径一致。
func groupSubtitle(batch *Batch) string {
	parts := []string{"合计与明细同口径：按「分」四舍五入后求和，红字发票负金额参与合计"}
	if len(batch.Summary.BySeller) > 1 {
		parts = append(parts, fmt.Sprintf("销售方 %d 家（按纳税人识别号分组）", len(batch.Summary.BySeller)))
	}
	return strings.Join(parts, "　·　")
}

func styleXLSXGroupRow(file *excelize.File, sheet string, row, index int, entry Group,
	styles xlsxStyles) error {
	alt := index%2 == 1
	bodyStyle, moneyStyle := styles.body, styles.money
	if alt {
		bodyStyle, moneyStyle = styles.bodyAlt, styles.moneyAlt
	}
	if entry.TotalAmount != nil && entry.TotalAmount.IsNegative() {
		moneyStyle = pick(alt, styles.moneyAlt, styles.money)
	}
	if err := file.SetCellStyle(sheet, cellName(1, row), cellName(5, row), bodyStyle); err != nil {
		return fmt.Errorf("设置 XLSX 分组行样式失败：%w", err)
	}
	if err := file.SetCellStyle(sheet, cellName(6, row), cellName(8, row), moneyStyle); err != nil {
		return fmt.Errorf("设置 XLSX 分组金额样式失败：%w", err)
	}
	return nil
}

// writeXLSXIssues 写异常与跳过表。
func writeXLSXIssues(file *excelize.File, batch *Batch, styles xlsxStyles) error {
	sheet := xlsxSheetIssue
	headers := []string{"来源", "类型", "说明"}
	rows := issueRows(batch)
	subtitle := fmt.Sprintf("共 %d 条：自校验不通过、抽取失败与被去重跳过的输入", len(rows))
	if err := xlsxHeader(file, sheet, "异常与跳过", subtitle, headers, styles); err != nil {
		return err
	}
	if len(rows) == 0 {
		values := []any{"—", "无异常", "全部发票均通过自校验，也没有重复被跳过"}
		if err := file.SetSheetRow(sheet, cellName(1, xlsxFirstRow), &values); err != nil {
			return fmt.Errorf("写入 XLSX 空异常提示失败：%w", err)
		}
		if err := file.SetCellStyle(sheet, cellName(1, xlsxFirstRow),
			cellName(len(headers), xlsxFirstRow), styles.note); err != nil {
			return fmt.Errorf("设置 XLSX 空异常提示样式失败：%w", err)
		}
		return xlsxFinish(file, sheet, issueWidths, xlsxFirstRow)
	}
	for index, row := range rows {
		number := xlsxFirstRow + index
		values := []any{row[0], row[1], row[2]}
		if err := file.SetSheetRow(sheet, cellName(1, number), &values); err != nil {
			return fmt.Errorf("写入 XLSX 异常行失败：%w", err)
		}
		style := pick(index%2 == 1, styles.bodyAlt, styles.body)
		if row[1] == "抽取失败" {
			style = pick(index%2 == 1, styles.issueTextAlt, styles.issueText)
		}
		if err := file.SetCellStyle(sheet, cellName(1, number),
			cellName(len(headers), number), style); err != nil {
			return fmt.Errorf("设置 XLSX 异常行样式失败：%w", err)
		}
	}
	last := xlsxFirstRow + len(rows) - 1
	if err := file.AutoFilter(sheet, fmt.Sprintf("A%d:%s%d", xlsxHeaderRow,
		columnName(len(headers)), last), nil); err != nil {
		return fmt.Errorf("设置 XLSX 异常自动筛选失败：%w", err)
	}
	return xlsxFinish(file, sheet, issueWidths, last)
}

// issueRows 汇总异常表的数据行，每行是来源、类型与说明。
func issueRows(batch *Batch) [][3]string {
	var rows [][3]string
	for _, item := range batch.Items {
		kind := "自校验"
		switch {
		case !item.OK():
			kind = "抽取失败"
		case item.DuplicateOf != "":
			kind = "重复"
		default:
			if len(item.Issues) == 0 {
				continue
			}
		}
		for _, issue := range issueTexts(item) {
			rows = append(rows, [3]string{item.Name, kind, issue})
		}
	}
	for _, skipped := range batch.Skipped {
		rows = append(rows, [3]string{skipped.Name, "已跳过", skipped.Reason})
	}
	return rows
}

// decimalCell 把金额转成 XLSX 数值单元格的值。
//
// 这里必须写数字而不是字符串：Excel 的金额单元格本身是双精度，写文本会让
// 「求和」直接失效，对账表也就没有意义。缺失的金额留空，与「金额为零」区分开。
// 写入前已按 MoneyScale 舍入，double 对票据金额足够。
func decimalCell(value *Decimal) any {
	if value == nil {
		return ""
	}
	number, err := strconv.ParseFloat(value.String(), 64)
	if err != nil {
		return value.String()
	}
	return number
}

func redText(item Item) string {
	if item.Red {
		return "红字"
	}
	return ""
}

func duplicateText(item Item) string {
	if item.DuplicateOf == "" {
		return ""
	}
	return "与 " + item.DuplicateOf + " 重复"
}

// pick 在两个样式里按条件取一个，省掉成对的 if。
func pick(alt bool, altStyle, style int) int {
	if alt {
		return altStyle
	}
	return style
}

// columnName 返回列号对应的列名。筛选范围、合并区域这类只给列不给行的写法必须
// 用它：cellName 会把行号一起拼进"单元格名"，写进 autoFilter 的 ref 里就是一个
// 跨行的错误范围。
func columnName(column int) string {
	name, err := excelize.ColumnNumberToName(column)
	if err != nil {
		return "A"
	}
	return name
}

func cellName(column, row int) string {
	name, err := excelize.CoordinatesToCellName(column, row)
	if err != nil {
		return "A1"
	}
	return name
}

func strPtr(value string) *string {
	return &value
}
