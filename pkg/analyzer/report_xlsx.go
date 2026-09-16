package analyzer

import (
	"fmt"
	"io"
	"strconv"

	"github.com/xuri/excelize/v2"
)

// RenderXLSX 将分析报告输出为 Office Open XML 工作簿。
func RenderXLSX(writer io.Writer, report Report) error {
	if writer == nil {
		return fmt.Errorf("分析报告输出为空")
	}
	file := excelize.NewFile()
	defer func() { _ = file.Close() }()

	if err := file.SetSheetName("Sheet1", "汇总"); err != nil {
		return fmt.Errorf("设置 XLSX 汇总页名称失败：%w", err)
	}
	sheets := []string{"文档体", "页面", "资源", "附件", "注解", "签名", "目录树", "文件引用", "ID 引用"}
	for _, name := range sheets {
		if _, err := file.NewSheet(name); err != nil {
			return fmt.Errorf("创建 XLSX %s 页失败：%w", name, err)
		}
	}
	styles, err := xlsxAnalyzerStyles(file)
	if err != nil {
		return err
	}
	if err := writeAnalyzerXLSXSummary(file, report, styles); err != nil {
		return err
	}
	if err := writeAnalyzerXLSXDocuments(file, report, styles); err != nil {
		return err
	}
	if err := writeAnalyzerXLSXPages(file, report, styles); err != nil {
		return err
	}
	if err := writeAnalyzerXLSXResources(file, report, styles); err != nil {
		return err
	}
	if err := writeAnalyzerXLSXAttachments(file, report, styles); err != nil {
		return err
	}
	if err := writeAnalyzerXLSXAnnotations(file, report, styles); err != nil {
		return err
	}
	if err := writeAnalyzerXLSXSignatures(file, report, styles); err != nil {
		return err
	}
	if err := writeAnalyzerXLSXTree(file, report, styles); err != nil {
		return err
	}
	if err := writeAnalyzerXLSXReferences(file, "文件引用", report.FileReferences, styles); err != nil {
		return err
	}
	if err := writeAnalyzerXLSXReferences(file, "ID 引用", report.IDReferences, styles); err != nil {
		return err
	}
	file.SetActiveSheet(0)
	if err := file.Write(writer); err != nil {
		return fmt.Errorf("写入 XLSX 工作簿失败：%w", err)
	}
	return nil
}

type analyzerXLSXStyleSet struct {
	title          int
	subtitle       int
	header         int
	body           int
	bodyAlt        int
	center         int
	centerAlt      int
	statusComplete int
	statusPartial  int
	statusFailed   int
}

func xlsxAnalyzerStyles(file *excelize.File) (analyzerXLSXStyleSet, error) {
	var styles analyzerXLSXStyleSet
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
		Border:    xlsxAnalyzerBorders("9FBAD0"),
	})
	if err != nil {
		return styles, fmt.Errorf("创建 XLSX 表头样式失败：%w", err)
	}
	styles.body, err = xlsxAnalyzerBodyStyle(file, "FFFFFF")
	if err != nil {
		return styles, err
	}
	styles.bodyAlt, err = xlsxAnalyzerBodyStyle(file, "F7FAFC")
	if err != nil {
		return styles, err
	}
	styles.center, err = xlsxAnalyzerCenterStyle(file, "FFFFFF")
	if err != nil {
		return styles, err
	}
	styles.centerAlt, err = xlsxAnalyzerCenterStyle(file, "F7FAFC")
	if err != nil {
		return styles, err
	}
	styles.statusComplete, err = xlsxAnalyzerStatusStyle(file, "E2F0D9", "2F6B2F")
	if err != nil {
		return styles, err
	}
	styles.statusPartial, err = xlsxAnalyzerStatusStyle(file, "FFF2CC", "7F6000")
	if err != nil {
		return styles, err
	}
	styles.statusFailed, err = xlsxAnalyzerStatusStyle(file, "FCE4D6", "9C0006")
	if err != nil {
		return styles, err
	}
	return styles, nil
}

