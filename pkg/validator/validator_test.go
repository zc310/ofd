package validator

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	fontpkg "github.com/tdewolff/font"
	"github.com/xuri/excelize/v2"
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

func TestValidateRejectsRawInputOverLimit(t *testing.T) {
	archiveData := makeArchive(t, map[string]string{
		"OFD.xml": `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"/>`,
	})
	validator, err := New(WithMode(ModeStructural), WithMaxInputSize(int64(len(archiveData)-1)))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "input-limit.ofd")
	for _, issue := range report.Issues {
		if issue.Code == "zip.read" {
			return
		}
	}
	t.Fatalf("missing raw input size issue: %+v", report.Issues)
}

func TestValidateAcceptsExactRawInputLimit(t *testing.T) {
	archiveData := makeArchive(t, map[string]string{
		"OFD.xml": `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"/>`,
	})
	validator, err := New(WithMode(ModeStructural), WithMaxInputSize(int64(len(archiveData))))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "exact-input-limit.ofd")
	if report.Checks[0].Status != "passed" || report.Input.Size != int64(len(archiveData)) {
		t.Fatalf("exact input limit rejected: %+v", report)
	}
}

func TestValidateDoesNotUseTotalLimitForRawInput(t *testing.T) {
	ofdXML := `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"/>`
	archiveData := makeArchive(t, map[string]string{"OFD.xml": ofdXML})
	if len(archiveData) <= len(ofdXML) {
		t.Fatalf("test archive size = %d, want greater than decompressed size %d", len(archiveData), len(ofdXML))
	}
	validator, err := New(WithMode(ModeStructural), WithMaxTotalSize(int64(len(ofdXML))))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "raw-vs-total.ofd")
	for _, issue := range report.Issues {
		if issue.Code == "zip.read" || issue.Code == "zip.total_too_large" {
			t.Fatalf("unexpected size issue: %+v", report.Issues)
		}
	}
}

func TestValidateRejectsDecompressedTotalOverLimit(t *testing.T) {
	archiveData := makeArchive(t, map[string]string{
		"OFD.xml": strings.Repeat("A", 16<<10),
	})
	validator, err := New(WithMode(ModeStructural), WithMaxTotalSize(8<<10))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "total-limit.ofd")
	for _, issue := range report.Issues {
		if issue.Code == "zip.total_too_large" {
			return
		}
	}
	t.Fatalf("missing decompressed total-size issue: %+v", report.Issues)
}

func TestValidateRejectsOversizedEntry(t *testing.T) {
	archiveData := makeArchive(t, map[string]string{
		"OFD.xml": "long content",
	})
	validator, err := New(WithMaxFileSize(4))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "file-limit.ofd")
	for _, issue := range report.Issues {
		if issue.Code == "zip.file_too_large" && issue.File == "OFD.xml" {
			return
		}
	}
	t.Fatalf("missing oversized entry issue: %+v", report.Issues)
}

func TestValidateAcceptsExactDecompressedLimits(t *testing.T) {
	ofdXML := `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"/>`
	archiveData := makeArchive(t, map[string]string{"OFD.xml": ofdXML})
	validator, err := New(
		WithMode(ModeStructural),
		WithMaxFileSize(int64(len(ofdXML))),
		WithMaxTotalSize(int64(len(ofdXML))),
	)
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "exact-decompressed-limits.ofd")
	if report.Checks[0].Status != "passed" {
		t.Fatalf("exact decompressed limits rejected: %+v", report)
	}
	for _, issue := range report.Issues {
		if issue.Code == "zip.file_too_large" || issue.Code == "zip.total_too_large" {
			t.Fatalf("exact decompressed limits produced a size issue: %+v", report.Issues)
		}
	}
}

func TestValidateRejectsDuplicateEntries(t *testing.T) {
	archiveData := makeArchiveEntries(t,
		archiveEntry{name: "OFD.xml", content: `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"/>`},
		archiveEntry{name: "data", content: "first"},
		archiveEntry{name: "data", content: "second"},
	)
	validator, err := New(WithMode(ModeStructural))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "duplicate.ofd")
	for _, issue := range report.Issues {
		if issue.Code == "zip.duplicate_entry" && issue.File == "data" {
			return
		}
	}
	t.Fatalf("missing duplicate entry issue: %+v", report.Issues)
}

func TestValidateRejectsNormalizedDuplicateEntries(t *testing.T) {
	archiveData := makeArchiveEntries(t,
		archiveEntry{name: "OFD.xml", content: `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"/>`},
		archiveEntry{name: "a//b", content: "first"},
		archiveEntry{name: "a/./b", content: "second"},
	)
	validator, err := New(WithMode(ModeStructural))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "normalized-duplicate.ofd")
	for _, issue := range report.Issues {
		if issue.Code == "zip.duplicate_entry" && issue.File == "a/b" {
			return
		}
	}
	t.Fatalf("missing normalized duplicate entry issue: %+v", report.Issues)
}

