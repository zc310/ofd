package validator

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// Status 表示整个 OFD 包的校验结论。
type Status string

const (
	StatusValid   Status = "valid"
	StatusInvalid Status = "invalid"
	StatusPartial Status = "partial"
	StatusError   Status = "error"
)

// Severity 表示单条问题的严重程度。
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Stage 表示问题所属的校验阶段。
type Stage string

const (
	StageContainer Stage = "container"
	StageXML       Stage = "xml"
	StageXSD       Stage = "xsd"
	StageReference Stage = "reference"
	StageSemantic  Stage = "semantic"
	StageDigest    Stage = "digest"
)

// InputInfo 描述被校验的输入文件。
type InputInfo struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// Summary 汇总报告中的问题和文件数量。
type Summary struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Infos    int `json:"infos"`
	Files    int `json:"files"`
}

// CheckResult 描述一个校验阶段的结果。
type CheckResult struct {
	Name     string `json:"name"`
	NameZh   string `json:"name_zh,omitempty"`
	Status   string `json:"status"`
	StatusZh string `json:"status_zh,omitempty"`
}

// Issue 描述一条可定位、可机器读取的校验问题。
type Issue struct {
	Severity   Severity `json:"severity"`
	SeverityZh string   `json:"severity_zh,omitempty"`
	Stage      Stage    `json:"stage"`
	StageZh    string   `json:"stage_zh,omitempty"`
	Code       string   `json:"code"`
	EngineCode string   `json:"engine_code,omitempty"`
	Message    string   `json:"message"`
	Hint       string   `json:"hint,omitempty"`
	File       string   `json:"file,omitempty"`
	Path       string   `json:"path,omitempty"`
	Line       int      `json:"line,omitempty"`
	Column     int      `json:"column,omitempty"`
}

// Report 是校验器的统一报告模型。
type Report struct {
	SchemaVersion string        `json:"schema_version"`
	Tool          ToolInfo      `json:"tool"`
	Input         InputInfo     `json:"input"`
	Status        Status        `json:"status"`
	StatusZh      string        `json:"status_zh,omitempty"`
	Summary       Summary       `json:"summary"`
	Checks        []CheckResult `json:"checks"`
	Issues        []Issue       `json:"issues"`
	StartedAt     time.Time     `json:"started_at"`
	DurationMS    int64         `json:"duration_ms"`
}

// ToolInfo 描述生成报告的工具版本。
type ToolInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ToolVersion 是校验器报告中的工具版本。
const ToolVersion = "0.1.0"

func newReport(input string) Report {
	return Report{
		SchemaVersion: "1",
		Tool: ToolInfo{
			Name:    "ofd-validator",
			Version: ToolVersion,
		},
		Input:     InputInfo{Path: input},
		Status:    StatusError,
		StartedAt: time.Now(),
		Checks: []CheckResult{
			{Name: "zip", Status: "pending"},
			{Name: "xml", Status: "pending"},
			{Name: "xsd", Status: "pending"},
			{Name: "references", Status: "pending"},
			{Name: "semantic", Status: "pending"},
			{Name: "digest", Status: "skipped"},
		},
	}
}

