package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/core"
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

func TestRunMergeVerifySignatures(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "merged.ofd")
	signed := filepath.Join("..", "..", "test", "testdata", "999.ofd")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"merge", "-i", signed, "-o", output, "--verify-signatures"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("verify signatures exit code = %d, stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("签名汇总")) || !bytes.Contains(stderr.Bytes(), []byte("摘要有效")) {
		t.Fatalf("应输出签名汇总与验证结果, stderr = %s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	hello := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	if code := run([]string{"merge", "-i", hello, "-o", output, "--verify-signatures"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("no signature exit code = %d, stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("没有签名")) {
		t.Fatalf("无签名文档应提示没有签名, stderr = %s", stderr.String())
	}
}

func TestRunMergeSignCmd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("使用 cat 作为外部签名命令")
	}
	directory := t.TempDir()
	output := filepath.Join(directory, "signed.ofd")
	hello := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"merge", "-i", hello, "-o", output, "--pages", "1", "--sign-cmd", "cat", "--verify-signatures"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("run merge --sign-cmd exit code = %d, stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("摘要有效")) {
		t.Fatalf("验签应报告摘要有效, stderr = %s", stderr.String())
	}
}

func TestRunReplaceSetAddDelete(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	setFile := filepath.Join(directory, "doc.xml")
	if err := os.WriteFile(setFile, []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?><Document/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(directory, "replaced.ofd")
	var stdout, stderr bytes.Buffer
	args := []string{
		"replace", "-i", input, "-o", output,
		"--set", "Doc_0/Document.xml=" + setFile,
		"--add", "Doc_0/Res/note.txt=" + setFile,
		"--delete", "Doc_0/DocumentRes.xml",
	}
	if code := run(args, &stdout, &stderr); code != exitOK {
		t.Fatalf("run replace exit code = %d, stderr = %s", code, stderr.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := core.OpenBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()
	if !pkg.Has("Doc_0/Res/note.txt") {
		t.Fatal("新增条目缺失")
	}
	if pkg.Has("Doc_0/DocumentRes.xml") {
		t.Fatal("删除条目仍存在")
	}
	content, err := pkg.Read("Doc_0/Document.xml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(content, []byte("<Document/>")) {
		t.Fatalf("替换内容不符: %s", content)
	}
}

func TestRunReplaceUsageErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"replace", "-i", "in.ofd"}, &stdout, &stderr); code != exitUsage {
		t.Fatalf("缺输出应返回 usage, got %d", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"replace", "-i", "in.ofd", "-o", "out.ofd"}, &stdout, &stderr); code != exitUsage {
		t.Fatalf("无操作应返回 usage, got %d", code)
	}
}