func TestValidateReportsInvalidEntryPath(t *testing.T) {
	t.Setenv("GODEBUG", "zipinsecurepath=0")
	archiveData := makeArchiveEntries(t,
		archiveEntry{name: "OFD.xml", content: `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"/>`},
		archiveEntry{name: "../data", content: "content"},
	)
	validator, err := New(WithMode(ModeStructural))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "invalid-path.ofd")
	for _, issue := range report.Issues {
		if issue.Code == "zip.invalid_path" && issue.File == "../data" {
			return
		}
	}
	t.Fatalf("missing invalid path issue: %+v", report.Issues)
}

func TestValidateRejectsWindowsDriveEntryPath(t *testing.T) {
	t.Setenv("GODEBUG", "zipinsecurepath=0")
	archiveData := makeArchiveEntries(t,
		archiveEntry{name: "OFD.xml", content: `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"/>`},
		archiveEntry{name: "C:/data", content: "content"},
	)
	validator, err := New(WithMode(ModeStructural))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "drive-path.ofd")
	for _, issue := range report.Issues {
		if issue.Code == "zip.invalid_path" && issue.File == "C:/data" {
			return
		}
	}
	t.Fatalf("missing Windows drive path issue: %+v", report.Issues)
}

func TestValidateReportsOtherInvalidEntryPaths(t *testing.T) {
	t.Setenv("GODEBUG", "zipinsecurepath=0")
	for _, name := range []string{"/absolute", `dir\file`, "bad\x00name"} {
		t.Run(name, func(t *testing.T) {
			archiveData := makeArchiveEntries(t,
				archiveEntry{name: "OFD.xml", content: `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"/>`},
				archiveEntry{name: name, content: "content"},
			)
			validator, err := New(WithMode(ModeStructural))
			if err != nil {
				t.Fatal(err)
			}
			report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "invalid-path-variant.ofd")
			for _, issue := range report.Issues {
				if issue.Code == "zip.invalid_path" && issue.File == name {
					return
				}
			}
			t.Fatalf("missing invalid path issue for %q: %+v", name, report.Issues)
		})
	}
}

