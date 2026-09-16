package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseArgsSupportsHelp(t *testing.T) {
	for _, arg := range []string{"--help", "-h"} {
		opts, err := parseArgs([]string{arg})
		if err != nil {
			t.Fatal(err)
		}
		if !opts.help {
			t.Fatalf("%s did not set help", arg)
		}
	}
}

func TestNormalizeConverterArgsPreservesLegacySingleDashFlags(t *testing.T) {
	args := normalizeConverterArgs([]string{"-format", "txt", "-dpi=300", "-o", "output.txt"})
	want := []string{"--format", "txt", "--dpi=300", "-o", "output.txt"}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args = %v, want %v", args, want)
	}
}

func TestParseArgsSupportsBatchMode(t *testing.T) {
	opts, err := parseArgs([]string{"--input-dir", "input", "--output-dir", "output", "--format", "pdf", "--workers", "8"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.inputDir != "input" || opts.outputDir != "output" || opts.format != "pdf" || opts.workers != 8 || !opts.recursive || !opts.overwrite {
		t.Fatalf("options = %+v", opts)
	}
}

func TestParseArgsRejectsMixedBatchAndSingleFileArguments(t *testing.T) {
	for _, args := range [][]string{
		{"--input-dir", "input", "input.ofd"},
		{"--output-dir", "output", "input.ofd"},
		{"--input-dir", "input", "--output-dir", "output", "-o", "other"},
	} {
		if _, err := parseArgs(args); err == nil {
			t.Fatalf("parseArgs(%v) returned nil error", args)
		}
	}
}

func TestParseArgsSupportsExistingOutputPolicies(t *testing.T) {
	opts, err := parseArgs([]string{"--input-dir", "input", "--output-dir", "output", "--format", "pdf", "--overwrite=false"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.overwrite {
		t.Fatal("overwrite should be false")
	}

	opts, err = parseArgs([]string{"--input-dir", "input", "--output-dir", "output", "--format", "pdf", "--skip-existing"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.overwrite || !opts.skipExisting {
		t.Fatalf("options = %+v", opts)
	}
}

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

func TestParseArgsSupportsMarkdownFormat(t *testing.T) {
	opts, err := parseArgs([]string{"-format", "markdown", "input.ofd", "output.md"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.format != "markdown" {
		t.Fatalf("format = %q, want markdown", opts.format)
	}
}

func TestFormatFromExtensionSupportsText(t *testing.T) {
	if got := formatFromExtension("output.txt"); got != "txt" {
		t.Fatalf("format = %q, want txt", got)
	}
}

func TestFormatFromExtensionSupportsMarkdown(t *testing.T) {
	for _, path := range []string{"output.md", "output.markdown"} {
		if got := formatFromExtension(path); got != "md" {
			t.Fatalf("formatFromExtension(%q) = %q, want md", path, got)
		}
	}
}

func TestBatchOutputPathPreservesRelativePath(t *testing.T) {
	inputRoot := filepath.FromSlash("/input")
	outputRoot := filepath.FromSlash("/output")
	input := filepath.FromSlash("/input/nested/document.ofd")

	if got, want := batchOutputPath(inputRoot, outputRoot, input, "pdf", false), filepath.FromSlash("/output/nested/document.pdf"); got != want {
		t.Fatalf("file output = %q, want %q", got, want)
	}
	if got, want := batchOutputPath(inputRoot, outputRoot, input, "png", true), filepath.FromSlash("/output/nested/document"); got != want {
		t.Fatalf("image output = %q, want %q", got, want)
	}
}

func TestCollectBatchInputs(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "output")
	paths := []string{
		filepath.Join(root, "b.OFD"),
		filepath.Join(root, "nested", "a.ofd"),
		filepath.Join(output, "ignored.ofd"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	inputs, err := collectBatchInputs(root, output, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 2 || !strings.HasSuffix(inputs[0], "b.OFD") || !strings.HasSuffix(inputs[1], filepath.Join("nested", "a.ofd")) {
		t.Fatalf("inputs = %v", inputs)
	}

	inputs, err = collectBatchInputs(root, output, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 || !strings.HasSuffix(inputs[0], "b.OFD") {
		t.Fatalf("non-recursive inputs = %v", inputs)
	}
}

func TestRunBatchRejectsInvalidOptions(t *testing.T) {
	root := t.TempDir()
	inputDir := filepath.Join(root, "input")
	outputDir := filepath.Join(root, "output")
	if err := os.Mkdir(inputDir, 0755); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name string
		opts options
		want string
	}{
		{name: "workers", opts: options{inputDir: inputDir, outputDir: outputDir, format: "pdf", workers: 0}, want: "workers"},
		{name: "format", opts: options{inputDir: inputDir, outputDir: outputDir, workers: 1}, want: "必须通过 --format"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := runBatch(&test.opts)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("runBatch error = %v, want %q", err, test.want)
			}
		})
	}
	if err := runBatch(&options{inputDir: inputDir, outputDir: inputDir, format: "pdf", workers: 1}); err == nil || !strings.Contains(err.Error(), "不能相同") {
		t.Fatalf("same directory error = %v", err)
	}
}

func TestRunBatchConvertsFilesWithRelativeOutputPaths(t *testing.T) {
	inputRoot := t.TempDir()
	outputRoot := filepath.Join(t.TempDir(), "output")
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"first.ofd", filepath.Join("nested", "second.OFD")} {
		path := filepath.Join(inputRoot, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}

	opts := &options{inputDir: inputRoot, outputDir: outputRoot, format: "txt", workers: 2, recursive: true, dpi: defaultDPI, bg: defaultBgColor}
	if err := runBatch(opts); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"first.txt", filepath.Join("nested", "second.txt")} {
		path := filepath.Join(outputRoot, relative)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("output %q: %v", relative, err)
		}
		if info.Size() == 0 {
			t.Fatalf("output %q is empty", relative)
		}
	}
}

func TestRunBatchExistingOutputPolicies(t *testing.T) {
	inputRoot := t.TempDir()
	outputRoot := filepath.Join(t.TempDir(), "output")
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(inputRoot, "document.ofd")
	if err := os.WriteFile(input, data, 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(outputRoot, "document.txt")
	if err := os.MkdirAll(outputRoot, 0755); err != nil {
		t.Fatal(err)
	}
	original := []byte("keep this output")
	if err := os.WriteFile(output, original, 0600); err != nil {
		t.Fatal(err)
	}

	noOverwrite := &options{inputDir: inputRoot, outputDir: outputRoot, format: "txt", workers: 1, recursive: true, overwrite: false, dpi: defaultDPI, bg: defaultBgColor}
	if err := runBatch(noOverwrite); err == nil || !strings.Contains(err.Error(), "输出已存在") {
		t.Fatalf("overwrite=false error = %v", err)
	}
	if got, err := os.ReadFile(output); err != nil || string(got) != string(original) {
		t.Fatalf("output after overwrite=false = %q, error = %v", got, err)
	}

	skip := &options{inputDir: inputRoot, outputDir: outputRoot, format: "txt", workers: 1, recursive: true, overwrite: true, skipExisting: true, dpi: defaultDPI, bg: defaultBgColor}
	if err := runBatch(skip); err != nil {
		t.Fatalf("skip-existing error = %v", err)
	}
	if got, err := os.ReadFile(output); err != nil || string(got) != string(original) {
		t.Fatalf("output after skip-existing = %q, error = %v", got, err)
	}

	overwrite := &options{inputDir: inputRoot, outputDir: outputRoot, format: "txt", workers: 1, recursive: true, overwrite: true, dpi: defaultDPI, bg: defaultBgColor}
	if err := runBatch(overwrite); err != nil {
		t.Fatalf("default overwrite error = %v", err)
	}
	if got, err := os.ReadFile(output); err != nil || string(got) == string(original) {
		t.Fatalf("output after default overwrite = %q, error = %v", got, err)
	}
}

func TestRunBatchRejectsConflictingExistingOutputPolicies(t *testing.T) {
	root := t.TempDir()
	inputRoot := filepath.Join(root, "input")
	outputRoot := filepath.Join(root, "output")
	if err := os.Mkdir(inputRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := runBatch(&options{
		inputDir:     inputRoot,
		outputDir:    outputRoot,
		format:       "pdf",
		workers:      1,
		overwrite:    false,
		skipExisting: true,
	}); err == nil || !strings.Contains(err.Error(), "不能与 --overwrite=false") {
		t.Fatalf("conflicting policies error = %v", err)
	}
}

func TestFormatFromExtensionSupportsHTML(t *testing.T) {
	for _, path := range []string{"output.html", "output.htm"} {
		if got := formatFromExtension(path); got != "html" {
			t.Fatalf("formatFromExtension(%q) = %q, want html", path, got)
		}
	}
}

func TestRunRejectsInvalidHTMLFormat(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.ofd")
	if err := os.WriteFile(input, []byte("input"), 0644); err != nil {
		t.Fatal(err)
	}
	err := run(&options{input: input, output: filepath.Join(dir, "output.html"), format: "html", htmlFormat: "webp"})
	if err == nil {
		t.Fatal("run returned nil error for invalid html-format")
	}
}

func TestParseArgsSupportsHTMLFormat(t *testing.T) {
	opts, err := parseArgs([]string{"-format", "html", "-html-format", "svg", "input.ofd", "output.html"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.format != "html" || opts.htmlFormat != "svg" {
		t.Fatalf("format = %q, htmlFormat = %q, want html/svg", opts.format, opts.htmlFormat)
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
