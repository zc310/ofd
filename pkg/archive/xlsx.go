package archive

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// RenderMatrixXLSX 将条文矩阵写入 Office Open XML 工作簿。
func RenderMatrixXLSX(writer io.Writer, matrix MatrixReport) error {
	if writer == nil {
		return fmt.Errorf("XLSX 矩阵输出为空")
	}
	file := excelize.NewFile()
	defer func() { _ = file.Close() }()

	if err := file.SetSheetName("Sheet1", "汇总"); err != nil {
		return fmt.Errorf("设置 XLSX 汇总页名称失败：%w", err)
	}
	if _, err := file.NewSheet("条文矩阵"); err != nil {
		return fmt.Errorf("创建 XLSX 条文矩阵页失败：%w", err)
	}

	styles, err := xlsxStyles(file)
	if err != nil {
		return err
	}
	if err := writeXLSXSummary(file, matrix, styles); err != nil {
		return err
	}
	if err := writeXLSXMatrix(file, matrix, styles); err != nil {
		return err
	}
	file.SetActiveSheet(1)
	if err := file.Write(writer); err != nil {
		return fmt.Errorf("写入 XLSX 工作簿失败：%w", err)
	}
	return nil
}

type xlsxStyleSet struct {
	title         int
	subtitle      int
	header        int
	label         int
	body          int
	bodyAlt       int
	center        int
	centerAlt     int
	statusPassed  int
	statusFailed  int
	statusWarning int
	statusReview  int
	statusOther   int
}

func xlsxStyles(file *excelize.File) (xlsxStyleSet, error) {
	var styles xlsxStyleSet
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
		Border:    xlsxBorders("9FBAD0"),
	})
	if err != nil {
		return styles, fmt.Errorf("创建 XLSX 表头样式失败：%w", err)
	}
	styles.label, err = file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Family: "Microsoft YaHei", Size: 10, Color: "1F2937"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"DDEBF7"}},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
		Border:    xlsxBorders("D9E2F3"),
	})
	if err != nil {
		return styles, fmt.Errorf("创建 XLSX 标签样式失败：%w", err)
	}
	styles.body, err = file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "Microsoft YaHei", Size: 10, Color: "1F2937"},
		Alignment: &excelize.Alignment{Vertical: "top", WrapText: true},
		Border:    xlsxBorders("E3EAF2"),
	})
	if err != nil {
		return styles, fmt.Errorf("创建 XLSX 明细样式失败：%w", err)
	}
	styles.bodyAlt, err = file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "Microsoft YaHei", Size: 10, Color: "1F2937"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"F7FAFC"}},
		Alignment: &excelize.Alignment{Vertical: "top", WrapText: true},
		Border:    xlsxBorders("E3EAF2"),
	})
	if err != nil {
		return styles, fmt.Errorf("创建 XLSX 交替行样式失败：%w", err)
	}
	styles.center, err = file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "Microsoft YaHei", Size: 10, Color: "1F2937"},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border:    xlsxBorders("E3EAF2"),
	})
	if err != nil {
		return styles, fmt.Errorf("创建 XLSX 居中样式失败：%w", err)
	}
	styles.centerAlt, err = file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "Microsoft YaHei", Size: 10, Color: "1F2937"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"F7FAFC"}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border:    xlsxBorders("E3EAF2"),
	})
	if err != nil {
		return styles, fmt.Errorf("创建 XLSX 交替行居中样式失败：%w", err)
	}
	styles.statusPassed, err = xlsxStatusStyle(file, "E2F0D9", "2F6B2F")
	if err != nil {
		return styles, err
	}
	styles.statusFailed, err = xlsxStatusStyle(file, "FCE4D6", "9C0006")
	if err != nil {
		return styles, err
	}
	styles.statusWarning, err = xlsxStatusStyle(file, "FFF2CC", "7F6000")
	if err != nil {
		return styles, err
	}
	styles.statusReview, err = xlsxStatusStyle(file, "DDEBF7", "1F4E78")
	if err != nil {
		return styles, err
	}
	styles.statusOther, err = xlsxStatusStyle(file, "E7E6E6", "595959")
	if err != nil {
		return styles, err
	}
	return styles, nil
}