func TestValidateSkipsDirectoryEntries(t *testing.T) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	root, err := writer.Create("OFD.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.Write([]byte(`<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"/>`)); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Create("directory/"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	validator, err := New(WithMode(ModeStructural))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(buffer.Bytes()), "directory.ofd")
	if report.HasErrors() {
		t.Fatalf("directory entry caused validation errors: %+v", report.Issues)
	}
	if report.Summary.Files != 2 {
		t.Fatalf("file count = %d, want 2", report.Summary.Files)
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

func TestFontChecksRejectUnparseableFont(t *testing.T) {
	archiveData := makeFontArchive(t, `
    <Font ID="1" FontName="Test"><FontFile>font.ttf</FontFile></Font>`, `<TextObject ID="2" Font="1"><TextCode>A</TextCode></TextObject>`, "bad font")
	validator, err := New(WithMode(ModeStructural), WithCheckDigest(false))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "bad-font.ofd")
	if !hasIssueCode(report, "semantic.font_parse_failed") {
		t.Fatalf("missing font parse issue: %+v", report.Issues)
	}
}

func TestFontChecksAcceptValidGlyphAndHonorGlyphCount(t *testing.T) {
	fontData, sfnt := testFont(t)
	archiveData := makeFontArchive(t, `<Font ID="1" FontName="Test"><FontFile>font.ttf</FontFile></Font>`, `<TextObject ID="2" Font="1"><CGTransform CodePosition="0" CodeCount="1" GlyphCount="1"><Glyphs>1 2</Glyphs></CGTransform><TextCode>A</TextCode></TextObject>`, string(fontData))
	validator, err := New(WithMode(ModeStructural), WithCheckDigest(false))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "valid-font.ofd")
	if hasIssueCode(report, "semantic.cgtransform_invalid") {
		t.Fatalf("valid GlyphCount produced an issue: %+v", report.Issues)
	}
	if hasIssueCode(report, "semantic.cgtransform_glyph_missing") || hasIssueCode(report, "semantic.font_missing_glyph") {
		t.Fatalf("valid glyph produced a font issue: %+v", report.Issues)
	}
	if sfnt.NumGlyphs() <= 1 {
		t.Fatalf("test font has too few glyphs: %d", sfnt.NumGlyphs())
	}
}

func TestFontChecksRejectExcessiveGlyphCount(t *testing.T) {
	fontData, _ := testFont(t)
	archiveData := makeFontArchive(t, `<Font ID="1" FontName="Test"><FontFile>font.ttf</FontFile></Font>`, `<TextObject ID="2" Font="1"><CGTransform CodePosition="0" CodeCount="1" GlyphCount="3"><Glyphs>1 2</Glyphs></CGTransform><TextCode>A</TextCode></TextObject>`, string(fontData))
	validator, err := New(WithMode(ModeStructural), WithCheckDigest(false))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "bad-glyph-count.ofd")
	if !hasIssueCode(report, "semantic.cgtransform_invalid") {
		t.Fatalf("missing excessive GlyphCount issue: %+v", report.Issues)
	}
}

func TestFontChecksRejectMissingCGTransformGlyph(t *testing.T) {
	fontData, sfnt := testFont(t)
	invalidGlyph := sfnt.NumGlyphs()
	archiveData := makeFontArchive(t, `<Font ID="1" FontName="Test"><FontFile>font.ttf</FontFile></Font>`, fmt.Sprintf(`<TextObject ID="2" Font="1"><CGTransform CodePosition="0" CodeCount="1" GlyphCount="1"><Glyphs>%d</Glyphs></CGTransform><TextCode>A</TextCode></TextObject>`, invalidGlyph), string(fontData))
	validator, err := New(WithMode(ModeStructural), WithCheckDigest(false))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "bad-glyph.ofd")
	if !hasIssueCode(report, "semantic.cgtransform_glyph_missing") {
		t.Fatalf("missing CGTransform glyph issue: %+v", report.Issues)
	}
}

func TestFontChecksRejectMissingTextGlyph(t *testing.T) {
	fontData, sfnt := testFont(t)
	missing := missingFontRune(sfnt)
	archiveData := makeFontArchive(t, `<Font ID="1" FontName="Test"><FontFile>font.ttf</FontFile></Font>`, fmt.Sprintf(`<TextObject ID="2" Font="1"><TextCode>%c</TextCode></TextObject>`, missing), string(fontData))
	validator, err := New(WithMode(ModeStructural), WithCheckDigest(false))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "missing-text-glyph.ofd")
	if !hasIssueCode(report, "semantic.font_missing_glyph") {
		t.Fatalf("missing text glyph issue: %+v", report.Issues)
	}
}

func TestFontChecksAcceptCharSetAliasAndReportConflict(t *testing.T) {
	archiveData := makeFontArchive(t, `<Font ID="1" FontName="Test" CharSet="prc"/>`, `<TextObject ID="2" Font="1"><TextCode>한</TextCode></TextObject>`, "")
	validator, err := New(WithMode(ModeStructural), WithCheckDigest(false))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "charset.ofd")
	if !hasIssueCode(report, "semantic.font_charset_conflict") {
		t.Fatalf("missing CharSet conflict warning: %+v", report.Issues)
	}
}

func TestFontResourceIDsAreDocumentScoped(t *testing.T) {
	fontData, _ := testFont(t)
	archiveData := makeArchive(t, map[string]string{
		"OFD.xml":               `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>one</DocID></DocInfo><DocRoot>Doc_0/Document.xml</DocRoot></DocBody><DocBody><DocInfo><DocID>two</DocID></DocInfo><DocRoot>Doc_1/Document.xml</DocRoot></DocBody></OFD>`,
		"Doc_0/Document.xml":    `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>1</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea><DocumentRes>DocumentRes.xml</DocumentRes></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages></Document>`,
		"Doc_1/Document.xml":    `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>1</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea><DocumentRes>DocumentRes.xml</DocumentRes></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages></Document>`,
		"Doc_0/DocumentRes.xml": `<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="Res"><Fonts><Font ID="1" FontName="Test"><FontFile>font.ttf</FontFile></Font></Fonts></Res>`,
		"Doc_1/DocumentRes.xml": `<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="Res"><Fonts><Font ID="1" FontName="Test"><FontFile>font.ttf</FontFile></Font></Fonts></Res>`,
		"Doc_0/Page.xml":        `<Page xmlns="http://www.ofdspec.org/2016"><Content><Layer ID="1"><TextObject ID="2" Font="1"><TextCode>A</TextCode></TextObject></Layer></Content></Page>`,
		"Doc_1/Page.xml":        `<Page xmlns="http://www.ofdspec.org/2016"><Content><Layer ID="1"><TextObject ID="2" Font="1"><TextCode>A</TextCode></TextObject></Layer></Content></Page>`,
		"Doc_0/Res/font.ttf":    string(fontData),
		"Doc_1/Res/font.ttf":    "not a font",
	})
	validator, err := New(WithMode(ModeStructural), WithCheckDigest(false))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "scoped-fonts.ofd")
	if hasIssueCode(report, "semantic.duplicate_font_id") {
		t.Fatalf("font IDs in separate documents were treated as duplicates: %+v", report.Issues)
	}
	if !hasIssueFileCode(report, "semantic.font_parse_failed", "Doc_1/DocumentRes.xml") {
		t.Fatalf("second document font was not checked in its own scope: %+v", report.Issues)
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

func TestRenderXLSXCreatesValidWorkbook(t *testing.T) {
	archiveData := makeArchive(t, map[string]string{
		"OFD.xml": `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>x</DocID></DocInfo><DocRoot>../Document.xml</DocRoot></DocBody></OFD>`,
	})
	validator, err := New(WithMode(ModeStructural))
	if err != nil {
		t.Fatal(err)
	}
	report := validator.ValidateReader(context.Background(), bytes.NewReader(archiveData), "labels.ofd")
	var output bytes.Buffer
	if err := RenderXLSX(&output, report); err != nil {
		t.Fatal(err)
	}
	workbook, err := excelize.OpenReader(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatalf("invalid XLSX workbook: %v", err)
	}
	defer func() { _ = workbook.Close() }()
	if sheets := workbook.GetSheetList(); len(sheets) != 2 || sheets[0] != "汇总" || sheets[1] != "问题" {
		t.Fatalf("unexpected sheets: %v", sheets)
	}
	summary, err := workbook.GetCellValue("汇总", "A1")
	if err != nil {
		t.Fatal(err)
	}
	if summary != "OFD 校验报告" {
		t.Fatalf("summary title = %q", summary)
	}
	status, err := workbook.GetCellValue("汇总", "B5")
	if err != nil {
		t.Fatal(err)
	}
	if status == "" {
		t.Fatal("summary status cell is empty")
	}
	issues, err := workbook.GetCellValue("问题", "A4")
	if err != nil {
		t.Fatal(err)
	}
	if issues != "序号" {
		t.Fatalf("issues header = %q", issues)
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
	entries := make([]archiveEntry, 0, len(files))
	for name, content := range files {
		entries = append(entries, archiveEntry{name: name, content: content})
	}
	return makeArchiveEntries(t, entries...)
}

type archiveEntry struct {
	name    string
	content string
}

func makeArchiveEntries(t *testing.T, entries ...archiveEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, item := range entries {
		file, err := writer.Create(item.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(item.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func makeFontArchive(t *testing.T, fontXML, textXML, fontData string) []byte {
	t.Helper()
	files := map[string]string{
		"OFD.xml":               `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>font-test</DocID></DocInfo><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`,
		"Doc_0/Document.xml":    `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>2</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea><DocumentRes>DocumentRes.xml</DocumentRes></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages></Document>`,
		"Doc_0/DocumentRes.xml": `<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="Res"><Fonts>` + fontXML + `</Fonts></Res>`,
		"Doc_0/Page.xml":        `<Page xmlns="http://www.ofdspec.org/2016"><Content><Layer ID="1">` + textXML + `</Layer></Content></Page>`,
	}
	if fontData != "" {
		files["Doc_0/Res/font.ttf"] = fontData
	}
	return makeArchive(t, files)
}

func testFont(t *testing.T) ([]byte, *fontpkg.SFNT) {
	t.Helper()
	for _, name := range []string{
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSerif.ttf",
	} {
		data, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		sfnt, _, parseErr := parseValidatorFont(data)
		if parseErr == nil && sfnt != nil && sfnt.NumGlyphs() > 2 && sfnt.GlyphIndex('A') != 0 {
			return data, sfnt
		}
	}
	if archiveData, err := os.ReadFile("../../test/testdata/intro.ofd"); err == nil {
		if archive, err := zip.NewReader(bytes.NewReader(archiveData), int64(len(archiveData))); err == nil {
			for _, entry := range archive.File {
				if !strings.HasSuffix(entry.Name, "font_83_83.cff") {
					continue
				}
				file, openErr := entry.Open()
				if openErr != nil {
					continue
				}
				data, readErr := io.ReadAll(file)
				_ = file.Close()
				if readErr != nil {
					continue
				}
				sfnt, _, parseErr := parseValidatorFont(data)
				if parseErr == nil && sfnt != nil && sfnt.NumGlyphs() > 2 && sfnt.GlyphIndex('A') != 0 {
					return data, sfnt
				}
			}
		}
	}
	t.Skip("no test TrueType font is available")
	return nil, nil
}

func missingFontRune(sfnt *fontpkg.SFNT) rune {
	for value := rune(0xE000); value <= 0xF8FF; value++ {
		if sfnt.GlyphIndex(value) == 0 {
			return value
		}
	}
	return '\uFFFF'
}

func hasIssueCode(report Report, code string) bool {
	for _, issue := range report.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func hasIssueFileCode(report Report, code, file string) bool {
	for _, issue := range report.Issues {
		if issue.Code == code && issue.File == file {
			return true
		}
	}
	return false
}
