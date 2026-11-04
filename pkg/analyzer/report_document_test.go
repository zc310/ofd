package analyzer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/zc310/ofd/internal/parser"
	reporting "github.com/zc310/ofd/internal/report"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/validator"
	"golang.org/x/image/font/sfnt"
)

func testReportFont(t *testing.T) string {
	t.Helper()
	path := os.Getenv("OFD_REPORT_TEST_FONT")
	if path == "" {
		path = "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc"
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		t.Skipf("本地中文测试字体不存在: %s", path)
	} else if err != nil {
		t.Fatal(err)
	}
	return path
}

func analysisModelText(doc *creator.Document) string {
	var out strings.Builder
	for _, page := range doc.Pages {
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok {
				for _, code := range text.TextCodes {
					out.WriteString(code.Value)
				}
				out.WriteByte('\n')
			}
		}
	}
	return out.String()
}

func sampleAnalysisReport() Report {
	return Report{Input: InputInfo{Path: "中文分析样例.ofd"}, Status: StatusPartial,
		ResourceAudit:   ResourceAudit{Status: "partial", Scope: "资源依赖检查范围不完整", Nodes: []ResourceNode{{Key: "doc[0]/font:99", Declared: true, SourceFile: "Doc_0/Res.xml"}}, Unobserved: 1},
		Conclusions:     []string{"结构分析完成不代表渲染正常"},
		Stages:          []StageResult{{Name: "templates", Status: "skipped", Reason: "用户关闭此阶段"}},
		Diagnostics:     []Diagnostic{{Code: "attachment.size_mismatch", Severity: "warning", Stage: "attachments", Message: "附件大小声明异常", File: "Attachments/readme.txt"}},
		Actions:         []URIActionInfo{{File: "Doc_0/Document.xml", URI: "https://example.invalid/", ResolvedURI: "https://example.invalid/", External: true}},
		OFD:             OFDInfo{Version: "1.0", DocType: "OFD"},
		Summary:         Summary{DocumentBodies: 1, Pages: 2, ParsedPages: 2, Objects: 20, TextCharacters: 100, Fonts: 1, Signatures: 1},
		Package:         PackageSummary{Entries: 5, Files: 5, XMLFiles: 4, Tree: &PackageTree{Name: "root", Children: []PackageTree{{Name: "Document.xml", Kind: "file", MediaType: "xml", Size: 123}}}},
		Documents:       []DocumentInfo{{Index: 0, DocID: "中文文档", DocRoot: "Doc_0/Document.xml", DeclaredPages: 2, ParsedPages: 2}},
		Pages:           []PageInfo{{PageNumber: 1, DocumentIndex: 0, DocumentPage: 1, ID: 10, Size: PageSize{Width: 210, Height: 297, Unit: "mm", Orientation: "portrait"}}},
		ResourceDetails: []ResourceInfo{{Kind: "font", ID: 20, FontName: "测试字体", FamilyName: "测试字体族", Path: "Doc_0/Res/font.ttf", Embedded: true, Used: 12}},
		Attachments:     []AttachmentInfo{{ID: "attachment-1", Name: "中文附件", Path: "Attachments/readme.txt", Exists: false}},
		Annotations:     []AnnotationInfo{{ID: "annotation-1", PageID: 10, Type: "Stamp", HasAppearance: true, Objects: ObjectCounts{Total: 2}}},
		Signatures: []SignatureInfo{{ID: "signature-1", Path: "Signs/Sign.xml", VerificationChecked: true, VerificationValid: true,
			Validation: &parser.SignatureValidation{Status: "incomplete", Checks: []parser.VerificationCheck{
				{Code: "signature.crypto", Status: "valid"}, {Code: "signature.trust", Status: "invalid", Message: "证书不受信任"},
				{Code: "signature.revocation", Status: "incomplete", Message: "缺少吊销证据"},
			}},
		}},
		Warnings: []string{"分析存在警告"}, Errors: []string{"发现错误明细"},
	}
}

