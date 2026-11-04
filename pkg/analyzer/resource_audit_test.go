package analyzer

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestResourceAuditReportsRemainIndependentOfDocumentValidation(t *testing.T) {
	report, err := Analyze(makeArchive(t, resourceAuditFiles()))
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range report.Stages {
		if stage.Name == "validation" {
			t.Fatal("分析器不应包含规范校验执行阶段")
		}
		if stage.Name == "xsd" {
			t.Fatal("分析报告不应包含 XSD 占位阶段")
		}
	}
	var output bytes.Buffer
	if err := RenderJSON(&output, report, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "XSD") || strings.Contains(output.String(), `"xsd"`) {
		t.Fatal("JSON 报告残留 XSD 提示")
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["validation"]; ok {
		t.Fatal("不应输出顶层规范校验报告")
	}
	if _, ok := decoded["resource_audit"]; !ok {
		t.Fatal("移除校验不应影响资源依赖审计")
	}
	for name, render := range map[string]func(io.Writer, Report) error{"text": RenderText, "markdown": RenderMarkdown} {
		t.Run(name, func(t *testing.T) {
			output.Reset()
			if err := render(&output, report); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), "资源可达性") || !strings.Contains(output.String(), "doc[0]/font:51") || strings.Contains(output.String(), "校验结果") {
				t.Fatal("资源报告丢失或混入规范校验结果")
			}
			if strings.Contains(output.String(), "XSD") {
				t.Fatal("报告残留 XSD 提示")
			}
		})
	}
}

func resourceAuditFiles() map[string]string {
	files := diagnosticPackageFiles()
	files["Doc/Document.xml"] = strings.Replace(files["Doc/Document.xml"], "</CommonData>", `<PublicRes>Res.xml</PublicRes><TemplatePage ID="10" BaseLoc="Template.xml"/><TemplatePage ID="11" BaseLoc="DeadTemplate.xml"/></CommonData>`, 1)
	files["Doc/Page.xml"] = `<Page xmlns="http://www.ofdspec.org/2016"><Template TemplateID="10"/><Content><Layer ID="20"><CompositeObject ID="21" ResourceID="30" Boundary="0 0 10 10"/><PathObject ID="22" DrawParam="40" Boundary="0 0 1 1"/></Layer></Content></Page>`
	files["Doc/Template.xml"] = `<Page xmlns="http://www.ofdspec.org/2016"><Content><Layer ID="23"><TextObject ID="24" Font="50" Size="3" Boundary="0 0 1 1"><TextCode>template</TextCode></TextObject></Layer></Content></Page>`
	files["Doc/DeadTemplate.xml"] = `<Page xmlns="http://www.ofdspec.org/2016"><Content><Layer ID="25"><TextObject ID="26" Font="51" Size="3" Boundary="0 0 1 1"><TextCode>dead</TextCode></TextObject></Layer></Content></Page>`
	files["Doc/Res.xml"] = `<Res xmlns="http://www.ofdspec.org/2016" BaseLoc=".">
<Fonts><Font ID="50" FontName="Live"/><Font ID="51" FontName="Dead"/><Font ID="52" FontName="Composite"/></Fonts>
<DrawParams><DrawParam ID="40" Relative="41"/><DrawParam ID="41"><FillColor><Pattern Width="1" Height="1"><CellContent><ImageObject ID="70" ResourceID="60" Boundary="0 0 1 1"/></CellContent></Pattern></FillColor></DrawParam><DrawParam ID="42" Relative="43"/><DrawParam ID="43" Relative="42"/></DrawParams>
<MultiMedias><MultiMedia ID="60" Type="Image"><MediaFile>image.bin</MediaFile></MultiMedia><MultiMedia ID="61" Type="Image"><MediaFile>mask.bin</MediaFile></MultiMedia></MultiMedias>
<CompositeGraphicUnits><CompositeGraphicUnit ID="30" Width="1" Height="1"><Content><CompositeObject ID="71" ResourceID="31" Boundary="0 0 1 1"/></Content></CompositeGraphicUnit><CompositeGraphicUnit ID="31" Width="1" Height="1"><Content><TextObject ID="72" Font="52" Size="3" Boundary="0 0 1 1"><TextCode>composite</TextCode></TextObject><CompositeObject ID="73" ResourceID="30" Boundary="0 0 1 1"/><ImageObject ID="74" ResourceID="60" ImageMask="61" Boundary="0 0 1 1"/></Content></CompositeGraphicUnit></CompositeGraphicUnits></Res>`
	files["Doc/image.bin"], files["Doc/mask.bin"] = "image", "mask"
	return files
}

func auditNode(t *testing.T, report Report, key string) ResourceNode {
	t.Helper()
	for _, node := range report.ResourceAudit.Nodes {
		if node.Key == key {
			return node
		}
	}
	t.Fatalf("缺少资源节点 %s", key)
	return ResourceNode{}
}

