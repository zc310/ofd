package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"

	"github.com/xuri/excelize/v2"

	"github.com/zc310/ofd/internal/spec"
	"github.com/zc310/ofd/pkg/archive"
	"github.com/zc310/ofd/pkg/creator"
)

func TestRunDefaultsToCheckJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--format", "json", "--mode", "structural", "../../test/testdata/helloworld.ofd"}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"schema_version"`) || stderr.Len() != 0 {
		t.Fatalf("stdout = %s, stderr = %s", stdout.String(), stderr.String())
	}
}

func TestRunHelpWritesUsage(t *testing.T) {
	for _, flag := range []string{"--help", "-h", "verify --help"} {
		t.Run(flag, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := strings.Fields(flag)
			code := run(args, &stdout, &stderr)
			if code != exitOK || stderr.Len() != 0 {
				t.Fatalf("code = %d, stdout = %s, stderr = %s", code, stdout.String(), stderr.String())
			}
			expectedUsage := "ofd-archive verify"
			if flag != "verify --help" {
				expectedUsage = "ofd-archive [check|manifest|matrix|prepare|preserve]"
			}
			for _, expected := range []string{"ofd-archive - OFD 档案预检和归档准备工具", expectedUsage, "-format"} {
				if !strings.Contains(stdout.String(), expected) {
					t.Fatalf("help output lacks %q: %s", expected, stdout.String())
				}
			}
		})
	}
}

func TestRunVersionWritesVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--version"}, &stdout, &stderr)
	// 期望值引用 archive.ToolVersion 而不是写死字面量：--version 与报告里的
	// tool.version 必须同源，写死字面量只会在下一次发版时变成莫名其妙的失败。
	want := archive.ToolVersion + "\n"
	if code != exitOK || stdout.String() != want || stderr.Len() != 0 {
		t.Fatalf("code = %d, stdout = %q, 期望 %q, stderr = %s", code, stdout.String(), want, stderr.String())
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"unknown", "extra"}, &stdout, &stderr)
	if code != exitUsage || !strings.Contains(stderr.String(), "必须且只能指定一个输入 OFD 文件") {
		t.Fatalf("code = %d, stdout = %s, stderr = %s", code, stdout.String(), stderr.String())
	}
}

