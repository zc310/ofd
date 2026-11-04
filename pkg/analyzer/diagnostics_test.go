package analyzer

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func diagnosticPackageFiles() map[string]string {
	return map[string]string{
		"OFD.xml":             `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>x</DocID></DocInfo><DocRoot>Doc/Document.xml</DocRoot></DocBody></OFD>`,
		"Doc/Document.xml":    `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>20</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages><Attachments>Attachments.xml</Attachments></Document>`,
		"Doc/Page.xml":        `<Page xmlns="http://www.ofdspec.org/2016"><Content><Layer ID="2"/></Content></Page>`,
		"Doc/Attachments.xml": `<Attachments xmlns="http://www.ofdspec.org/2016"><Attachment ID="3" Name="sample" Size="1"><FileLoc>asset.bin</FileLoc></Attachment></Attachments>`,
		"Doc/asset.bin":       strings.Repeat("a", 1024),
	}
}

func stageByName(t *testing.T, report Report, name string) StageResult {
	t.Helper()
	for _, stage := range report.Stages {
		if stage.Name == name {
			return stage
		}
	}
	t.Fatalf("缺少阶段 %s", name)
	return StageResult{}
}

func TestAttachmentSizeUsesKBAndFindingsDoNotMakeAnalysisPartial(t *testing.T) {
	for _, test := range []struct {
		size, check string
		diagnostics int
	}{
		{"1", "match", 0}, {"0.5", "mismatch", 1}, {"-1", "invalid", 1}, {"NaN", "invalid", 1}, {"INF", "invalid", 1}, {"", "not_checked", 0},
	} {
		t.Run(test.size, func(t *testing.T) {
			files := diagnosticPackageFiles()
			files["Doc/Attachments.xml"] = strings.ReplaceAll(files["Doc/Attachments.xml"], `Size="1"`, `Size="`+test.size+`"`)
			if test.size == "" {
				files["Doc/Attachments.xml"] = strings.ReplaceAll(files["Doc/Attachments.xml"], ` Size=""`, "")
			}
			report, err := Analyze(makeArchive(t, files))
			if err != nil {
				t.Fatal(err)
			}
			if report.Status != StatusComplete || stageByName(t, report, "attachments").Status != "complete" {
				t.Fatalf("文档问题不应导致分析未完成：%+v", report)
			}
			if len(report.Attachments) != 1 || report.Attachments[0].SizeCheck != test.check || len(report.Diagnostics) != test.diagnostics {
				t.Fatalf("大小诊断错误：%+v %+v", report.Attachments, report.Diagnostics)
			}
			if test.diagnostics > 0 {
				d := report.Diagnostics[0]
				if d.DocumentIndex == nil || *d.DocumentIndex != 0 || d.File != "Doc/Attachments.xml" || d.ObjectID != "3" || d.Basis == "" || len(report.Warnings) != 1 {
					t.Fatalf("诊断不可定位：%+v", d)
				}
			}
			var output bytes.Buffer
			if err := RenderJSON(&output, report, false); err != nil {
				t.Fatalf("非法声明不能阻止 JSON 报告生成：%v", err)
			}
		})
	}
}

func TestAttachmentMissingIsNotComparedAndFloatToleranceIsExplicit(t *testing.T) {
	files := diagnosticPackageFiles()
	delete(files, "Doc/asset.bin")
	report, err := Analyze(makeArchive(t, files))
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusComplete || report.Attachments[0].SizeCheck != "not_checked" {
		t.Fatalf("缺失文件不得作为零字节比较：%+v", report.Attachments)
	}
	for _, test := range []struct {
		size float64
		want string
	}{
		{0, "match"}, {0.49 / 1024, "match"}, {0.51 / 1024, "mismatch"}, {math.Inf(1), "invalid"},
	} {
		a := analyzer{report: Report{Stages: initialStages()}, warningSet: make(map[string]struct{})}
		info := AttachmentInfo{DeclaredSizeKB: &test.size, Exists: true}
		a.checkAttachmentSize(&info, "Attachments.xml")
		if info.SizeCheck != test.want {
			t.Fatalf("%g: %s != %s", test.size, info.SizeCheck, test.want)
		}
	}
}