func xlsxAnalyzerBodyStyle(file *excelize.File, fill string) (int, error) {
	style, err := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "Microsoft YaHei", Size: 10, Color: "1F2937"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{fill}},
		Alignment: &excelize.Alignment{Vertical: "top", WrapText: true},
		Border:    xlsxAnalyzerBorders("E3EAF2"),
	})
	if err != nil {
		return 0, fmt.Errorf("创建 XLSX 明细样式失败：%w", err)
	}
	return style, nil
}

func xlsxAnalyzerCenterStyle(file *excelize.File, fill string) (int, error) {
	style, err := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "Microsoft YaHei", Size: 10, Color: "1F2937"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{fill}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border:    xlsxAnalyzerBorders("E3EAF2"),
	})
	if err != nil {
		return 0, fmt.Errorf("创建 XLSX 居中样式失败：%w", err)
	}
	return style, nil
}

func xlsxAnalyzerStatusStyle(file *excelize.File, fill, color string) (int, error) {
	style, err := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Family: "Microsoft YaHei", Size: 10, Color: color},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{fill}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border:    xlsxAnalyzerBorders("D9E2F3"),
	})
	if err != nil {
		return 0, fmt.Errorf("创建 XLSX 状态样式失败：%w", err)
	}
	return style, nil
}

func xlsxAnalyzerBorders(color string) []excelize.Border {
	return []excelize.Border{
		{Type: "left", Style: 1, Color: color},
		{Type: "right", Style: 1, Color: color},
		{Type: "top", Style: 1, Color: color},
		{Type: "bottom", Style: 1, Color: color},
	}
}

func xlsxAnalyzerStatusStyleID(status Status, styles analyzerXLSXStyleSet) int {
	switch status {
	case StatusComplete:
		return styles.statusComplete
	case StatusPartial:
		return styles.statusPartial
	case StatusFailed:
		return styles.statusFailed
	default:
		return styles.statusFailed
	}
}

// writeAnalyzerSheet 写入标题、副标题和通用表头/数据区域。
// 返回写入后最后一个数据行的行号。
func writeAnalyzerSheet(file *excelize.File, sheet, title, subtitle string, header []string, rows [][]string, widths []float64, styles analyzerXLSXStyleSet) (int, error) {
	lastColumn := xlsxAnalyzerColumnName(len(header))
	if err := file.MergeCell(sheet, "A1", lastColumn+"1"); err != nil {
		return 0, fmt.Errorf("合并 XLSX %s 标题失败：%w", sheet, err)
	}
	if err := file.SetCellValue(sheet, "A1", title); err != nil {
		return 0, fmt.Errorf("写入 XLSX %s 标题失败：%w", sheet, err)
	}
	if err := file.SetCellStyle(sheet, "A1", lastColumn+"1", styles.title); err != nil {
		return 0, fmt.Errorf("设置 XLSX %s 标题样式失败：%w", sheet, err)
	}
	if subtitle != "" {
		if err := file.MergeCell(sheet, "A2", lastColumn+"2"); err != nil {
			return 0, fmt.Errorf("合并 XLSX %s 副标题失败：%w", sheet, err)
		}
		if err := file.SetCellValue(sheet, "A2", subtitle); err != nil {
			return 0, fmt.Errorf("写入 XLSX %s 副标题失败：%w", sheet, err)
		}
		if err := file.SetCellStyle(sheet, "A2", lastColumn+"2", styles.subtitle); err != nil {
			return 0, fmt.Errorf("设置 XLSX %s 副标题样式失败：%w", sheet, err)
		}
	}
	if err := file.SetSheetRow(sheet, "A4", &header); err != nil {
		return 0, fmt.Errorf("写入 XLSX %s 表头失败：%w", sheet, err)
	}
	if err := file.SetCellStyle(sheet, "A4", lastColumn+"4", styles.header); err != nil {
		return 0, fmt.Errorf("设置 XLSX %s 表头样式失败：%w", sheet, err)
	}
	startRow := 5
	for index, row := range rows {
		rowNumber := startRow + index
		cell := fmt.Sprintf("A%d", rowNumber)
		if err := file.SetSheetRow(sheet, cell, &row); err != nil {
			return 0, fmt.Errorf("写入 XLSX %s 数据失败：%w", sheet, err)
		}
		bodyStyle := styles.body
		if index%2 == 1 {
			bodyStyle = styles.bodyAlt
		}
		if err := file.SetCellStyle(sheet, cell, lastColumn+strconv.Itoa(rowNumber), bodyStyle); err != nil {
			return 0, fmt.Errorf("设置 XLSX %s 数据样式失败：%w", sheet, err)
		}
	}
	for index, width := range widths {
		column := xlsxAnalyzerColumnName(index + 1)
		if err := file.SetColWidth(sheet, column, column, width); err != nil {
			return 0, fmt.Errorf("设置 XLSX %s 列宽失败：%w", sheet, err)
		}
	}
	for _, rowNumber := range []int{1, 2, 4} {
		if err := file.SetRowHeight(sheet, rowNumber, 24); err != nil {
			return 0, fmt.Errorf("设置 XLSX %s 行高失败：%w", sheet, err)
		}
	}
	lastRow := startRow + len(rows) - 1
	if lastRow >= startRow {
		if err := file.AutoFilter(sheet, fmt.Sprintf("A4:%s%d", lastColumn, lastRow), nil); err != nil {
			return 0, fmt.Errorf("设置 XLSX %s 自动筛选失败：%w", sheet, err)
		}
	}
	if err := file.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 4, TopLeftCell: "A5"}); err != nil {
		return 0, fmt.Errorf("冻结 XLSX %s 表头失败：%w", sheet, err)
	}
	return lastRow, nil
}

