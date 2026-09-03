package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseArgsSupportsTextFormat(t *testing.T) {
	opts, err := parseArgs([]string{"-format", "txt", "input.ofd", "output.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.format != "txt" {
		t.Fatalf("format = %q, want txt", opts.format)
	}
	if opts.output != "output.txt" {
		t.Fatalf("output = %q, want output.txt", opts.output)
	}
}

func TestFormatFromExtensionSupportsText(t *testing.T) {
	if got := formatFromExtension("output.txt"); got != "txt" {
		t.Fatalf("format = %q, want txt", got)
	}
}

func TestValidateOutputPathRejectsInputFile(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.ofd")
	if err := os.WriteFile(input, []byte("input"), 0644); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		format string
		page   int
		output string
	}{
		{format: "pdf", output: input},
		{format: "txt", output: input},
		{format: "png", page: 1, output: input},
	} {
		opts := &options{input: input, output: test.output, format: test.format, page: test.page}
		if err := validateOutputPath(opts, test.format); err != ErrInputOutputSame {
			t.Fatalf("validateOutputPath(%q, %q) = %v, want %v", test.format, test.output, err, ErrInputOutputSame)
		}
	}
}

func TestRunInvalidPageDoesNotCreateOutput(t *testing.T) {
	output := filepath.Join(t.TempDir(), "invalid.pdf")
	opts := &options{
		input:  filepath.Join("..", "..", "test", "testdata", "multi_demo.ofd"),
		output: output,
		format: "pdf",
		page:   99,
	}
	if err := run(opts); err == nil {
		t.Fatal("run returned nil error for an invalid page")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("invalid-page output stat error = %v, want os.ErrNotExist", err)
	}
}
