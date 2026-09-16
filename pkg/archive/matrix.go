package archive

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/zc310/ofd/pkg/analyzer"
)

const (
	MatrixSchemaVersion = "1"
	MatrixStandard      = "GB/T 42133-2022"

	MatrixPassed        = "passed"
	MatrixFailed        = "failed"
	MatrixWarning       = "warning"
	MatrixManualReview  = "manual_review"
	MatrixNotApplicable = "not_applicable"
	MatrixNotAssessed   = "not_assessed"
	MatrixUnsupported   = "unsupported"
)

// MatrixClause 表示条文级符合性矩阵中的一行。
type MatrixClause struct {
	ID            string   `json:"clause"`
	Title         string   `json:"title"`
	Requirement   string   `json:"requirement"`
	Applicability string   `json:"applicability"`
	Status        string   `json:"status"`
	Assessment    string   `json:"assessment"`
	Evidence      []string `json:"evidence,omitempty"`
	Detector      string   `json:"detector,omitempty"`
	ManualReview  bool     `json:"manual_review,omitempty"`
	Limitations   string   `json:"limitations,omitempty"`
}

type MatrixSummary struct {
	Total         int `json:"total"`
	Passed        int `json:"passed"`
	Failed        int `json:"failed"`
	Warnings      int `json:"warnings"`
	ManualReview  int `json:"manual_review"`
	NotApplicable int `json:"not_applicable"`
	NotAssessed   int `json:"not_assessed"`
	Unsupported   int `json:"unsupported"`
}

// MatrixReport 包含针对指定整理版条款目录生成的完整矩阵。
// 它是评估辅助工具，不构成法律或认证结论。
type MatrixReport struct {
	SchemaVersion string         `json:"schema_version"`
	Standard      string         `json:"standard"`
	Source        string         `json:"source"`
	GeneratedAt   time.Time      `json:"generated_at"`
	Input         FileInfo       `json:"input"`
	OverallStatus string         `json:"overall_status"`
	Summary       MatrixSummary  `json:"summary"`
	Clauses       []MatrixClause `json:"clauses"`
	Preflight     Report         `json:"preflight"`
}

// BuildMatrix 执行技术预检，并评估指定整理版条款目录中的每一条条款。
// 没有客观检测器的条款保持为 manual_review，而不是 passed。
func BuildMatrix(ctx context.Context, input string, options Options) (MatrixReport, error) {
	preflight, err := Check(ctx, input, options)
	if err != nil {
		return MatrixReport{}, err
	}
	matrix := MatrixReport{
		SchemaVersion: MatrixSchemaVersion,
		Standard:      MatrixStandard,
		Source:        "用户提供的 GB/T 42133-2022 整理版 Markdown 条款目录",
		GeneratedAt:   time.Now(),
		Input:         preflight.Input,
		Preflight:     preflight,
		Clauses:       assessClauses(preflight),
	}
	for _, clause := range matrix.Clauses {
		matrix.Summary.Total++
		switch clause.Status {
		case MatrixPassed:
			matrix.Summary.Passed++
		case MatrixFailed:
			matrix.Summary.Failed++
		case MatrixWarning:
			matrix.Summary.Warnings++
		case MatrixManualReview:
			matrix.Summary.ManualReview++
		case MatrixNotApplicable:
			matrix.Summary.NotApplicable++
		case MatrixNotAssessed:
			matrix.Summary.NotAssessed++
		case MatrixUnsupported:
			matrix.Summary.Unsupported++
		}
	}
	matrix.OverallStatus = matrixStatus(matrix.Summary)
	return matrix, nil
}

