package analyzer

import (
	"fmt"
	"io"
	"strings"
)

// 各报告共享结论与明细的数据源，避免格式之间漏报检查范围。
type analysisSection struct {
	title  string
	header []string
	rows   [][]string
}

func diagnosticSections(report Report) []analysisSection {
	conclusions := analysisSection{title: "结论摘要", header: []string{"结论"}}
	for _, line := range report.Conclusions {
		conclusions.rows = append(conclusions.rows, []string{line})
	}
	stages := analysisSection{title: "检查阶段", header: []string{"阶段", "执行状态", "说明"}}
	for _, stage := range report.Stages {
		stages.rows = append(stages.rows, []string{stage.Name, stageStatusLabel(stage.Status), stage.Reason})
	}
	diagnostics := analysisSection{title: "结构化诊断", header: []string{"代码 / 级别", "阶段", "来源", "说明 / 依据"}}
	for _, d := range report.Diagnostics {
		stage := d.Stage
		location := d.Location
		diagnostics.rows = append(diagnostics.rows, []string{d.Code + " / " + d.Severity, stage,
			diagnosticSource(d.DocumentIndex, d.PageNumber, d.ObjectID, d.File, location), d.Message + "\n" + d.Basis})
	}
	actions := analysisSection{title: "URI 动作", header: []string{"来源", "事件", "URI / Base", "解析目标 / 分类"}}
	for _, action := range report.Actions {
		kind := "片段引用"
		if action.External {
			kind = "外部目标"
		}
		if action.ResolvedURI == "" {
			kind = "无法解析"
		}
		actions.rows = append(actions.rows, []string{diagnosticSource(&action.DocumentIndex, action.PageNumber, action.ObjectID, action.File, action.Location), action.Event,
			action.URI + "\nBase: " + action.Base, action.ResolvedURI + "\n" + kind})
	}
	resources := analysisSection{title: "资源可达性", header: []string{"资源 / 状态", "来源 / 检查范围"}}
	if report.ResourceAudit.Status != "" {
		resources.rows = append(resources.rows, []string{stageStatusLabel(report.ResourceAudit.Status), report.ResourceAudit.Scope})
		for _, node := range report.ResourceAudit.Nodes {
			if node.Declared && !node.Reachable {
				resources.rows = append(resources.rows, []string{node.Key + "：未观测到可达引用", node.SourceFile})
			}
			if !node.Declared && node.Reachable {
				resources.rows = append(resources.rows, []string{node.Key + "：可达引用缺少定义", node.SourceFile})
			}
		}
	}
	return []analysisSection{conclusions, stages, diagnostics, actions, resources}
}

func diagnosticSource(document, page *int, object, file, location string) string {
	var parts []string
	if document != nil {
		parts = append(parts, fmt.Sprintf("文档体 %d", *document))
	}
	if page != nil {
		parts = append(parts, fmt.Sprintf("页码 %d", *page))
	}
	if object != "" {
		parts = append(parts, "ID "+object)
	}
	if file != "" {
		parts = append(parts, file)
	}
	if location != "" {
		parts = append(parts, location)
	}
	return strings.Join(parts, " · ")
}

func stageStatusLabel(status string) string {
	if status == "skipped" {
		return "未执行"
	}
	return reportStatusLabel(Status(status))
}

func writeDiagnosticSections(writer io.Writer, report Report, markdown bool) error {
	for _, section := range diagnosticSections(report) {
		if len(section.rows) == 0 {
			continue
		}
		var text strings.Builder
		if markdown {
			fmt.Fprintf(&text, "## %s\n\n| %s |\n|", section.title, strings.Join(section.header, " | "))
			for range section.header {
				text.WriteString(" --- |")
			}
			text.WriteByte('\n')
			for _, row := range section.rows {
				text.WriteString("|")
				for _, cell := range row {
					fmt.Fprintf(&text, " %s |", escapeMarkdown(cell))
				}
				text.WriteByte('\n')
			}
		} else {
			fmt.Fprintf(&text, "%s：\n", section.title)
			for _, row := range section.rows {
				fmt.Fprintf(&text, "  - %s\n", strings.ReplaceAll(strings.Join(row, " | "), "\n", "；"))
			}
		}
		text.WriteByte('\n')
		if _, err := io.WriteString(writer, text.String()); err != nil {
			return err
		}
	}
	return nil
}

func attachmentSizeLabel(a AttachmentInfo) string {
	declared := "未声明"
	if a.SizeCheck == "invalid" {
		declared = "无效（详见诊断）"
	}
	if a.DeclaredSizeKB != nil {
		declared = fmt.Sprintf("%g KB", *a.DeclaredSizeKB)
	}
	return fmt.Sprintf("声明 %s，实际 %d 字节，检查 %s", declared, a.ActualSize, a.SizeCheck)
}