// 长路径、目录树和跨页列表应完整写出，报告字体必须真正子集化。
func TestFlowAnalysisReportPreservesDetailsAndWritesStrictOFD(t *testing.T) {
	opts := DocumentOptions{Font: testReportFont(t)}
	r := sampleAnalysisReport()
	r.Input.Path = strings.Repeat("W", 300) + ".ofd"
	r.Warnings = append(r.Warnings, "非法字符\x00\uffff")
	for i := 2; i < 50; i++ {
		p := r.Pages[0]
		p.PageNumber, p.DocumentPage = i, i
		r.Pages = append(r.Pages, p)
	}
	doc, err := BuildReport(t.Context(), r, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages) < 3 || len(doc.Outlines) != 1 {
		t.Fatalf("缺少分页或大纲: pages=%d outlines=%d", len(doc.Pages), len(doc.Outlines))
	}
	text := analysisModelText(doc)
	for _, want := range []string{"统计汇总", "资源与引用", "文档体", "页面", "字体明细", "测试字体族", "中文附件", "annotation-1", "完整验证：未完成", "证书不受信任", "缺少吊销证据", "OFD 包目录结构", "Document.xml", "发现错误明细", `\u0000`, `\uFFFF`} {
		if !strings.Contains(text, want) {
			t.Fatalf("报告缺少 %q", want)
		}
	}
	if strings.Count(text, "物理尺寸") < 2 {
		t.Fatal("跨页页面表未重复表头")
	}
	colors := map[string]creator.Color{"已通过": reporting.Green, "未通过": reporting.Red, "未完成": reporting.Orange}
	for _, page := range doc.Pages {
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok {
				var value strings.Builder
				for _, code := range text.TextCodes {
					value.WriteString(code.Value)
				}
				if want, exists := colors[value.String()]; exists {
					if text.FillColor == nil || text.FillColor.R != want.R || text.FillColor.G != want.G || text.FillColor.B != want.B {
						t.Fatalf("状态颜色错误: %s", &value)
					}
					delete(colors, value.String())
				}
			}
		}
	}
	if len(colors) != 0 {
		t.Fatalf("未检查全部状态: %v", colors)
	}
	var output bytes.Buffer
	if err := RenderOFD(&output, r, opts); err != nil {
		t.Fatal(err)
	}
	v, err := validator.New(validator.WithMode(validator.ModeStrict))
	if err != nil {
		t.Fatal(err)
	}
	checked := v.ValidateReader(t.Context(), bytes.NewReader(output.Bytes()), "analysis-report.ofd")
	if checked.HasErrors() || checked.HasWarnings() {
		t.Fatalf("报告严格校验失败: %+v", checked.Issues)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	if ofd.Documents[0].PageCount() != len(doc.Pages) {
		t.Fatal("报告写出改变页数")
	}
	original, err := reporting.LoadFont(opts.Font)
	if err != nil {
		t.Fatal(err)
	}
	for _, font := range ofd.Documents[0].Fonts() {
		if font.FontFile == "" {
			t.Fatal("报告字体未嵌入")
		}
		embedded, err := ofd.Documents[0].FileCache.Read(font.FontFile.String())
		if err != nil {
			t.Fatal(err)
		}
		face, err := sfnt.Parse(embedded)
		if err != nil {
			t.Fatal(err)
		}
		if face.NumGlyphs() >= original.Face.NumGlyphs() {
			t.Fatal("报告携带了完整字体")
		}
	}
}