func assessClauses(report Report) []MatrixClause {
	a := report.AnalysisReport
	v := report.ValidatorReport
	clauses := []MatrixClause{
		clause("1", "范围", "适用于包含字符、光栅图像和矢量数据的电子文档及其处理软件。", MatrixManualReview, "当前输入是单个 OFD 文件；适用范围和业务场景需人工确认。", nil, "scope", true, "工具不能从文件内容确认业务场景。"),
		clause("2", "规范性引用文件", "GB/T 33190-2016、GB/T 18894-2016 和 DA/T 47-2009 等引用文件适用性。", MatrixManualReview, "引用文件的版本、适用性和制度采用情况需人工确认。", nil, "reference-catalogue", true, "工具未内置规范性引用文件适用性判定。"),
		clause("3", "术语和定义", "OFD、长期保存和档案化等术语适用于当前处理过程。", MatrixManualReview, "术语使用和档案化流程需结合项目制度确认。", nil, "process-review", true, "工具只分析文件技术属性。"),
		clause("4", "缩略语", "OFD、XML 等缩略语使用正确。", MatrixManualReview, "术语和缩略语正确性不属于当前文件自动检测范围。", nil, "document-review", true, "需人工审阅制度、报告或交付材料。"),
		clause("5.1", "基本原则", "真实性、完整性、可用性和安全性。", MatrixManualReview, "工具提供完整性、可解析性和安全风险证据，但不能证明真实性或法律效力。", []string{"preflight.validator_report", "preflight.analysis_report", "preflight.input.sha256"}, "combined-evidence", true, "真实性、形成过程和授权访问仍需业务证据。"),
		clause("5.2", "应用场景", "适用于电子公文、证照、凭证、票据等长期保存场景。", MatrixManualReview, "当前文件的业务场景和档案属性需人工确认。", nil, "scope", true, "技术报告不能判断文件业务类别。"),
		checkedClause("6.1.1", "文件结构", "采用 GB/T 33190 规定的标准文件结构。", !v.HasErrors(), "校验器未发现结构、XML、引用或语义错误。", "校验器发现结构或语义错误。", []string{"validator.status", "validator.checks", "validator.issues"}, "validator", true, "底层校验结果不替代标准逐条认证。"),
		clause("6.1.2", "外部链接和资源", "禁止外部链接引用，资源应内嵌于 OFD 包内。", resourceStatus(a, v), resourceAssessment(a), []string{"analysis.resource_details", "analysis.file_references", "validator.references"}, "resource-reference-check", true, "当前模型能确认包内文件和缺失资源，但不能证明所有 URI 均无外部网络语义。"),
		checkedClause("6.1.3", "目录和命名", "包内目录结构和命名符合标准规范。", !v.HasErrors(), "底层 OFD 容器和引用检查通过。", "底层 OFD 检查发现错误。", []string{"validator.checks.zip", "validator.checks.references"}, "validator", true, "具体目录命名条款仍以正式标准文本为准。"),
		clause("6.2.1", "页面尺寸", "页面尺寸应明确指定，推荐使用 A4、A3 等标准尺寸。", pageStatus(a), pageAssessment(a), []string{"analysis.pages", "analysis.documents"}, "page-dimensions", true, "是否符合具体业务推荐尺寸需人工确认。"),
		clause("6.2.2", "页面自包含", "页面内容不依赖外部渲染环境。", resourceStatus(a, v), resourceAssessment(a), []string{"analysis.resource_details", "analysis.file_references"}, "resource-reference-check", true, "字体兼容性和渲染环境差异不能完全由包检查证明。"),
		clause("6.3.1", "字体嵌入", "使用的非通用字体必须嵌入 OFD 文件。", fontStatus(a), fontAssessment(a), []string{"analysis.fonts", "analysis.resource_details"}, "font-embedding-check", true, "工具无法自动判断某字体是否属于通用字体。"),
		clause("6.3.2", "标准字体", "优先使用宋体、黑体、楷体、仿宋等通用字体。", MatrixManualReview, "已记录字体名称，但是否满足业务字体策略需人工确认。", []string{"analysis.fonts", "analysis.resource_details"}, "font-policy-review", true, "优先使用属于策略判断，不能从 OFD 自动证明。"),
		clause("6.3.3", "私有字体", "不得仅依赖操作系统安装的私有字体而不嵌入。", fontStatus(a), fontAssessment(a), []string{"analysis.fonts", "analysis.resource_details"}, "font-embedding-check", true, "私有字体分类需要人工确认。"),
		clause("6.4.1", "矢量图形", "线条、图表等内容优先使用矢量图形。", MatrixManualReview, "工具统计矢量对象和图像对象，但不能判断是否应优先使用矢量。", []string{"analysis.objects", "analysis.images"}, "object-statistics", true, "内容语义和原始来源需要人工审阅。"),
		clause("6.4.2", "光栅图像参数", "扫描图像的位深、格式和分辨率应满足长期保存要求。", MatrixNotAssessed, "当前版本未实现图像位深、DPI 和压缩质量的逐图检测。", []string{"analysis.images"}, "not-implemented", false, "需要增加图像元数据和实际像素分析。"),
		clause("6.4.3", "图像内嵌", "图像应嵌入文件中，不得仅保留外部引用路径。", imageStatus(a), imageAssessment(a), []string{"analysis.images", "analysis.resource_details"}, "image-resource-check", true, "缺失文件可被发现，但外部 URI 语义仍需人工确认。"),
		signatureClause(a),
		annotationClause(a),
		customMarkupClause(a),
		attachmentClause(a),
		encryptionClause(report.Features),
		versionClause(a),
		actionClause(report.Features),
		clause("7.1", "生成软件要求", "软件应支持转换、保持版式/字体/图像/元数据并进行完整性校验。", MatrixUnsupported, "当前矩阵针对输入 OFD 进行文件级评估，不能据此证明生成软件完整满足第 7.1 条。", []string{"preflight.input.sha256"}, "software-capability-review", true, "需要软件功能测试、转换对照样本和验收记录。"),
		clause("7.2", "阅读软件要求", "软件应支持解析渲染、印章验证、文本利用和元数据查看。", MatrixUnsupported, "当前矩阵不能替代阅读软件功能测试。", []string{"analysis_report"}, "software-capability-review", true, "需要阅读器功能和兼容性测试记录。"),
		appendixAttachmentPathClause(a),
		appendixAttachmentMetadataClause(a),
		clause("A.3", "附件格式转换", "无法嵌入原格式时可转换为 OFD 副本，同时保留原件。", MatrixUnsupported, "当前版本不执行附件格式转换，只原样复制原始附件。", []string{"analysis.attachments", "archive.attachments"}, "attachment-preservation", true, "转换策略和副本格式需由业务制度确定。"),
		clause("A.4", "音视频附件", "保留原始编码格式，并建议生成低码率预览版本。", MatrixNotAssessed, "当前版本不生成音视频预览版本。", []string{"analysis.attachments", "features.media"}, "not-implemented", true, "需增加媒体编码识别和预览生成能力。"),
	}
	return clauses
}

