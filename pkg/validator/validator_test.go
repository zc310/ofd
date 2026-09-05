package validator

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestValidateMinimalPackage(t *testing.T) {
	archiveData := makeArchive(t, map[string]string{
		"OFD.xml": `<?xml version="1.0" encoding="UTF-8"?>
<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD">
  <DocBody>
    <DocInfo><DocID>minimal</DocID></DocInfo>
    <DocRoot>Doc_0/Document.xml</DocRoot>
  </DocBody>
</OFD>`,
		"Doc_0/Document.xml": `<?xml version="1.0" encoding="UTF-8"?>
<Document xmlns="http://www.ofdspec.org/2016">
  <CommonData>
    <MaxUnitID>1</MaxUnitID>
    <PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea>
  </CommonData>
  <Pages><Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></Pages>
</Document>`,
		"Doc_0/Pages/Page_0/Content.xml": `<?xml version="1.0" encoding="UTF-8"?>
<Page xmlns="http://www.ofdspec.org/2016"/>`,
	})

	validator, err := New()
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "minimal.ofd")
	if report.Status != StatusValid {
		t.Fatalf("status = %s, issues = %+v", report.Status, report.Issues)
	}
	if report.Summary.Errors != 0 {
		t.Fatalf("unexpected errors: %+v", report.Issues)
	}
}

func TestValidateRejectsReferenceEscape(t *testing.T) {
	archiveData := makeArchive(t, map[string]string{
		"OFD.xml": `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>x</DocID></DocInfo><DocRoot>../Document.xml</DocRoot></DocBody></OFD>`,
	})
	validator, err := New()
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "escape.ofd")
	if !report.HasErrors() {
		t.Fatalf("expected reference error, report = %+v", report)
	}
	for _, issue := range report.Issues {
		if issue.Code == "reference.path_escape" {
			return
		}
	}
	t.Fatalf("missing path escape issue: %+v", report.Issues)
}

func TestValidateMarksXMLLimitFailure(t *testing.T) {
	archiveData := makeArchive(t, map[string]string{
		"OFD.xml":      `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>x</DocID></DocInfo><DocRoot>Document.xml</DocRoot></DocBody></OFD>`,
		"Document.xml": `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>1</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages></Document>`,
		"Page.xml":     `<Page xmlns="http://www.ofdspec.org/2016"/>`,
	})
	validator, err := New(WithMode(ModeStructural), WithMaxXMLBytes(1))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "xml-limit.ofd")
	if report.Checks[1].Status != "failed" {
		t.Fatalf("XML check status = %q, want failed; report = %+v", report.Checks[1].Status, report)
	}
}

func TestValidateRejectsTooManyEntries(t *testing.T) {
	archiveData := makeArchive(t, map[string]string{
		"OFD.xml": `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"/>`,
		"extra":   "data",
	})
	validator, err := New(WithMaxEntries(1))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "entries.ofd")
	if report.Checks[0].Status != "failed" || report.Summary.Errors == 0 {
		t.Fatalf("entry limit was not reported: %+v", report)
	}
}

func TestSemanticChecksUnresolvedResourceID(t *testing.T) {
	archiveData := makeArchive(t, map[string]string{
		"OFD.xml":      `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>x</DocID></DocInfo><DocRoot>Document.xml</DocRoot></DocBody></OFD>`,
		"Document.xml": `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>2</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages></Document>`,
		"Page.xml":     `<Page xmlns="http://www.ofdspec.org/2016"><Content><Layer ID="1"><ImageObject ID="2" Boundary="0 0 1 1" ResourceID="9"/></Layer></Content></Page>`,
	})
	validator, err := New(WithMode(ModeStructural))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "semantic.ofd")
	for _, issue := range report.Issues {
		if issue.Code == "semantic.unresolved_id" && issue.Stage == StageSemantic {
			return
		}
	}
	t.Fatalf("missing unresolved resource ID issue: %+v", report.Issues)
}

func TestScanXMLChecksReferencesFromUnreferencedOFDDocument(t *testing.T) {
	archiveData := makeArchive(t, map[string]string{
		"OFD.xml":      `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>x</DocID></DocInfo><DocRoot>Document.xml</DocRoot></DocBody></OFD>`,
		"Document.xml": `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>1</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages></Document>`,
		"Page.xml":     `<Page xmlns="http://www.ofdspec.org/2016"/>`,
		"Extra.xml":    `<Extensions xmlns="http://www.ofdspec.org/2016"><Extension AppName="test" RefId="1"><ExtendData>Missing.bin</ExtendData></Extension></Extensions>`,
	})
	validator, err := New(WithMode(ModeStructural))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "scan.ofd")
	for _, issue := range report.Issues {
		if issue.Code == "reference.missing" && issue.File == "Extra.xml" {
			return
		}
	}
	t.Fatalf("unreferenced OFD XML was not scanned for references: %+v", report.Issues)
}

func TestScanXMLIgnoresForeignDocumentReferences(t *testing.T) {
	archiveData := makeArchive(t, map[string]string{
		"OFD.xml":      `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>x</DocID></DocInfo><DocRoot>Document.xml</DocRoot></DocBody></OFD>`,
		"Document.xml": `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>1</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages></Document>`,
		"Page.xml":     `<Page xmlns="http://www.ofdspec.org/2016"/>`,
		"Extra.xml":    `<Extra xmlns="urn:example"><Link>Missing.bin</Link></Extra>`,
	})
	validator, err := New(WithMode(ModeStructural))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "foreign.ofd")
	if report.HasErrors() {
		t.Fatalf("foreign XML should not create OFD reference errors: %+v", report.Issues)
	}
}

