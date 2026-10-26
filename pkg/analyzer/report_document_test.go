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
	for _, want := range []string{"OFD 分析报告", "中文分析样例.ofd", "资源与引用", "测试字体族", "中文附件", "缺少吊销证据", "OFD 包目录结构"} {
		if !strings.Contains(string(text), want) {
			t.Fatalf("PDF 文字层缺少 %q", want)
		}
	}
	if !strings.Contains(strings.Join(strings.Fields(string(text)), ""), fmt.Sprintf("共%d页", len(doc.Pages))) {
		t.Fatal("PDF 页脚缺少最终总页数")
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