func clause(id, title, requirement, status, assessment string, evidence []string, detector string, manual bool, limitations string) MatrixClause {
	applicability := "applicable"
	if status == MatrixNotApplicable {
		applicability = "not_applicable"
	}
	return MatrixClause{ID: id, Title: title, Requirement: requirement, Applicability: applicability, Status: status, Assessment: assessment, Evidence: evidence, Detector: detector, ManualReview: manual, Limitations: limitations}
}

func checkedClause(id, title, requirement string, ok bool, pass, fail string, evidence []string, detector string, manual bool, limitations string) MatrixClause {
	status, assessment := MatrixFailed, fail
	if ok {
		status, assessment = MatrixPassed, pass
	}
	return clause(id, title, requirement, status, assessment, evidence, detector, manual, limitations)
}

func resourceStatus(report analyzer.Report, validation interface{ HasErrors() bool }) string {
	if validation.HasErrors() || report.Resources.MissingFiles > 0 || report.Resources.Unresolved > 0 {
		return MatrixFailed
	}
	return MatrixWarning
}

func resourceAssessment(report analyzer.Report) string {
	if report.Resources.MissingFiles > 0 || report.Resources.Unresolved > 0 {
		return "发现缺失资源或无法解析的资源引用。"
	}
	return "未发现缺失资源；外部链接语义和渲染环境依赖仍需人工确认。"
}

func pageStatus(report analyzer.Report) string {
	if report.Summary.Pages == 0 || report.Summary.ParsedPages != report.Summary.Pages {
		return MatrixFailed
	}
	return MatrixPassed
}

