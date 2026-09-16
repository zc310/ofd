package validator

import (
	"fmt"
	"io"
	"strconv"

	"github.com/xuri/excelize/v2"
)

// RenderXLSX 将校验报告输出为 Office Open XML 工作簿。
func RenderXLSX(writer io.Writer, report Report) error {
	if writer == nil {
		return fmt.Errorf("校验报告输出为空")
	}
	report.applyChineseLabels()
	file := excelize.NewFile()
	defer func() { _ = file.Close() }()

	if err := file.SetSheetName("Sheet1", "汇总"); err != nil {
		return fmt.Errorf("设置 XLSX 汇总页名称失败：%w", err)
	}
	if _, err := file.NewSheet("问题"); err != nil {
		return fmt.Errorf("创建 XLSX 问题页失败：%w", err)
	}
	styles, err := xlsxValidatorStyles(file)
	if err != nil {
		return err
	}
	if err := writeValidatorXLSXSummary(file, report, styles); err != nil {
		return err
	}
	if err := writeValidatorXLSXIssues(file, report, styles); err != nil {
		return err
	}
	file.SetActiveSheet(0)
	if err := file.Write(writer); err != nil {
		return fmt.Errorf("写入 XLSX 工作簿失败：%w", err)
	}
	return nil
}

type xlsxValidatorStyleSet struct {
	title           int
	subtitle        int
	header          int
	label           int
	body            int
	bodyAlt         int
	center          int
	centerAlt       int
	statusValid     int
	statusInvalid   int
	statusPartial   int
	severityError   int
	severityWarning int
	severityInfo    int
}

func xlsxValidatorStyles(file *excelize.File) (xlsxValidatorStyleSet, error) {
	var styles xlsxValidatorStyleSet
	var err error
	styles.title, err = file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Family: "Microsoft YaHei", Size: 16, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"17365D"}},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center"},
	})
	if err != nil {
		return styles, fmt.Errorf("创建 XLSX 标题样式失败：%w", err)
	}
	styles.subtitle, err = file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "Microsoft YaHei", Size: 10, Color: "5B6573"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"EAF2F8"}},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
	})
	if err != nil {
		return styles, fmt.Errorf("创建 XLSX 副标题样式失败：%w", err)
	}
	styles.header, err = file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Family: "Microsoft YaHei", Size: 10, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"1F4E78"}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border:    xlsxValidatorBorders("9FBAD0"),
	})
	if err != nil {
		return styles, fmt.Errorf("创建 XLSX 表头样式失败：%w", err)
	}
	styles.label, err = file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Family: "Microsoft YaHei", Size: 10, Color: "1F2937"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"DDEBF7"}},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
		Border:    xlsxValidatorBorders("D9E2F3"),
	})
	if err != nil {
		return styles, fmt.Errorf("创建 XLSX 标签样式失败：%w", err)
	}
	styles.body, err = xlsxValidatorBodyStyle(file, "FFFFFF")
	if err != nil {
		return styles, err
	}
	styles.bodyAlt, err = xlsxValidatorBodyStyle(file, "F7FAFC")
	if err != nil {
		return styles, err
	}
	styles.center, err = xlsxValidatorCenterStyle(file, "FFFFFF")
	if err != nil {
		return styles, err
	}
	styles.centerAlt, err = xlsxValidatorCenterStyle(file, "F7FAFC")
	if err != nil {
		return styles, err
	}
	styles.statusValid, err = xlsxValidatorStatusStyle(file, "E2F0D9", "2F6B2F")
	if err != nil {
		return styles, err
	}
	styles.statusInvalid, err = xlsxValidatorStatusStyle(file, "FCE4D6", "9C0006")
	if err != nil {
		return styles, err
	}
	styles.statusPartial, err = xlsxValidatorStatusStyle(file, "FFF2CC", "7F6000")
	if err != nil {
		return styles, err
	}
	styles.severityError, err = xlsxValidatorStatusStyle(file, "FCE4D6", "9C0006")
	if err != nil {
		return styles, err
	}
	styles.severityWarning, err = xlsxValidatorStatusStyle(file, "FFF2CC", "7F6000")
	if err != nil {
		return styles, err
	}
	styles.severityInfo, err = xlsxValidatorStatusStyle(file, "E7E6E6", "595959")
	if err != nil {
		return styles, err
	}
	return styles, nil
}

