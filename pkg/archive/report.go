package archive

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func RenderJSON(writer io.Writer, report any, pretty bool) error {
	if writer == nil {
		return fmt.Errorf("归档报告输出为空")
	}
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(report)
}

func RenderText(writer io.Writer, report Report) error {
	if writer == nil {
		return fmt.Errorf("归档报告输出为空")
	}
	if _, err := fmt.Fprintf(writer, "OFD 归档预检报告\n状态：%s\n输入文件：%s\nSHA-256：%s\n错误：%d  警告：%d\n\n", report.Status, report.Input.Path, report.Input.SHA256, countIssues(report, "error"), countIssues(report, "warning")); err != nil {
		return err
	}
	if len(report.Issues) == 0 {
		_, err := io.WriteString(writer, "未发现问题。\n")
		return err
	}
	if _, err := io.WriteString(writer, "问题：\n"); err != nil {
		return err
	}
	for index, issue := range report.Issues {
		if _, err := fmt.Fprintf(writer, "%d. [%s] %s：%s\n", index+1, issue.Severity, issue.Code, issue.Message); err != nil {
			return err
		}
	}
	return nil
}

// RenderMarkdown 将归档预检报告输出为 Markdown。
func RenderMarkdown(writer io.Writer, report Report) error {
	if writer == nil {
		return fmt.Errorf("归档报告输出为空")
	}
	if _, err := fmt.Fprintf(writer, "# OFD 归档预检报告\n\n- 状态：**%s**\n- 输入文件：`%s`\n- 输入大小：%d 字节\n- SHA-256：`%s`\n- 错误：%d\n- 警告：%d\n- 耗时：%d ms\n\n", report.Status, escapeMarkdown(report.Input.Path), report.Input.Size, report.Input.SHA256, countIssues(report, "error"), countIssues(report, "warning"), report.DurationMS); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "## 分析汇总\n\n- 分析状态：**%s**\n- 文档体：%d\n- 页面：%d（已解析 %d）\n- 对象：%d\n- 文字字符：%d\n- 图片：%d\n- 字体：%d\n- 附件：%d\n- 注解：%d\n- 签名：%d\n\n", report.AnalysisReport.Status, report.AnalysisReport.Summary.DocumentBodies, report.AnalysisReport.Summary.Pages, report.AnalysisReport.Summary.ParsedPages, report.AnalysisReport.Summary.Objects, report.AnalysisReport.Summary.TextCharacters, report.AnalysisReport.Summary.Images, report.AnalysisReport.Summary.Fonts, report.AnalysisReport.Summary.Attachments, report.AnalysisReport.Summary.Annotations, report.AnalysisReport.Summary.Signatures); err != nil {
		return err
	}
	if len(report.AnalysisReport.Warnings) > 0 || len(report.AnalysisReport.Errors) > 0 {
		if _, err := io.WriteString(writer, "## 分析信息\n\n"); err != nil {
			return err
		}
		for _, warning := range report.AnalysisReport.Warnings {
			if _, err := fmt.Fprintf(writer, "- 警告：%s\n", escapeMarkdown(warning)); err != nil {
				return err
			}
		}
		for _, analysisError := range report.AnalysisReport.Errors {
			if _, err := fmt.Fprintf(writer, "- 错误：%s\n", escapeMarkdown(analysisError)); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(writer, "\n"); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(writer, "## 特征统计\n\n| 特征 | 数量 |\n| --- | ---: |\n"); err != nil {
		return err
	}
	features := []struct {
		name  string
		count int
	}{
		{"动作", report.Features.Actions},
		{"音频", report.Features.Audio},
		{"视频", report.Features.Video},
		{"媒体", report.Features.Media},
		{"加密", report.Features.Encryption},
		{"扩展", report.Features.Extensions},
	}
	for _, feature := range features {
		if _, err := fmt.Fprintf(writer, "| %s | %d |\n", feature.name, feature.count); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(writer, "\n## 校验阶段\n\n| 阶段 | 状态 |\n| --- | --- |\n"); err != nil {
		return err
	}
	for _, check := range report.ValidatorReport.Checks {
		name := check.NameZh
		if name == "" {
			name = check.Name
		}
		status := check.StatusZh
		if status == "" {
			status = check.Status
		}
		if _, err := fmt.Fprintf(writer, "| %s | **%s** |\n", escapeMarkdown(name), escapeMarkdown(status)); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(writer, "\n## 附件\n\n| 文档体 | 名称 | 路径 | 存在 | 大小 |\n| ---: | --- | --- | --- | ---: |\n"); err != nil {
		return err
	}
	if len(report.Attachments) == 0 {
		if _, err := io.WriteString(writer, "| - | - | - | 否 | 0 |\n"); err != nil {
			return err
		}
	} else {
		for _, attachment := range report.Attachments {
			exists := "否"
			if attachment.Exists {
				exists = "是"
			}
			if _, err := fmt.Fprintf(writer, "| %d | %s | `%s` | %s | %d |\n", attachment.DocumentIndex, escapeMarkdown(attachment.Name), escapeMarkdown(attachment.SourcePath), exists, attachment.Size); err != nil {
				return err
			}
		}
	}
	if _, err := io.WriteString(writer, "\n## 问题\n\n"); err != nil {
		return err
	}
	if len(report.Issues) == 0 {
		_, err := io.WriteString(writer, "未发现问题。\n")
		return err
	}
	for index, issue := range report.Issues {
		if _, err := fmt.Fprintf(writer, "%d. **[%s] %s**：%s", index+1, escapeMarkdown(issue.Severity), escapeMarkdown(issue.Code), escapeMarkdown(issue.Message)); err != nil {
			return err
		}
		if issue.Path != "" {
			if _, err := fmt.Fprintf(writer, "（路径：`%s`）", escapeMarkdown(issue.Path)); err != nil {
				return err
			}
		}
		if issue.Hint != "" {
			if _, err := fmt.Fprintf(writer, "（建议：%s）", escapeMarkdown(issue.Hint)); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(writer, "\n"); err != nil {
			return err
		}
	}
	return nil
}

func escapeMarkdown(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	return value
}

func countIssues(report Report, severity string) int {
	count := 0
	for _, issue := range report.Issues {
		if issue.Severity == severity {
			count++
		}
	}
	return count
}
