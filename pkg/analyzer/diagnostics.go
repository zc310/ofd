package analyzer

import (
	"fmt"
	"math"
)

// StageResult 描述执行阶段；complete 只表示执行完成，不代表没有文档问题。
type StageResult struct {
	Name   string `json:"name"`
	Status string `json:"status"` // complete、partial、failed、skipped。
	Reason string `json:"reason,omitempty"`
}

// Diagnostic 描述一个可定位的发现；索引为零时仍通过指针保留。
type Diagnostic struct {
	Code          string `json:"code"`
	Severity      string `json:"severity"` // info、warning、error。
	Stage         string `json:"stage"`
	Message       string `json:"message"`
	DocumentIndex *int   `json:"document_index,omitempty"`
	PageNumber    *int   `json:"page_number,omitempty"`
	ObjectID      string `json:"object_id,omitempty"`
	File          string `json:"file,omitempty"`
	Location      string `json:"location,omitempty"`
	Basis         string `json:"basis,omitempty"`
}

// URIActionInfo 记录 URI 动作的原值和解析值；不会发出网络请求。
type URIActionInfo struct {
	DocumentIndex int    `json:"document_index"`
	PageNumber    *int   `json:"page_number,omitempty"`
	ObjectID      string `json:"object_id,omitempty"`
	File          string `json:"file"`
	Location      string `json:"location"`
	Event         string `json:"event"`
	URI           string `json:"uri"`
	Base          string `json:"base,omitempty"`
	ResolvedURI   string `json:"resolved_uri,omitempty"`
	Scheme        string `json:"scheme,omitempty"`
	External      bool   `json:"external"`
}

func initialStages() []StageResult {
	stages := make([]StageResult, 0, 9)
	for _, name := range []string{"package", "documents", "pages", "resources", "templates", "attachments", "annotations", "signatures", "actions"} {
		stages = append(stages, StageResult{Name: name, Status: "skipped", Reason: "尚未执行"})
	}
	return stages
}

func (r *Report) setStage(name, status, reason string) {
	for i := range r.Stages {
		if r.Stages[i].Name == name {
			r.Stages[i].Status, r.Stages[i].Reason = status, reason
			return
		}
	}
}

func (a *analyzer) finding(d Diagnostic, incomplete bool) {
	if d.Stage == "" {
		d.Stage = a.stage
	}
	if d.DocumentIndex == nil {
		d.DocumentIndex = a.documentIndex
	}
	if d.File == "" {
		d.File = a.sourceFile
	}
	for _, existing := range a.report.Diagnostics {
		if existing.Code == d.Code && existing.Message == d.Message && existing.File == d.File && existing.Location == d.Location && sameIndex(existing.DocumentIndex, d.DocumentIndex) {
			return
		}
	}
	a.report.Diagnostics = append(a.report.Diagnostics, d)
	if d.Severity == "warning" {
		a.appendWarning(d.Message)
	}
	if d.Severity == "error" {
		a.report.Errors = append(a.report.Errors, d.Message)
	}
	if incomplete {
		a.report.setStage(d.Stage, "partial", "部分内容无法读取或分析，详见诊断")
	}
}

func sameIndex(a, b *int) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func (a *analyzer) beginStages() {
	for _, name := range []string{"package", "documents", "pages", "resources", "templates", "attachments", "annotations", "signatures", "actions"} {
		a.report.setStage(name, "complete", "")
	}
	for _, option := range []struct {
		name    string
		enabled bool
	}{
		{"templates", a.options.IncludeTemplates}, {"annotations", a.options.IncludeAnnotations}, {"signatures", a.options.IncludeSignatures},
	} {
		if !option.enabled {
			a.report.setStage(option.name, "skipped", "用户关闭此阶段")
		}
	}
	a.report.setStage("actions", "complete", "仅审计文档、大纲、页面及已启用的模板/注解结构和资源定义；不读取附件或孤立 XML")
}