func xlsxStatusStyle(file *excelize.File, fill, color string) (int, error) {
	style, err := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Family: "Microsoft YaHei", Size: 10, Color: color},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{fill}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border:    xlsxBorders("D9E2F3"),
	})
	if err != nil {
		return 0, fmt.Errorf("创建 XLSX 状态样式失败：%w", err)
	}
	return style, nil
}

func xlsxBorders(color string) []excelize.Border {
	return []excelize.Border{
		{Type: "left", Style: 1, Color: color},
		{Type: "right", Style: 1, Color: color},
		{Type: "top", Style: 1, Color: color},
		{Type: "bottom", Style: 1, Color: color},
	}
}

func writeXLSXSummary(file *excelize.File, matrix MatrixReport, styles xlsxStyleSet) error {
	if err := file.MergeCell("汇总", "A1", "B1"); err != nil {
		return fmt.Errorf("合并 XLSX 汇总标题失败：%w", err)
	}
	if err := file.SetCellValue("汇总", "A1", matrix.Standard+" 条文符合性矩阵"); err != nil {
		return fmt.Errorf("写入 XLSX 汇总标题失败：%w", err)
	}
	if err := file.SetCellStyle("汇总", "A1", "B1", styles.title); err != nil {
		return fmt.Errorf("设置 XLSX 汇总标题样式失败：%w", err)
	}
	if err := file.MergeCell("汇总", "A2", "B2"); err != nil {
		return fmt.Errorf("合并 XLSX 汇总副标题失败：%w", err)
	}
	if err := file.SetCellValue("汇总", "A2", "来源："+matrix.Source+"；输入文件："+matrix.Input.Path); err != nil {
		return fmt.Errorf("写入 XLSX 汇总副标题失败：%w", err)
	}
	if err := file.SetCellStyle("汇总", "A2", "B2", styles.subtitle); err != nil {
		return fmt.Errorf("设置 XLSX 汇总副标题样式失败：%w", err)
	}
	rows := [][]string{
		{"字段", "值"},
		{"总体状态", xlsxStatus(matrix.OverallStatus)},
		{"条款总数", strconv.Itoa(matrix.Summary.Total)},
		{"通过", strconv.Itoa(matrix.Summary.Passed)},
		{"失败", strconv.Itoa(matrix.Summary.Failed)},
		{"警告", strconv.Itoa(matrix.Summary.Warnings)},
		{"人工确认", strconv.Itoa(matrix.Summary.ManualReview)},
		{"不适用", strconv.Itoa(matrix.Summary.NotApplicable)},
		{"未评估", strconv.Itoa(matrix.Summary.NotAssessed)},
		{"不支持", strconv.Itoa(matrix.Summary.Unsupported)},
	}
	for rowIndex, row := range rows {
		rowNumber := rowIndex + 4
		cell := fmt.Sprintf("A%d", rowNumber)
		if err := file.SetSheetRow("汇总", cell, &row); err != nil {
			return fmt.Errorf("写入 XLSX 汇总数据失败：%w", err)
		}
		style := styles.label
		if rowIndex == 0 {
			style = styles.header
		}
		if err := file.SetCellStyle("汇总", cell, fmt.Sprintf("A%d", rowNumber), style); err != nil {
			return fmt.Errorf("设置 XLSX 汇总标签样式失败：%w", err)
		}
		valueStyle := styles.body
		if rowIndex%2 == 0 {
			valueStyle = styles.bodyAlt
		}
		if rowIndex == 0 {
			valueStyle = styles.header
		}
		if rowIndex == 1 {
			valueStyle = xlsxStatusStyleID(matrix.OverallStatus, styles)
		} else if rowIndex > 1 {
			if rowIndex%2 == 0 {
				valueStyle = styles.centerAlt
			} else {
				valueStyle = styles.center
			}
		}
		if err := file.SetCellStyle("汇总", fmt.Sprintf("B%d", rowNumber), fmt.Sprintf("B%d", rowNumber), valueStyle); err != nil {
			return fmt.Errorf("设置 XLSX 汇总值样式失败：%w", err)
		}
	}
	if err := file.SetColWidth("汇总", "A", "A", 18); err != nil {
		return fmt.Errorf("设置 XLSX 汇总标签列宽失败：%w", err)
	}
	if err := file.SetColWidth("汇总", "B", "B", 96); err != nil {
		return fmt.Errorf("设置 XLSX 汇总值列宽失败：%w", err)
	}
	if err := file.SetRowHeight("汇总", 1, 28); err != nil {
		return err
	}
	if err := file.SetRowHeight("汇总", 2, 32); err != nil {
		return err
	}
	if err := file.SetRowHeight("汇总", 4, 24); err != nil {
		return err
	}
	if err := file.SetPanes("汇总", &excelize.Panes{Freeze: true, YSplit: 4, TopLeftCell: "A5"}); err != nil {
		return fmt.Errorf("冻结 XLSX 汇总表头失败：%w", err)
	}
	return nil
}

