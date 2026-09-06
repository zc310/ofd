package analyzer

import (
	"fmt"
	"io"
	"strings"
)

// RenderText 将分析报告输出为中文纯文本。
func RenderText(writer io.Writer, report Report) error {
	write := func(format string, values ...any) error {
		_, err := fmt.Fprintf(writer, format, values...)
		return err
	}
	if writer == nil {
		return fmt.Errorf("分析报告输出为空")
	}

	if err := write("OFD 分析报告\n状态：%s\n输入文件：%s\nOFD 版本：%s\n\n", reportStatusLabel(report.Status), report.Input.Path, report.OFD.Version); err != nil {
		return err
	}
	if err := write("汇总：\n  文档体：%d  页面：%d（已解析 %d）  对象：%d\n  文字字符：%d  图片：%d  字体：%d  绘制参数：%d  颜色空间：%d\n  模板：%d  复合图元：%d  图案：%d\n  附件：%d  注解：%d  签名：%d\n\n", report.Summary.DocumentBodies, report.Summary.Pages, report.Summary.ParsedPages, report.Summary.Objects, report.Summary.TextCharacters, report.Summary.Images, report.Summary.Fonts, report.Summary.DrawParams, report.Summary.ColorSpaces, report.Summary.Templates, report.Summary.Composites, report.Summary.Patterns, report.Summary.Attachments, report.Summary.Annotations, report.Summary.Signatures); err != nil {
		return err
	}
	if err := writeResourceText(writer, "", report.Resources); err != nil {
		return err
	}
	if err := writeResourceText(writer, "图片", report.Images); err != nil {
		return err
	}
	if err := writeFontResourceText(writer, report.Fonts); err != nil {
		return err
	}
	if err := writeDrawParamResourceText(writer, report.DrawParams); err != nil {
		return err
	}
	if err := writeResourceText(writer, "颜色空间", report.ColorSpaces); err != nil {
		return err
	}
	if err := writeResourceText(writer, "模板", report.Templates); err != nil {
		return err
	}
	if err := writeResourceText(writer, "复合图元", report.Composites); err != nil {
		return err
	}
	if err := writeResourceText(writer, "图案", report.Patterns); err != nil {
		return err
	}
	if err := writeFontsText(writer, report); err != nil {
		return err
	}
	if err := writeDocumentsText(writer, report); err != nil {
		return err
	}
	if err := writePagesText(writer, report); err != nil {
		return err
	}
	if err := writeAttachmentsText(writer, report); err != nil {
		return err
	}
	if err := writeAnnotationsText(writer, report); err != nil {
		return err
	}
	if err := writeSignaturesText(writer, report); err != nil {
		return err
	}
	if err := writeReferencesText(writer, "文件引用", report.FileReferences); err != nil {
		return err
	}
	if err := writeReferencesText(writer, "ID 引用", report.IDReferences); err != nil {
		return err
	}
	if len(report.Warnings) > 0 {
		if err := write("警告：\n"); err != nil {
			return err
		}
		for _, warning := range report.Warnings {
			if err := write("  - %s\n", warning); err != nil {
				return err
			}
		}
	}
	if len(report.Errors) > 0 {
		if err := write("错误：\n"); err != nil {
			return err
		}
		for _, reportError := range report.Errors {
			if err := write("  - %s\n", reportError); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeResourceText(writer io.Writer, name string, summary ResourceSummary) error {
	if name == "" {
		name = "资源"
	}
	if name == "模板" || name == "复合图元" || name == "图案" {
		_, err := fmt.Fprintf(writer, "%s：定义数 %d，引用次数 %d，唯一引用 %d，无法解析引用 %d\n", name, summary.Declared, summary.References, summary.UniqueUsed, summary.Unresolved)
		return err
	}
	_, err := fmt.Fprintf(writer, "%s：文件 %d，声明 %d，使用 %d，唯一使用 %d，未解析 %d，缺失文件 %d，未使用 %d\n", name, summary.Files, summary.Declared, summary.Used, summary.UniqueUsed, summary.Unresolved, summary.MissingFiles, summary.Unused)
	return err
}

func writeFontResourceText(writer io.Writer, summary FontResourceSummary) error {
	_, err := fmt.Fprintf(writer, "字体：文件 %d，声明 %d，被引用 %d，嵌入 %d，未解析 %d，缺失文件 %d，未使用 %d\n", summary.Files, summary.Declared, summary.UniqueUsed, summary.Embedded, summary.Unresolved, summary.MissingFiles, summary.Unused)
	return err
}

func writeDrawParamResourceText(writer io.Writer, summary DrawParamResourceSummary) error {
	_, err := fmt.Fprintf(writer, "绘制参数：文件 %d，定义数 %d，引用次数 %d，唯一引用 %d，无法解析引用 %d，继承循环 %d，缺失文件 %d，未使用 %d\n", summary.Files, summary.Declared, summary.References, summary.UniqueUsed, summary.Unresolved, summary.InheritanceCycles, summary.MissingFiles, summary.Unused)
	return err
}

func writeDefinitionResourceText(writer io.Writer, name string, summary ResourceSummary) error {
	_, err := fmt.Fprintf(writer, "%s：定义数 %d，引用次数 %d，唯一引用 %d，无法解析引用 %d\n", name, summary.Declared, summary.References, summary.UniqueUsed, summary.Unresolved)
	return err
}

func writeFontsText(writer io.Writer, report Report) error {
	if _, err := io.WriteString(writer, "字体明细：\n"); err != nil {
		return err
	}
	for _, resource := range report.ResourceDetails {
		if resource.Kind != "font" {
			continue
		}
		if _, err := fmt.Fprintf(writer, "  文档体 %d，ID %d，名称 %s，族名 %s，字符集 %s，文件 %s，嵌入 %s，样式 %s，使用 %d\n", resource.DocumentIndex, resource.ID, nonEmptyLabel(resource.FontName), nonEmptyLabel(resource.FamilyName), nonEmptyLabel(resource.Charset), nonEmptyLabel(resource.Path), yesNoLabel(resource.Embedded), fontStyleLabel(resource), resource.Used); err != nil {
			return err
		}
	}
	return nil
}

func writeDocumentsText(writer io.Writer, report Report) error {
	if _, err := io.WriteString(writer, "\n文档体：\n"); err != nil {
		return err
	}
	for _, document := range report.Documents {
		if _, err := fmt.Fprintf(writer, "  [%d] %s，页面 %d/%d，资源文件 %d，根文件 %s\n", document.Index, firstNonEmpty(document.Title, document.DocID), document.ParsedPages, document.DeclaredPages, document.ResourceFiles, document.DocRoot); err != nil {
			return err
		}
	}
	return nil
}

func writePagesText(writer io.Writer, report Report) error {
	if _, err := io.WriteString(writer, "页面：\n"); err != nil {
		return err
	}
	for _, page := range report.Pages {
		if _, err := fmt.Fprintf(writer, "  [%d] 文档体 %d 第 %d 页，ID %d，尺寸 %.2fx%.2f %s，对象 %d，文字 %d\n", page.PageNumber, page.DocumentIndex, page.DocumentPage, page.ID, page.Size.Width, page.Size.Height, orientationLabel(page.Size.Orientation), page.Objects.Total, page.Text.UnicodeCodePoints); err != nil {
			return err
		}
	}
	return nil
}

func writeAttachmentsText(writer io.Writer, report Report) error {
	if _, err := io.WriteString(writer, "附件：\n"); err != nil {
		return err
	}
	for _, attachment := range report.Attachments {
		if _, err := fmt.Fprintf(writer, "  [%d] %s (%s)，路径 %s，存在 %s，大小 %d\n", attachment.DocumentIndex, attachment.Name, attachment.ID, attachment.Path, yesNoLabel(attachment.Exists), attachment.ActualSize); err != nil {
			return err
		}
	}
	return nil
}

func writeAnnotationsText(writer io.Writer, report Report) error {
	if _, err := io.WriteString(writer, "注解：\n"); err != nil {
		return err
	}
	for _, annotation := range report.Annotations {
		if _, err := fmt.Fprintf(writer, "  [%d] 文档体 %d 页面 %d，ID %s，类型 %s，外观对象 %d\n", annotation.PageID, annotation.DocumentIndex, annotation.PageID, annotation.ID, annotation.Type, annotation.Objects.Total); err != nil {
			return err
		}
	}
	return nil
}

func writeSignaturesText(writer io.Writer, report Report) error {
	if _, err := io.WriteString(writer, "签名：\n"); err != nil {
		return err
	}
	for _, signature := range report.Signatures {
		if _, err := fmt.Fprintf(writer, "  [%d] 文档体 %d，ID %s，路径 %s，引用 %d，盖章 %d\n", signature.DocumentIndex, signature.DocumentIndex, signature.ID, signature.Path, signature.ReferenceCount, signature.StampCount); err != nil {
			return err
		}
	}
	return nil
}

func writeReferencesText(writer io.Writer, name string, references []ReferenceEdge) error {
	if _, err := fmt.Fprintf(writer, "%s：\n", name); err != nil {
		return err
	}
	for _, reference := range references {
		if _, err := fmt.Fprintf(writer, "  %s -> %s (%s，存在 %s，次数 %d)\n", reference.From, reference.To, reference.Type, yesNoLabel(reference.Exists), reference.Count); err != nil {
			return err
		}
	}
	return nil
}

// RenderMarkdown 将分析报告输出为中文 Markdown。
func RenderMarkdown(writer io.Writer, report Report) error {
	if writer == nil {
		return fmt.Errorf("分析报告输出为空")
	}
	write := func(format string, values ...any) error {
		_, err := fmt.Fprintf(writer, format, values...)
		return err
	}
	if err := write("# OFD 分析报告\n\n- 状态：**%s**\n- 输入文件：`%s`\n- OFD 版本：`%s`\n\n## 汇总\n\n| 指标 | 数值 |\n| --- | ---: |\n| 文档体 | %d |\n| 页面 | %d（已解析 %d） |\n| 对象 | %d |\n| 文字字符 | %d |\n| 图片 | %d |\n| 字体 | %d |\n| 绘制参数 | %d |\n| 颜色空间 | %d |\n| 附件 | %d |\n| 注解 | %d |\n| 签名 | %d |\n\n", reportStatusLabel(report.Status), escapeMarkdown(report.Input.Path), escapeMarkdown(report.OFD.Version), report.Summary.DocumentBodies, report.Summary.Pages, report.Summary.ParsedPages, report.Summary.Objects, report.Summary.TextCharacters, report.Summary.Images, report.Summary.Fonts, report.Summary.DrawParams, report.Summary.ColorSpaces, report.Summary.Attachments, report.Summary.Annotations, report.Summary.Signatures); err != nil {
		return err
	}
	if err := writeMarkdownResources(writer, report); err != nil {
		return err
	}
	if err := writeMarkdownDrawParamSummary(writer, report.DrawParams); err != nil {
		return err
	}
	if err := writeMarkdownFontSummary(writer, report.Fonts); err != nil {
		return err
	}
	if err := writeMarkdownDefinitionSummary(writer, report); err != nil {
		return err
	}
	if err := writeMarkdownFonts(writer, report); err != nil {
		return err
	}
	if err := writeMarkdownDocuments(writer, report); err != nil {
		return err
	}
	if err := writeMarkdownPages(writer, report); err != nil {
		return err
	}
	if err := writeMarkdownAttachments(writer, report); err != nil {
		return err
	}
	if err := writeMarkdownAnnotations(writer, report); err != nil {
		return err
	}
	if err := writeMarkdownSignatures(writer, report); err != nil {
		return err
	}
	if err := writeMarkdownReferences(writer, "文件引用", report.FileReferences); err != nil {
		return err
	}
	if err := writeMarkdownReferences(writer, "ID 引用", report.IDReferences); err != nil {
		return err
	}
	if len(report.Warnings) > 0 {
		if err := write("## 警告\n\n"); err != nil {
			return err
		}
		for _, warning := range report.Warnings {
			if err := write("- %s\n", escapeMarkdown(warning)); err != nil {
				return err
			}
		}
	}
	if len(report.Errors) > 0 {
		if err := write("\n## 错误\n\n"); err != nil {
			return err
		}
		for _, reportError := range report.Errors {
			if err := write("- %s\n", escapeMarkdown(reportError)); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeMarkdownResources(writer io.Writer, report Report) error {
	if _, err := io.WriteString(writer, "## 资源\n\n| 类型 | 文件 | 声明 | 使用 | 唯一使用 | 未解析 | 缺失文件 | 未使用 |\n| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n"); err != nil {
		return err
	}
	rows := []struct {
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
	for _, row := range rows {
		if _, err := fmt.Fprintf(writer, "| %s | %d | %d | %d | %d | %d | %d | %d |\n", row.name, row.summary.Files, row.summary.Declared, row.summary.Used, row.summary.UniqueUsed, row.summary.Unresolved, row.summary.MissingFiles, row.summary.Unused); err != nil {
			return err
		}
	}
	_, err := io.WriteString(writer, "\n")
	return err
}

func writeMarkdownDrawParamSummary(writer io.Writer, summary DrawParamResourceSummary) error {
	if _, err := io.WriteString(writer, "## 绘制参数统计\n\n| 定义数 | 引用次数 | 无法解析引用 | 继承循环 |\n| ---: | ---: | ---: | ---: |\n"); err != nil {
		return err
	}
	_, err := fmt.Fprintf(writer, "| %d | %d | %d | %d |\n\n", summary.Declared, summary.References, summary.Unresolved, summary.InheritanceCycles)
	return err
}

func writeMarkdownDefinitionSummary(writer io.Writer, report Report) error {
	if _, err := io.WriteString(writer, "## 定义资源统计\n\n| 类型 | 定义数 | 引用次数 | 唯一引用 | 无法解析引用 |\n| --- | ---: | ---: | ---: | ---: |\n"); err != nil {
		return err
	}
	rows := []struct {
		name    string
		summary ResourceSummary
	}{
		{"模板", report.Templates},
		{"复合图元", report.Composites},
		{"图案", report.Patterns},
	}
	for _, row := range rows {
		if _, err := fmt.Fprintf(writer, "| %s | %d | %d | %d | %d |\n", row.name, row.summary.Declared, row.summary.References, row.summary.UniqueUsed, row.summary.Unresolved); err != nil {
			return err
		}
	}
	_, err := io.WriteString(writer, "\n")
	return err
}

func writeMarkdownFontSummary(writer io.Writer, summary FontResourceSummary) error {
	if _, err := io.WriteString(writer, "## 字体统计\n\n| 声明字体 | 被引用字体 | 嵌入字体 |\n| ---: | ---: | ---: |\n"); err != nil {
		return err
	}
	_, err := fmt.Fprintf(writer, "| %d | %d | %d |\n\n", summary.Declared, summary.UniqueUsed, summary.Embedded)
	return err
}

func writeMarkdownFonts(writer io.Writer, report Report) error {
	if _, err := io.WriteString(writer, "## 字体明细\n\n| 文档体 | ID | 字体名称 | 字族名称 | 字符集 | 字体文件 | 嵌入 | 样式 | 使用次数 |\n| ---: | ---: | --- | --- | --- | --- | --- | --- | ---: |\n"); err != nil {
		return err
	}
	for _, resource := range report.ResourceDetails {
		if resource.Kind != "font" {
			continue
		}
		if _, err := fmt.Fprintf(writer, "| %d | %d | %s | %s | %s | `%s` | %s | %s | %d |\n", resource.DocumentIndex, resource.ID, escapeMarkdown(nonEmptyLabel(resource.FontName)), escapeMarkdown(nonEmptyLabel(resource.FamilyName)), escapeMarkdown(nonEmptyLabel(resource.Charset)), escapeMarkdown(nonEmptyLabel(resource.Path)), yesNoLabel(resource.Embedded), escapeMarkdown(fontStyleLabel(resource)), resource.Used); err != nil {
			return err
		}
	}
	_, err := io.WriteString(writer, "\n")
	return err
}

func writeMarkdownDocuments(writer io.Writer, report Report) error {
	if _, err := io.WriteString(writer, "## 文档体\n\n| 索引 | 标识 | 页面 | 资源文件 | 根文件 |\n| ---: | --- | ---: | ---: | --- |\n"); err != nil {
		return err
	}
	for _, document := range report.Documents {
		if _, err := fmt.Fprintf(writer, "| %d | `%s` | %d/%d | %d | `%s` |\n", document.Index, escapeMarkdown(firstNonEmpty(document.Title, document.DocID)), document.ParsedPages, document.DeclaredPages, document.ResourceFiles, escapeMarkdown(document.DocRoot)); err != nil {
			return err
		}
	}
	_, err := io.WriteString(writer, "\n")
	return err
}

func writeMarkdownPages(writer io.Writer, report Report) error {
	if _, err := io.WriteString(writer, "## 页面\n\n| 全局页码 | 文档体 | 文档页码 | ID | 宽度 | 高度 | 单位 | 方向 | 对象 | 文字字符 |\n| ---: | ---: | ---: | ---: | ---: | ---: | --- | --- | ---: | ---: |\n"); err != nil {
		return err
	}
	for _, page := range report.Pages {
		if _, err := fmt.Fprintf(writer, "| %d | %d | %d | %d | %.2f | %.2f | %s | %s | %d | %d |\n", page.PageNumber, page.DocumentIndex, page.DocumentPage, page.ID, page.Size.Width, page.Size.Height, page.Size.Unit, orientationLabel(page.Size.Orientation), page.Objects.Total, page.Text.UnicodeCodePoints); err != nil {
			return err
		}
	}
	_, err := io.WriteString(writer, "\n")
	return err
}

func writeMarkdownAttachments(writer io.Writer, report Report) error {
	if _, err := io.WriteString(writer, "## 附件\n\n| 文档体 | ID | 名称 | 路径 | 存在 | 实际大小 |\n| ---: | --- | --- | --- | --- | ---: |\n"); err != nil {
		return err
	}
	for _, attachment := range report.Attachments {
		if _, err := fmt.Fprintf(writer, "| %d | `%s` | %s | `%s` | %s | %d |\n", attachment.DocumentIndex, escapeMarkdown(attachment.ID), escapeMarkdown(attachment.Name), escapeMarkdown(attachment.Path), yesNoLabel(attachment.Exists), attachment.ActualSize); err != nil {
			return err
		}
	}
	_, err := io.WriteString(writer, "\n")
	return err
}

func writeMarkdownAnnotations(writer io.Writer, report Report) error {
	if _, err := io.WriteString(writer, "## 注解\n\n| 文档体 | 页面 ID | 注解 ID | 类型 | 外观 | 对象 |\n| ---: | ---: | --- | --- | --- | ---: |\n"); err != nil {
		return err
	}
	for _, annotation := range report.Annotations {
		if _, err := fmt.Fprintf(writer, "| %d | %d | `%s` | %s | %s | %d |\n", annotation.DocumentIndex, annotation.PageID, escapeMarkdown(annotation.ID), escapeMarkdown(annotation.Type), yesNoLabel(annotation.HasAppearance), annotation.Objects.Total); err != nil {
			return err
		}
	}
	_, err := io.WriteString(writer, "\n")
	return err
}

func writeMarkdownSignatures(writer io.Writer, report Report) error {
	if _, err := io.WriteString(writer, "## 签名\n\n| 文档体 | ID | 路径 | 引用 | 盖章 |\n| ---: | --- | --- | ---: | ---: |\n"); err != nil {
		return err
	}
	for _, signature := range report.Signatures {
		if _, err := fmt.Fprintf(writer, "| %d | `%s` | `%s` | %d | %d |\n", signature.DocumentIndex, escapeMarkdown(signature.ID), escapeMarkdown(signature.Path), signature.ReferenceCount, signature.StampCount); err != nil {
			return err
		}
	}
	_, err := io.WriteString(writer, "\n")
	return err
}

func writeMarkdownReferences(writer io.Writer, name string, references []ReferenceEdge) error {
	if _, err := fmt.Fprintf(writer, "## %s\n\n| 来源 | 目标 | 类型 | 存在 | 次数 |\n| --- | --- | --- | --- | ---: |\n", name); err != nil {
		return err
	}
	for _, reference := range references {
		if _, err := fmt.Fprintf(writer, "| `%s` | `%s` | `%s` | %s | %d |\n", escapeMarkdown(reference.From), escapeMarkdown(reference.To), escapeMarkdown(reference.Type), yesNoLabel(reference.Exists), reference.Count); err != nil {
			return err
		}
	}
	_, err := io.WriteString(writer, "\n")
	return err
}

func yesNoLabel(value bool) string {
	if value {
		return "是"
	}
	return "否"
}

func nonEmptyLabel(value string) string {
	if strings.TrimSpace(value) == "" {
		return "未指定"
	}
	return value
}

func fontStyleLabel(resource ResourceInfo) string {
	styles := make([]string, 0, 4)
	if resource.Bold {
		styles = append(styles, "粗体")
	}
	if resource.Italic {
		styles = append(styles, "斜体")
	}
	if resource.Serif {
		styles = append(styles, "衬线")
	}
	if resource.FixedWidth {
		styles = append(styles, "等宽")
	}
	if len(styles) == 0 {
		return "常规"
	}
	return strings.Join(styles, "、")
}

func orientationLabel(value string) string {
	switch value {
	case "portrait":
		return "纵向"
	case "landscape":
		return "横向"
	case "square":
		return "方形"
	default:
		return value
	}
}

func reportStatusLabel(status Status) string {
	switch status {
	case StatusComplete:
		return "完成"
	case StatusPartial:
		return "部分完成"
	case StatusFailed:
		return "失败"
	default:
		return string(status)
	}
}

func firstNonEmpty(value *string, fallback string) string {
	if value != nil && strings.TrimSpace(*value) != "" {
		return *value
	}
	return fallback
}

func escapeMarkdown(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "`", "\\`")
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}