func (r *Report) finalizeAnalysis() {
	// 入口失败也必须保留结构化诊断及未执行范围，不把零值当作检查结论。
	if r.Status == StatusFailed {
		failedStage := "package"
		for _, stage := range r.Stages {
			if stage.Status == "failed" {
				failedStage = stage.Name
				break
			}
		}
		r.setStage(failedStage, "failed", "无法进入结构分析，详见诊断")
		for _, message := range r.Errors {
			found := false
			for _, diagnostic := range r.Diagnostics {
				if diagnostic.Severity == "error" && diagnostic.Message == message {
					found = true
					break
				}
			}
			if found {
				continue
			}
			r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: "analysis.failed", Severity: "error", Stage: failedStage, Message: message})
		}
	}
	if r.Encryption != nil && r.Encryption.State != "unlocked" && r.Status == StatusPartial {
		r.setStage("package", "partial", "仅检查可见加密元数据")
	}
	r.Conclusions = nil
	r.Conclusions = append(r.Conclusions, fmt.Sprintf("分析状态：%s；警告 %d 条，错误 %d 条（完成不代表无问题）", reportStatusLabel(r.Status), len(r.Warnings), len(r.Errors)))
	for _, stage := range r.Stages {
		if stage.Status != "complete" && stage.Status != "partial" {
			continue
		}
		prefix := ""
		if stage.Status == "partial" {
			prefix = "已分析部分："
		}
		if stage.Name == "documents" && stage.Status == "complete" {
			r.Conclusions = append(r.Conclusions, fmt.Sprintf("包含 %d 个文档体，共 %d 页，已解析 %d 页", r.Summary.DocumentBodies, r.Summary.Pages, r.Summary.ParsedPages))
		}
		if stage.Name == "resources" {
			r.Conclusions = append(r.Conclusions, prefix+fmt.Sprintf("字体资源 %d 个，声明的字体文件缺失 %d 个；图片资源 %d 个（不等于图片对象数）", r.Fonts.Declared, r.Fonts.MissingFiles, r.Images.Declared))
		}
		if stage.Name == "attachments" {
			mismatch := 0
			for _, item := range r.Attachments {
				if item.SizeCheck == "mismatch" || item.SizeCheck == "invalid" {
					mismatch++
				}
			}
			r.Conclusions = append(r.Conclusions, prefix+fmt.Sprintf("附件 %d 个，其中 %d 个大小声明异常", len(r.Attachments), mismatch))
		}
		if stage.Name == "signatures" {
			checked, matched, trusted := 0, 0, 0
			for _, sig := range r.Signatures {
				if sig.DigestChecked {
					checked++
					if sig.DigestValid {
						matched++
					}
				}
				if sig.TrustChecked {
					trusted++
				}
			}
			r.Conclusions = append(r.Conclusions, prefix+fmt.Sprintf("签名 %d 个，摘要已检查 %d 个、匹配 %d 个，证书信任已检查 %d 个；摘要匹配不代表完整验证通过", len(r.Signatures), checked, matched, trusted))
		}
		if stage.Name == "actions" {
			external := 0
			for _, action := range r.Actions {
				if action.External {
					external++
				}
			}
			r.Conclusions = append(r.Conclusions, fmt.Sprintf("已检查结构中发现 %d 个 URI 动作，其中 %d 个外部目标；未访问目标地址", len(r.Actions), external))
		}
	}
	if r.ResourceAudit.Status == "complete" || r.ResourceAudit.Status == "partial" {
		r.Conclusions = append(r.Conclusions, fmt.Sprintf("资源依赖审计：%s，%d 个已声明资源未观测到可达引用；不代表可安全删除", stageStatusLabel(r.ResourceAudit.Status), r.ResourceAudit.Unobserved))
	}
	r.Conclusions = append(r.Conclusions, "分析范围不含实际渲染和文本提取测试；结构解析成功不代表合规或渲染正常")
}

func (a *analyzer) checkAttachmentSize(info *AttachmentInfo, source string) {
	info.SizeCheck = "not_checked"
	if info.DeclaredSizeKB == nil {
		return
	}
	declared := *info.DeclaredSizeKB
	if math.IsNaN(declared) || math.IsInf(declared, 0) || declared < 0 {
		info.SizeCheck = "invalid"
	} else if info.Exists {
		info.SizeCheck = "match"
		// 表 73 指定 KB，但未规定舍入精度；本工具按 1024 字节/KB，允许半个字节的浮点换算误差。
		if math.Abs(declared*1024-float64(info.ActualSize)) > 0.5 {
			info.SizeCheck = "mismatch"
		}
	}
	if info.SizeCheck == "invalid" || info.SizeCheck == "mismatch" {
		a.finding(Diagnostic{Code: "attachment.size_" + info.SizeCheck, Severity: "warning", Stage: "attachments", File: source, ObjectID: info.ID,
			Message: fmt.Sprintf("附件 %s 大小声明异常：声明 %g KB，实际 %d 字节", info.ID, declared, info.ActualSize),
			Basis:   "GB/T 33190—2016 第 20.2 节表 73；按 1024 字节/KB 换算，容差 0.5 字节"}, false)
	}
	// 非有限数不属于 JSON 数值；原值保留在 invalid 诊断中，避免整份报告编码失败。
	if math.IsNaN(declared) || math.IsInf(declared, 0) {
		info.DeclaredSizeKB = nil
	}
}