func TestURIAuditFindsDocumentOutlinePageAndNestedObjectWithoutReadingAttachments(t *testing.T) {
	files := diagnosticPackageFiles()
	files["Doc/Document.xml"] = strings.Replace(files["Doc/Document.xml"], "</Document>", `<Actions><Action Event="DO"><URI URI="https://example.invalid/doc"/></Action></Actions><Outlines><OutlineElem Title="链接"><Actions><Action Event="DO"><URI URI="relative" Base="https://example.invalid/base/"/></Action></Actions></OutlineElem></Outlines></Document>`, 1)
	files["Doc/Page.xml"] = `<Page xmlns="http://www.ofdspec.org/2016"><Actions><Action Event="PO"><URI URI="#fragment"/></Action></Actions><Content><Layer ID="2"><PageBlock ID="4"><TextObject ID="5" Boundary="0 0 10 10" Font="9" Size="3"><Actions><Action Event="CLICK"><URI URI="//example.invalid/object"/></Action></Actions><TextCode>x</TextCode></TextObject></PageBlock></Layer></Content></Page>`
	files["Doc/asset.bin"] = `<Actions><Action><URI URI="https://must-not-visit.invalid"/></Action></Actions>`
	files["orphan.xml"] = files["Doc/asset.bin"]
	report, err := Analyze(makeArchive(t, files))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Actions) != 4 {
		t.Fatalf("动作数错误：%+v", report.Actions)
	}
	if report.Actions[1].ResolvedURI != "https://example.invalid/base/relative" || report.Actions[2].External || !report.Actions[3].External {
		t.Fatalf("URI 分类或 Base 解析错误：%+v", report.Actions)
	}
	object := report.Actions[3]
	if object.PageNumber == nil || *object.PageNumber != 1 || object.ObjectID != "5" || !strings.Contains(object.Location, "PageBlock[1]/TextObject[1]/Actions[1]/Action[1]/URI[1]") {
		t.Fatalf("动作来源错误：%+v", object)
	}
	for _, diagnostic := range report.Diagnostics {
		if strings.HasPrefix(diagnostic.Code, "action.") && diagnostic.Severity != "info" {
			t.Fatalf("外部地址不应直接判为警告或错误：%+v", diagnostic)
		}
	}
}

func TestURIAuditHonorsOptionalScopeAndKeepsInvalidURIAsDiagnostic(t *testing.T) {
	files := diagnosticPackageFiles()
	files["Doc/Document.xml"] = strings.Replace(files["Doc/Document.xml"], "</CommonData>", `<TemplatePage ID="7" BaseLoc="Template.xml"/></CommonData>`, 1)
	files["Doc/Template.xml"] = `<Page xmlns="http://www.ofdspec.org/2016"><Actions><Action Event="PO"><URI URI="https://example.invalid/template"/></Action></Actions></Page>`
	files["Doc/Page.xml"] = `<Page xmlns="http://www.ofdspec.org/2016"><Actions><Action Event="PO"><URI URI="%xx"/></Action><Action Event="PO"><URI URI="https://example.invalid" Base="%xx"/></Action></Actions></Page>`
	report, err := Analyze(makeArchive(t, files), WithTemplates(false), WithAnnotations(false), WithSignatures(false))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Actions) != 2 || stageByName(t, report, "templates").Status != "skipped" || stageByName(t, report, "signatures").Status != "skipped" || report.Status != StatusComplete {
		t.Fatalf("跳过范围或完成度错误：%+v", report)
	}
	for _, action := range report.Actions {
		if action.ResolvedURI != "" {
			t.Fatalf("非法 URI 不应成功解析：%+v", action)
		}
	}
	if len(report.Warnings) != 2 {
		t.Fatalf("缺少非法 URI 诊断：%+v", report.Diagnostics)
	}
	for _, stage := range report.Stages {
		if stage.Name == "render" || stage.Name == "text_extract" {
			t.Fatal("分析报告不应包含未实现的探测占位阶段")
		}
	}
}

func TestURIAuditReadFailureMarksOnlyExecutionIncomplete(t *testing.T) {
	files := diagnosticPackageFiles()
	files["Doc/Document.xml"] = strings.Replace(files["Doc/Document.xml"], "</CommonData>", `<TemplatePage ID="7" BaseLoc="Missing.xml"/></CommonData>`, 1)
	report, err := Analyze(makeArchive(t, files))
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusPartial || stageByName(t, report, "actions").Status != "partial" {
		t.Fatalf("缺少审计失败状态：%+v", report.Stages)
	}
	failed, err := Analyze([]byte("not a zip"))
	if err == nil || failed.Status != StatusFailed || len(failed.Diagnostics) == 0 || stageByName(t, failed, "pages").Status != "skipped" {
		t.Fatalf("入口错误缺少范围：%+v %v", failed, err)
	}
}

func TestFailedPageIsNotCountedAsParsed(t *testing.T) {
	files := diagnosticPackageFiles()
	files["Doc/Page.xml"] = `<Page xmlns="http://www.ofdspec.org/2016"><Content>`
	report, err := Analyze(makeArchive(t, files))
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusPartial || report.Summary.Pages != 1 || report.Summary.ParsedPages != 0 || report.Documents[0].ParsedPages != 0 || stageByName(t, report, "pages").Status != "partial" {
		t.Fatalf("解析失败页被算作成功：%+v %+v", report.Summary, report.Stages)
	}
}