func writeAnalyzerXLSXSummary(file *excelize.File, report Report, styles analyzerXLSXStyleSet) error {
	rows := [][]string{
		{"状态", reportStatusLabel(report.Status)},
		{"输入文件", report.Input.Path},
		{"OFD 版本", report.OFD.Version},
		{"文档类型", report.OFD.DocType},
		{"文档体", strconv.Itoa(report.Summary.DocumentBodies)},
		{"页面", strconv.Itoa(report.Summary.Pages)},
		{"已解析页面", strconv.Itoa(report.Summary.ParsedPages)},
		{"对象", strconv.Itoa(report.Summary.Objects)},
		{"文字字符", strconv.Itoa(report.Summary.TextCharacters)},
		{"图片", strconv.Itoa(report.Summary.Images)},
		{"字体", strconv.Itoa(report.Summary.Fonts)},
		{"绘制参数", strconv.Itoa(report.Summary.DrawParams)},
		{"颜色空间", strconv.Itoa(report.Summary.ColorSpaces)},
		{"模板", strconv.Itoa(report.Summary.Templates)},
		{"复合图元", strconv.Itoa(report.Summary.Composites)},
		{"图案", strconv.Itoa(report.Summary.Patterns)},
		{"附件", strconv.Itoa(report.Summary.Attachments)},
		{"注解", strconv.Itoa(report.Summary.Annotations)},
		{"签名", strconv.Itoa(report.Summary.Signatures)},
	}
	if err := file.MergeCell("汇总", "A1", "B1"); err != nil {
		return fmt.Errorf("合并 XLSX 汇总标题失败：%w", err)
	}
	if err := file.SetCellValue("汇总", "A1", "OFD 分析报告"); err != nil {
		return fmt.Errorf("写入 XLSX 汇总标题失败：%w", err)
	}
	if err := file.SetCellStyle("汇总", "A1", "B1", styles.title); err != nil {
		return fmt.Errorf("设置 XLSX 汇总标题样式失败：%w", err)
	}
	if err := file.MergeCell("汇总", "A2", "B2"); err != nil {
		return fmt.Errorf("合并 XLSX 汇总副标题失败：%w", err)
	}
	if err := file.SetCellValue("汇总", "A2", fmt.Sprintf("工具：%s %s", report.Tool.Name, report.Tool.Version)); err != nil {
		return fmt.Errorf("写入 XLSX 汇总副标题失败：%w", err)
	}
	if err := file.SetCellStyle("汇总", "A2", "B2", styles.subtitle); err != nil {
		return fmt.Errorf("设置 XLSX 汇总副标题样式失败：%w", err)
	}
	for rowIndex, row := range rows {
		rowNumber := rowIndex + 4
		cell := fmt.Sprintf("A%d", rowNumber)
		if err := file.SetSheetRow("汇总", cell, &row); err != nil {
			return fmt.Errorf("写入 XLSX 汇总数据失败：%w", err)
		}
		labelStyle := styles.body
		valueStyle := styles.center
		if rowIndex == 0 {
			labelStyle = styles.header
			valueStyle = styles.header
		} else if rowIndex%2 == 0 {
			valueStyle = styles.centerAlt
		}
		if row[0] == "状态" {
			valueStyle = xlsxAnalyzerStatusStyleID(report.Status, styles)
		}
		if err := file.SetCellStyle("汇总", cell, fmt.Sprintf("A%d", rowNumber), labelStyle); err != nil {
			return fmt.Errorf("设置 XLSX 汇总标签样式失败：%w", err)
		}
		if err := file.SetCellStyle("汇总", fmt.Sprintf("B%d", rowNumber), fmt.Sprintf("B%d", rowNumber), valueStyle); err != nil {
			return fmt.Errorf("设置 XLSX 汇总值样式失败：%w", err)
		}
	}
	if err := file.SetColWidth("汇总", "A", "A", 18); err != nil {
		return fmt.Errorf("设置 XLSX 汇总字段列宽失败：%w", err)
	}
	if err := file.SetColWidth("汇总", "B", "B", 40); err != nil {
		return fmt.Errorf("设置 XLSX 汇总值列宽失败：%w", err)
	}
	packageHeader := []string{"条目", "目录", "文件", "XML 文件", "压缩后", "解压后"}
	packageRows := [][]string{
		{
			strconv.Itoa(report.Package.Entries),
			strconv.Itoa(report.Package.Directories),
			strconv.Itoa(report.Package.Files),
			strconv.Itoa(report.Package.XMLFiles),
			formatPackageSize(report.Package.CompressedBytes),
			formatPackageSize(report.Package.UncompressedBytes),
		},
	}
	packageStart := len(rows) + 6
	packageLastRow, err := writeAnalyzerSheetAt(file, "汇总", "OFD 包", "", packageHeader, packageRows, []float64{12, 12, 12, 12, 18, 18}, styles, packageStart)
	if err != nil {
		return err
	}
	resourceHeader := []string{"类型", "文件", "声明", "使用", "唯一使用", "未解析", "缺失文件", "未使用"}
	resourceRows := [][]string{}
	resourceSummaries := []struct {
		name    string
		summary ResourceSummary
	}{
		{"全部", report.Resources},
		{"图片", report.Images},
		{"字体", report.Fonts.ResourceSummary},
		{"绘制参数", report.DrawParams.ResourceSummary},
		{"颜色空间", report.ColorSpaces},
		{"模板", report.Templates},
		{"复合图元", report.Composites},
		{"图案", report.Patterns},
	}
	for _, resource := range resourceSummaries {
		resourceRows = append(resourceRows, []string{
			resource.name,
			strconv.Itoa(resource.summary.Files),
			strconv.Itoa(resource.summary.Declared),
			strconv.Itoa(resource.summary.Used),
			strconv.Itoa(resource.summary.UniqueUsed),
			strconv.Itoa(resource.summary.Unresolved),
			strconv.Itoa(resource.summary.MissingFiles),
			strconv.Itoa(resource.summary.Unused),
		})
	}
	resourceStart := packageLastRow + 4
	if _, err := writeAnalyzerSheetAt(file, "汇总", "资源统计", "", resourceHeader, resourceRows, []float64{12, 10, 10, 10, 10, 10, 10, 10}, styles, resourceStart); err != nil {
		return err
	}
	if err := file.SetColWidth("汇总", "A", "A", 18); err != nil {
		return fmt.Errorf("设置 XLSX 汇总字段列宽失败：%w", err)
	}
	return nil
}

