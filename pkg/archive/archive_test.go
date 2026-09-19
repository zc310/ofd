package archive

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"

	"github.com/zc310/ofd/pkg/analyzer"
	"github.com/zc310/ofd/pkg/validator"
)

func TestPrepareAndVerifyCreatesSelfContainedDirectory(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	output := filepath.Join(t.TempDir(), "archive")
	metadata := Metadata{ArchiveCode: "A-1", FondsCode: "F-1"}
	options := DefaultOptions()
	options.ValidatorOptions = []validator.Option{validator.WithMode(validator.ModeStructural)}
	report, err := Prepare(context.Background(), input, output, metadata, "", options)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status == StatusFailed {
		t.Fatalf("unexpected failed report: %+v", report.Issues)
	}
	for _, name := range []string{"original/document.ofd", "metadata/archive.json", "metadata/manifest.json", "reports/check.json", "reports/analysis.json", "fixity/manifest.sha256"} {
		if _, err := os.Stat(filepath.Join(output, filepath.FromSlash(name))); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	verified, err := Verify(output)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Status != StatusPassed {
		t.Fatalf("verify status = %s, issues = %+v", verified.Status, verified.Issues)
	}
}

func TestVerifyDetectsFixityMismatch(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	output := filepath.Join(t.TempDir(), "archive")
	options := DefaultOptions()
	options.ValidatorOptions = []validator.Option{validator.WithMode(validator.ModeStructural)}
	if _, err := Prepare(context.Background(), input, output, Metadata{}, "", options); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(output, "original", "document.ofd")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, 0)
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Verify(output)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusFailed || !strings.Contains(report.Issues[0].Code, "fixity") {
		t.Fatalf("report = %+v", report)
	}
}