func xlsxStatusStyleID(value string, styles xlsxStyleSet) int {
	switch value {
	case MatrixPassed:
		return styles.statusPassed
	case MatrixFailed:
		return styles.statusFailed
	case MatrixWarning:
		return styles.statusWarning
	case MatrixManualReview:
		return styles.statusReview
	default:
		return styles.statusOther
	}
}

func writeXLSXMatrix(file *excelize.File, matrix MatrixReport, styles xlsxStyleSet) error {
	if err := file.MergeCell("条文矩阵", "A1", "J1"); err != nil {
		return fmt.Errorf("合并 XLSX 条文标题失败：%w", err)
	}
	if err := file.SetCellValue("条文矩阵", "A1", matrix.Standard+" 条文符合性矩阵"); err != nil {
		return fmt.Errorf("写入 XLSX 条文标题失败：%w", err)
	}
	if err := file.SetCellStyle("条文矩阵", "A1", "J1", styles.title); err != nil {
		return fmt.Errorf("设置 XLSX 条文标题样式失败：%w", err)
	}
	if err := file.MergeCell("条文矩阵", "A2", "J2"); err != nil {
		return fmt.Errorf("合并 XLSX 条文副标题失败：%w", err)
	}
	subtitle := "来源：" + matrix.Source + "；状态码已转换为中文展示，JSON/Markdown 保持原状态码"
	if err := file.SetCellValue("条文矩阵", "A2", subtitle); err != nil {
		return fmt.Errorf("写入 XLSX 条文副标题失败：%w", err)
	}
	if err := file.SetCellStyle("条文矩阵", "A2", "J2", styles.subtitle); err != nil {
		return fmt.Errorf("设置 XLSX 条文副标题样式失败：%w", err)
	}
	headers := []string{"条款", "标题", "适用性", "状态", "要求", "评估", "证据", "检测器", "人工确认", "限制"}
	if err := file.SetSheetRow("条文矩阵", "A4", &headers); err != nil {
		return fmt.Errorf("写入 XLSX 条文矩阵表头失败：%w", err)
	}
	if err := file.SetCellStyle("条文矩阵", "A4", "J4", styles.header); err != nil {
		return fmt.Errorf("设置 XLSX 条文矩阵表头样式失败：%w", err)
	}
	for index, item := range matrix.Clauses {
		manual := "否"
		if item.ManualReview {
			manual = "是"
		}
		row := []string{item.ID, item.Title, xlsxApplicability(item.Applicability), xlsxStatus(item.Status), item.Requirement, item.Assessment, strings.Join(item.Evidence, "; "), xlsxDetector(item.Detector), manual, item.Limitations}
		rowNumber := index + 5
		cell := fmt.Sprintf("A%d", rowNumber)
		if err := file.SetSheetRow("条文矩阵", cell, &row); err != nil {
			return fmt.Errorf("写入 XLSX 条文数据失败：%w", err)
		}
		bodyStyle := styles.body
		centerStyle := styles.center
		if index%2 == 1 {
			bodyStyle = styles.bodyAlt
			centerStyle = styles.centerAlt
		}
		if err := file.SetCellStyle("条文矩阵", cell, fmt.Sprintf("J%d", rowNumber), bodyStyle); err != nil {
			return fmt.Errorf("设置 XLSX 条文基础样式失败：%w", err)
		}
		if err := file.SetCellStyle("条文矩阵", fmt.Sprintf("A%d", rowNumber), fmt.Sprintf("C%d", rowNumber), centerStyle); err != nil {
			return fmt.Errorf("设置 XLSX 条文标识样式失败：%w", err)
		}
		if err := file.SetCellStyle("条文矩阵", fmt.Sprintf("D%d", rowNumber), fmt.Sprintf("D%d", rowNumber), xlsxStatusStyleID(item.Status, styles)); err != nil {
			return fmt.Errorf("设置 XLSX 条文状态样式失败：%w", err)
		}
		if err := file.SetCellStyle("条文矩阵", fmt.Sprintf("I%d", rowNumber), fmt.Sprintf("I%d", rowNumber), centerStyle); err != nil {
			return fmt.Errorf("设置 XLSX 人工确认样式失败：%w", err)
		}
		if err := file.SetRowHeight("条文矩阵", rowNumber, 60); err != nil {
			return fmt.Errorf("设置 XLSX 条文行高失败：%w", err)
		}
	}
	widths := []float64{12, 22, 12, 12, 52, 60, 36, 24, 12, 52}
	for index, width := range widths {
		column := xlsxColumnName(index + 1)
		if err := file.SetColWidth("条文矩阵", column, column, width); err != nil {
			return fmt.Errorf("设置 XLSX 列宽失败：%w", err)
		}
	}
	if err := file.SetRowHeight("条文矩阵", 1, 28); err != nil {
		return err
	}
	if err := file.SetRowHeight("条文矩阵", 2, 30); err != nil {
		return err
	}
	if err := file.SetRowHeight("条文矩阵", 4, 30); err != nil {
		return err
	}
	lastRow := len(matrix.Clauses) + 4
	if err := file.AutoFilter("条文矩阵", fmt.Sprintf("A4:J%d", lastRow), nil); err != nil {
		return fmt.Errorf("设置 XLSX 自动筛选失败：%w", err)
	}
	if err := file.SetPanes("条文矩阵", &excelize.Panes{Freeze: true, YSplit: 4, TopLeftCell: "A5"}); err != nil {
		return fmt.Errorf("冻结 XLSX 条文表头失败：%w", err)
	}
	return nil
}