func xlsxValidatorBodyStyle(file *excelize.File, fill string) (int, error) {
	style, err := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "Microsoft YaHei", Size: 10, Color: "1F2937"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{fill}},
		Alignment: &excelize.Alignment{Vertical: "top", WrapText: true},
		Border:    xlsxValidatorBorders("E3EAF2"),
	})
	if err != nil {
		return 0, fmt.Errorf("创建 XLSX 明细样式失败：%w", err)
	}
	return style, nil
}

func xlsxValidatorCenterStyle(file *excelize.File, fill string) (int, error) {
	style, err := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "Microsoft YaHei", Size: 10, Color: "1F2937"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{fill}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border:    xlsxValidatorBorders("E3EAF2"),
	})
	if err != nil {
		return 0, fmt.Errorf("创建 XLSX 居中样式失败：%w", err)
	}
	return style, nil
}

func xlsxValidatorStatusStyle(file *excelize.File, fill, color string) (int, error) {
	style, err := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Family: "Microsoft YaHei", Size: 10, Color: color},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{fill}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border:    xlsxValidatorBorders("D9E2F3"),
	})
	if err != nil {
		return 0, fmt.Errorf("创建 XLSX 状态样式失败：%w", err)
	}
	return style, nil
}

func xlsxValidatorBorders(color string) []excelize.Border {
	return []excelize.Border{
		{Type: "left", Style: 1, Color: color},
		{Type: "right", Style: 1, Color: color},
		{Type: "top", Style: 1, Color: color},
		{Type: "bottom", Style: 1, Color: color},
	}
}