func writeAnalyzerSheetAt(file *excelize.File, sheet, title, subtitle string, header []string, rows [][]string, widths []float64, styles analyzerXLSXStyleSet, startRow int) (int, error) {
	lastColumn := xlsxAnalyzerColumnName(len(header))
	titleRow := startRow - 2
	if err := file.MergeCell(sheet, fmt.Sprintf("A%d", titleRow), fmt.Sprintf("%s%d", lastColumn, titleRow)); err != nil {
		return 0, fmt.Errorf("合并 XLSX %s 标题失败：%w", sheet, err)
	}
	if err := file.SetCellValue(sheet, fmt.Sprintf("A%d", titleRow), title); err != nil {
		return 0, fmt.Errorf("写入 XLSX %s 标题失败：%w", sheet, err)
	}
	style, err := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Family: "Microsoft YaHei", Size: 11, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"2E75B6"}},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center"},
	})
	if err != nil {
		return 0, fmt.Errorf("创建 XLSX %s 标题样式失败：%w", sheet, err)
	}
	if err := file.SetCellStyle(sheet, fmt.Sprintf("A%d", titleRow), fmt.Sprintf("%s%d", lastColumn, titleRow), style); err != nil {
		return 0, fmt.Errorf("设置 XLSX %s 标题样式失败：%w", sheet, err)
	}
	if err := file.SetSheetRow(sheet, fmt.Sprintf("A%d", startRow-1), &header); err != nil {
		return 0, fmt.Errorf("写入 XLSX %s 表头失败：%w", sheet, err)
	}
	if err := file.SetCellStyle(sheet, fmt.Sprintf("A%d", startRow-1), fmt.Sprintf("%s%d", lastColumn, startRow-1), styles.header); err != nil {
		return 0, fmt.Errorf("设置 XLSX %s 表头样式失败：%w", sheet, err)
	}
	for index, row := range rows {
		rowNumber := startRow + index
		cell := fmt.Sprintf("A%d", rowNumber)
		if err := file.SetSheetRow(sheet, cell, &row); err != nil {
			return 0, fmt.Errorf("写入 XLSX %s 数据失败：%w", sheet, err)
		}
		bodyStyle := styles.body
		if index%2 == 1 {
			bodyStyle = styles.bodyAlt
		}
		if err := file.SetCellStyle(sheet, cell, lastColumn+strconv.Itoa(rowNumber), bodyStyle); err != nil {
			return 0, fmt.Errorf("设置 XLSX %s 数据样式失败：%w", sheet, err)
		}
	}
	for index, width := range widths {
		column := xlsxAnalyzerColumnName(index + 1)
		if err := file.SetColWidth(sheet, column, column, width); err != nil {
			return 0, fmt.Errorf("设置 XLSX %s 列宽失败：%w", sheet, err)
		}
	}
	lastRow := startRow + len(rows) - 1
	if lastRow >= startRow {
		if err := file.AutoFilter(sheet, fmt.Sprintf("A%d:%s%d", startRow-1, lastColumn, lastRow), nil); err != nil {
			return 0, fmt.Errorf("设置 XLSX %s 自动筛选失败：%w", sheet, err)
		}
	}
	return lastRow, nil
}