func (r *Report) finish(start time.Time, partial bool, failOnWarning bool) {
	r.DurationMS = time.Since(start).Milliseconds()
	for i := range r.Checks {
		if r.Checks[i].Status == "pending" {
			r.Checks[i].Status = "skipped"
		}
	}
	if r.Summary.Errors > 0 || (failOnWarning && r.Summary.Warnings > 0) {
		r.Status = StatusInvalid
	} else if partial {
		r.Status = StatusPartial
	} else {
		r.Status = StatusValid
	}
	r.applyChineseLabels()
	sort.SliceStable(r.Issues, func(i, j int) bool {
		left, right := r.Issues[i], r.Issues[j]
		if left.File != right.File {
			return left.File < right.File
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		if left.Column != right.Column {
			return left.Column < right.Column
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		return left.Code < right.Code
	})
}

func (r Report) hasErrors() bool {
	return r.Summary.Errors > 0
}

// HasErrors 报告是否包含错误。
func (r Report) HasErrors() bool {
	return r.hasErrors()
}

// HasWarnings 报告是否包含警告。
func (r Report) HasWarnings() bool {
	return r.Summary.Warnings > 0
}

func (r Report) hasStageErrors(stage Stage) bool {
	for _, issue := range r.Issues {
		if issue.Stage == stage && issue.Severity == SeverityError {
			return true
		}
	}
	return false
}

// ExitCode 返回适合命令行使用的退出码。
func (r Report) ExitCode(failOnWarning bool) int {
	if r.hasErrors() || (failOnWarning && r.HasWarnings()) {
		return 1
	}
	return 0
}

func (r *Report) addIssue(issue Issue, maxErrors int) {
	if issue.Severity == SeverityError && maxErrors > 0 && r.Summary.Errors >= maxErrors {
		return
	}
	r.Issues = append(r.Issues, issue)
	r.Issues[len(r.Issues)-1].SeverityZh = severityLabel(issue.Severity)
	r.Issues[len(r.Issues)-1].StageZh = stageLabel(issue.Stage)
	switch issue.Severity {
	case SeverityError:
		r.Summary.Errors++
	case SeverityWarning:
		r.Summary.Warnings++
	case SeverityInfo:
		r.Summary.Infos++
	}
}

func (r *Report) setCheck(name, status string) {
	for i := range r.Checks {
		if r.Checks[i].Name == name {
			// 检查状态只允许向更严重的结果升级，避免兼容模式的 XSD 警告
			// 在后续文件校验成功时被覆盖为已通过。
			if checkStatusRank(status) < checkStatusRank(r.Checks[i].Status) {
				return
			}
			r.Checks[i].Status = status
			r.Checks[i].NameZh = checkLabel(name)
			r.Checks[i].StatusZh = checkStatusLabel(status)
			return
		}
	}
	r.Checks = append(r.Checks, CheckResult{Name: name, NameZh: checkLabel(name), Status: status, StatusZh: checkStatusLabel(status)})
}

func (r *Report) applyChineseLabels() {
	r.StatusZh = statusLabel(r.Status)
	for i := range r.Checks {
		r.Checks[i].NameZh = checkLabel(r.Checks[i].Name)
		r.Checks[i].StatusZh = checkStatusLabel(r.Checks[i].Status)
	}
	for i := range r.Issues {
		r.Issues[i].SeverityZh = severityLabel(r.Issues[i].Severity)
		r.Issues[i].StageZh = stageLabel(r.Issues[i].Stage)
	}
}

func checkStatusRank(status string) int {
	switch status {
	case "failed":
		return 4
	case "warning":
		return 3
	case "passed":
		return 2
	case "skipped":
		return 1
	default:
		return 0
	}
}

func statusLabel(status Status) string {
	switch status {
	case StatusValid:
		return "有效"
	case StatusInvalid:
		return "无效"
	case StatusPartial:
		return "部分通过"
	case StatusError:
		return "校验错误"
	default:
		return string(status)
	}
}

func severityLabel(severity Severity) string {
	switch severity {
	case SeverityError:
		return "错误"
	case SeverityWarning:
		return "警告"
	case SeverityInfo:
		return "提示"
	default:
		return string(severity)
	}
}

func stageLabel(stage Stage) string {
	switch stage {
	case StageContainer:
		return "容器"
	case StageXML:
		return "XML"
	case StageXSD:
		return "XSD"
	case StageReference:
		return "文件引用"
	case StageSemantic:
		return "语义"
	case StageDigest:
		return "摘要"
	default:
		return string(stage)
	}
}

func checkLabel(name string) string {
	switch name {
	case "zip":
		return "ZIP 容器"
	case "xml":
		return "XML"
	case "xsd":
		return "XSD"
	case "references":
		return "文件引用"
	case "semantic":
		return "语义"
	case "digest":
		return "摘要"
	default:
		return name
	}
}

func checkStatusLabel(status string) string {
	switch status {
	case "passed":
		return "已通过"
	case "failed":
		return "未通过"
	case "warning":
		return "有警告"
	case "skipped":
		return "已跳过"
	case "pending":
		return "未执行"
	default:
		return status
	}
}

// RenderJSON 将报告编码为 JSON；英文枚举用于机器处理，*_zh 字段提供中文标签。
func RenderJSON(w io.Writer, report Report, pretty bool) error {
	report.applyChineseLabels()
	encoder := json.NewEncoder(w)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(report)
}

// RenderText 将报告输出为中文纯文本。
func RenderText(w io.Writer, report Report) error {
	if _, err := fmt.Fprintf(w, "OFD 校验报告\n状态：%s\n输入文件：%s\n检测时间：%s\n", statusLabel(report.Status), report.Input.Path, formatDetectionTime(report.StartedAt)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "错误：%d  警告：%d  文件：%d  耗时：%d ms\n\n", report.Summary.Errors, report.Summary.Warnings, report.Summary.Files, report.DurationMS); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "检查结果：\n"); err != nil {
		return err
	}
	for _, check := range report.Checks {
		if _, err := fmt.Fprintf(w, "  %-12s %s\n", checkLabel(check.Name), checkStatusLabel(check.Status)); err != nil {
			return err
		}
	}
	if len(report.Issues) == 0 {
		_, err := io.WriteString(w, "\n未发现问题。\n")
		return err
	}
	if _, err := io.WriteString(w, "\n问题：\n"); err != nil {
		return err
	}
	for i, issue := range report.Issues {
		location := issue.File
		if issue.Line > 0 {
			location = fmt.Sprintf("%s:%d:%d", location, issue.Line, issue.Column)
		}
		if issue.Path != "" {
			location += " " + issue.Path
		}
		if _, err := fmt.Fprintf(w, "%d. [%s] %s：%s\n", i+1, severityLabel(issue.Severity), location, issue.Message); err != nil {
			return err
		}
		if issue.Code != "" {
			if _, err := fmt.Fprintf(w, "   代码=%s 阶段=%s", issue.Code, stageLabel(issue.Stage)); err != nil {
				return err
			}
			if issue.EngineCode != "" {
				if _, err := fmt.Fprintf(w, " 引擎代码=%s", issue.EngineCode); err != nil {
					return err
				}
			}
			if _, err := io.WriteString(w, "\n"); err != nil {
				return err
			}
		}
	}
	return nil
}

// RenderMarkdown 将报告输出为中文 Markdown。
func RenderMarkdown(w io.Writer, report Report) error {
	write := func(format string, args ...any) error {
		_, err := fmt.Fprintf(w, format, args...)
		return err
	}
	if err := write("# OFD 校验报告\n\n- 输入文件：`%s`\n- 检测时间：`%s`\n- 状态：**%s**\n- 错误：%d\n- 警告：%d\n- 文件：%d\n- 耗时：%d ms\n\n", escapeMarkdown(report.Input.Path), formatDetectionTime(report.StartedAt), statusLabel(report.Status), report.Summary.Errors, report.Summary.Warnings, report.Summary.Files, report.DurationMS); err != nil {
		return err
	}
	if err := write("## 检查结果\n\n| 检查项 | 状态 |\n| --- | --- |\n"); err != nil {
		return err
	}
	for _, check := range report.Checks {
		if err := write("| %s | %s |\n", escapeMarkdown(checkLabel(check.Name)), markdownCheckStatusLabel(check.Status)); err != nil {
			return err
		}
	}
	if err := write("\n## 问题\n\n"); err != nil {
		return err
	}
	if len(report.Issues) == 0 {
		_, err := io.WriteString(w, "未发现问题。\n")
		return err
	}
	for i, issue := range report.Issues {
		location := issue.File
		if issue.Line > 0 {
			location = fmt.Sprintf("%s:%d:%d", location, issue.Line, issue.Column)
		}
		if err := write("### %d. `%s`\n\n- 严重级别：`%s`\n- 阶段：`%s`\n- 代码：`%s`\n", i+1, escapeMarkdown(location), severityLabel(issue.Severity), stageLabel(issue.Stage), issue.Code); err != nil {
			return err
		}
		if issue.EngineCode != "" {
			if err := write("- 引擎代码：`%s`\n", escapeMarkdown(issue.EngineCode)); err != nil {
				return err
			}
		}
		if issue.Path != "" {
			if err := write("- XML 路径：`%s`\n", escapeMarkdown(issue.Path)); err != nil {
				return err
			}
		}
		if err := write("- 信息：%s\n\n", escapeMarkdown(issue.Message)); err != nil {
			return err
		}
	}
	return nil
}

func formatDetectionTime(value time.Time) string {
	if value.IsZero() {
		return "未知"
	}
	return value.Format("2006-01-02 15:04:05")
}

func markdownCheckStatusLabel(status string) string {
	label := checkStatusLabel(status)
	if status == "failed" {
		return "**" + label + "**"
	}
	return label
}

func escapeMarkdown(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "`", "\\`")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}