func writeValidatorXLSXSummary(file *excelize.File, report Report, styles xlsxValidatorStyleSet) error {
	if err := file.MergeCell("汇总", "A1", "B1"); err != nil {
		return fmt.Errorf("合并 XLSX 汇总标题失败：%w", err)
	}
	if err := file.SetCellValue("汇总", "A1", "OFD 校验报告"); err != nil {
		return fmt.Errorf("写入 XLSX 汇总标题失败：%w", err)
	}
	if err := file.SetCellStyle("汇总", "A1", "B1", styles.title); err != nil {
		return fmt.Errorf("设置 XLSX 汇总标题样式失败：%w", err)
	}
	if err := file.MergeCell("汇总", "A2", "B2"); err != nil {
		return fmt.Errorf("合并 XLSX 汇总副标题失败：%w", err)
	}
	subtitle := fmt.Sprintf("输入文件：%s；检测时间：%s；耗时：%d ms", report.Input.Path, formatDetectionTime(report.StartedAt), report.DurationMS)
	if err := file.SetCellValue("汇总", "A2", subtitle); err != nil {
		return fmt.Errorf("写入 XLSX 汇总副标题失败：%w", err)
	}
	if err := file.SetCellStyle("汇总", "A2", "B2", styles.subtitle); err != nil {
		return fmt.Errorf("设置 XLSX 汇总副标题样式失败：%w", err)
	}
	rows := [][]string{
		{"字段", "值"},
		{"状态", statusLabel(report.Status)},
		{"错误", strconv.Itoa(report.Summary.Errors)},
		{"警告", strconv.Itoa(report.Summary.Warnings)},
		{"提示", strconv.Itoa(report.Summary.Infos)},
		{"文件", strconv.Itoa(report.Summary.Files)},
		{"工具", report.Tool.Name + " " + report.Tool.Version},
	}
	for rowIndex, row := range rows {
		rowNumber := rowIndex + 4
		cell := fmt.Sprintf("A%d", rowNumber)
		if err := file.SetSheetRow("汇总", cell, &row); err != nil {
			return fmt.Errorf("写入 XLSX 汇总数据失败：%w", err)
		}
		labelStyle := styles.label
		valueStyle := styles.center
		if rowIndex == 0 {
			labelStyle = styles.header
			valueStyle = styles.header
		} else if rowIndex%2 == 0 {
			valueStyle = styles.centerAlt
		}
		switch row[0] {
		case "状态":
			valueStyle = xlsxValidatorStatusStyleID(report.Status, styles)
		}
		if err := file.SetCellStyle("汇总", cell, fmt.Sprintf("A%d", rowNumber), labelStyle); err != nil {
			return fmt.Errorf("设置 XLSX 汇总标签样式失败：%w", err)
		}
		if err := file.SetCellStyle("汇总", fmt.Sprintf("B%d", rowNumber), fmt.Sprintf("B%d", rowNumber), valueStyle); err != nil {
			return fmt.Errorf("设置 XLSX 汇总值样式失败：%w", err)
		}
	}

	if err := file.MergeCell("汇总", "A12", "B12"); err != nil {
		return fmt.Errorf("合并 XLSX 检查结果标题失败：%w", err)
	}
	if err := file.SetCellValue("汇总", "A12", "检查结果"); err != nil {
		return fmt.Errorf("写入 XLSX 检查结果标题失败：%w", err)
	}
	if err := file.SetCellStyle("汇总", "A12", "B12", styles.label); err != nil {
		return fmt.Errorf("设置 XLSX 检查结果标题样式失败：%w", err)
	}
	checkRows := [][]string{{"检查项", "状态"}}
	for _, check := range report.Checks {
		checkRows = append(checkRows, []string{checkLabel(check.Name), checkStatusLabel(check.Status)})
	}
	for rowIndex, row := range checkRows {
		rowNumber := rowIndex + 13
		cell := fmt.Sprintf("A%d", rowNumber)
		if err := file.SetSheetRow("汇总", cell, &row); err != nil {
			return fmt.Errorf("写入 XLSX 检查结果失败：%w", err)
		}
		labelStyle := styles.body
		valueStyle := styles.center
		if rowIndex == 0 {
			labelStyle = styles.header
			valueStyle = styles.header
		} else if rowIndex%2 == 0 {
			valueStyle = styles.centerAlt
		}
		if err := file.SetCellStyle("汇总", cell, fmt.Sprintf("A%d", rowNumber), labelStyle); err != nil {
			return fmt.Errorf("设置 XLSX 检查项样式失败：%w", err)
		}
		if err := file.SetCellStyle("汇总", fmt.Sprintf("B%d", rowNumber), fmt.Sprintf("B%d", rowNumber), valueStyle); err != nil {
			return fmt.Errorf("设置 XLSX 检查状态样式失败：%w", err)
		}
	}
	if err := file.SetColWidth("汇总", "A", "A", 18); err != nil {
		return fmt.Errorf("设置 XLSX 汇总标签列宽失败：%w", err)
	}
	if err := file.SetColWidth("汇总", "B", "B", 96); err != nil {
		return fmt.Errorf("设置 XLSX 汇总值列宽失败：%w", err)
	}
	for _, rowNumber := range []int{1, 2, 4, 12, 13} {
		if err := file.SetRowHeight("汇总", rowNumber, 24); err != nil {
			return fmt.Errorf("设置 XLSX 汇总行高失败：%w", err)
		}
	}
	return nil
}

func xlsxValidatorStatusStyleID(status Status, styles xlsxValidatorStyleSet) int {
	switch status {
	case StatusValid:
		return styles.statusValid
	case StatusInvalid:
		return styles.statusInvalid
	case StatusPartial:
		return styles.statusPartial
	default:
		return styles.statusInvalid
	}
}

func xlsxValidatorSeverityStyleID(severity Severity, styles xlsxValidatorStyleSet) int {
	switch severity {
	case SeverityError:
		return styles.severityError
	case SeverityWarning:
		return styles.severityWarning
	case SeverityInfo:
		return styles.severityInfo
	default:
		return styles.severityInfo
	}
}