func writeAnalyzerXLSXDocuments(file *excelize.File, report Report, styles analyzerXLSXStyleSet) error {
	header := []string{"索引", "标识", "标题", "作者", "页面", "已解析", "模板", "资源文件", "根文件", "封面", "附件", "注解", "签名"}
	rows := [][]string{}
	for _, document := range report.Documents {
		rows = append(rows, []string{
			strconv.Itoa(document.Index),
			document.DocID,
			firstNonEmpty(document.Title, ""),
			firstNonEmpty(document.Author, ""),
			strconv.Itoa(document.DeclaredPages),
			strconv.Itoa(document.ParsedPages),
			strconv.Itoa(document.TemplateCount),
			strconv.Itoa(document.ResourceFiles),
			document.DocRoot,
			yesNoLabel(document.HasCover),
			yesNoLabel(document.HasAttachments),
			yesNoLabel(document.HasAnnotations),
			yesNoLabel(document.HasSignatures),
		})
	}
	_, err := writeAnalyzerSheet(file, "文档体", "OFD 文档体", fmt.Sprintf("共 %d 个文档体", len(report.Documents)), header, rows, []float64{8, 20, 24, 20, 8, 8, 8, 10, 28, 8, 8, 8, 8}, styles)
	return err
}

func writeAnalyzerXLSXPages(file *excelize.File, report Report, styles analyzerXLSXStyleSet) error {
	header := []string{"全局页码", "文档体", "文档页码", "ID", "宽度", "高度", "单位", "方向", "对象", "文字字符"}
	rows := [][]string{}
	for _, page := range report.Pages {
		rows = append(rows, []string{
			strconv.Itoa(page.PageNumber),
			strconv.Itoa(page.DocumentIndex),
			strconv.Itoa(page.DocumentPage),
			strconv.FormatUint(page.ID, 10),
			fmt.Sprintf("%.2f", page.Size.Width),
			fmt.Sprintf("%.2f", page.Size.Height),
			page.Size.Unit,
			orientationLabel(page.Size.Orientation),
			strconv.Itoa(page.Objects.Total),
			strconv.Itoa(page.Text.UnicodeCodePoints),
		})
	}
	_, err := writeAnalyzerSheet(file, "页面", "OFD 页面", fmt.Sprintf("共 %d 页", len(report.Pages)), header, rows, []float64{10, 8, 10, 10, 10, 10, 8, 10, 8, 10}, styles)
	return err
}

