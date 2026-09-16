package main

import (
	"os"
	"path/filepath"
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
