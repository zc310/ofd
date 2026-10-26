// Package report 将校验结果排版为 OFD 报告，并由同一 OFD 生成可搜索的 PDF。
package report

import (
	"context"
	"fmt"
	"io"
	"strings"

	reporting "github.com/zc310/ofd/internal/report"
	"github.com/zc310/ofd/internal/version"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"github.com/zc310/ofd/pkg/validator"
)

// Options 配置 OFD 和 PDF 共用的报告字体。
type Options struct {
	// Font 是中文 TTF、OTF 或 TTC 文件；为空时查找中文系统字体。
	// TTC 自动选择含中文的字体面，最终嵌入独立字体。
	Font string
}

// RenderOFD 以 flow 排版并写出带嵌入字体、页码和大纲的标准 OFD 报告。
func RenderOFD(ctx context.Context, w io.Writer, report validator.Report, options Options) error {
	if w == nil {
		return fmt.Errorf("OFD 输出写入器为空")
	}
	doc, err := Build(ctx, report, options)
	if err != nil {
		return err
	}
	return reporting.WriteOFD(ctx, w, doc)
}

// RenderPDF 先生成 OFD，再经 converter.PDF 写出文字和矢量 PDF；不创建中间文件。
func RenderPDF(ctx context.Context, w io.Writer, report validator.Report, options Options) error {
	if w == nil {
		return fmt.Errorf("PDF 输出写入器为空")
	}
	doc, err := Build(ctx, report, options)
	if err != nil {
		return err
	}
	return reporting.WritePDF(ctx, w, doc)
}

// Build 构建已分页的报告模型，供 OFD 输出、预览和其他标准格式转换复用。
func Build(ctx context.Context, report validator.Report, options Options) (*creator.Document, error) {
	b, err := reporting.New(ctx, creator.Document{ID: "validation-report", Title: "OFD 校验报告", Subject: "OFD 文件包校验",
		Creator: "ofd-validator", CreatorVersion: version.Version, CreationDate: report.StartedAt}, options.Font)
	if err != nil {
		return nil, err
	}
	b.Heading(1, "OFD 校验报告", nil)
	info := "输入文件：" + report.Input.Path + "\n检测时间：" + reportTime(report)
	if report.Profile != "" {
		info += "\n校验规范：" + report.Profile
	}
	b.Paragraph(info, nil)
	statusStyle := b.Document.Options.Theme.Headings[1]
	statusStyle.Color = statusColor(report.Status)
	b.Block("paragraph", 0, "校验状态："+report.Status.Label(), statusStyle)
	b.Table([]string{"错误", "警告", "提示", "文件", "耗时"}, [][]flow.TableCell{{
		{Text: fmt.Sprint(report.Summary.Errors)}, {Text: fmt.Sprint(report.Summary.Warnings)}, {Text: fmt.Sprint(report.Summary.Infos)},
		{Text: fmt.Sprint(report.Summary.Files)}, {Text: fmt.Sprintf("%d ms", report.DurationMS)},
	}}, []float64{34.8, 34.8, 34.8, 34.8, 34.8})
	b.Heading(2, "检查结果", nil)
	var checks [][]flow.TableCell
	for _, check := range report.Checks {
		checks = append(checks, []flow.TableCell{
			{Text: check.Label()},
			{Text: check.StatusLabel(), Style: &flow.TextStyle{Color: checkStatusColor(check.Status)}},
		})
	}
	b.Table([]string{"检查项", "状态"}, checks, []float64{120, 54})
	b.Heading(2, fmt.Sprintf("问题明细（%d 条）", len(report.Issues)), nil)
	if len(report.Issues) == 0 {
		b.Paragraph("未发现问题。", nil)
	}
	var issues [][]flow.TableCell
	for i, issue := range report.Issues {
		location := issue.File
		if issue.Line > 0 {
			location = fmt.Sprintf("%s:%d:%d", location, issue.Line, issue.Column)
		}
		if issue.Path != "" {
			if location != "" {
				location += "\n"
			}
			location += "XML 路径：" + issue.Path
		}
		var details []string
		for _, field := range []struct{ label, value string }{
			{"", issue.Message}, {"代码", issue.Code}, {"引擎代码", issue.EngineCode},
			{"依据", strings.Join(issue.Clause, "、")}, {"提示", issue.Hint},
		} {
			if field.value != "" {
				value := field.value
				if field.label != "" {
					value = field.label + "：" + value
				}
				details = append(details, value)
			}
		}
		issues = append(issues, []flow.TableCell{
			{Text: fmt.Sprint(i + 1)},
			{Text: issue.Severity.Label() + "\n" + issue.Stage.Label(), Style: &flow.TextStyle{Color: severityColor(issue.Severity)}},
			{Text: location}, {Text: strings.Join(details, "\n")},
		})
	}
	if len(issues) > 0 {
		b.Table([]string{"序号", "级别 / 阶段", "文件 / XML 路径", "问题说明"}, issues,
			[]float64{12, 25, 57, 80}, reporting.TableOptions{SplitRows: true, ColumnAlign: []string{"center", "center"}})
	}
	return b.Build()
}

func reportTime(report validator.Report) string {
	if report.StartedAt.IsZero() {
		return "未知"
	}
	return report.StartedAt.Format("2006-01-02 15:04:05")
}

func statusColor(status validator.Status) *creator.Color {
	switch status {
	case validator.StatusValid:
		return &reporting.Green
	case validator.StatusPartial:
		return severityColor(validator.SeverityWarning)
	default:
		return severityColor(validator.SeverityError)
	}
}

func checkStatusColor(status string) *creator.Color {
	switch status {
	case "failed":
		return severityColor(validator.SeverityError)
	case "warning", "incomplete":
		return severityColor(validator.SeverityWarning)
	case "passed":
		return statusColor(validator.StatusValid)
	default:
		return &reporting.Muted
	}
}

func severityColor(severity validator.Severity) *creator.Color {
	switch severity {
	case validator.SeverityError:
		return &reporting.Red
	case validator.SeverityWarning:
		return &reporting.Orange
	default:
		return &reporting.Blue
	}
}