func xlsxApplicability(value string) string {
	switch value {
	case "applicable":
		return "适用"
	case "not_applicable":
		return "不适用"
	default:
		return value
	}
}

func xlsxStatus(value string) string {
	switch value {
	case MatrixPassed:
		return "通过"
	case MatrixFailed:
		return "失败"
	case MatrixWarning:
		return "警告"
	case MatrixManualReview:
		return "人工确认"
	case MatrixNotApplicable:
		return "不适用"
	case MatrixNotAssessed:
		return "未评估"
	case MatrixUnsupported:
		return "不支持"
	default:
		return value
	}
}

func xlsxDetector(value string) string {
	detectors := map[string]string{
		"scope":                      "适用范围检查",
		"reference-catalogue":        "引用文件目录检查",
		"process-review":             "处理流程审查",
		"document-review":            "文档审查",
		"combined-evidence":          "综合证据检查",
		"validator":                  "校验器",
		"resource-reference-check":   "资源引用检查",
		"page-dimensions":            "页面尺寸检查",
		"font-embedding-check":       "字体嵌入检查",
		"font-policy-review":         "字体策略审查",
		"object-statistics":          "对象统计",
		"not-implemented":            "暂未实现",
		"image-resource-check":       "图像资源检查",
		"signature-analysis":         "签名分析",
		"annotation-analysis":        "注释分析",
		"attachment-analysis":        "附件分析",
		"feature-scan":               "特征扫描",
		"version-check":              "版本检查",
		"attachment-path-check":      "附件路径检查",
		"attachment-metadata-check":  "附件元数据检查",
		"attachment-preservation":    "附件保留检查",
		"software-capability-review": "软件能力审查",
	}
	if translated, ok := detectors[value]; ok {
		return translated
	}
	return value
}

func xlsxColumnName(column int) string {
	var letters []byte
	for column > 0 {
		column--
		letters = append([]byte{byte('A' + column%26)}, letters...)
		column /= 26
	}
	return string(letters)
}