func TestResourceAuditFollowsTemplateCompositePatternAndRelativeWithoutCountingDefinitionObjects(t *testing.T) {
	report, err := Analyze(makeArchive(t, resourceAuditFiles()))
	if err != nil {
		t.Fatal(err)
	}
	if report.ResourceAudit.Status != "complete" || report.ResourceAudit.Unobserved != 4 {
		t.Fatalf("闭包或未观测数错误：%+v", report.ResourceAudit)
	}
	for _, key := range []string{"template:10", "font:50", "font:52", "composite:30", "composite:31", "draw-param:40", "draw-param:41", "pattern:1", "image:60", "image:61"} {
		if node := auditNode(t, report, "doc[0]/"+key); !node.Reachable || !node.Declared {
			t.Fatalf("间接资源被误判未使用：%+v", node)
		}
	}
	for _, key := range []string{"template:11", "font:51", "draw-param:42", "draw-param:43"} {
		if node := auditNode(t, report, "doc[0]/"+key); node.Reachable || !node.Declared {
			t.Fatalf("孤立定义或继承环被误认为已使用：%+v", node)
		}
	}
	if report.Fonts.Unused != 1 || report.DrawParams.Unused != 2 || report.Images.Unused != 0 || report.Templates.Unused != 1 {
		t.Fatalf("分类未使用统计未使用闭包：%+v %+v", report.Fonts, report.DrawParams)
	}
	if report.Summary.PageObjects != 2 || report.Summary.PageTextCharacters != 0 || report.Text.UnicodeCodePoints != 0 {
		t.Fatal("定义内部对象污染页面统计")
	}
}

func TestResourceAuditMarksSkippedScopeAndSeparatesDocuments(t *testing.T) {
	files := resourceAuditFiles()
	files["OFD.xml"] = strings.Replace(files["OFD.xml"], "</OFD>", `<DocBody><DocInfo><DocID>second</DocID></DocInfo><DocRoot>Second/Document.xml</DocRoot></DocBody></OFD>`, 1)
	files["Second/Document.xml"] = `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>90</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea><PublicRes>Res.xml</PublicRes></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages></Document>`
	files["Second/Page.xml"] = `<Page xmlns="http://www.ofdspec.org/2016"/>`
	files["Second/Res.xml"] = `<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><Fonts><Font ID="52" FontName="SameID"/></Fonts></Res>`
	report, err := Analyze(makeArchive(t, files), WithTemplates(false), WithAnnotations(false))
	if err != nil {
		t.Fatal(err)
	}
	if report.ResourceAudit.Status != "partial" || !auditNode(t, report, "doc[0]/font:52").Reachable || auditNode(t, report, "doc[1]/font:52").Reachable {
		t.Fatalf("跳过范围或文档隔离错误：%+v", report.ResourceAudit)
	}
	for _, node := range report.ResourceAudit.Nodes {
		if node.Kind == "template" {
			t.Fatal("关闭模板后仍计入模板引用")
		}
	}
}

func TestResourceAuditIncludesClippingFontGradientSpaceAndMultimediaActions(t *testing.T) {
	files := diagnosticPackageFiles()
	files["Doc/Document.xml"] = strings.Replace(files["Doc/Document.xml"], "</CommonData>", `<PublicRes>Res.xml</PublicRes></CommonData>`, 1)
	files["Doc/Page.xml"] = `<Page xmlns="http://www.ofdspec.org/2016"><Actions><Action Event="PO"><Sound ResourceID="8"/></Action></Actions><Content><Layer ID="2"><PathObject ID="3" Boundary="0 0 1 1"><Clips><Clip><Area><Text Font="9" Size="3"><TextCode>x</TextCode></Text></Area></Clip></Clips><FillColor><AxialShd StartPoint="0 0" EndPoint="1 1"><Segment><Color ColorSpace="10" Value="0"/></Segment></AxialShd></FillColor></PathObject></Layer></Content></Page>`
	files["Doc/Res.xml"] = `<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><Fonts><Font ID="9" FontName="Clip"/></Fonts><ColorSpaces><ColorSpace ID="10" Type="GRAY"/></ColorSpaces><MultiMedias><MultiMedia ID="8" Type="Audio"><MediaFile>audio.bin</MediaFile></MultiMedia></MultiMedias></Res>`
	files["Doc/audio.bin"] = "audio"
	report, err := Analyze(makeArchive(t, files))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"font:9", "color-space:10", "image:8"} {
		if !auditNode(t, report, "doc[0]/"+key).Reachable {
			t.Fatalf("遗漏引用 %s", key)
		}
	}
	if report.Summary.PageObjects != 1 || report.Summary.PageTextCharacters != 0 {
		t.Fatal("裁剪文字污染正文统计")
	}
}

func TestResourceAuditReportsUnresolvedReferenceInsideReachableDefinition(t *testing.T) {
	files := resourceAuditFiles()
	files["Doc/Res.xml"] = strings.Replace(files["Doc/Res.xml"], `Font="52"`, `Font="999"`, 1)
	report, err := Analyze(makeArchive(t, files))
	if err != nil {
		t.Fatal(err)
	}
	node := auditNode(t, report, "doc[0]/font:999")
	if node.Declared || !node.Reachable {
		t.Fatalf("未解析资源被误报：%+v", node)
	}
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code == "resource.unresolved_dependency" && strings.Contains(diagnostic.Message, "font:999") {
			return
		}
	}
	t.Fatal("缺少内部资源引用诊断")
}

func TestResourceAuditDoesNotClaimCompletenessWhenResourceDefinitionFileIsMissing(t *testing.T) {
	files := resourceAuditFiles()
	delete(files, "Doc/Res.xml")
	report, err := Analyze(makeArchive(t, files))
	if err != nil {
		t.Fatal(err)
	}
	if report.ResourceAudit.Status != "partial" || stageByName(t, report, "resources").Status != "partial" {
		t.Fatalf("资源定义缺失却声称审计完整：%+v", report.ResourceAudit)
	}
}