func pageAssessment(report analyzer.Report) string {
	if report.Summary.Pages == 0 {
		return "未发现页面。"
	}
	if report.Summary.ParsedPages != report.Summary.Pages {
		return fmt.Sprintf("声明 %d 页，成功解析 %d 页。", report.Summary.Pages, report.Summary.ParsedPages)
	}
	return fmt.Sprintf("已解析全部 %d 页并记录页面尺寸。", report.Summary.Pages)
}

func fontStatus(report analyzer.Report) string {
	if report.Fonts.MissingFiles > 0 || report.Fonts.Unresolved > 0 {
		return MatrixFailed
	}
	for _, item := range report.ResourceDetails {
		if item.Kind == "font" && item.Used > 0 && !item.Embedded {
			return MatrixWarning
		}
	}
	return MatrixPassed
}

func fontAssessment(report analyzer.Report) string {
	if report.Fonts.MissingFiles > 0 || report.Fonts.Unresolved > 0 {
		return "发现缺失或无法解析的字体资源。"
	}
	for _, item := range report.ResourceDetails {
		if item.Kind == "font" && item.Used > 0 && !item.Embedded {
			return "发现被使用但未嵌入的字体，是否为通用字体需人工确认。"
		}
	}
	return "已检查字体资源，未发现缺失或未嵌入的已使用字体。"
}

func imageStatus(report analyzer.Report) string {
	if report.Images.MissingFiles > 0 || report.Images.Unresolved > 0 {
		return MatrixFailed
	}
	return MatrixPassed
}

func imageAssessment(report analyzer.Report) string {
	if report.Images.MissingFiles > 0 || report.Images.Unresolved > 0 {
		return "发现缺失或无法解析的图像资源。"
	}
	return "已检查图像资源引用，未发现缺失文件。"
}

func signatureClause(report analyzer.Report) MatrixClause {
	if len(report.Signatures) == 0 {
		return clause("6.5", "数字签名与签章", "电子印章、长期验证格式、签名时间戳、签名值和证书信息应满足要求。", MatrixManualReview, "当前文档未发现签名；是否必须签名取决于业务场景。", []string{"analysis.signatures"}, "signature-analysis", true, "工具不能判断签章规范、时间戳充分性或法律效力。")
	}
	for _, item := range report.Signatures {
		if !item.SignedValueExists || (item.DigestChecked && !item.DigestValid) {
			return clause("6.5", "数字签名与签章", "电子印章、长期验证格式、签名时间戳、签名值和证书信息应满足要求。", MatrixFailed, "发现缺失签名值或签名摘要校验失败。", []string{"analysis.signatures", "analysis.signatures.digest"}, "signature-analysis", true, "证书信任和法律效力仍不由此结论覆盖。")
		}
	}
	return clause("6.5", "数字签名与签章", "电子印章、长期验证格式、签名时间戳、签名值和证书信息应满足要求。", MatrixWarning, "签名文件和摘要关系可读取；长期验证、时间戳、证书信任和法律效力需人工确认。", []string{"analysis.signatures", "analysis.signatures.digest"}, "signature-analysis", true, "数学签名验证不等于证书信任或法律效力。")
}

func annotationClause(report analyzer.Report) MatrixClause {
	if len(report.Annotations) == 0 {
		return clause("6.6.1", "注释保存", "注释应作为文档内容的一部分保存。", MatrixNotApplicable, "当前文档未发现注释。", []string{"analysis.annotations"}, "annotation-analysis", false, "")
	}
	return clause("6.6.1", "注释保存", "注释应作为文档内容的一部分保存。", MatrixManualReview, fmt.Sprintf("发现 %d 个注释并已纳入分析报告；是否属于应保留内容需人工确认。", len(report.Annotations)), []string{"analysis.annotations"}, "annotation-analysis", true, "分析结果不能证明转换前后的注释未丢失。")
}

