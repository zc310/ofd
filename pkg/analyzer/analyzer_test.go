package analyzer

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixturePath(name string) string {
	return filepath.Join("..", "..", "test", "testdata", name)
}

func TestAnalyzeHelloWorld(t *testing.T) {
	report, err := Analyze(fixturePath("helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusComplete {
		t.Fatalf("status = %q, warnings = %v", report.Status, report.Warnings)
	}
	if report.Summary.DocumentBodies != 1 || report.Summary.Pages != 2 || report.Summary.ParsedPages != 2 {
		t.Fatalf("summary = %+v", report.Summary)
	}
	if report.Package.Entries != 9 || report.Package.Files != 5 || report.Package.Directories != 4 || report.Package.XMLFiles != 5 {
		t.Fatalf("package = %+v", report.Package)
	}
	if report.Objects.Total != 10 || report.Objects.Text != 1 || report.Objects.Path != 9 {
		t.Fatalf("objects = %+v", report.Objects)
	}
	if report.Text.UnicodeCodePoints == 0 || report.Text.UTF8Bytes == 0 {
		t.Fatalf("text = %+v", report.Text)
	}
	if report.Fonts.Declared != 1 || report.Fonts.Used != 1 || report.Fonts.UniqueUsed != 1 || report.Fonts.Embedded != 0 {
		t.Fatalf("fonts = %+v", report.Fonts)
	}
	if len(report.ResourceDetails) == 0 || report.ResourceDetails[0].FontName == "" {
		t.Fatalf("font details = %+v", report.ResourceDetails)
	}
	if report.Resources.Files != 1 || report.Summary.Fonts != 1 {
		t.Fatalf("resources = %+v, summary = %+v", report.Resources, report.Summary)
	}
}

func TestAnalyzeResourceFixtures(t *testing.T) {
	drawParamReport, err := Analyze(fixturePath("drawparam.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	if drawParamReport.DrawParams.Declared != 12 || drawParamReport.DrawParams.Used == 0 || drawParamReport.DrawParams.Unresolved != 0 || drawParamReport.DrawParams.InheritanceCycles != 0 {
		t.Fatalf("draw params = %+v", drawParamReport.DrawParams)
	}
	if drawParamReport.ColorSpaces.Declared != 1 || drawParamReport.Fonts.Declared != 1 {
		t.Fatalf("resource summaries = colors %+v fonts %+v", drawParamReport.ColorSpaces, drawParamReport.Fonts)
	}
	if drawParamReport.Resources.Files != 2 {
		t.Fatalf("resource files = %d, want 2", drawParamReport.Resources.Files)
	}
	if !hasIDReference(drawParamReport.IDReferences, "doc[0]/draw-param:20", "doc[0]/draw-param:10", "relative") {
		t.Fatalf("missing DrawParam relative reference: %+v", drawParamReport.IDReferences)
	}

	anoReport, err := Analyze(fixturePath("ano.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	if len(anoReport.Annotations) != 55 {
		t.Fatalf("annotations = %d, want 55", len(anoReport.Annotations))
	}
	if len(anoReport.Warnings) != 4 || anoReport.Status != StatusPartial {
		t.Fatalf("annotation warnings = %v, status = %q", anoReport.Warnings, anoReport.Status)
	}
	if anoReport.Images.Declared != 8 || anoReport.Images.Files != 8 || anoReport.Images.MissingFiles != 0 {
		t.Fatalf("images = %+v", anoReport.Images)
	}
	for _, resource := range anoReport.ResourceDetails {
		if resource.Kind == "image" && (!resource.Exists || !strings.HasPrefix(resource.Path, "Doc_0/Res/")) {
			t.Fatalf("image resource path = %+v", resource)
		}
	}
}

func TestAnalyzeMultiDocumentAttachmentAndScope(t *testing.T) {
	report, err := Analyze(fixturePath("multi_demo.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.DocumentBodies != 3 || report.Summary.Pages != 4 || report.Summary.ParsedPages != 4 {
		t.Fatalf("summary = %+v", report.Summary)
	}
	if report.Package.Entries != 27 || report.Package.Files != 16 || report.Package.Directories != 11 {
		t.Fatalf("package = %+v", report.Package)
	}
	if report.Resources.Files != 6 || report.Fonts.Declared != 6 {
		t.Fatalf("resources = %+v fonts = %+v", report.Resources, report.Fonts)
	}
	if len(report.Attachments) != 1 {
		t.Fatalf("attachments = %+v", report.Attachments)
	}
	attachment := report.Attachments[0]
	if attachment.Path != "Doc_2/Attachs/original_invoice.xml" || !attachment.Exists || attachment.ActualSize == 0 || attachment.Visible {
		t.Fatalf("attachment = %+v", attachment)
	}
	if !hasIDReference(report.IDReferences, "Doc_0/Pages/Page_0/Content.xml", "doc[0]/font:20", "font") {
		t.Fatalf("missing scoped font reference: %+v", report.IDReferences)
	}
	if !hasIDReference(report.IDReferences, "Doc_1/Pages/Page_0/Content.xml", "doc[1]/font:20", "font") {
		t.Fatalf("missing second document scoped font reference: %+v", report.IDReferences)
	}
}

func TestAnalyzeInputFormsAndPackageOption(t *testing.T) {
	data, err := os.ReadFile(fixturePath("helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	fromBytes, err := Analyze(data, WithPackage(false))
	if err != nil {
		t.Fatal(err)
	}
	if fromBytes.Input.Size != int64(len(data)) || fromBytes.Package != (PackageSummary{}) {
		t.Fatalf("bytes input = %+v package = %+v", fromBytes.Input, fromBytes.Package)
	}
	fromReader, err := Analyze(bytes.NewReader(data), WithPackage(false))
	if err != nil {
		t.Fatal(err)
	}
	if fromReader.Summary != fromBytes.Summary || fromReader.Text != fromBytes.Text {
		t.Fatalf("reader report differs: bytes summary %+v reader summary %+v", fromBytes.Summary, fromReader.Summary)
	}
}

func TestRenderJSONIsStableAndDoesNotEscapeUnicode(t *testing.T) {
	report, err := Analyze(fixturePath("helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	var first, second bytes.Buffer
	if err := RenderJSON(&first, report, false); err != nil {
		t.Fatal(err)
	}
	if err := RenderJSON(&second, report, false); err != nil {
		t.Fatal(err)
	}
	if first.String() != second.String() {
		t.Fatalf("unstable or escaped JSON: %s", first.String())
	}
	var unicodeOutput bytes.Buffer
	if err := RenderJSON(&unicodeOutput, Report{Warnings: []string{"你好"}}, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unicodeOutput.String(), "你好") || strings.Contains(unicodeOutput.String(), `\u4f60`) {
		t.Fatalf("JSON escaped Unicode: %s", unicodeOutput.String())
	}
	var decoded Report
	if err := json.Unmarshal(first.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
}

func TestRenderJSONOmitsResourceSpecificFieldsForOtherKinds(t *testing.T) {
	report, err := Analyze(fixturePath("helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"images", "color_spaces", "templates", "composites", "patterns", "resources"} {
		var summary map[string]json.RawMessage
		if err := json.Unmarshal(document[name], &summary); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, exists := summary["embedded"]; exists {
			t.Fatalf("%s unexpectedly contains embedded: %s", name, summary["embedded"])
		}
		if _, exists := summary["inheritance_cycles"]; exists {
			t.Fatalf("%s unexpectedly contains inheritance_cycles: %s", name, summary["inheritance_cycles"])
		}
	}
	var fonts, drawParams map[string]json.RawMessage
	if err := json.Unmarshal(document["fonts"], &fonts); err != nil {
		t.Fatal(err)
	}
	if _, exists := fonts["embedded"]; !exists {
		t.Fatal("fonts omits embedded field")
	}
	if err := json.Unmarshal(document["draw_params"], &drawParams); err != nil {
		t.Fatal(err)
	}
	if _, exists := drawParams["inheritance_cycles"]; !exists {
		t.Fatal("draw_params omits inheritance_cycles field")
	}
}

func TestRenderTextAndMarkdown(t *testing.T) {
	report, err := Analyze(fixturePath("helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	var text, markdown bytes.Buffer
	if err := RenderText(&text, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.String(), "OFD 分析报告") || !strings.Contains(text.String(), "页面") || !strings.Contains(text.String(), "资源") {
		t.Fatalf("text report = %s", text.String())
	}
	if !strings.Contains(text.String(), "模板：0  复合图元：0  图案：0") {
		t.Fatalf("text definition summary = %s", text.String())
	}
	if !strings.Contains(text.String(), "字体：文件 0，声明 1，被引用 1，嵌入 0") {
		t.Fatalf("text font summary = %s", text.String())
	}
	if err := RenderMarkdown(&markdown, report); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(markdown.String(), "# OFD 分析报告") || !strings.Contains(markdown.String(), "| 文档体 |") || !strings.Contains(markdown.String(), "## 页面") {
		t.Fatalf("markdown report = %s", markdown.String())
	}
	if !strings.Contains(markdown.String(), "| 单位 | 方向 |") || !strings.Contains(markdown.String(), "| mm | 纵向 |") || !strings.Contains(markdown.String(), "是") || strings.Contains(markdown.String(), "portrait") || strings.Contains(markdown.String(), "true") || strings.Contains(markdown.String(), "false") {
		t.Fatalf("markdown labels = %s", markdown.String())
	}
	if !strings.Contains(markdown.String(), "## 字体明细") || !strings.Contains(markdown.String(), "字体名称") {
		t.Fatalf("markdown font details = %s", markdown.String())
	}
	if !strings.Contains(markdown.String(), "| 模板 |") || !strings.Contains(markdown.String(), "| 复合图元 |") || !strings.Contains(markdown.String(), "| 图案 |") {
		t.Fatalf("markdown definition resources = %s", markdown.String())
	}
	if !strings.Contains(markdown.String(), "## 定义资源统计") || !strings.Contains(markdown.String(), "| 类型 | 定义数 | 引用次数 | 唯一引用 | 无法解析引用 |") {
		t.Fatalf("markdown definition summary = %s", markdown.String())
	}
	if !strings.Contains(markdown.String(), "## 字体统计") || !strings.Contains(markdown.String(), "| 1 | 1 | 0 |") || !strings.Contains(markdown.String(), "| 嵌入 |") {
		t.Fatalf("markdown font summary = %s", markdown.String())
	}
	if !strings.Contains(markdown.String(), "| 嵌入 |") || !strings.Contains(markdown.String(), "| 否 |") {
		t.Fatalf("markdown font details = %s", markdown.String())
	}
}

func TestRenderPDF(t *testing.T) {
	report, err := Analyze(fixturePath("helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	font := "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
	if _, err := os.Stat(font); err != nil {
		t.Skipf("test font unavailable: %v", err)
	}
	var pdf bytes.Buffer
	if err := RenderPDF(&pdf, report, PDFOptions{Font: font}); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdf.Bytes(), []byte("%PDF-")) || pdf.Len() < 1000 {
		t.Fatalf("invalid PDF output: %d bytes", pdf.Len())
	}
}

func TestAnalyzePageResourceUsesPageDirectory(t *testing.T) {
	data := makeArchive(t, map[string]string{
		"OFD.xml":                        `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>x</DocID></DocInfo><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`,
		"Doc_0/Document.xml":             `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>1</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></Pages></Document>`,
		"Doc_0/Pages/Page_0/Content.xml": `<Page xmlns="http://www.ofdspec.org/2016"><PageRes>../../PageRes.xml</PageRes><Content><Layer ID="1"/></Content></Page>`,
		"Doc_0/PageRes.xml":              `<Res xmlns="http://www.ofdspec.org/2016"><Fonts><Font ID="1" FontName="Test"/></Fonts><MultiMedias><MultiMedia ID="2" Type="Image"><MediaFile>missing.png</MediaFile></MultiMedia></MultiMedias></Res>`,
	})
	report, err := Analyze(data)
	if err != nil {
		t.Fatal(err)
	}
	if report.Fonts.Declared != 1 || report.Resources.Files != 1 {
		t.Fatalf("fonts = %+v resources = %+v", report.Fonts, report.Resources)
	}
	if report.Images.MissingFiles != 1 || report.Resources.MissingFiles != 1 {
		t.Fatalf("missing files = images %+v resources %+v", report.Images, report.Resources)
	}
	var missingImage ResourceInfo
	for _, resource := range report.ResourceDetails {
		if resource.Kind == "image" {
			missingImage = resource
		}
	}
	if missingImage.Path != "Doc_0/missing.png" || missingImage.Exists {
		t.Fatalf("missing page asset = %+v", missingImage)
	}
	if edge, ok := findFileReference(report.FileReferences, "Doc_0/Pages/Page_0/Content.xml", "Doc_0/PageRes.xml", "page-resource"); !ok || !edge.Exists || edge.Count != 1 {
		t.Fatalf("page resource edge = %+v, found = %v", edge, ok)
	}
}

func TestAnalyzeEmbeddedFont(t *testing.T) {
	data := makeArchive(t, map[string]string{
		"OFD.xml":                        `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>x</DocID></DocInfo><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`,
		"Doc_0/Document.xml":             `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>1</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></Pages></Document>`,
		"Doc_0/Pages/Page_0/Content.xml": `<Page xmlns="http://www.ofdspec.org/2016"><PageRes>../../PageRes.xml</PageRes><Content><Layer ID="1"><TextObject ID="2" Font="1" Size="12"><TextCode>test</TextCode></TextObject></Layer></Content></Page>`,
		"Doc_0/PageRes.xml":              `<Res xmlns="http://www.ofdspec.org/2016"><Fonts><Font ID="1" FontName="Test"><FontFile>font.bin</FontFile></Font></Fonts></Res>`,
		"Doc_0/font.bin":                 "font data",
	})
	report, err := Analyze(data)
	if err != nil {
		t.Fatal(err)
	}
	if report.Fonts.Declared != 1 || report.Fonts.UniqueUsed != 1 || report.Fonts.Embedded != 1 {
		t.Fatalf("font summary = %+v", report.Fonts)
	}
	if len(report.ResourceDetails) != 1 || report.ResourceDetails[0].FontName != "Test" || !report.ResourceDetails[0].Embedded {
		t.Fatalf("font details = %+v", report.ResourceDetails)
	}
}

func TestAnalyzeDrawParamReferencesAndInheritanceCycles(t *testing.T) {
	data := makeArchive(t, map[string]string{
		"OFD.xml":                        `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>x</DocID></DocInfo><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`,
		"Doc_0/Document.xml":             `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>1</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></Pages></Document>`,
		"Doc_0/Pages/Page_0/Content.xml": `<Page xmlns="http://www.ofdspec.org/2016"><PageRes>../../PageRes.xml</PageRes><Content><Layer ID="1" DrawParam="1"/></Content></Page>`,
		"Doc_0/PageRes.xml":              `<Res xmlns="http://www.ofdspec.org/2016"><DrawParams><DrawParam ID="1" Relative="2"/><DrawParam ID="2" Relative="1"/><DrawParam ID="3" Relative="9"/></DrawParams></Res>`,
	})
	report, err := Analyze(data)
	if err != nil {
		t.Fatal(err)
	}
	if report.DrawParams.Declared != 3 || report.DrawParams.References != 4 || report.DrawParams.Used != 3 || report.DrawParams.UniqueUsed != 2 || report.DrawParams.Unresolved != 1 || report.DrawParams.InheritanceCycles != 1 {
		t.Fatalf("draw param summary = %+v", report.DrawParams)
	}
	if !hasIDReference(report.IDReferences, "doc[0]/draw-param:1", "doc[0]/draw-param:2", "relative") || !hasIDReference(report.IDReferences, "doc[0]/draw-param:2", "doc[0]/draw-param:1", "relative") {
		t.Fatalf("missing DrawParam inheritance references: %+v", report.IDReferences)
	}
	var markdown bytes.Buffer
	if err := RenderMarkdown(&markdown, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markdown.String(), "## 绘制参数统计") || !strings.Contains(markdown.String(), "| 3 | 4 | 1 | 1 |") {
		t.Fatalf("draw param markdown summary = %s", markdown.String())
	}
	var text bytes.Buffer
	if err := RenderText(&text, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.String(), "绘制参数：文件 0，定义数 3，引用次数 4，唯一引用 2，无法解析引用 1，继承循环 1") {
		t.Fatalf("draw param text summary = %s", text.String())
	}
	pdfLines := strings.Join(pdfReportLines(report), "\n")
	if !strings.Contains(pdfLines, "绘制参数：定义数 3    引用次数 4    无法解析引用 1    继承循环 1") {
		t.Fatalf("draw param PDF summary = %s", pdfLines)
	}
}

func TestAnalyzePatternDefinitionsAreNotDuplicatedByDrawParamUsage(t *testing.T) {
	data := makeArchive(t, map[string]string{
		"OFD.xml":                        `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>x</DocID></DocInfo><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`,
		"Doc_0/Document.xml":             `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>1</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea><DocumentRes>DocumentRes.xml</DocumentRes></CommonData><Pages><Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></Pages></Document>`,
		"Doc_0/Pages/Page_0/Content.xml": `<Page xmlns="http://www.ofdspec.org/2016"><PageRes>../../DocumentRes.xml</PageRes><Content><Layer ID="1"><PathObject ID="2" DrawParam="1"/><PathObject ID="3" DrawParam="1"/></Layer></Content></Page>`,
		"Doc_0/DocumentRes.xml":          `<Res xmlns="http://www.ofdspec.org/2016"><DrawParams><DrawParam ID="1"><FillColor><Pattern Width="1" Height="1"><CellContent/></Pattern></FillColor></DrawParam></DrawParams></Res>`,
	})

	report, err := Analyze(data)
	if err != nil {
		t.Fatal(err)
	}
	if report.Patterns.Declared != 1 || report.Patterns.Used != 2 || report.Patterns.References != 2 || report.Patterns.UniqueUsed != 1 {
		t.Fatalf("pattern summary = %+v", report.Patterns)
	}
}

func TestAnalyzeTemplatePageResources(t *testing.T) {
	data := makeArchive(t, map[string]string{
		"OFD.xml":                        `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>x</DocID></DocInfo><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`,
		"Doc_0/Document.xml":             `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>1</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea><TemplatePage ID="7" BaseLoc="Templates/T.xml"/></CommonData><Pages><Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></Pages></Document>`,
		"Doc_0/Pages/Page_0/Content.xml": `<Page xmlns="http://www.ofdspec.org/2016"><Template TemplateID="7"/><Content><Layer ID="1"><PathObject ID="2"/></Layer></Content></Page>`,
		"Doc_0/Templates/T.xml":          `<Page xmlns="http://www.ofdspec.org/2016"><PageRes>../TemplateRes.xml</PageRes><Content><Layer ID="1"><TextObject ID="3" Font="2"><TextCode>template text</TextCode></TextObject></Layer></Content></Page>`,
		"Doc_0/TemplateRes.xml":          `<Res xmlns="http://www.ofdspec.org/2016"><Fonts><Font ID="2" FontName="TemplateFont"/></Fonts></Res>`,
	})

	report, err := Analyze(data)
	if err != nil {
		t.Fatal(err)
	}
	if report.Fonts.Declared != 1 || len(report.ResourceDetails) != 1 {
		t.Fatalf("template resources = %+v, details = %+v", report.Fonts, report.ResourceDetails)
	}
	resource := report.ResourceDetails[0]
	if resource.Kind != "font" || resource.Scope != "template" || resource.SourceFile != "Doc_0/TemplateRes.xml" {
		t.Fatalf("template font detail = %+v", resource)
	}
	if report.Summary.TextCharacters != 0 || report.Summary.Objects != 1 {
		t.Fatalf("template content leaked into page summary = %+v", report.Summary)
	}

	withoutTemplates, err := Analyze(data, WithTemplates(false))
	if err != nil {
		t.Fatal(err)
	}
	if withoutTemplates.Templates.Declared != 0 || withoutTemplates.Templates.References != 0 || withoutTemplates.Fonts.Declared != 0 || len(withoutTemplates.ResourceDetails) != 0 {
		t.Fatalf("template analysis was not disabled: templates = %+v, fonts = %+v, details = %+v", withoutTemplates.Templates, withoutTemplates.Fonts, withoutTemplates.ResourceDetails)
	}
	if len(withoutTemplates.Documents) != 1 || withoutTemplates.Documents[0].TemplateCount != 1 {
		t.Fatalf("template metadata was not preserved: %+v", withoutTemplates.Documents)
	}
	if edge, ok := findFileReference(withoutTemplates.FileReferences, "Doc_0/Document.xml", "Doc_0/Templates/T.xml", "template"); !ok || !edge.Exists {
		t.Fatalf("template structural reference was not preserved: %+v, found = %v", edge, ok)
	}
}

func TestAnalyzeDefinitionsWithoutExpansion(t *testing.T) {
	data := makeArchive(t, map[string]string{
		"OFD.xml":                        `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>x</DocID></DocInfo><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`,
		"Doc_0/Document.xml":             `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>1</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea><TemplatePage ID="7" BaseLoc="Templates/T.xml"/><PublicRes>PublicRes.xml</PublicRes></CommonData><Pages><Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></Pages></Document>`,
		"Doc_0/Pages/Page_0/Content.xml": `<Page xmlns="http://www.ofdspec.org/2016"><Template TemplateID="7"/><Content><Layer ID="1"><CompositeObject ID="2" ResourceID="9"/><PathObject ID="5" Fill="true"><FillColor><Pattern Width="1" Height="1"><CellContent/></Pattern></FillColor></PathObject></Layer></Content></Page>`,
		"Doc_0/Templates/T.xml":          `<Page xmlns="http://www.ofdspec.org/2016"><Content><Layer ID="1"><TextObject ID="3" Font="1" Size="12"><TextCode>template text</TextCode></TextObject></Layer></Content></Page>`,
		"Doc_0/PublicRes.xml":            `<Res xmlns="http://www.ofdspec.org/2016"><CompositeGraphicUnits><CompositeGraphicUnit ID="9"><Content><TextObject ID="4" Font="1" Size="12"><TextCode>composite text</TextCode></TextObject></Content></CompositeGraphicUnit></CompositeGraphicUnits></Res>`,
	})
	report, err := Analyze(data)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.TextCharacters != 0 || report.Summary.Objects != 2 {
		t.Fatalf("expanded definition content leaked into page summary: summary = %+v", report.Summary)
	}
	if report.Templates.Declared != 1 || report.Templates.References != 1 || report.Templates.UniqueUsed != 1 {
		t.Fatalf("template summary = %+v", report.Templates)
	}
	if report.Composites.Declared != 1 || report.Composites.References != 1 || report.Composites.UniqueUsed != 1 {
		t.Fatalf("composite summary = %+v", report.Composites)
	}
	if report.Patterns.Declared != 1 || report.Patterns.References != 1 || report.Patterns.UniqueUsed != 1 {
		t.Fatalf("pattern summary = %+v", report.Patterns)
	}
	if report.Resources.Declared != report.Images.Declared+report.Fonts.Declared+report.DrawParams.Declared+report.Composites.Declared+report.ColorSpaces.Declared+report.Templates.Declared+report.Patterns.Declared {
		t.Fatalf("resources total does not include all resource kinds: resources = %+v, templates = %+v, patterns = %+v", report.Resources, report.Templates, report.Patterns)
	}
	if report.Resources.Used != report.Images.Used+report.Fonts.Used+report.DrawParams.Used+report.Composites.Used+report.ColorSpaces.Used+report.Templates.Used+report.Patterns.Used ||
		report.Resources.References != report.Images.References+report.Fonts.References+report.DrawParams.References+report.Composites.References+report.ColorSpaces.References+report.Templates.References+report.Patterns.References ||
		report.Resources.UniqueUsed != report.Images.UniqueUsed+report.Fonts.UniqueUsed+report.DrawParams.UniqueUsed+report.Composites.UniqueUsed+report.ColorSpaces.UniqueUsed+report.Templates.UniqueUsed+report.Patterns.UniqueUsed ||
		report.Resources.Unresolved != report.Images.Unresolved+report.Fonts.Unresolved+report.DrawParams.Unresolved+report.Composites.Unresolved+report.ColorSpaces.Unresolved+report.Templates.Unresolved+report.Patterns.Unresolved ||
		report.Resources.Unused != report.Images.Unused+report.Fonts.Unused+report.DrawParams.Unused+report.Composites.Unused+report.ColorSpaces.Unused+report.Templates.Unused+report.Patterns.Unused {
		t.Fatalf("resources aggregate does not include all resource kinds: resources = %+v", report.Resources)
	}
	if report.Resources.ByType["template"] != report.Templates.Declared || report.Resources.ByType["pattern"] != report.Patterns.Declared {
		t.Fatalf("resources by_type does not include all resource kinds: %+v", report.Resources.ByType)
	}
}

func hasIDReference(edges []ReferenceEdge, from, to, kind string) bool {
	for _, edge := range edges {
		if edge.From == from && edge.To == to && edge.Type == kind && edge.Exists {
			return true
		}
	}
	return false
}

func findFileReference(edges []ReferenceEdge, from, to, kind string) (ReferenceEdge, bool) {
	for _, edge := range edges {
		if edge.From == from && edge.To == to && edge.Type == kind {
			return edge, true
		}
	}
	return ReferenceEdge{}, false
}

func makeArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for name, content := range files {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
