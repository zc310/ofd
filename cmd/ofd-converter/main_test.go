package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/testutil"
	"github.com/zc310/ofd/internal/version"
	"github.com/zc310/ofd/pkg/converter"
)

func TestPDFImportFlagsAndFailurePolicies(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "input.pdf"), filepath.Join(dir, "output.ofd")
	if err := os.WriteFile(input, testutil.MinimalPDF([]byte("/Missing Do"), 200, 200), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("existing output"), 0600); err != nil {
		t.Fatal(err)
	}
	opts, err := parseArgs([]string{"-pdf-strict", "-pdf-image-mode=compact", "--pdf-jpeg-quality", "76", "--pdf-jpx-backend=openjpeg", "-pdf-max-input-bytes=4KiB", "--pdf-max-total-decoded-bytes", "4GiB", "--pdf-max-image-pixels", "100", "--pdf-max-total-image-pixels=200", "--pdf-max-operations", "300", input, output})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.pdfStrict || opts.pdfImageMode != "compact" || opts.pdfJPEGQuality != 76 || opts.pdfJPXBackend != "openjpeg" || opts.pdfMaxInputBytes != 4096 || opts.pdfMaxTotalDecodedBytes != 4294967296 || opts.pdfMaxImagePixels != 100 || opts.pdfMaxTotalImagePixels != 200 || opts.pdfMaxOperations != 300 {
		t.Fatalf("opts=%+v", opts)
	}
	if got := opts.pdfLimits().MaxTotalDecodedBytes; got != 4294967296 {
		t.Fatalf("MaxTotalDecodedBytes = %d, want 4294967296", got)
	}
	var diagnostics bytes.Buffer
	opts.diagnosticsWriter = &diagnostics
	err = run(opts)
	if !errors.Is(err, converter.ErrPDFImportUnsupported) || !strings.Contains(diagnostics.String(), "[resource.missing]") {
		t.Fatalf("error=%v diagnostics=%s", err, diagnostics.String())
	}
	data, err := os.ReadFile(output)
	if err != nil || string(data) != "existing output" {
		t.Fatalf("read=%v data=%q", err, data)
	}
	opts.pdfStrict, opts.pdfMaxInputBytes = false, 1
	if err := run(opts); !errors.Is(err, converter.ErrPDFImportLimitExceeded) || errors.Is(err, ErrUsage) {
		t.Fatalf("budget error=%v", err)
	}
	for _, args := range [][]string{{"--pdf-image-mode=source", input, output}, {"--pdf-jpeg-quality=101", input, output}, {"--pdf-jpx-backend=invalid", input, output}, {"--pdf-max-input-bytes=-1", input, output}, {"--pdf-max-total-decoded-bytes=-1", input, output}, {"--pdf-max-image-pixels=-1", "--input-dir", dir, "--output-dir", filepath.Join(dir, "out"), "--format", "ofd"}, {"--pdf-max-operations=-1", input, output}} {
		opts, err := parseArgs(args)
		if err != nil {
			t.Fatal(err)
		}
		if err := run(opts); !errors.Is(err, ErrUsage) {
			t.Fatalf("args=%v error=%v", args, err)
		}
	}
}

func TestByteSizeValue(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"4096", 4096},
		{"4KiB", 4 << 10},
		{"2MiB", 2 << 20},
		{"100GiB", 100 << 30},
		{"1TB", 1_000_000_000_000},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			var got int64
			if err := (byteSizeValue{value: &got}).Set(test.input); err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("Set(%q) = %d, want %d", test.input, got, test.want)
			}
		})
	}
	for _, input := range []string{"1.5GiB", "1PB", "9223372036854775807GiB"} {
		var got int64
		if err := (byteSizeValue{value: &got}).Set(input); err == nil {
			t.Errorf("Set(%q) unexpectedly succeeded", input)
		}
	}
}

