package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/parser"
)

func TestValidateOptionsInfersFormatFromOutput(t *testing.T) {
	input := filepath.Join(t.TempDir(), "document.ofd")
	if err := os.WriteFile(input, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		extension string
		format    string
	}{
		{extension: ".txt", format: "text"},
		{extension: ".md", format: "markdown"},
		{extension: ".MD", format: "markdown"},
		{extension: ".markdown", format: "markdown"},
		{extension: ".json", format: "json"},
		{extension: ".PDF", format: "pdf"},
		{extension: ".ofd", format: "ofd"},
		{extension: ".OFD", format: "ofd"},
		{extension: ".xlsx", format: "xlsx"},
	} {
		output := "report" + test.extension
		opts := &options{input: input, output: filepath.Join(t.TempDir(), output), format: "text", mode: "strict"}
		if err := validateOptions(opts); err != nil {
			t.Fatal(err)
		}
		if opts.format != test.format {
			t.Fatalf("format for %q = %q, want %s", output, opts.format, test.format)
		}
	}
}

func TestOFDReportFontOptionsAndInputProtection(t *testing.T) {
	input := filepath.Join(t.TempDir(), "input.ofd")
	if err := os.WriteFile(input, []byte("input"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"ofd", "pdf", "text", "json"} {
		opts := &options{input: input, format: format, formatSet: true, mode: "strict", font: "cjk.ttf"}
		err := validateOptions(opts)
		if (format == "ofd" || format == "pdf") != (err == nil) {
			t.Fatalf("--font 与 %s 的校验结果错误: %v", format, err)
		}
	}
	opts := &options{input: input, output: input, format: "ofd", mode: "strict"}
	if err := validateOptions(opts); err == nil || !strings.Contains(err.Error(), "覆盖") {
		t.Fatalf("未阻止报告覆盖输入: %v", err)
	}
}

// 校验失败仍应生成可打开的报告；文件和标准输出的二进制内容不混入诊断。
func TestRunWritesOFDAndPDFReportsForInvalidInput(t *testing.T) {
	font := os.Getenv("OFD_REPORT_TEST_FONT")
	if font == "" {
		font = "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc"
	}
	if _, err := os.Stat(font); errors.Is(err, os.ErrNotExist) {
		t.Skipf("本地中文字体不存在: %s", font)
	} else if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	input := filepath.Join(root, "broken.ofd")
	if err := os.WriteFile(input, []byte("not a ZIP package"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"ofd", "pdf"} {
		for _, stdout := range []bool{false, true} {
			var output, diagnostics bytes.Buffer
			path := filepath.Join(root, "report."+format)
			args := []string{"--font", font, "-o", path, input}
			if stdout {
				args = []string{"--format", format, "--font", font, "-o", "-", input}
			}
			if code := run(args, &output, &diagnostics); code != 1 || diagnostics.Len() != 0 {
				t.Fatalf("%s stdout=%v: code=%d stderr=%s", format, stdout, code, &diagnostics)
			}
			data := output.Bytes()
			if !stdout {
				if output.Len() != 0 {
					t.Fatalf("文件输出污染 stdout: %q", &output)
				}
				var err error
				data, err = os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
			}
			if format == "pdf" {
				if !bytes.HasPrefix(data, []byte("%PDF-")) {
					t.Fatal("PDF 输出无效")
				}
			} else {
				ofd, err := parser.NewOFD(data)
				if err != nil {
					t.Fatal(err)
				}
				if len(ofd.Documents) != 1 || ofd.Documents[0].PageCount() == 0 {
					t.Fatal("OFD 报告没有页面")
				}
				_ = ofd.Close()
			}
		}
	}
}

func TestReportFontFailurePreservesExistingOutput(t *testing.T) {
	root := t.TempDir()
	input, output := filepath.Join(root, "input.ofd"), filepath.Join(root, "report.ofd")
	if err := os.WriteFile(input, []byte("broken input"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("existing report"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--font", filepath.Join(root, "missing.ttf"), "-o", output, input}, &stdout, &stderr); code != 2 || stderr.Len() == 0 || stdout.Len() != 0 {
		t.Fatalf("字体错误的退出码或输出错误: code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "existing report" {
		t.Fatal("报告生成失败时覆盖了现有文件")
	}
}

func TestCompleteSignatureCheckReportsMissingEvidence(t *testing.T) {
	var output, diagnostics bytes.Buffer
	code := run([]string{"--mode", "structural", "--format", "json", "--check-signatures", "../../testdata/ofdrw/999.ofd"}, &output, &diagnostics)
	if code != 1 {
		t.Fatalf("缺少必要证据时退出码=%d：%s %s", code, output.String(), diagnostics.String())
	}
	if !strings.Contains(output.String(), "signature.incomplete.") || !strings.Contains(output.String(), `"name":"signatures"`) {
		t.Fatalf("报告缺少完整验签明细: %s", output.String())
	}
}

func TestValidateOptionsPreservesExplicitFormatForMDOutput(t *testing.T) {
	input := filepath.Join(t.TempDir(), "document.ofd")
	if err := os.WriteFile(input, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	opts := &options{
		input:     input,
		output:    filepath.Join(t.TempDir(), "report.md"),
		format:    "text",
		formatSet: true,
		mode:      "strict",
	}
	if err := validateOptions(opts); err != nil {
		t.Fatal(err)
	}
	if opts.format != "text" {
		t.Fatalf("explicit format = %q, want text", opts.format)
	}
}