func TestURIAuditKeepsDuplicateLocationsAndDocumentScope(t *testing.T) {
	files := diagnosticPackageFiles()
	files["OFD.xml"] = strings.Replace(files["OFD.xml"], "</OFD>", `<DocBody><DocInfo><DocID>y</DocID></DocInfo><DocRoot>Second/Document.xml</DocRoot></DocBody></OFD>`, 1)
	files["Second/Document.xml"] = files["Doc/Document.xml"]
	files["Second/Attachments.xml"] = files["Doc/Attachments.xml"]
	files["Second/asset.bin"] = files["Doc/asset.bin"]
	page := `<Page xmlns="http://www.ofdspec.org/2016"><Actions><Action Event="PO"><URI URI="https://example.invalid/"/></Action><Action Event="PO"><URI URI="https://example.invalid/"/></Action></Actions><Other xmlns="urn:foreign"><Actions><Action><URI URI="https://ignored.invalid/"/></Action></Actions></Other></Page>`
	files["Doc/Page.xml"], files["Second/Page.xml"] = page, page
	report, err := Analyze(makeArchive(t, files))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Actions) != 4 || len(report.Diagnostics) != 4 {
		t.Fatalf("重复地址不同位置或文档被合并：%+v", report.Actions)
	}
	for i, action := range report.Actions {
		if action.DocumentIndex != i/2 || action.PageNumber == nil || *action.PageNumber != i/2+1 {
			t.Fatalf("多文档页码定位错误：%+v", action)
		}
	}
	if report.Actions[0].Location == report.Actions[1].Location {
		t.Fatal("未区分同名兄弟动作")
	}
}

func TestFontCharsetDefaultsOnlyInReadableReports(t *testing.T) {
	r := sampleAnalysisReport()
	r.ResourceDetails = []ResourceInfo{{Kind: "font", ID: 1}, {Kind: "font", ID: 2, Charset: "big5"}}
	for _, charset := range []string{"unicode", "big5", "symbol", "vendor-custom", " "} {
		if got := fontCharsetLabel(ResourceInfo{Charset: charset}); got != charset {
			t.Fatalf("显式字符集被改写：%q -> %q", charset, got)
		}
	}
	for name, render := range map[string]func(io.Writer, Report) error{"text": RenderText, "markdown": RenderMarkdown} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if err := render(&out, r); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"unicode（默认）", "big5"} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("字符集缺少 %q", want)
				}
			}
		})
	}
	var out bytes.Buffer
	if err := RenderXLSX(&out, r); err != nil {
		t.Fatal(err)
	}
	book, err := excelize.OpenReader(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	rows, err := book.GetRows("资源")
	if err != nil {
		t.Fatal(err)
	}
	values := make([]string, 0, len(rows))
	for _, row := range rows {
		values = append(values, strings.Join(row, "|"))
	}
	for _, want := range []string{"unicode（默认）", "big5"} {
		if !strings.Contains(strings.Join(values, "\n"), want) {
			t.Fatalf("XLSX 字符集缺少 %q", want)
		}
	}
	out.Reset()
	if err := RenderJSON(&out, r, false); err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ResourceDetails[0].Charset != "" || decoded.ResourceDetails[1].Charset != "big5" {
		t.Fatal("JSON 不应补写字符集默认值或改写声明")
	}
}

func TestDiagnosticDetailsSurviveReportFormats(t *testing.T) {
	files := diagnosticPackageFiles()
	files["Doc/asset.bin"] = "short"
	files["Doc/Page.xml"] = `<Page xmlns="http://www.ofdspec.org/2016"><Actions><Action Event="PO"><URI URI="https://example.invalid/a?x=1&amp;y=2"/></Action></Actions></Page>`
	report, err := Analyze(makeArchive(t, files))
	if err != nil {
		t.Fatal(err)
	}
	for name, render := range map[string]func(io.Writer, Report) error{"text": RenderText, "markdown": RenderMarkdown} {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			if err := render(&output, report); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"结论摘要", "检查阶段", "attachment.size_mismatch", "https://example.invalid/a?x=1&y=2", "分析范围不含实际渲染和文本提取测试"} {
				if !strings.Contains(output.String(), want) {
					t.Fatalf("报告缺少 %s", want)
				}
			}
		})
	}
	var output bytes.Buffer
	if err := RenderJSON(&output, report, true); err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != "1" || len(decoded.Diagnostics) != 2 || decoded.Attachments[0].DeclaredSizeKB == nil || decoded.Actions[0].PageNumber == nil {
		t.Fatalf("JSON 丢失明细：%s", output.String())
	}
	output.Reset()
	if err := RenderXLSX(&output, report); err != nil {
		t.Fatal(err)
	}
	book, err := excelize.OpenReader(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	for _, sheet := range []string{"结论摘要", "检查阶段", "结构化诊断", "URI 动作"} {
		rows, readErr := book.GetRows(sheet)
		if readErr != nil || len(rows) < 5 {
			t.Fatalf("XLSX 缺少 %s: %v", sheet, readErr)
		}
	}
	t.Run("flow", func(t *testing.T) {
		doc, buildErr := BuildReport(t.Context(), report, DocumentOptions{Font: testReportFont(t)})
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		text := analysisModelText(doc)
		for _, want := range []string{"结论摘要", "检查阶段", "attachment.size_mismatch", "URI 动作", "未执行"} {
			if !strings.Contains(text, want) {
				t.Fatalf("flow 报告缺少 %s", want)
			}
		}
	})
}