func TestVerifyDetectsManifestTampering(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	output := filepath.Join(t.TempDir(), "archive")
	options := DefaultOptions()
	options.ValidatorOptions = []validator.Option{validator.WithMode(validator.ModeStructural)}
	if _, err := Prepare(context.Background(), input, output, Metadata{}, "", options); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(output, "metadata", "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, ' ')
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Verify(output)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusFailed || !strings.Contains(issueCodes(report.Issues), "fixity.mismatch") {
		t.Fatalf("report = %+v", report)
	}
}

func TestVerifyDetectsInvalidStoredCheckReport(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	output := filepath.Join(t.TempDir(), "archive")
	options := DefaultOptions()
	options.ValidatorOptions = []validator.Option{validator.WithMode(validator.ModeStructural)}
	if _, err := Prepare(context.Background(), input, output, Metadata{}, "", options); err != nil {
		t.Fatal(err)
	}
	checkPath := filepath.Join(output, "reports", "check.json")
	if err := os.WriteFile(checkPath, []byte(`{"schema_version":"1","unexpected":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(output); err == nil {
		t.Fatal("expected invalid stored check report error")
	}
}

func TestVerifyDetectsStoredReportSourceMismatch(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	output := filepath.Join(t.TempDir(), "archive")
	options := DefaultOptions()
	options.ValidatorOptions = []validator.Option{validator.WithMode(validator.ModeStructural)}
	if _, err := Prepare(context.Background(), input, output, Metadata{}, "", options); err != nil {
		t.Fatal(err)
	}
	checkPath := filepath.Join(output, "reports", "check.json")
	data, err := os.ReadFile(checkPath)
	if err != nil {
		t.Fatal(err)
	}
	var checkReport Report
	if err := json.Unmarshal(data, &checkReport); err != nil {
		t.Fatal(err)
	}
	checkReport.Input.SHA256 = strings.Repeat("0", 64)
	data, err = json.Marshal(checkReport)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(checkPath, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Verify(output)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusFailed || !strings.Contains(issueCodes(report.Issues), "report.check_source") {
		t.Fatalf("report = %+v", report)
	}
}

func TestBuildManifestRetainsSourceHash(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	options := DefaultOptions()
	options.ValidatorOptions = []validator.Option{validator.WithMode(validator.ModeStructural)}
	manifest, _, err := BuildManifest(context.Background(), input, Metadata{}, "", options)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Source.SHA256 == "" || manifest.Source.Size == 0 {
		t.Fatalf("source = %+v", manifest.Source)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "sha256") {
		t.Fatalf("manifest = %s", data)
	}
}

func TestBuildManifestRejectsUnsupportedProfileExtension(t *testing.T) {
	directory := t.TempDir()
	profile := filepath.Join(directory, "profile.txt")
	if err := os.WriteFile(profile, []byte(`{"required_fields":["archive_code"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	options := DefaultOptions()
	options.ValidatorOptions = []validator.Option{validator.WithMode(validator.ModeStructural)}
	if _, _, err := BuildManifest(context.Background(), input, Metadata{ArchiveCode: "A-1"}, profile, options); err == nil {
		t.Fatal("expected unsupported profile extension error")
	}
}

func TestRenderMarkdownIncludesIssueDetails(t *testing.T) {
	report := Report{
		Status: StatusWarning,
		Input:  FileInfo{Path: "document.ofd", Size: 12, SHA256: strings.Repeat("a", 64)},
		Issues: []Issue{{Severity: "warning", Code: "test.issue", Message: "消息 | 内容", Path: "Document.xml", Hint: "人工确认"}},
		AnalysisReport: analyzer.Report{
			Status:  analyzer.StatusComplete,
			Summary: analyzer.Summary{Pages: 2, ParsedPages: 2},
		},
	}
	var output strings.Builder
	if err := RenderMarkdown(&output, report); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"输入大小：12 字节", "## 分析汇总", "页面：2（已解析 2）", "消息 \\| 内容", "路径：`Document.xml`", "建议：人工确认"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("Markdown output lacks %q: %s", expected, output.String())
		}
	}
}

func TestExtractedAttachmentNamesAreSafe(t *testing.T) {
	if got := uniquePath("../bad/name?.txt", map[string]bool{}); got != "name_.txt" {
		t.Fatalf("name = %q", got)
	}
}

func TestCleanRelativeRejectsBackslash(t *testing.T) {
	if _, err := cleanRelative(`attachments\file.txt`); err == nil {
		t.Fatal("expected backslash path error")
	}
}

func TestScanXMLHonorsSizeLimit(t *testing.T) {
	var summary FeatureSummary
	if err := scanXML(strings.NewReader(`<Document><Action/></Document>`), &summary, 10); err == nil {
		t.Fatal("expected XML size limit error")
	}
}

func TestPrepareRejectsNonEmptyOutputDirectory(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	output := filepath.Join(t.TempDir(), "archive")
	if err := os.MkdirAll(output, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "old.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	options := DefaultOptions()
	options.ValidatorOptions = []validator.Option{validator.WithMode(validator.ModeStructural)}
	if _, err := Prepare(context.Background(), input, output, Metadata{}, "", options); err == nil {
		t.Fatal("expected non-empty output directory error")
	}
}

func TestCommitPreparedDirectoryReplacesEmptyOutput(t *testing.T) {
	parent := t.TempDir()
	output := filepath.Join(parent, "archive")
	staging := filepath.Join(parent, "staging")
	if err := os.MkdirAll(output, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(staging, "metadata"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "metadata", "manifest.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := commitPreparedDirectory(staging, output); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(output, "metadata", "manifest.json")); err != nil {
		t.Fatalf("committed file missing: %v", err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatalf("staging directory still exists, err = %v", err)
	}
	if _, err := os.Stat(output + ".old"); !os.IsNotExist(err) {
		t.Fatalf("backup directory still exists, err = %v", err)
	}
}

func TestCommitPreparedDirectoryRejectsExistingBackup(t *testing.T) {
	parent := t.TempDir()
	output := filepath.Join(parent, "archive")
	staging := filepath.Join(parent, "staging")
	if err := os.MkdirAll(output, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(output+".old", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := commitPreparedDirectory(staging, output); err == nil {
		t.Fatal("expected existing backup error")
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("original output missing: %v", err)
	}
	if _, err := os.Stat(staging); err != nil {
		t.Fatalf("staging directory missing: %v", err)
	}
}

func TestPrepareCopiesProfileAndVerifiesIt(t *testing.T) {
	directory := t.TempDir()
	profile := filepath.Join(directory, "profile.json")
	profileData := []byte(`{"name":"minimum","required_fields":["archive_code"]}`)
	if err := os.WriteFile(profile, profileData, 0o600); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	output := filepath.Join(directory, "archive")
	options := DefaultOptions()
	options.ValidatorOptions = []validator.Option{validator.WithMode(validator.ModeStructural)}
	if _, err := Prepare(context.Background(), input, output, Metadata{ArchiveCode: "A-1"}, profile, options); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(output, "metadata", "profile.json")); err != nil {
		t.Fatal(err)
	}
	verified, err := Verify(output)
	if err != nil || verified.Status != StatusPassed {
		t.Fatalf("verify = %+v, err = %v", verified, err)
	}
}

func TestLoadMetadataRejectsUnknownJSONField(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "archive.json")
	if err := os.WriteFile(filename, []byte(`{"archive_code":"A-1","unknown":"value"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMetadata(filename); err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestVerifyDetectsUnregisteredFile(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	output := filepath.Join(t.TempDir(), "archive")
	options := DefaultOptions()
	options.ValidatorOptions = []validator.Option{validator.WithMode(validator.ModeStructural)}
	if _, err := Prepare(context.Background(), input, output, Metadata{}, "", options); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "unexpected.txt"), []byte("unexpected"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Verify(output)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusFailed {
		t.Fatalf("status = %s, issues = %+v", report.Status, report.Issues)
	}
}

func TestPrepareCopiesAttachmentAndRegistersFixity(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "multi_demo.ofd")
	output := filepath.Join(t.TempDir(), "archive")
	options := DefaultOptions()
	options.ValidatorOptions = []validator.Option{validator.WithMode(validator.ModeStructural)}
	if _, err := Prepare(context.Background(), input, output, Metadata{}, "", options); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(output, "metadata", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Attachments) != 1 || !manifest.Attachments[0].Copied || manifest.Attachments[0].ArchivePath == "" {
		t.Fatalf("attachments = %+v", manifest.Attachments)
	}
	if _, err := os.Stat(filepath.Join(output, filepath.FromSlash(manifest.Attachments[0].ArchivePath))); err != nil {
		t.Fatal(err)
	}
	if report, err := Verify(output); err != nil || report.Status != StatusPassed {
		t.Fatalf("verify = %+v, err = %v", report, err)
	}
}

func TestVerifyRejectsSymlink(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	output := filepath.Join(t.TempDir(), "archive")
	options := DefaultOptions()
	options.ValidatorOptions = []validator.Option{validator.WithMode(validator.ModeStructural)}
	if _, err := Prepare(context.Background(), input, output, Metadata{}, "", options); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(output, "original", "document.ofd"), filepath.Join(output, "link.ofd")); err != nil {
		t.Fatal(err)
	}
	report, err := Verify(output)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusFailed {
		t.Fatalf("status = %s, issues = %+v", report.Status, report.Issues)
	}
}

func TestFixitySupportsSpacesInPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "fixity"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "file with spaces.txt")
	if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest, err := hashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fixity", "manifest.sha256"), []byte(digest+"  file with spaces.txt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	issues, err := VerifyFixity(root)
	if err != nil || len(issues) != 0 {
		t.Fatalf("issues = %+v, err = %v", issues, err)
	}
}

func TestPrepareRejectsSymlinkOutputDirectory(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	directory := t.TempDir()
	realOutput := filepath.Join(directory, "real")
	if err := os.MkdirAll(realOutput, 0o700); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(directory, "link")
	if err := os.Symlink(realOutput, output); err != nil {
		t.Fatal(err)
	}
	options := DefaultOptions()
	options.ValidatorOptions = []validator.Option{validator.WithMode(validator.ModeStructural)}
	if _, err := Prepare(context.Background(), input, output, Metadata{}, "", options); err == nil {
		t.Fatal("expected symlink output error")
	}
}

func TestVerifyRejectsSymlinkArchiveRoot(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	directory := t.TempDir()
	realOutput := filepath.Join(directory, "real")
	options := DefaultOptions()
	options.ValidatorOptions = []validator.Option{validator.WithMode(validator.ModeStructural)}
	if _, err := Prepare(context.Background(), input, realOutput, Metadata{}, "", options); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "link")
	if err := os.Symlink(realOutput, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(link); err == nil {
		t.Fatal("expected symlink archive root error")
	}
}

func TestBuildMatrixIncludesClauseStatuses(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	options := DefaultOptions()
	options.ValidatorOptions = []validator.Option{validator.WithMode(validator.ModeStructural)}
	matrix, err := BuildMatrix(context.Background(), input, options)
	if err != nil {
		t.Fatal(err)
	}
	if matrix.Standard != MatrixStandard || len(matrix.Clauses) < 25 {
		t.Fatalf("matrix = %+v", matrix)
	}
	if matrix.Summary.Total != len(matrix.Clauses) || matrix.OverallStatus == "" {
		t.Fatalf("summary = %+v, status = %s", matrix.Summary, matrix.OverallStatus)
	}
	var markdown strings.Builder
	if err := RenderMatrixMarkdown(&markdown, matrix); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markdown.String(), "6.1.1") || !strings.Contains(markdown.String(), "A.4") {
		t.Fatalf("markdown omitted clauses: %s", markdown.String())
	}
}

func TestZipFixtureCanBeRead(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	file, err := os.Open(input)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(file, info.Size())
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) == 0 {
		t.Fatal("empty OFD fixture")
	}
}

func issueCodes(issues []Issue) string {
	var values []string
	for _, issue := range issues {
		values = append(values, issue.Code)
	}
	return strings.Join(values, ",")
}