func writeAnalyzerXLSXResources(file *excelize.File, report Report, styles analyzerXLSXStyleSet) error {
	header := []string{"文档体", "类型", "ID", "名称", "字体名", "族名", "字符集", "样式", "文件", "存在", "使用", "嵌入"}
	rows := [][]string{}
	for _, resource := range report.ResourceDetails {
		rows = append(rows, []string{
			strconv.Itoa(resource.DocumentIndex),
			xlsxAnalyzerResourceKind(resource.Kind),
			strconv.FormatUint(resource.ID, 10),
			nonEmptyLabel(resource.Path),
			nonEmptyLabel(resource.FontName),
			nonEmptyLabel(resource.FamilyName),
			nonEmptyLabel(resource.Charset),
			fontStyleLabel(resource),
			nonEmptyLabel(resource.SourceFile),
			yesNoLabel(resource.Exists),
			strconv.Itoa(resource.Used),
			yesNoLabel(resource.Embedded),
		})
	}
	_, err := writeAnalyzerSheet(file, "资源", "OFD 资源", fmt.Sprintf("共 %d 个资源", len(report.ResourceDetails)), header, rows, []float64{8, 10, 10, 24, 20, 20, 12, 12, 28, 8, 8, 8}, styles)
	return err
}

func writeAnalyzerXLSXAttachments(file *excelize.File, report Report, styles analyzerXLSXStyleSet) error {
	header := []string{"文档体", "ID", "名称", "格式", "路径", "声明大小", "实际大小", "存在", "可见", "用途"}
	rows := [][]string{}
	for _, attachment := range report.Attachments {
		format := ""
		if attachment.Format != nil {
			format = *attachment.Format
		}
		declared := ""
		if attachment.DeclaredSize != nil {
			declared = strconv.FormatFloat(*attachment.DeclaredSize, 'f', 0, 64)
		}
		rows = append(rows, []string{
			strconv.Itoa(attachment.DocumentIndex),
			attachment.ID,
			attachment.Name,
			format,
			attachment.Path,
			declared,
			strconv.FormatUint(attachment.ActualSize, 10),
			yesNoLabel(attachment.Exists),
			yesNoLabel(attachment.Visible),
			attachment.Usage,
		})
	}
	_, err := writeAnalyzerSheet(file, "附件", "OFD 附件", fmt.Sprintf("共 %d 个附件", len(report.Attachments)), header, rows, []float64{8, 14, 22, 10, 28, 12, 12, 8, 8, 16}, styles)
	return err
}