func TestRunReplaceXMLCheckDefault(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	badFile := filepath.Join(directory, "bad.xml")
	if err := os.WriteFile(badFile, []byte("<Document><unclosed>"), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(directory, "out.ofd")
	var stdout, stderr bytes.Buffer
	if code := run([]string{
		"replace", "-i", input, "-o", output,
		"--set", "Doc_0/Document.xml=" + badFile,
	}, &stdout, &stderr); code != exitResource {
		t.Fatalf("默认应拒绝坏 XML，exit=%d, stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{
		"replace", "-i", input, "-o", output,
		"--set", "Doc_0/Document.xml=" + badFile, "--no-validate",
	}, &stdout, &stderr); code != exitOK {
		t.Fatalf("--no-validate 应跳过 XML 检查，exit=%d, stderr=%s", code, stderr.String())
	}
}

func TestRunReplaceVerifySignatures(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	setFile := filepath.Join(directory, "doc.xml")
	if err := os.WriteFile(setFile, []byte("<Document/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(directory, "out.ofd")
	var stdout, stderr bytes.Buffer
	if code := run([]string{
		"replace", "-i", input, "-o", output,
		"--set", "Doc_0/Document.xml=" + setFile,
		"--verify-signatures",
	}, &stdout, &stderr); code != exitOK {
		t.Fatalf("--verify-signatures exit=%d, stderr=%s", code, stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("没有签名")) {
		t.Fatalf("应报告输出没有签名, stderr=%s", stderr.String())
	}
}

func TestRunReplaceSignCmd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("使用 cat 作为外部签名命令")
	}
	directory := t.TempDir()
	input := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	setFile := filepath.Join(directory, "doc.xml")
	if err := os.WriteFile(setFile, []byte("<Document/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(directory, "signed.ofd")
	var stdout, stderr bytes.Buffer
	if code := run([]string{
		"replace", "-i", input, "-o", output,
		"--set", "Doc_0/Document.xml=" + setFile,
		"--sign-cmd", "cat", "--verify-signatures",
	}, &stdout, &stderr); code != exitOK {
		t.Fatalf("run replace --sign-cmd exit=%d, stderr=%s", code, stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("摘要有效")) {
		t.Fatalf("验签应报告摘要有效, stderr=%s", stderr.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := core.OpenBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()
	if !pkg.Has("Doc_0/Signatures/Signature_sign-1.xml") {
		t.Fatal("替换后输出缺少追加的签名文件")
	}
	content, err := pkg.Read("Doc_0/Document.xml")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "<Document/>" {
		t.Fatalf("替换后的条目内容不符: %s", content)
	}
}

func TestRunMergeSignMetadataFlags(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("使用 cat 作为外部签名命令")
	}
	directory := t.TempDir()
	output := filepath.Join(directory, "signed.ofd")
	hello := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	var stdout, stderr bytes.Buffer
	args := []string{
		"merge", "-i", hello, "-o", output, "--pages", "1", "--sign-cmd", "cat",
		"--sign-id", "sign-9",
		"--sign-provider", "Acme Signer",
		"--sign-provider-version", "2.1",
		"--sign-company", "Acme Inc",
		"--sign-method", "1.2.156.10197.1.501",
		"--sign-check-method", "SM3",
	}
	if code := run(args, &stdout, &stderr); code != exitOK {
		t.Fatalf("run merge exit code = %d, stderr = %s", code, stderr.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := core.OpenBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()
	signature, err := pkg.Read("Doc_0/Signatures/Signature_sign-9.xml")
	if err != nil {
		t.Fatalf("读取签名 XML 失败: %v", err)
	}
	for _, want := range []string{`ProviderName="Acme Signer"`, `Version="2.1"`, `Company="Acme Inc"`, "1.2.156.10197.1.501", "SM3"} {
		if !bytes.Contains(signature, []byte(want)) {
			t.Fatalf("Signature.xml 缺少 %q:\n%s", want, signature)
		}
	}
}

func TestRunWatermarkAdd(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	output := filepath.Join(directory, "wm.ofd")
	var stdout, stderr bytes.Buffer
	if code := run([]string{
		"watermark", "add", "-i", input, "-o", output,
		"--text", "保密资料", "--color", "200 100 50",
	}, &stdout, &stderr); code != exitOK {
		t.Fatalf("run watermark add exit code = %d, stderr = %s", code, stderr.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := core.OpenBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()
	page, err := pkg.Read("Doc_0/Annotations/Page_1.xml")
	if err != nil {
		t.Fatalf("缺少页面注解文件: %v", err)
	}
	for _, want := range []string{`ID="6"`, `Type="Watermark"`, `ReadOnly="false"`, `Boundary="0 0 210 297"`, `Value="200 100 50"`} {
		if !bytes.Contains(page, []byte(want)) {
			t.Fatalf("页面注解缺少 %q:\n%s", want, page)
		}
	}
	if count := strings.Count(string(page), "保密资料"); count <= 1 {
		t.Fatalf("默认平铺应产生多个 TextCode，实际出现 %d 次:\n%s", count, page)
	}
}

func TestRunWatermarkAddValidate(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	output := filepath.Join(directory, "wm.ofd")
	var stdout, stderr bytes.Buffer
	if code := run([]string{
		"watermark", "add", "-i", input, "-o", output, "--text", "保密资料", "--validate",
	}, &stdout, &stderr); code != exitOK {
		t.Fatalf("run watermark add --validate exit code = %d, stderr = %s", code, stderr.String())
	}
}

func TestRunWatermarkReplaceRemove(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	added := filepath.Join(directory, "wm.ofd")
	var stdout, stderr bytes.Buffer
	if code := run([]string{
		"watermark", "add", "-i", input, "-o", added, "--text", "初稿",
	}, &stdout, &stderr); code != exitOK {
		t.Fatalf("add exit code = %d, stderr = %s", code, stderr.String())
	}
	replaced := filepath.Join(directory, "wm2.ofd")
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{
		"watermark", "replace", "-i", added, "-o", replaced, "--text", "定稿",
	}, &stdout, &stderr); code != exitOK {
		t.Fatalf("replace exit code = %d, stderr = %s", code, stderr.String())
	}
	data, err := os.ReadFile(replaced)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := core.OpenBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	page, err := pkg.Read("Doc_0/Annotations/Page_1.xml")
	_ = pkg.Close()
	if err != nil {
		t.Fatalf("读取替换后页面注解失败: %v", err)
	}
	if !bytes.Contains(page, []byte(`ID="6"`)) || strings.Count(string(page), "定稿") <= 1 {
		t.Fatalf("替换应保留 ID 并平铺更新内容:\n%s", page)
	}
	removed := filepath.Join(directory, "wm3.ofd")
	stdout.Reset()
	stderr.Reset()
	// 加/改的水印显式写 ReadOnly=false，删除无需额外开关。
	if code := run([]string{
		"watermark", "remove", "-i", replaced, "-o", removed, "--match-id", "6",
	}, &stdout, &stderr); code != exitOK {
		t.Fatalf("remove exit code = %d, stderr = %s", code, stderr.String())
	}
	data, err = os.ReadFile(removed)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err = core.OpenBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()
	if pkg.Has("Doc_0/Annotations/Page_1.xml") {
		t.Fatal("删除后页面注解文件应被清理")
	}
}

func TestRunWatermarkUsageErrors(t *testing.T) {
	hello := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	var stdout, stderr bytes.Buffer
	for name, args := range map[string][]string{
		"缺操作":   {"watermark", "-i", hello, "-o", "out.ofd"},
		"未知操作":  {"watermark", "rename", "-i", hello, "-o", "out.ofd"},
		"缺输入":   {"watermark", "add", "-o", "out.ofd"},
		"缺输出":   {"watermark", "add", "-i", hello},
		"外观互斥":  {"watermark", "add", "-i", hello, "-o", "out.ofd", "--text", "x", "--appearance", "a.xml"},
		"图片互斥":  {"watermark", "add", "-i", hello, "-o", "out.ofd", "--text", "x", "--image", "a.png"},
		"布局错误":  {"watermark", "add", "-i", hello, "-o", "out.ofd", "--text", "x", "--layout", "rotate"},
		"透明度范围": {"watermark", "add", "-i", hello, "-o", "out.ofd", "--text", "x", "--opacity", "101"},
		"旋转需文字": {"watermark", "add", "-i", hello, "-o", "out.ofd", "--image", "a.png", "--rotate", "45"},
		"字体不存在": {"watermark", "add", "-i", hello, "-o", "out.ofd", "--text", "x", "--font", "999"},
		"颜色错误":  {"watermark", "add", "-i", hello, "-o", "out.ofd", "--text", "x", "--color", "1 2"},
		"覆盖输入":  {"watermark", "add", "-i", hello, "-o", hello},
	} {
		t.Run(name, func(t *testing.T) {
			stdout.Reset()
			stderr.Reset()
			if code := run(args, &stdout, &stderr); code != exitUsage {
				t.Fatalf("应返回 usage, got %d, stderr=%s", code, stderr.String())
			}
		})
	}
}

func TestRunWatermarkLayoutCenter(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	output := filepath.Join(directory, "center.ofd")
	var stdout, stderr bytes.Buffer
	if code := run([]string{
		"watermark", "add", "-i", input, "-o", output,
		"--text", "保密资料", "--layout", "center", "--opacity", "50", "--validate",
	}, &stdout, &stderr); code != exitOK {
		t.Fatalf("run watermark add --layout center exit code = %d, stderr = %s", code, stderr.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := core.OpenBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()
	page, err := pkg.Read("Doc_0/Annotations/Page_1.xml")
	if err != nil {
		t.Fatalf("缺少页面注解文件: %v", err)
	}
	for _, want := range []string{"<TextCode X=\"87\" Y=\"144\">保密资料</TextCode>", `Alpha="127"`} {
		if !bytes.Contains(page, []byte(want)) {
			t.Fatalf("居中外观缺少 %q:\n%s", want, page)
		}
	}
	if count := strings.Count(string(page), "保密资料"); count != 1 {
		t.Fatalf("居中应产生单个 TextCode，实际 %d 次:\n%s", count, page)
	}
}

func TestRunWatermarkRotate(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	output := filepath.Join(directory, "rotate.ofd")
	var stdout, stderr bytes.Buffer
	if code := run([]string{
		"watermark", "add", "-i", input, "-o", output,
		"--text", "保密资料", "--layout", "center", "--rotate", "45", "--validate",
	}, &stdout, &stderr); code != exitOK {
		t.Fatalf("run watermark add --rotate exit code = %d, stderr = %s", code, stderr.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := core.OpenBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()
	page, err := pkg.Read("Doc_0/Annotations/Page_1.xml")
	if err != nil {
		t.Fatalf("缺少页面注解文件: %v", err)
	}
	for _, want := range []string{`CTM="0.7071 0.7071 -0.7071 0.7071 0 0"`, `<TextCode X="155.524" Y="28.6316">保密资料</TextCode>`} {
		if !bytes.Contains(page, []byte(want)) {
			t.Fatalf("旋转外观缺少 %q:\n%s", want, page)
		}
	}
}

func TestRunWatermarkSign(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("使用 cat 作为外部签名命令")
	}
	directory := t.TempDir()
	input := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	output := filepath.Join(directory, "signed-watermark.ofd")
	var stdout, stderr bytes.Buffer
	if code := run([]string{
		"watermark", "add", "-i", input, "-o", output,
		"--text", "保密资料", "--layout", "center", "--opacity", "50",
		"--sign-cmd", "cat", "--verify-signatures",
	}, &stdout, &stderr); code != exitOK {
		t.Fatalf("run watermark add --sign-cmd exit=%d, stderr=%s", code, stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("摘要有效")) {
		t.Fatalf("验签应报告摘要有效, stderr=%s", stderr.String())
	}
	pkg, err := core.OpenFile(output)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()
	if !pkg.Has("Doc_0/Signatures/Signature_sign-1.xml") {
		t.Fatal("水印输出缺少追加的签名文件")
	}
}

func TestRunWatermarkImage(t *testing.T) {
	directory := t.TempDir()
	imageFile := filepath.Join(directory, "logo.png")
	if err := os.WriteFile(imageFile, testPNG(120, 60), 0o600); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	output := filepath.Join(directory, "image.ofd")
	var stdout, stderr bytes.Buffer
	if code := run([]string{
		"watermark", "add", "-i", input, "-o", output,
		"--image", imageFile, "--image-width", "40", "--opacity", "50", "--validate",
	}, &stdout, &stderr); code != exitOK {
		t.Fatalf("run watermark add --image exit code = %d, stderr = %s", code, stderr.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := core.OpenBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()
	res, err := pkg.Read("Doc_0/DocumentRes.xml")
	if err != nil {
		t.Fatalf("读取 DocumentRes 失败: %v", err)
	}
	if !bytes.Contains(res, []byte(`Type="Image" Format="PNG"`)) || !bytes.Contains(res, []byte("<MediaFile>Images/Image_")) {
		t.Fatalf("DocumentRes 未注册图片媒体:\n%s", res)
	}
	match := regexp.MustCompile(`Images/(Image_\d+\.png)`).FindSubmatch(res)
	if match == nil {
		t.Fatalf("DocumentRes 缺少 MediaFile:\n%s", res)
	}
	name := "Doc_0/Res/" + string(match[0])
	if !pkg.Has(name) {
		t.Fatalf("缺少图片条目 %s", name)
	}
	page, err := pkg.Read("Doc_0/Annotations/Page_1.xml")
	if err != nil {
		t.Fatalf("缺少页面注解文件: %v", err)
	}
	if !bytes.Contains(page, []byte(`ResourceID="`)) || !bytes.Contains(page, []byte(`Type="Watermark"`)) {
		t.Fatalf("页面注解缺少 ImageObject 引用:\n%s", page)
	}
}

func TestRunWatermarkAddResolvesFontName(t *testing.T) {
	// --font 传字体名称时应解析为数值字体 ID，产出合法外观；默认（不传 --font）
	// 也会自动选用文档中的字体，不再写出默认的非法 ID 4。
	directory := t.TempDir()
	input := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	output := filepath.Join(directory, "font-name.ofd")
	var stdout, stderr bytes.Buffer
	if code := run([]string{
		"watermark", "add", "-i", input, "-o", output, "--text", "保密", "--font", "楷体", "--validate",
	}, &stdout, &stderr); code != exitOK {
		t.Fatalf("--font 楷体 exit code = %d, stderr = %s", code, stderr.String())
	}
	page := readPackageEntry(t, output, "Doc_0/Annotations/Page_1.xml")
	if !bytes.Contains(page, []byte(`Font="4"`)) {
		t.Fatalf("字体名应解析为 ID 4:\n%s", page)
	}

	defaulted := filepath.Join(directory, "font-default.ofd")
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{
		"watermark", "add", "-i", input, "-o", defaulted, "--text", "保密",
	}, &stdout, &stderr); code != exitOK {
		t.Fatalf("默认字体 exit code = %d, stderr = %s", code, stderr.String())
	}
	page = readPackageEntry(t, defaulted, "Doc_0/Annotations/Page_1.xml")
	if !bytes.Contains(page, []byte(`Font="4"`)) {
		t.Fatalf("默认字体应选文档中的 ID 4:\n%s", page)
	}
}

func TestRunWatermarkFontFromPublicRes(t *testing.T) {
	// multi_demo.ofd 的字体位于 PublicRes，且 ID 20/21，默认自动选 ID 20。
	directory := t.TempDir()
	input := filepath.Join("..", "..", "test", "testdata", "multi_demo.ofd")
	output := filepath.Join(directory, "public-res.ofd")
	var stdout, stderr bytes.Buffer
	if code := run([]string{
		"watermark", "add", "-i", input, "-o", output, "--text", "保密", "--font", "宋体",
	}, &stdout, &stderr); code != exitOK {
		t.Fatalf("PublicRes 字体 exit code = %d, stderr = %s", code, stderr.String())
	}
	page := readPackageEntry(t, output, "Doc_0/Annotations/Page_1000.xml")
	if !bytes.Contains(page, []byte(`Font="20"`)) {
		t.Fatalf("宋体应解析为 PublicRes 中的 ID 20:\n%s", page)
	}
}

func TestRunWatermarkImageRemoveReclaims(t *testing.T) {
	directory := t.TempDir()
	imageFile := filepath.Join(directory, "logo.png")
	if err := os.WriteFile(imageFile, testPNG(40, 20), 0o600); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	added := filepath.Join(directory, "image.ofd")
	var stdout, stderr bytes.Buffer
	if code := run([]string{
		"watermark", "add", "-i", input, "-o", added, "--image", imageFile,
	}, &stdout, &stderr); code != exitOK {
		t.Fatalf("add image exit code = %d, stderr = %s", code, stderr.String())
	}
	if res := readPackageBytesEntry(t, added, "Doc_0/DocumentRes.xml"); !bytes.Contains(res, []byte("<MediaFile>")) {
		t.Fatalf("应注册图片媒体:\n%s", res)
	}

	removed := filepath.Join(directory, "clean.ofd")
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{
		"watermark", "remove", "-i", added, "-o", removed,
	}, &stdout, &stderr); code != exitOK {
		t.Fatalf("remove exit code = %d, stderr = %s", code, stderr.String())
	}
	data, err := os.ReadFile(removed)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := core.OpenBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()
	res, _ := pkg.Read("Doc_0/DocumentRes.xml")
	if bytes.Contains(res, []byte("MultiMedias")) || bytes.Contains(res, []byte("<MediaFile>")) {
		t.Fatalf("删除水印后应回收媒体:\n%s", res)
	}
	for _, entry := range pkg.Entries() {
		if strings.HasPrefix(entry.Name, "Doc_0/Res/Images/") {
			t.Fatalf("删除水印后应删除图片条目 %s", entry.Name)
		}
	}
}

func readPackageBytesEntry(t *testing.T, path, entry string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := core.OpenBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()
	content, err := pkg.Read(entry)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", entry, err)
	}
	return content
}

func TestRunWatermarkInvisible(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join("..", "..", "test", "testdata", "hello.ofd")
	output := filepath.Join(directory, "invisible.ofd")
	var stdout, stderr bytes.Buffer
	if code := run([]string{
		"watermark", "add", "-i", input, "-o", output, "--text", "保密", "--visible=false",
	}, &stdout, &stderr); code != exitOK {
		t.Fatalf("--visible=false exit code = %d, stderr = %s", code, stderr.String())
	}
	page := readPackageEntry(t, output, "Doc_0/Annotations/Page_1.xml")
	if !bytes.Contains(page, []byte(`Visible="false"`)) {
		t.Fatalf("--visible=false 应写出 Visible=\"false\":\n%s", page)
	}
}

func readPackageEntry(t *testing.T, path, entry string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := core.OpenBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()
	content, err := pkg.Read(entry)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", entry, err)
	}
	return content
}

// testPNG 生成指定尺寸的纯色 PNG 字节。
func testPNG(width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			img.Set(x, y, color.RGBA{R: 200, G: 50, B: 80, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		panic(err)
	}
	return buffer.Bytes()
}
