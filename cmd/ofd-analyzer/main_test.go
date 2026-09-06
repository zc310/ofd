package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseArgs(t *testing.T) {
	opts, err := parseArgs([]string{"--pretty", "--format", "markdown", "--no-package", "-o", "report.md", "input.ofd"}, new(bytes.Buffer))
	if err != nil {
		t.Fatal(err)
	}
	if !opts.pretty || opts.format != "markdown" || !opts.noPackage || opts.output != "report.md" || opts.input != "input.ofd" {
		t.Fatalf("options = %+v", opts)
	}
}

func TestValidateOptionsRejectsUnsupportedFormat(t *testing.T) {
	opts := &options{input: filepath.Join("..", "..", "test", "testdata", "helloworld.ofd"), format: "html"}
	if err := validateOptions(opts); err == nil {
		t.Fatal("expected unsupported format error")
	}
}

func TestValidateOptionsRejectsFontForNonPDF(t *testing.T) {
	opts := &options{input: filepath.Join("..", "..", "test", "testdata", "helloworld.ofd"), format: "text", font: "font.ttf"}
	if err := validateOptions(opts); err == nil {
		t.Fatal("expected --font validation error")
	}
}

func TestRunWritesJSONReport(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--format", "json", "--pretty", filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 || !strings.Contains(stdout.String(), "schema_version") {
		t.Fatalf("stdout = %s, stderr = %s", stdout.String(), stderr.String())
	}
	var report map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
}

func TestRunDefaultsToTextReport(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")}, &stdout, &stderr)
	if code != exitOK || stderr.Len() != 0 || !strings.HasPrefix(stdout.String(), "OFD 分析报告") {
		t.Fatalf("exit code = %d, stdout = %s, stderr = %s", code, stdout.String(), stderr.String())
	}
}

func TestRunRejectsOverwritingInput(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	var stdout, stderr bytes.Buffer
	code := run([]string{"-o", input, input}, &stdout, &stderr)
	if code != exitUsage || !strings.Contains(stderr.String(), "覆盖") {
		t.Fatalf("exit code = %d, stdout = %s, stderr = %s", code, stdout.String(), stderr.String())
	}
}

func TestRunAnalysisFailureStillWritesFailedReport(t *testing.T) {
	input := filepath.Join(t.TempDir(), "invalid.ofd")
	if err := os.WriteFile(input, []byte("not an ofd"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"--format", "json", input}, &stdout, &stderr)
	if code != exitFailed || !strings.Contains(stderr.String(), "分析失败") {
		t.Fatalf("exit code = %d, stdout = %s, stderr = %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"status":"failed"`) {
		t.Fatalf("failed report = %s", stdout.String())
	}
}

func TestRunFailOnWarningReturnsFailure(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "ano.ofd")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--format", "json", "--fail-on-warning", input}, &stdout, &stderr)
	if code != exitFailed {
		t.Fatalf("exit code = %d, stdout = %s, stderr = %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"status":"partial"`) {
		t.Fatalf("partial report = %s", stdout.String())
	}
}

func TestRunWritesTextAndMarkdownReports(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	for _, test := range []struct {
		format string
		want   string
	}{
		{format: "text", want: "OFD 分析报告"},
		{format: "markdown", want: "# OFD 分析报告"},
	} {
		t.Run(test.format, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run([]string{"--format", test.format, input}, &stdout, &stderr)
			if code != exitOK || stderr.Len() != 0 || !strings.Contains(stdout.String(), test.want) {
				t.Fatalf("exit code = %d, stdout = %s, stderr = %s", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunWritesPDFReport(t *testing.T) {
	font := "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
	if _, err := os.Stat(font); err != nil {
		t.Skipf("test font unavailable: %v", err)
	}
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--format", "pdf", "--font", font, input}, &stdout, &stderr)
	if code != exitOK || stderr.Len() != 0 || !bytes.HasPrefix(stdout.Bytes(), []byte("%PDF-")) {
		t.Fatalf("exit code = %d, PDF prefix = %q, stderr = %s", code, stdout.Bytes()[:min(len(stdout.Bytes()), 5)], stderr.String())
	}
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}