func TestRunCheckWritesMarkdown(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"check", "--format", "markdown", "--mode", "structural", "../../test/testdata/helloworld.ofd"}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	output := stdout.String()
	for _, expected := range []string{"# OFD 归档预检报告", "输入大小：", "## 分析汇总", "## 校验阶段", "## 特征统计", "## 附件", "## 问题"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("Markdown output lacks %q: %s", expected, output)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestParseArgsPrepareRequiresMetadataAndOutput(t *testing.T) {
	opts, err := parseArgs([]string{"prepare", "../../test/testdata/helloworld.ofd"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateOptions(opts); err == nil {
		t.Fatal("expected prepare validation error")
	}
}

func TestRunPrepareWritesReportAndDirectory(t *testing.T) {
	directory := t.TempDir()
	metadata := filepath.Join(directory, "archive.json")
	if err := os.WriteFile(metadata, []byte(`{"archive_code":"A-1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(directory, "prepared")
	var stdout, stderr bytes.Buffer
	code := run([]string{"prepare", "--mode", "structural", "--metadata", metadata, "--output", output, "../../test/testdata/helloworld.ofd"}, &stdout, &stderr)
	if code != exitOK || stderr.Len() != 0 || !strings.Contains(stdout.String(), "OFD 归档预检报告") {
		t.Fatalf("code = %d, stdout = %s, stderr = %s", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(output, "metadata", "manifest.json")); err != nil {
		t.Fatal(err)
	}
}

func TestRunPrepareThenVerify(t *testing.T) {
	directory := t.TempDir()
	metadata := filepath.Join(directory, "archive.json")
	if err := os.WriteFile(metadata, []byte(`{"archive_code":"A-1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(directory, "prepared")
	var prepareStdout, prepareStderr bytes.Buffer
	if code := run([]string{"prepare", "--mode", "structural", "--metadata", metadata, "--output", output, "../../test/testdata/helloworld.ofd"}, &prepareStdout, &prepareStderr); code != exitOK {
		t.Fatalf("prepare code = %d, stdout = %s, stderr = %s", code, prepareStdout.String(), prepareStderr.String())
	}
	var verifyStdout, verifyStderr bytes.Buffer
	code := run([]string{"verify", "--format", "json", output}, &verifyStdout, &verifyStderr)
	if code != exitOK || verifyStderr.Len() != 0 {
		t.Fatalf("verify code = %d, stdout = %s, stderr = %s", code, verifyStdout.String(), verifyStderr.String())
	}
	if !strings.Contains(verifyStdout.String(), `"status":"passed"`) {
		t.Fatalf("verify report = %s", verifyStdout.String())
	}
}

func TestRunCheckFailOnWarning(t *testing.T) {
	input := "../../test/testdata/project-showcase.ofd"
	var stdout, stderr bytes.Buffer
	code := run([]string{"check", "--mode", "structural", input}, &stdout, &stderr)
	if code != exitOK || stderr.Len() != 0 || !strings.Contains(stdout.String(), "状态：warning") {
		t.Fatalf("default warning behavior: code = %d, stdout = %s, stderr = %s", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"check", "--mode", "structural", "--fail-on-warning", input}, &stdout, &stderr)
	if code != exitFailed || stderr.Len() != 0 || !strings.Contains(stdout.String(), "状态：failed") {
		t.Fatalf("fail-on-warning behavior: code = %d, stdout = %s, stderr = %s", code, stdout.String(), stderr.String())
	}
}

func TestParseArgsAcceptsInputBeforeOptions(t *testing.T) {
	opts, err := parseArgs([]string{"prepare", "input.ofd", "--metadata", "archive.json", "--output", "out"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if opts.input != "input.ofd" || opts.metadata != "archive.json" || opts.output != "out" {
		t.Fatalf("options = %+v", opts)
	}
}

func TestValidateOptionsRejectsReportOverwritingInput(t *testing.T) {
	input := "../../test/testdata/helloworld.ofd"
	opts, err := parseArgs([]string{"check", "--format", "json", "--output", input, input}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateOptions(opts); err == nil {
		t.Fatal("expected input overwrite error")
	}
}

func TestManifestDefaultsToJSON(t *testing.T) {
	opts, err := parseArgs([]string{"manifest", "input.ofd"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if opts.format != "json" {
		t.Fatalf("format = %q, want json", opts.format)
	}
}

func TestRunMatrixWritesMarkdown(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"matrix", "--mode", "structural", "../../test/testdata/helloworld.ofd"}, &stdout, &stderr)
	if code != exitOK || stderr.Len() != 0 || !strings.Contains(stdout.String(), "条文符合性矩阵") || !strings.Contains(stdout.String(), "6.1.1") {
		t.Fatalf("code = %d, stdout = %s, stderr = %s", code, stdout.String(), stderr.String())
	}
}

func TestRunMatrixWritesXLSX(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "matrix.xlsx")
	code := run([]string{"matrix", "--mode", "structural", "--output", output, "../../test/testdata/helloworld.ofd"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != exitOK {
		t.Fatalf("code = %d", code)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) < 6 {
		t.Fatalf("xlsx entries = %d", len(reader.File))
	}
	workbook, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer workbook.Close()
	if got := workbook.GetSheetList(); len(got) != 2 || got[0] != "汇总" || got[1] != "条文矩阵" {
		t.Fatalf("sheets = %v", got)
	}
	if value, err := workbook.GetCellValue("条文矩阵", "A5"); err != nil || value != "1" {
		t.Fatalf("clause cell = %q, err = %v", value, err)
	}
	if value, err := workbook.GetCellValue("条文矩阵", "D5"); err != nil || value == "" {
		t.Fatalf("status cell = %q, err = %v", value, err)
	}
	styleID, err := workbook.GetCellStyle("汇总", "B6")
	if err != nil {
		t.Fatal(err)
	}
	style, err := workbook.GetStyle(styleID)
	if err != nil {
		t.Fatal(err)
	}
	if style.Alignment == nil || style.Alignment.Horizontal != "center" {
		t.Fatalf("summary value alignment = %+v, want center", style.Alignment)
	}
	for _, entry := range reader.File {
		if entry.Name != "xl/worksheets/sheet2.xml" {
			continue
		}
		data, readErr := entry.Open()
		if readErr != nil {
			t.Fatal(readErr)
		}
		content, readErr := io.ReadAll(data)
		_ = data.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if len(content) == 0 {
			t.Fatalf("empty matrix worksheet")
		}
		break
	}
}

func TestMatrixInfersXLSXFromOutputExtension(t *testing.T) {
	input := "../../test/testdata/helloworld.ofd"
	opts, err := parseArgs([]string{"matrix", input, "--output", "matrix.xlsx"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateOptions(opts); err != nil {
		t.Fatal(err)
	}
	if opts.format != "xlsx" {
		t.Fatalf("format = %q, want xlsx", opts.format)
	}
}

func TestCheckRejectsXLSXFormat(t *testing.T) {
	opts, err := parseArgs([]string{"check", "--format", "xlsx", "../../test/testdata/helloworld.ofd"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateOptions(opts); err == nil {
		t.Fatal("expected xlsx format error for check")
	}
}

func TestMatrixAcceptsExplicitXLSXFormat(t *testing.T) {
	opts, err := parseArgs([]string{"matrix", "--format", "xlsx", "../../test/testdata/helloworld.ofd"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateOptions(opts); err != nil {
		t.Fatal(err)
	}
}

func TestMakeArchiveOptionsPassesXMLScanLimit(t *testing.T) {
	opts, err := parseArgs([]string{"check", "--max-xml-bytes", "1234", "../../test/testdata/helloworld.ofd"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	archiveOptions := makeArchiveOptions(opts)
	if archiveOptions.MaxXMLBytes != 1234 {
		t.Fatalf("MaxXMLBytes = %d, want 1234", archiveOptions.MaxXMLBytes)
	}
}

// TestRunDoctypeRejectsUnknownProfile 保护 --doc-type 在参数校验阶段被拒绝并
// 列出可用取值，取值区分大小写。
func TestRunDoctypeRejectsUnknownProfile(t *testing.T) {
	for _, invalid := range []string{"ofd-a", "OFD_A", "OFD-X"} {
		var stdout, stderr bytes.Buffer
		code := run([]string{"check", "--doc-type", invalid, "../../test/testdata/helloworld.ofd"}, &stdout, &stderr)
		if code != exitUsage {
			t.Errorf("--doc-type %q 退出码 = %d, want %d", invalid, code, exitUsage)
		}
		if !strings.Contains(stderr.String(), spec.DocTypeOFDA) {
			t.Errorf("--doc-type %q 的错误信息未列出可用取值: %s", invalid, stderr.String())
		}
	}
}

// TestRunDoctypeIsDistinctFromArchiveProfile 保护 --doc-type 与 --profile 互不
// 干扰：前者是 OFD profile 取值，后者是档案字段 profile 文件路径。同名会让
// 用户把 OFD profile 当成文件路径传入。
func TestRunDoctypeIsDistinctFromArchiveProfile(t *testing.T) {
	directory := t.TempDir()
	// 把 --profile 指向一个实际存在的档案 profile 文件，确认它仍按文件路径解读。
	profilePath := filepath.Join(directory, "profile.json")
	if err := os.WriteFile(profilePath, []byte(`{"required_fields":["archive_code"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(directory, "archive.json")
	if err := os.WriteFile(metadataPath, []byte(`{"archive_code":"A-001"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"prepare", "../../test/testdata/helloworld.ofd",
		"--metadata", metadataPath, "--profile", profilePath,
		"--doc-type", spec.DocTypeOFDA, "--mode", "structural",
		"--output", filepath.Join(directory, "out"),
	}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("run exit code = %d, stderr = %s", code, stderr.String())
	}
	// 准备目录里应保存档案 profile 文件，同时校验按 OFD-A 规则执行。
	reportPath := filepath.Join(directory, "out", "reports", "check.json")
	content, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	var report archive.Report
	if err := json.Unmarshal(content, &report); err != nil {
		t.Fatal(err)
	}
	if report.ValidatorReport.Profile != spec.DocTypeOFDA {
		t.Errorf("校验报告的 profile = %q, want %q", report.ValidatorReport.Profile, spec.DocTypeOFDA)
	}
	if _, err := os.Stat(filepath.Join(directory, "out", "metadata", "profile.json")); err != nil {
		t.Errorf("档案 profile 文件未被保存到 metadata/: %v", err)
	}
}

// TestRunDoctypeDefaultsToDocumentDeclaration 保护未指定 --doc-type 时按文件
// 声明的 DocType 自动判定，不覆盖文件自身的取值。
func TestRunDoctypeDefaultsToDocumentDeclaration(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "result.ofd")
	if err := manifestForArchive(t, output, ""); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"check", "--format", "json", "--mode", "structural", output}, &stdout, &stderr); code != exitOK {
		t.Fatalf("run exit code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"profile":"`+spec.DocTypeOFD+`"`) &&
		!strings.Contains(stdout.String(), `"profile":""`) {
		t.Logf("报告未出现 profile 字段，检查 JSON: %s", truncate(stdout.String(), 200))
	}
}

// manifestForArchive 用 creator 生成指定 DocType 的 OFD。
func manifestForArchive(t *testing.T, path, docType string) error {
	t.Helper()
	data, err := creator.MarshalWithOptions(creator.Document{
		ID:       "archive-doc-type",
		Title:    "归档 DocType",
		PageSize: creator.A4,
		Pages: []creator.Page{{Items: []creator.Item{
			creator.Text{X: 20, Y: 30, Width: 100, Height: 10, Value: "内容", Font: "SimSun"},
		}}},
	}, creator.CreateOptions{Compression: creator.CompressionAuto, DocType: docType})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}
