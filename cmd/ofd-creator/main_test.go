package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/pkg/validator"
)

func TestParseArgsTextCodeDeltaOptionDefaultsOff(t *testing.T) {
	opts, err := parseArgs([]string{"-i", "input.yaml", "-o", "output.ofd"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if opts.completeTextCodeDeltas {
		t.Fatal("complete text code deltas should default to false")
	}

	opts, err = parseArgs([]string{"-i", "input.yaml", "-o", "output.ofd", "--complete-text-code-deltas"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.completeTextCodeDeltas {
		t.Fatal("complete text code deltas option was not enabled")
	}
}

func TestRunCreatesValidatedYAMLDocument(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "document.yaml")
	output := filepath.Join(directory, "result.ofd")
	manifest := []byte("version: 1\ndocument:\n  id: cli-test\n  title: CLI 测试\n  page_size:\n    name: A4\npages:\n  - items:\n      - type: text\n        x: 20\n        y: 30\n        width: 100\n        height: 10\n        value: hello\n        font: SimSun\n")
	if err := os.WriteFile(input, manifest, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-i", input, "-o", output, "--validate"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("run exit code = %d, stderr = %s", code, stderr.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	if report := instance.ValidateReader(t.Context(), bytes.NewReader(data), output); report.HasErrors() {
		t.Fatalf("generated OFD failed validation: %+v", report.Issues)
	}
}

func TestRunRejectsUnknownManifestField(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "document.json")
	if err := os.WriteFile(input, []byte(`{"document":{"id":"test"},"pages":[{}],"unknown":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--input", input, "--output", filepath.Join(directory, "result.ofd")}, &stdout, &stderr); code != exitResource {
		t.Fatalf("run exit code = %d, stderr = %s", code, stderr.String())
	}
}

func TestRunCreatesTOMLDocument(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "document.toml")
	output := filepath.Join(directory, "result.ofd")
	manifest := []byte("version = 1\n\n[document]\nid = \"toml-cli-test\"\n\n[[pages]]\n")
	if err := os.WriteFile(input, manifest, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-i", input, "-o", output, "--check"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("run exit code = %d, stderr = %s", code, stderr.String())
	}
}

func TestRunCheckDoesNotRequireOutput(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "document.yaml")
	if err := os.WriteFile(input, []byte("version: 1\ndocument:\n  id: check-test\npages:\n  - items: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-i", input, "--check"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("run exit code = %d, stderr = %s", code, stderr.String())
	}
}

func TestWriteOutputUsesAtomicReplacement(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "result.ofd")
	if err := writeOutput(output, []byte("new"), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("output = %q, want new", data)
	}
}

func TestParseExportArgs(t *testing.T) {
	opts, err := parseExportArgs([]string{"-i", "input.ofd", "-o", "document.yaml", "--asset-root", "assets"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if opts.input != "input.ofd" || opts.output != "document.yaml" || opts.assetRoot != "assets" || opts.format != "" || opts.document != -1 {
		t.Fatalf("export options = %+v", opts)
	}
	opts, err = parseExportArgs([]string{"--document", "1", "input.ofd", "document.yaml"}, &bytes.Buffer{})
	if err != nil || opts.document != 1 {
		t.Fatalf("selected export options = %+v, error = %v", opts, err)
	}
}

func TestRunExportWritesManifest(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "exported", "document.yaml")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"export", "-i", filepath.Join("..", "..", "test", "testdata", "helloworld.ofd"), "-o", output, "--asset-root", "assets"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("run export exit code = %d, stderr = %s", code, stderr.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("version: 1")) || !bytes.Contains(data, []byte("pages:")) {
		t.Fatalf("exported manifest is missing required fields")
	}
	if _, err := os.Stat(filepath.Join(directory, "exported", "assets")); err != nil {
		t.Fatalf("asset directory was not created: %v", err)
	}
}

func TestRunExportWritesJSONAndTOMLManifests(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	for _, format := range []string{"json", "toml"} {
		t.Run(format, func(t *testing.T) {
			directory := t.TempDir()
			output := filepath.Join(directory, "document."+format)
			var stdout, stderr bytes.Buffer
			if code := run([]string{"export", "-i", input, "-o", output}, &stdout, &stderr); code != exitOK {
				t.Fatalf("run %s export exit code = %d, stderr = %s", format, code, stderr.String())
			}
			if _, err := os.Stat(output); err != nil {
				t.Fatalf("%s manifest was not created: %v", format, err)
			}
		})
	}
}

func TestRunExportJSONIndent(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	directory := t.TempDir()
	compact := filepath.Join(directory, "compact.json")
	pretty := filepath.Join(directory, "pretty.json")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"export", "-i", input, "-o", compact}, &stdout, &stderr); code != exitOK {
		t.Fatalf("compact JSON export exit code = %d, stderr = %s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"export", "-i", input, "-o", pretty, "--json-indent"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("indented JSON export exit code = %d, stderr = %s", code, stderr.String())
	}
	compactData, err := os.ReadFile(compact)
	if err != nil {
		t.Fatal(err)
	}
	prettyData, err := os.ReadFile(pretty)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(compactData, []byte("\n")) != 1 || !bytes.Contains(prettyData, []byte("\n  \"")) {
		t.Fatalf("JSON indentation mismatch: compact=%q pretty=%q", compactData, prettyData)
	}
}

func TestRunExportRejectsMultipleDocumentBodies(t *testing.T) {
	directory := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := run([]string{"export", "-i", filepath.Join("..", "..", "test", "testdata", "multi_demo.ofd"), "-o", filepath.Join(directory, "document.yaml"), "--asset-root", "assets"}, &stdout, &stderr); code != exitResource {
		t.Fatalf("run export exit code = %d, stderr = %s", code, stderr.String())
	}
}

func TestRunExportSelectsDocumentBody(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "document.yaml")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"export", "-i", filepath.Join("..", "..", "test", "testdata", "multi_demo.ofd"), "-o", output, "--document", "1"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("run selected export exit code = %d, stderr = %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(directory, "assets")); err != nil {
		t.Fatalf("default asset directory was not created: %v", err)
	}
}

func TestRunExportAllWritesBundle(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bundle")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"export-all", "-i", filepath.Join("..", "..", "test", "testdata", "multi_demo.ofd"), "-o", root}, &stdout, &stderr); code != exitOK {
		t.Fatalf("run export-all exit code = %d, stderr = %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, "index.yaml")); err != nil {
		t.Fatalf("bundle index was not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "documents", "000.yaml")); err != nil {
		t.Fatalf("first document manifest was not created: %v", err)
	}
}

func TestRunExportAllRejectsStandardOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"export-all", "-i", filepath.Join("..", "..", "test", "testdata", "multi_demo.ofd"), "-o", "-"}, &stdout, &stderr); code != exitUsage {
		t.Fatalf("run export-all stdout exit code = %d, stderr = %s", code, stderr.String())
	}
}

func TestParseMergeArgs(t *testing.T) {
	opts, err := parseMergeArgs([]string{"-i", "a.ofd", "-i", "b.ofd", "-o", "merged.ofd", "--deterministic"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.inputs) != 2 || opts.inputs[0] != "a.ofd" || opts.inputs[1] != "b.ofd" || opts.output != "merged.ofd" || !opts.deterministic {
		t.Fatalf("merge options = %+v", opts)
	}

	opts, err = parseMergeArgs([]string{"-o", "merged.ofd", "a.ofd", "b.ofd"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.inputs) != 2 || opts.inputs[0] != "a.ofd" || opts.inputs[1] != "b.ofd" {
		t.Fatalf("positional merge options = %+v", opts)
	}
}

func TestRunMergeWritesValidatedDocument(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "merged.ofd")
	var stdout, stderr bytes.Buffer
	hello := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	helloworld := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	if code := run([]string{"merge", "-i", hello, "-i", helloworld, "-o", output, "--validate"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("run merge exit code = %d, stderr = %s", code, stderr.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	if report := instance.ValidateReader(t.Context(), bytes.NewReader(data), output); report.HasErrors() {
		t.Fatalf("merged OFD failed validation: %+v", report.Issues)
	}
}

func TestRunMergeRejectsUsageErrors(t *testing.T) {
	directory := t.TempDir()
	hello := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	output := filepath.Join(directory, "merged.ofd")
	cases := [][]string{
		{"merge", "-o", output},
		{"merge", "-i", hello},
		{"merge", "-i", "-", "-o", output},
		{"merge", "-i", hello, "-o", hello},
		{"merge", "-i", hello, "-o", output, "--compression", "bogus"},
		{"merge", "-i", hello, "-o", output, "--orphans", "bogus"},
		{"merge", "-i", hello, "-o", output, "--max-entry-mb", "-1"},
	}
	for _, args := range cases {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != exitUsage {
			t.Fatalf("run %v exit code = %d, want %d, stderr = %s", args, code, exitUsage, stderr.String())
		}
	}
}

func TestRunMergeSignatureModes(t *testing.T) {
	directory := t.TempDir()
	signed := filepath.Join("..", "..", "test", "testdata", "999.ofd")
	output := filepath.Join(directory, "merged.ofd")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"merge", "-i", signed, "-i", signed, "-o", output}, &stdout, &stderr); code != exitResource {
		t.Fatalf("preserve 模式下签名无法保留时 exit code = %d, want %d, stderr = %s", code, exitResource, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"merge", "-i", signed, "-i", signed, "-o", output, "--signatures", "drop"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("drop 模式 exit code = %d, stderr = %s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"merge", "-i", signed, "-o", output, "--signatures", "bogus"}, &stdout, &stderr); code != exitUsage {
		t.Fatalf("非法签名模式 exit code = %d, want %d", code, exitUsage)
	}
}

func TestRunMergeReportsSignatureRewriteWarning(t *testing.T) {
	directory := t.TempDir()
	signed := filepath.Join("..", "..", "test", "testdata", "999.ofd")
	output := filepath.Join(directory, "merged.ofd")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"merge", "-i", signed, "-i", signed, "-o", output, "--signatures", "rewrite"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("rewrite 模式 exit code = %d, stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("警告")) {
		t.Fatalf("应输出签名重写警告, stderr = %s", stderr.String())
	}
}

func TestRunMergePages(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "merged.ofd")
	hello := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	helloworld := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"merge", "-i", hello, "-i", helloworld, "-o", output, "--pages", "2,1", "--validate"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("run merge --pages exit code = %d, stderr = %s", code, stderr.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	if report := instance.ValidateReader(t.Context(), bytes.NewReader(data), output); report.HasErrors() {
		t.Fatalf("选页合并结果未通过校验: %+v", report.Issues)
	}
}

func TestRunMergePagesRejectsIncompatibleFlags(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "merged.ofd")
	hello := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	cases := [][]string{
		{"merge", "-i", hello, "-o", output, "--pages", "0"},
		{"merge", "-i", hello, "-o", output, "--pages", "1", "--signatures", "rewrite"},
		{"merge", "-i", hello, "-o", output, "--pages", "1", "--orphans", "ignore"},
		{"merge", "-i", hello, "-o", output, "--pages", "1", "--max-entries", "5"},
	}
	for _, args := range cases {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != exitUsage {
			t.Fatalf("run %v exit code = %d, want %d, stderr = %s", args, code, exitUsage, stderr.String())
		}
	}
}

func TestRunMergePagesBySource(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "merged.ofd")
	hello := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	helloworld := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"merge", "-i", hello, "-i", helloworld, "-o", output, "--pages", "s2:1;s1", "--validate"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("run merge --pages s2:1;s1 exit code = %d, stderr = %s", code, stderr.String())
	}
}

func TestRunMergePagesOverridesMetadata(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "merged.ofd")
	hello := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"merge", "-i", hello, "-o", output, "--pages", "1", "--document-id", "cli-id", "--title", "CLI 标题", "--validate"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("run merge metadata exit code = %d, stderr = %s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"merge", "-i", hello, "-o", output, "--title", "无 pages"}, &stdout, &stderr); code != exitUsage {
		t.Fatalf("无 --pages 时元数据选项 exit code = %d, want %d", code, exitUsage)
	}
}

func TestRunMergePagesConcurrency(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "merged.ofd")
	hello := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	helloworld := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"merge", "-i", hello, "-i", helloworld, "-o", output, "--pages", "1", "--workers", "2", "--validate"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("run merge --workers exit code = %d, stderr = %s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"merge", "-i", hello, "-o", output, "--workers", "2"}, &stdout, &stderr); code != exitUsage {
		t.Fatalf("无 --pages 时 --workers exit code = %d, want %d", code, exitUsage)
	}
}