func TestFlowAnalysisFontsTablePreservesDetailsAndRepeatsHeaders(t *testing.T) {
	r := sampleAnalysisReport()
	r.ResourceDetails = []ResourceInfo{{Kind: "image", FontName: "不应展示的图片"}}
	for i := range 24 {
		r.ResourceDetails = append(r.ResourceDetails, ResourceInfo{Kind: "font", DocumentIndex: i % 2, ID: uint64(i + 100), FontName: fmt.Sprintf("字体样例%d", i), FamilyName: "字体族样例", Path: "Doc_0/" + strings.Repeat("long-font-path/", 16) + fmt.Sprintf("font%d.ttf", i), Embedded: true, Exists: i != 0, Bold: true, Italic: true, Charset: "unicode", Used: i})
	}
	r.ResourceDetails = append(r.ResourceDetails, ResourceInfo{Kind: "font", ID: 999, FontName: "未嵌入字体"})
	doc, err := BuildReport(t.Context(), r, DocumentOptions{Font: testReportFont(t)})
	if err != nil {
		t.Fatal(err)
	}
	text := analysisModelText(doc)
	for _, want := range []string{"字体明细", "字体名称 / 族名", "嵌入状态 / 文件", "样式 / 字符集", "字体样例0", "字体样例23", "字体族样例", "嵌入文件缺失", "已嵌入", "未嵌入字体", "unicode（默认）", "ID 100", "ID 123", "使用次数为结构扫描计数"} {
		if !strings.Contains(strings.Join(strings.Fields(text), ""), strings.Join(strings.Fields(want), "")) {
			t.Fatalf("字体表格缺少 %q", want)
		}
	}
	if strings.Count(text, "嵌入状态 / 文件") < 2 || strings.Contains(text, "不应展示的图片") || strings.Contains(text, "· 字体 ID") {
		t.Fatal("字体表未重复表头、混入其他资源或残留旧段落")
	}
	compact := strings.Join(strings.Fields(text), "")
	// 跨页续排会在路径片段之间插入重复表头，检查目录及每个文件名。
	if !strings.Contains(compact, "Doc_0/long-font-path/") {
		t.Fatal("字体长路径目录丢失")
	}
	for i := range 24 {
		if !strings.Contains(compact, fmt.Sprintf("font%d.ttf", i)) {
			t.Fatalf("字体文件名丢失：font%d.ttf", i)
		}
	}
	r.ResourceDetails = []ResourceInfo{{Kind: "image"}}
	doc, err = BuildReport(t.Context(), r, DocumentOptions{Font: testReportFont(t)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(analysisModelText(doc), "字体明细") {
		t.Fatal("无字体资源时不应显示空表")
	}
}

func TestFlowAnalysisPDFKeepsPaginationAndChineseText(t *testing.T) {
	opts := DocumentOptions{Font: testReportFont(t)}
	r := sampleAnalysisReport()
	doc, err := BuildReport(t.Context(), r, opts)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RenderPDF(&output, r, opts); err != nil {
		t.Fatal(err)
	}
	pdf, err := api.ReadAndValidate(t.Context(), bytes.NewReader(output.Bytes()), model.NewDefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	if pdf.PageCount != len(doc.Pages) {
		t.Fatalf("页数不一致: ofd=%d pdf=%d", len(doc.Pages), pdf.PageCount)
	}
	tool, err := exec.LookPath("pdftotext")
	if err != nil {
		t.Log("未安装 pdftotext，省略外部文字提取检查")
		return
	}
	path := filepath.Join(t.TempDir(), "analysis.pdf")
	if err := os.WriteFile(path, output.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	text, err := exec.CommandContext(t.Context(), tool, "-enc", "UTF-8", path, "-").CombinedOutput()
	if err != nil {
		t.Fatalf("提取 PDF 文字: %v %s", err, text)
	}
	for _, want := range []string{"OFD 分析报告", "中文分析样例.ofd", "资源与引用", "字体明细", "测试字体族", "嵌入文件缺失", "中文附件", "缺少吊销证据", "OFD 包目录结构", "结论摘要", "检查阶段", "结构化诊断", "URI 动作", "附件大小声明异常", "资源可达性"} {
		if !strings.Contains(string(text), want) {
			t.Fatalf("PDF 文字层缺少 %q", want)
		}
	}
	if !strings.Contains(strings.Join(strings.Fields(string(text)), ""), fmt.Sprintf("共%d页", len(doc.Pages))) {
		t.Fatal("PDF 页脚缺少最终总页数")
	}
}

func TestFlowAnalysisOnlySupplementsMessagesNotCoveredBySameSeverityDiagnostics(t *testing.T) {
	for _, allCovered := range []bool{false, true} {
		r := sampleAnalysisReport()
		r.Diagnostics = []Diagnostic{
			{Code: "test.warning", Severity: "warning", Stage: "resources", Message: "已覆盖警告"},
			{Code: "test.error", Severity: "error", Stage: "resources", Message: "已覆盖错误"},
			{Code: "test.info", Severity: "info", Stage: "resources", Message: "仅提示同文"},
		}
		r.Warnings, r.Errors = []string{"已覆盖警告"}, []string{"已覆盖错误"}
		if !allCovered {
			r.Warnings = append(r.Warnings, "补充警告", "仅提示同文", "已覆盖错误")
			r.Errors = append(r.Errors, "补充错误")
		}
		doc, err := BuildReport(t.Context(), r, DocumentOptions{Font: testReportFont(t)})
		if err != nil {
			t.Fatal(err)
		}
		text := analysisModelText(doc)
		if strings.Count(text, "已覆盖警告") != 1 {
			t.Fatal("同级警告重复展示")
		}
		if allCovered {
			if strings.Count(text, "已覆盖错误") != 1 || strings.Contains(text, "警告（") || strings.Contains(text, "错误（") {
				t.Fatal("已覆盖消息重复展示或残留空标题")
			}
		} else {
			for _, want := range []string{"警告（3 条）", "错误（1 条）", "补充警告", "补充错误"} {
				if !strings.Contains(text, want) {
					t.Fatalf("补充消息缺少 %q", want)
				}
			}
			if strings.Count(text, "仅提示同文") != 2 || strings.Count(text, "已覆盖错误") != 2 {
				t.Fatal("不同级别的同文消息不应被过滤")
			}
		}
		if len(r.Warnings) != 1 && allCovered || len(r.Warnings) != 4 && !allCovered || r.Warnings[0] != "已覆盖警告" || r.Errors[0] != "已覆盖错误" {
			t.Fatal("排版不应修改原始报告消息")
		}
	}
}

func TestFlowAnalysisSummaryAndStagesUseCompactTablesBeforeDetails(t *testing.T) {
	r := sampleAnalysisReport()
	r.Conclusions = []string{"摘要首条", "摘要末条"}
	r.Stages = []StageResult{{Name: "pages", Status: "complete"}, {Name: "resources", Status: "partial", Reason: "部分资源读取失败"}, {Name: "attachments", Status: "failed", Reason: "无法读取附件"}, {Name: "templates", Status: "skipped", Reason: "用户关闭此阶段"}, {Name: "future_stage", Status: "skipped"}}
	doc, err := BuildReport(t.Context(), r, DocumentOptions{Font: testReportFont(t)})
	if err != nil {
		t.Fatal(err)
	}
	text := analysisModelText(doc)
	for _, want := range []string{"序号", "分析结论", "摘要首条", "摘要末条", "检查项目", "范围 / 说明", "页面内容", "资源统计", "附件检查", "模板引用", "future_stage", "执行状态不等于校验结论"} {
		if !strings.Contains(text, want) {
			t.Fatalf("缺少表格内容 %q", want)
		}
	}
	if strings.Contains(text, "XSD") {
		t.Fatal("OFD/PDF 报告残留 XSD 提示")
	}
	if strings.Contains(text, "结论：摘要") || strings.Contains(text, "阶段：pages") {
		t.Fatal("仍在逐条输出重复字段前缀")
	}
	if strings.Count(text, "结论摘要") != 1 || strings.Count(text, "检查阶段") != 1 {
		t.Fatal("摘要或阶段被重复输出")
	}
	previous := -1
	for _, section := range []string{"摘要末条", "统计汇总", "检查阶段", "结构化诊断"} {
		position := strings.Index(text, section)
		if position <= previous {
			t.Fatal("摘要、统计及明细顺序错误")
		}
		previous = position
	}
}

func TestFlowAnalysisDiagnosticsTablePreservesLongLocationsAndRepeatsHeaders(t *testing.T) {
	r := sampleAnalysisReport()
	document, page := 0, 3
	r.Diagnostics = nil
	for i := range 18 {
		r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: fmt.Sprintf("resource.issue_%d", i), Severity: "warning", Stage: "resources",
			Message: fmt.Sprintf("诊断问题 %d", i), DocumentIndex: &document, PageNumber: &page, ObjectID: "42", File: "Doc_0/" + strings.Repeat("long-path/", 20) + "Resource.xml",
			Location: "/Document[1]/Resources[1]/Font[2]", Basis: "GB/T 33190—2016 第 9 章"})
	}
	doc, err := BuildReport(t.Context(), r, DocumentOptions{Font: testReportFont(t)})
	if err != nil {
		t.Fatal(err)
	}
	text := analysisModelText(doc)
	for _, want := range []string{"结构化诊断（18 条）", "编号 / 级别", "代码 / 阶段", "问题说明与定位", "诊断问题 0", "诊断问题 17", "文档体 0", "第 3 页", "对象 ID 42", "Resource.xml", "/Document[1]/Resources[1]/Font[2]", "GB/T 33190—2016 第 9 章"} {
		if !strings.Contains(text, want) {
			t.Fatalf("诊断排版丢失 %q", want)
		}
	}
	if strings.Count(text, "问题说明与定位") < 2 {
		t.Fatal("诊断表跨页未重复表头")
	}
	if strings.Contains(text, "说明 / 依据：") || strings.Contains(text, "代码 / 级别：") {
		t.Fatal("仍使用重复标签堆叠的段落")
	}
}

func TestFlowAnalysisBuildPreservesCancellationAndRejectsBadFont(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := BuildReport(ctx, Report{}, DocumentOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消未传递: %v", err)
	}
	if _, err := BuildReport(nil, Report{}, DocumentOptions{}); err == nil {
		t.Fatal("未拒绝空上下文")
	}
	var output bytes.Buffer
	if err := RenderOFD(&output, Report{}, DocumentOptions{Font: filepath.Join(t.TempDir(), "missing.ttf")}); err == nil || output.Len() != 0 {
		t.Fatalf("缺失字体未报错或提前输出: %v", err)
	}
	if err := RenderOFD(nil, Report{}, DocumentOptions{}); err == nil {
		t.Fatal("未拒绝空输出")
	}
	if err := RenderPDF(nil, Report{}, PDFOptions{}); err == nil {
		t.Fatal("未拒绝空输出")
	}
}