func customMarkupClause(report analyzer.Report) MatrixClause {
	return clause("6.6.2", "自定义标引", "自定义标引应通过扩展机制保存并保留语义。", MatrixNotAssessed, "当前版本未实现自定义标引语义和扩展信息的逐项检测。", []string{"analysis_report"}, "not-implemented", true, "需要扩展信息解析和语义保真测试。")
}

func attachmentClause(report analyzer.Report) MatrixClause {
	if len(report.Attachments) == 0 {
		return clause("6.7", "附件处理", "关联附件应一并打包，保持原格式或按策略转换，并说明关联关系。", MatrixNotApplicable, "当前文档未发现附件。", []string{"analysis.attachments"}, "attachment-analysis", false, "")
	}
	for _, item := range report.Attachments {
		if !item.Exists {
			return clause("6.7", "附件处理", "关联附件应一并打包，保持原格式或按策略转换，并说明关联关系。", MatrixFailed, "发现附件登记项但包内文件不存在。", []string{"analysis.attachments", "analysis.file_references"}, "attachment-analysis", true, "附件转换策略仍需人工确认。")
		}
	}
	return clause("6.7", "附件处理", "关联附件应一并打包，保持原格式或按策略转换，并说明关联关系。", MatrixWarning, fmt.Sprintf("发现 %d 个附件，均存在并可登记；格式长期保存策略和关联关系需人工确认。", len(report.Attachments)), []string{"analysis.attachments", "analysis.file_references"}, "attachment-analysis", true, "工具保留原始附件，不执行格式转换。")
}

func encryptionClause(features FeatureSummary) MatrixClause {
	if features.Encryption > 0 {
		return clause("6.8", "加密与权限", "长期保存档案类 OFD 不应使用无法长期解密的加密；必要时应归档密钥。", MatrixFailed, "检测到加密相关 XML 元素。", []string{"features.encryption"}, "feature-scan", true, "需要结合实际加密容器和密钥管理制度复核。")
	}
	return clause("6.8", "加密与权限", "长期保存档案类 OFD 不应使用无法长期解密的加密；必要时应归档密钥。", MatrixManualReview, "未检测到加密相关 XML 元素，但是否存在容器级或业务侧加密仍需人工确认。", []string{"features.encryption"}, "feature-scan", true, "未检测到不等于证明不存在所有加密和权限控制。")
}

func versionClause(report analyzer.Report) MatrixClause {
	if strings.TrimSpace(report.OFD.Version) == "" {
		return clause("6.9", "版本与兼容性", "应明确声明 OFD 版本，并具备旧版本兼容能力。", MatrixFailed, "OFD 根元素未声明版本。", []string{"analysis.ofd.version"}, "version-check", true, "向下兼容能力需软件测试证明。")
	}
	return clause("6.9", "版本与兼容性", "应明确声明 OFD 版本，并具备旧版本兼容能力。", MatrixWarning, "OFD 版本已声明；向下兼容能力需软件测试证明。", []string{"analysis.ofd.version"}, "version-check", true, "版本声明不等于软件兼容性证明。")
}

func actionClause(features FeatureSummary) MatrixClause {
	if features.Actions > 0 {
		return clause("6.10", "动作与脚本", "长期保存档案 OFD 不应包含可执行脚本、自动播放动作或外部跳转链接。", MatrixFailed, fmt.Sprintf("检测到 %d 个动作元素。", features.Actions), []string{"features.actions"}, "feature-scan", true, "需要人工区分内部定位与外部跳转，并确认动作具体语义。")
	}
	return clause("6.10", "动作与脚本", "长期保存档案 OFD 不应包含可执行脚本、自动播放动作或外部跳转链接。", MatrixManualReview, "未检测到动作元素；仍需人工确认扩展命名空间和外部链接语义。", []string{"features.actions"}, "feature-scan", true, "特征扫描不能证明所有脚本或动态内容均不存在。")
}