func writeAnalyzerXLSXAnnotations(file *excelize.File, report Report, styles analyzerXLSXStyleSet) error {
	header := []string{"文档体", "页面 ID", "注解 ID", "类型", "子类型", "创建者", "可见", "打印", "外观", "对象"}
	rows := [][]string{}
	for _, annotation := range report.Annotations {
		rows = append(rows, []string{
			strconv.Itoa(annotation.DocumentIndex),
			strconv.FormatUint(annotation.PageID, 10),
			annotation.ID,
			annotation.Type,
			annotation.Subtype,
			annotation.Creator,
			yesNoLabel(annotation.Visible),
			yesNoLabel(annotation.Print),
			yesNoLabel(annotation.HasAppearance),
			strconv.Itoa(annotation.Objects.Total),
		})
	}
	_, err := writeAnalyzerSheet(file, "注解", "OFD 注解", fmt.Sprintf("共 %d 个注解", len(report.Annotations)), header, rows, []float64{8, 10, 14, 12, 12, 18, 8, 8, 8, 8}, styles)
	return err
}

func writeAnalyzerXLSXSignatures(file *excelize.File, report Report, styles analyzerXLSXStyleSet) error {
	header := []string{"文档体", "ID", "路径", "提供方", "签名时间", "引用", "盖章", "摘要", "摘要算法", "DataHash", "密码学验证", "内部签名", "外层签名", "证书信任", "吊销校验", "验证错误"}
	rows := [][]string{}
	for _, signature := range report.Signatures {
		rows = append(rows, []string{
			strconv.Itoa(signature.DocumentIndex),
			signature.ID,
			signature.Path,
			signature.Provider,
			signature.Date,
			strconv.Itoa(signature.ReferenceCount),
			strconv.Itoa(signature.StampCount),
			digestCheckedLabel(signature),
			signature.DigestMethod,
			dataHashMatchLabel(signature.DataHash),
			verificationCheckedLabel(signature),
			componentVerificationLabel(signature.SealVerification),
			componentVerificationLabel(signature.OuterVerification),
			trustVerificationLabel(signature),
			revocationVerificationLabel(signature),
			signature.VerificationError,
		})
	}
	_, err := writeAnalyzerSheet(file, "签名", "OFD 签名", fmt.Sprintf("共 %d 个签名", len(report.Signatures)), header, rows, []float64{8, 14, 28, 20, 18, 8, 8, 10, 18, 10, 12, 12, 12, 12, 12, 36}, styles)
	return err
}

func writeAnalyzerXLSXTree(file *excelize.File, report Report, styles analyzerXLSXStyleSet) error {
	if report.Package.Tree == nil {
		return nil
	}
	lines := packageTreeLines(*report.Package.Tree)
	rows := make([][]string, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, []string{line})
	}
	_, err := writeAnalyzerSheet(file, "目录树", "OFD 包目录结构", "", []string{"路径"}, rows, []float64{60}, styles)
	return err
}

func writeAnalyzerXLSXReferences(file *excelize.File, sheet string, references []ReferenceEdge, styles analyzerXLSXStyleSet) error {
	header := []string{"来源", "目标", "类型", "来源位置", "存在", "次数"}
	rows := [][]string{}
	for _, reference := range references {
		rows = append(rows, []string{
			reference.From,
			reference.To,
			reference.Type,
			reference.Source,
			yesNoLabel(reference.Exists),
			strconv.Itoa(reference.Count),
		})
	}
	_, err := writeAnalyzerSheet(file, sheet, sheet, fmt.Sprintf("共 %d 条", len(references)), header, rows, []float64{30, 30, 16, 24, 8, 8}, styles)
	return err
}

func xlsxAnalyzerResourceKind(value string) string {
	kinds := map[string]string{
		"image":       "图片",
		"font":        "字体",
		"draw-param":  "绘制参数",
		"color-space": "颜色空间",
		"template":    "模板",
		"composite":   "复合图元",
		"pattern":     "图案",
		"media":       "多媒体",
		"attachment":  "附件",
	}
	if translated, ok := kinds[value]; ok {
		return translated
	}
	return value
}

func xlsxAnalyzerColumnName(column int) string {
	var letters []byte
	for column > 0 {
		column--
		letters = append([]byte{byte('A' + column%26)}, letters...)
		column /= 26
	}
	return string(letters)
}