func writeValidatorXLSXIssues(file *excelize.File, report Report, styles xlsxValidatorStyleSet) error {
	if err := file.MergeCell("问题", "A1", "K1"); err != nil {
		return fmt.Errorf("合并 XLSX 问题标题失败：%w", err)
	}
	if err := file.SetCellValue("问题", "A1", "OFD 校验问题"); err != nil {
		return fmt.Errorf("写入 XLSX 问题标题失败：%w", err)
	}
	if err := file.SetCellStyle("问题", "A1", "K1", styles.title); err != nil {
		return fmt.Errorf("设置 XLSX 问题标题样式失败：%w", err)
	}
	if err := file.MergeCell("问题", "A2", "K2"); err != nil {
		return fmt.Errorf("合并 XLSX 问题副标题失败：%w", err)
	}
	if err := file.SetCellValue("问题", "A2", fmt.Sprintf("共 %d 条问题", len(report.Issues))); err != nil {
		return fmt.Errorf("写入 XLSX 问题副标题失败：%w", err)
	}
	if err := file.SetCellStyle("问题", "A2", "K2", styles.subtitle); err != nil {
		return fmt.Errorf("设置 XLSX 问题副标题样式失败：%w", err)
	}
	headers := []string{"序号", "严重级别", "阶段", "代码", "引擎代码", "文件", "行", "列", "XML 路径", "信息", "提示"}
	if err := file.SetSheetRow("问题", "A4", &headers); err != nil {
		return fmt.Errorf("写入 XLSX 问题表头失败：%w", err)
	}
	if err := file.SetCellStyle("问题", "A4", "K4", styles.header); err != nil {
		return fmt.Errorf("设置 XLSX 问题表头样式失败：%w", err)
	}
	for index, issue := range report.Issues {
		line := ""
		column := ""
		if issue.Line > 0 {
			line = strconv.Itoa(issue.Line)
		}
		if issue.Column > 0 {
			column = strconv.Itoa(issue.Column)
		}
		row := []string{
			strconv.Itoa(index + 1),
			issue.SeverityZh,
			issue.StageZh,
			issue.Code,
			issue.EngineCode,
			issue.File,
			line,
			column,
			issue.Path,
			issue.Message,
			issue.Hint,
		}
		rowNumber := index + 5
		cell := fmt.Sprintf("A%d", rowNumber)
		if err := file.SetSheetRow("问题", cell, &row); err != nil {
			return fmt.Errorf("写入 XLSX 问题数据失败：%w", err)
		}
		bodyStyle := styles.body
		centerStyle := styles.center
		if index%2 == 1 {
			bodyStyle = styles.bodyAlt
			centerStyle = styles.centerAlt
		}
		if err := file.SetCellStyle("问题", cell, fmt.Sprintf("K%d", rowNumber), bodyStyle); err != nil {
			return fmt.Errorf("设置 XLSX 问题基础样式失败：%w", err)
		}
		if err := file.SetCellStyle("问题", fmt.Sprintf("G%d", rowNumber), fmt.Sprintf("H%d", rowNumber), centerStyle); err != nil {
			return fmt.Errorf("设置 XLSX 问题行列样式失败：%w", err)
		}
		if err := file.SetCellStyle("问题", fmt.Sprintf("B%d", rowNumber), fmt.Sprintf("B%d", rowNumber), xlsxValidatorSeverityStyleID(issue.Severity, styles)); err != nil {
			return fmt.Errorf("设置 XLSX 问题严重级别样式失败：%w", err)
		}
	}
	widths := []float64{8, 12, 12, 18, 14, 32, 8, 8, 32, 60, 40}
	for index, width := range widths {
		column := xlsxValidatorColumnName(index + 1)
		if err := file.SetColWidth("问题", column, column, width); err != nil {
			return fmt.Errorf("设置 XLSX 问题列宽失败：%w", err)
		}
	}
	for _, rowNumber := range []int{1, 2, 4} {
		if err := file.SetRowHeight("问题", rowNumber, 24); err != nil {
			return fmt.Errorf("设置 XLSX 问题行高失败：%w", err)
		}
	}
	if len(report.Issues) > 0 {
		lastRow := len(report.Issues) + 4
		if err := file.AutoFilter("问题", fmt.Sprintf("A4:K%d", lastRow), nil); err != nil {
			return fmt.Errorf("设置 XLSX 问题自动筛选失败：%w", err)
		}
	}
	if err := file.SetPanes("问题", &excelize.Panes{Freeze: true, YSplit: 4, TopLeftCell: "A5"}); err != nil {
		return fmt.Errorf("冻结 XLSX 问题表头失败：%w", err)
	}
	return nil
}

func xlsxValidatorColumnName(column int) string {
	var letters []byte
	for column > 0 {
		column--
		letters = append([]byte{byte('A' + column%26)}, letters...)
		column /= 26
	}
	return string(letters)
}