func appendixAttachmentPathClause(report analyzer.Report) MatrixClause {
	if len(report.Attachments) == 0 {
		return clause("A.1", "附件目录", "附件应置于 OFD 包的 Attachs 目录下。", MatrixNotApplicable, "当前文档未发现附件。", []string{"analysis.attachments"}, "attachment-path-check", false, "")
	}
	for _, item := range report.Attachments {
		if !strings.Contains(strings.ToLower(item.Path), "/attachs/") {
			return clause("A.1", "附件目录", "附件应置于 OFD 包的 Attachs 目录下。", MatrixFailed, "发现附件路径不在 Attachs 目录下："+item.Path, []string{"analysis.attachments"}, "attachment-path-check", true, "路径规则以用户提供的整理版条款为基线。")
		}
	}
	return clause("A.1", "附件目录", "附件应置于 OFD 包的 Attachs 目录下。", MatrixPassed, "所有已分析附件均位于 Attachs 目录。", []string{"analysis.attachments"}, "attachment-path-check", false, "")
}

func appendixAttachmentMetadataClause(report analyzer.Report) MatrixClause {
	if len(report.Attachments) == 0 {
		return clause("A.2", "附件描述信息", "每个附件应包含名称、格式、大小和关联关系描述。", MatrixNotApplicable, "当前文档未发现附件。", []string{"analysis.attachments"}, "attachment-metadata-check", false, "")
	}
	for _, item := range report.Attachments {
		if item.Name == "" || item.Path == "" || item.ActualSize == 0 {
			return clause("A.2", "附件描述信息", "每个附件应包含名称、格式、大小和关联关系描述。", MatrixWarning, "附件描述存在缺少名称、路径或大小的项目。", []string{"analysis.attachments"}, "attachment-metadata-check", true, "关联关系和格式字段的业务完整性需人工确认。")
		}
	}
	return clause("A.2", "附件描述信息", "每个附件应包含名称、格式、大小和关联关系描述。", MatrixWarning, "附件名称、路径和实际大小已记录；格式和业务关联关系需人工确认。", []string{"analysis.attachments"}, "attachment-metadata-check", true, "当前分析报告不包含完整档案著录关联关系。")
}

func matrixStatus(summary MatrixSummary) string {
	if summary.Failed > 0 {
		return MatrixFailed
	}
	if summary.Warnings > 0 || summary.ManualReview > 0 || summary.NotAssessed > 0 || summary.Unsupported > 0 {
		return MatrixWarning
	}
	return MatrixPassed
}

// RenderMatrixMarkdown 将完整矩阵渲染为便于人工审阅的 Markdown。
func RenderMatrixMarkdown(writer io.Writer, matrix MatrixReport) error {
	if writer == nil {
		return fmt.Errorf("符合性矩阵输出为空")
	}
	if _, err := fmt.Fprintf(writer, "# %s 条文符合性矩阵\n\n- 来源：%s\n- 输入文件：`%s`\n- 总体状态：**%s**\n- 条款数：%d\n- 通过：%d\n- 失败：%d\n- 警告：%d\n- 人工确认：%d\n- 未评估：%d\n- 不支持：%d\n\n> 本矩阵依据用户提供的整理版条款生成。`passed` 只表示当前技术检测证据支持通过，不构成法律、合规或认证结论。\n\n| 条款 | 标题 | 状态 | 要求 | 评估和证据 | 人工确认 |\n| --- | --- | --- | --- | --- | --- |\n", matrix.Standard, matrix.Source, matrix.Input.Path, matrix.OverallStatus, matrix.Summary.Total, matrix.Summary.Passed, matrix.Summary.Failed, matrix.Summary.Warnings, matrix.Summary.ManualReview, matrix.Summary.NotAssessed, matrix.Summary.Unsupported); err != nil {
		return err
	}
	for _, item := range matrix.Clauses {
		evidence := strings.Join(item.Evidence, ", ")
		manual := "否"
		if item.ManualReview {
			manual = "是"
		}
		if _, err := fmt.Fprintf(writer, "| `%s` | %s | **%s** | %s | %s（证据：`%s`） | %s |\n", item.ID, escapeMatrix(item.Title), item.Status, escapeMatrix(item.Requirement), escapeMatrix(item.Assessment), escapeMatrix(evidence), manual); err != nil {
			return err
		}
	}
	return nil
}

func escapeMatrix(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}