func TestReferencesIgnoreForeignNestedElements(t *testing.T) {
	archiveData := makeArchive(t, map[string]string{
		"OFD.xml":        `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>x</DocID></DocInfo><DocRoot>Document.xml</DocRoot></DocBody></OFD>`,
		"Document.xml":   `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>1</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages><Extensions>Extensions.xml</Extensions></Document>`,
		"Page.xml":       `<Page xmlns="http://www.ofdspec.org/2016"/>`,
		"Extensions.xml": `<Extensions xmlns="http://www.ofdspec.org/2016" xmlns:ext="urn:example"><ext:ExtendData>Missing.bin</ext:ExtendData></Extensions>`,
	})
	validator, err := New(WithMode(ModeStructural))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "foreign-nested.ofd")
	for _, issue := range report.Issues {
		if issue.Code == "reference.missing" && issue.File == "Extensions.xml" {
			t.Fatalf("foreign nested element created a reference error: %+v", report.Issues)
		}
	}
}

func TestResourceBaseLocResolvesMediaFromResourceDirectory(t *testing.T) {
	archiveData := makeArchive(t, map[string]string{
		"OFD.xml":               `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>x</DocID></DocInfo><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`,
		"Doc_0/Document.xml":    `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>1</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea><DocumentRes>DocumentRes.xml</DocumentRes></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages></Document>`,
		"Doc_0/Page.xml":        `<Page xmlns="http://www.ofdspec.org/2016"/>`,
		"Doc_0/DocumentRes.xml": `<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="Res"><MultiMedias><MultiMedia ID="1" Type="Image"><MediaFile>image.png</MediaFile></MultiMedia></MultiMedias></Res>`,
		"Doc_0/Res/image.png":   "image",
	})
	validator, err := New(WithMode(ModeStructural))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "resource-base.ofd")
	if report.HasErrors() {
		t.Fatalf("resource BaseLoc should resolve media from Res directory: %+v", report.Issues)
	}
}

func TestAttachmentFileLocSupportsDocumentDirectoryBase(t *testing.T) {
	archiveData := makeArchive(t, map[string]string{
		"OFD.xml":                            `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>x</DocID></DocInfo><DocRoot>Doc_2/Document.xml</DocRoot></DocBody></OFD>`,
		"Doc_2/Document.xml":                 `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>1</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages><Attachments>Attachs/Attachments.xml</Attachments></Document>`,
		"Doc_2/Page.xml":                     `<Page xmlns="http://www.ofdspec.org/2016"/>`,
		"Doc_2/Attachs/Attachments.xml":      `<Attachments xmlns="http://www.ofdspec.org/2016"><Attachment ID="1" Name="invoice.xml"><FileLoc>Attachs/original_invoice.xml</FileLoc></Attachment></Attachments>`,
		"Doc_2/Attachs/original_invoice.xml": `<invoice/>`,
	})
	validator, err := New(WithMode(ModeStructural))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "attachment-base.ofd")
	if report.HasErrors() {
		t.Fatalf("attachment FileLoc should support document directory base: %+v", report.Issues)
	}
}

func TestRenderJSONIncludesChineseLabels(t *testing.T) {
	archiveData := makeArchive(t, map[string]string{
		"OFD.xml": `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>x</DocID></DocInfo><DocRoot>../Document.xml</DocRoot></DocBody></OFD>`,
	})
	validator, err := New(WithMode(ModeStructural))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "labels.ofd")
	var output bytes.Buffer
	if err := RenderJSON(&output, report, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"status_zh"`) || !strings.Contains(output.String(), "无效") {
		t.Fatalf("JSON report lacks Chinese labels: %s", output.String())
	}
}

func TestReportsIncludeDetectionTime(t *testing.T) {
	report := Report{
		Input:     InputInfo{Path: "sample.ofd"},
		Status:    StatusValid,
		StartedAt: time.Date(2026, time.September, 5, 10, 20, 30, 0, time.UTC),
		Checks: []CheckResult{
			{Name: "xsd", Status: "failed"},
		},
	}
	const wantText = "检测时间：2026-09-05 10:20:30"

	var textOutput bytes.Buffer
	if err := RenderText(&textOutput, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOutput.String(), wantText) {
		t.Fatalf("text report lacks detection time: %s", textOutput.String())
	}

	var markdownOutput bytes.Buffer
	if err := RenderMarkdown(&markdownOutput, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markdownOutput.String(), "- 检测时间：`2026-09-05 10:20:30`") {
		t.Fatalf("Markdown report lacks detection time: %s", markdownOutput.String())
	}
	if !strings.Contains(markdownOutput.String(), "| XSD | **未通过** |") {
		t.Fatalf("Markdown report does not bold failed check status: %s", markdownOutput.String())
	}

	var jsonOutput bytes.Buffer
	if err := RenderJSON(&jsonOutput, report, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jsonOutput.String(), `"started_at":"2026-09-05T10:20:30Z"`) {
		t.Fatalf("JSON report lacks detection time: %s", jsonOutput.String())
	}
}

func TestSM3DigestHash(t *testing.T) {
	hashFunc, ok := digestHash("1.2.156.10197.1.401")
	if !ok {
		t.Fatal("SM3 digest method is not supported")
	}
	hasher := hashFunc()
	_, _ = hasher.Write([]byte("abc"))
	if got := hex.EncodeToString(hasher.Sum(nil)); got != "66c7f0f462eeedd9d1f2d46bdc10e4e24167c4875cf2f7a2297da02b8f4ba8e0" {
		t.Fatalf("SM3(abc) = %s", got)
	}
}

func makeArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
