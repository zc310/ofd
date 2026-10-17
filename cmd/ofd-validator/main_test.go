package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