func TestPDFStrictImportAcceptsGlyphClipping(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "clip.pdf"), filepath.Join(dir, "clip.ofd")
	if err := os.WriteFile(input, testutil.MinimalPDF([]byte("BT /F1 12 Tf 7 Tr (A) Tj ET 0 0 200 200 re f"), 200, 200), 0600); err != nil {
		t.Fatal(err)
	}
	opts, err := parseArgs([]string{"--from", "pdf", "--pdf-strict", input, output})
	if err != nil {
		t.Fatal(err)
	}
	var diagnostics bytes.Buffer
	opts.diagnosticsWriter = &diagnostics
	if err := run(opts); err != nil || diagnostics.Len() != 0 {
		t.Fatalf("clip import=%v diagnostics=%s", err, diagnostics.String())
	}
	if info, err := os.Stat(output); err != nil || info.Size() == 0 {
		t.Fatalf("clip output missing: %v", err)
	}
}

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

func TestParseArgsSupportsImageOCR(t *testing.T) {
	opts, err := parseArgs([]string{"--ocr", "--ocr-language", "chi_sim+eng", "scan.png", "scan.ofd"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.ocr || opts.ocrLanguage != "chi_sim+eng" {
		t.Fatalf("OCR options = enabled:%v language:%q", opts.ocr, opts.ocrLanguage)
	}
	if options, err := parseArgs([]string{"scan.png", "scan.ofd"}); err != nil {
		t.Fatal(err)
	} else if options.ocr || options.ocrLanguage != "chi_sim+eng" {
		t.Fatalf("default OCR options = enabled:%v language:%q", options.ocr, options.ocrLanguage)
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

func TestFormatFromExtensionSupportsTIFF(t *testing.T) {
	for _, path := range []string{"output.tif", "output.tiff"} {
		if got := formatFromExtension(path); got != "tiff" {
			t.Fatalf("formatFromExtension(%q) = %q, want tiff", path, got)
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
	if got, want := batchOutputPath(inputRoot, outputRoot, input, "tiff", false), filepath.FromSlash("/output/nested/document.tiff"); got != want {
		t.Fatalf("TIFF output = %q, want %q", got, want)
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

func TestBatchInputExtensionSupportsXPS(t *testing.T) {
	for _, name := range []string{"document.xps", "DOCUMENT.XPS"} {
		if !batchInputExtension(name) {
			t.Fatalf("batchInputExtension(%q) = false, want true", name)
		}
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
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "ofd-creator/helloworld.ofd"))
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
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "ofd-creator/helloworld.ofd"))
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

func TestParseArgsSupportsExternalToolOptions(t *testing.T) {
	opts, err := parseArgs([]string{
		"-temp-dir", "/tmp/custom",
		"-xpstopdf", "/opt/libgxps/bin/xpstopdf",
		"-paper", "A3",
		"-landscape",
		"-chrome-no-sandbox",
		"-allow-remote",
		"-no-print-background",
		"-md-tables",
		"-external-workers", "3",
		"input.html", "output.pdf",
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.tempDir != "/tmp/custom" {
		t.Fatalf("tempDir = %q", opts.tempDir)
	}
	if opts.xpstopdf != "/opt/libgxps/bin/xpstopdf" {
		t.Fatalf("xpstopdf = %q", opts.xpstopdf)
	}
	if opts.paper != "A3" || !opts.landscape || !opts.chromeNoSandbox || !opts.allowRemote || !opts.noPrintBackground {
		t.Fatalf("外部工具选项解析不正确: %+v", opts)
	}
	if !opts.markdownTables {
		t.Fatalf("markdownTables = false, want true")
	}
	if opts.externalWorkers != 3 {
		t.Fatalf("externalWorkers = %d, want 3", opts.externalWorkers)
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
		input:  filepath.Join("..", "..", "testdata", "ofd-creator/multi_demo.ofd"),
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

func TestRegistryFormatNameRoundTripsCLINames(t *testing.T) {
	for cliName, registryName := range map[string]string{
		"txt": "text",
		"md":  "markdown",
		"jpg": "jpeg",
		"pdf": "pdf",
		"png": "png",
	} {
		if got := registryFormatName(cliName); got != registryName {
			t.Fatalf("registryFormatName(%q) = %q, want %q", cliName, got, registryName)
		}
		if got := cliFormatName(registryName); got != cliName {
			t.Fatalf("cliFormatName(%q) = %q, want %q", registryName, got, cliName)
		}
	}
}

func TestIsImageFormatUsesRegistryKind(t *testing.T) {
	for _, format := range []string{"png", "jpg", "svg", "eps", "tex"} {
		if !isImageFormat(format) {
			t.Errorf("isImageFormat(%q) = false, want true", format)
		}
	}
	for _, format := range []string{"pdf", "txt", "md", "html"} {
		if isImageFormat(format) {
			t.Errorf("isImageFormat(%q) = true, want false", format)
		}
	}
}

// TestUsageErrorsAreMarkedAsExitCodeTwo 锁定退出码的分层：命令写错了（参数、目录、
// 格式、范围、输入输出同路径）是 ErrUsage，退出码 2；文档本身转不出来才是 1。
//
// 两者混在一起时，脚本无法判断该改命令还是该换文件——批量任务里一个拼错的
// --format 会让整批看起来像"文件有问题"。
func TestUsageErrorsAreMarkedAsExitCodeTwo(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.ofd")
	if err := os.WriteFile(input, []byte("input"), 0o644); err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(dir, "out")

	for _, test := range []struct {
		name string
		opts options
	}{
		{name: "输入文件不存在", opts: options{input: filepath.Join(dir, "missing.ofd"), format: "pdf", dpi: 150}},
		{name: "不支持的输出格式", opts: options{input: input, output: filepath.Join(dir, "o.pdf"), format: "bogus", dpi: 150}},
		{name: "dpi 越界", opts: options{input: input, output: filepath.Join(dir, "o.pdf"), format: "pdf", dpi: 0}},
		{name: "page 为负", opts: options{input: input, output: filepath.Join(dir, "o.pdf"), format: "pdf", dpi: 150, page: -1}},
		{name: "html-format 非法", opts: options{input: input, output: filepath.Join(dir, "o.html"), format: "html", dpi: 150, htmlFormat: "webp"}},
		{name: "输入输出同路径", opts: options{input: input, output: input, format: "pdf", dpi: 150}},
		{name: "批量未指定格式", opts: options{inputDir: dir, outputDir: outputDir, workers: 1}},
		{name: "批量格式非法", opts: options{inputDir: dir, outputDir: outputDir, format: "bogus", workers: 1}},
		{name: "批量目录不存在", opts: options{inputDir: filepath.Join(dir, "missing"), outputDir: outputDir, format: "pdf", workers: 1}},
		{name: "批量输入输出同目录", opts: options{inputDir: dir, outputDir: dir, format: "pdf", workers: 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := run(&test.opts)
			if err == nil {
				t.Fatal("run 返回 nil error")
			}
			if !errors.Is(err, ErrUsage) {
				t.Fatalf("error = %v，应标记为 ErrUsage（退出码 2）", err)
			}
		})
	}
}

// TestBatchFileFailuresKeepExitCodeOne 确认批量里单个文件转换失败仍汇总成普通错误：
// 它们是文档问题，不能因为新增的 ErrUsage 分层被当成参数错误（退出码 2）。
func TestBatchFileFailuresKeepExitCodeOne(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := filepath.Join(t.TempDir(), "out")
	if err := os.WriteFile(filepath.Join(inputDir, "broken.pdf"), []byte("not a pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := run(&options{inputDir: inputDir, outputDir: outputDir, format: "pdf", workers: 1})
	if err == nil {
		t.Fatal("损坏输入应导致批量失败")
	}
	if errors.Is(err, ErrUsage) {
		t.Fatalf("批量文件失败不应标记为 ErrUsage：%v", err)
	}
}

// TestParseArgsSupportsVersion 记录 --version 与其它参数共存时仍只输出版本号：
// 版本查询不该要求输入文件，也不该先做参数校验。
func TestParseArgsSupportsVersion(t *testing.T) {
	opts, err := parseArgs([]string{"--version", "--format", "pdf", "input.ofd"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.version {
		t.Fatal("--version 未设置 version")
	}
	opts, err = parseArgs([]string{"-h"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.help {
		t.Fatal("-h 未设置 help")
	}
	if got := version.Version; got == "" {
		t.Fatal("version.Version 不应为空")
	}
}
