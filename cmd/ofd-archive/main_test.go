package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"

	"github.com/xuri/excelize/v2"
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
				expectedUsage = "ofd-archive [check|manifest|matrix|prepare]"
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
	if code != exitOK || stdout.String() != "0.0.1\n" || stderr.Len() != 0 {
		t.Fatalf("code = %d, stdout = %q, stderr = %s", code, stdout.String(), stderr.String())
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
