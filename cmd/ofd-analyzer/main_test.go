package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/pem"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/parser"

	"github.com/xuri/excelize/v2"
)

func TestParseArgs(t *testing.T) {
	opts, err := parseArgs([]string{"--pretty", "--format", "markdown", "--tree", "-o", "report.md", "input.ofd"}, new(bytes.Buffer))
	if err != nil {
		t.Fatal(err)
	}
	if !opts.pretty || opts.format != "markdown" || !opts.formatSet || !opts.tree || opts.output != "report.md" || opts.input != "input.ofd" {
		t.Fatalf("options = %+v", opts)
	}
}

func TestParseArgsSupportsHelp(t *testing.T) {
	for _, arg := range []string{"--help", "-h"} {
		var output bytes.Buffer
		opts, err := parseArgs([]string{arg}, &output)
		if err != nil || !opts.help {
			t.Fatalf("parseArgs(%s) = %+v, err = %v", arg, opts, err)
		}
		if !strings.Contains(output.String(), "ofd-analyzer - OFD 结构分析工具") {
			t.Fatalf("help output = %s", output.String())
		}
	}
}

func TestParseArgsSupportsVersion(t *testing.T) {
	opts, err := parseArgs([]string{"--version"}, new(bytes.Buffer))
	if err != nil || !opts.version {
		t.Fatalf("options = %+v, err = %v", opts, err)
	}
}

func TestValidateOptionsInfersFormatFromOutput(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	for _, test := range []struct {
		extension string
		format    string
	}{
		{extension: ".txt", format: "text"},
		{extension: ".md", format: "markdown"},
		{extension: ".markdown", format: "markdown"},
		{extension: ".json", format: "json"},
		{extension: ".pdf", format: "pdf"},
		{extension: ".xlsx", format: "xlsx"},
	} {
		t.Run(test.extension, func(t *testing.T) {
			opts := &options{input: input, output: filepath.Join(t.TempDir(), "report"+test.extension), format: "text"}
			if err := validateOptions(opts); err != nil {
				t.Fatal(err)
			}
			if opts.format != test.format {
				t.Fatalf("format = %q, want %q", opts.format, test.format)
			}
		})
	}
}

func TestValidateOptionsPreservesExplicitFormatForOutputExtension(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	opts := &options{input: input, output: filepath.Join(t.TempDir(), "report.json"), format: "text", formatSet: true}
	if err := validateOptions(opts); err != nil {
		t.Fatal(err)
	}
	if opts.format != "text" {
		t.Fatalf("explicit format = %q, want text", opts.format)
	}
}

func TestParseArgsSignatureOptions(t *testing.T) {
	opts, err := parseArgs([]string{"--signature-uid", "custom-id", "--signature-format", "raw", "--signature-roots", "roots.pem", "--signature-crls", "revoked.crl", "--signature-revocation-issuers", "issuer.pem", "input.ofd"}, new(bytes.Buffer))
	if err != nil {
		t.Fatal(err)
	}
	if opts.signatureUID != "custom-id" || opts.signatureFormat != "raw" || opts.signatureRoots != "roots.pem" || opts.signatureCRLs != "revoked.crl" || opts.signatureIssuers != "issuer.pem" {
		t.Fatalf("signature options = %+v", opts)
	}
}

func TestValidateOptionsRejectsInvalidSignatureFormat(t *testing.T) {
	opts := &options{input: filepath.Join("..", "..", "test", "testdata", "helloworld.ofd"), format: "text", signatureFormat: "invalid"}
	if err := validateOptions(opts); err == nil {
		t.Fatal("expected invalid signature format error")
	}
}

func TestRunAcceptsSignatureFormatOption(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "999.ofd")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--format", "json", "--signature-format", "raw", input}, &stdout, &stderr)
	if code != exitOK || stderr.Len() != 0 || !strings.Contains(stdout.String(), `"verification_checked":true`) || !strings.Contains(stdout.String(), `"verification_valid":false`) {
		t.Fatalf("exit code = %d, stdout = %s, stderr = %s", code, stdout.String(), stderr.String())
	}
}

func TestRunRejectsInvalidSignatureRoots(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "999.ofd")
	roots := filepath.Join(t.TempDir(), "roots.pem")
	if err := os.WriteFile(roots, []byte("not a certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"--format", "json", "--signature-roots", roots, input}, &stdout, &stderr)
	if code != exitFailed || !strings.Contains(stderr.String(), "分析失败") || !strings.Contains(stdout.String(), `"status":"failed"`) {
		t.Fatalf("exit code = %d, stdout = %s, stderr = %s", code, stdout.String(), stderr.String())
	}
}

func TestRunAcceptsSignatureRoots(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "999.ofd")
	signedValue := readArchiveEntry(t, input, "Doc_0/Signs/Sign_0/SignedValue.dat")
	value, err := parser.ParseSignedValue(signedValue)
	if err != nil {
		t.Fatal(err)
	}
	roots := filepath.Join(t.TempDir(), "roots.pem")
	rootPEM := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: value.SES.Certificate}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: value.SES.TBS.Seal.Certificate})...)
	if err := os.WriteFile(roots, rootPEM, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"--format", "json", "--signature-roots", roots, input}, &stdout, &stderr)
	if code != exitOK || stderr.Len() != 0 || !strings.Contains(stdout.String(), `"trust_checked":true`) || !strings.Contains(stdout.String(), `"trusted":true`) {
		t.Fatalf("exit code = %d, stdout = %s, stderr = %s", code, stdout.String(), stderr.String())
	}
}

func TestRunRejectsInvalidSignatureCRL(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "999.ofd")
	crl := filepath.Join(t.TempDir(), "revoked.crl")
	if err := os.WriteFile(crl, []byte("not a CRL"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"--format", "json", "--signature-crls", crl, input}, &stdout, &stderr)
	if code != exitFailed || !strings.Contains(stderr.String(), "分析失败") || !strings.Contains(stdout.String(), `"status":"failed"`) {
		t.Fatalf("exit code = %d, stdout = %s, stderr = %s", code, stdout.String(), stderr.String())
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
	if !strings.Contains(stdout.String(), `"entries": 9`) {
		t.Fatalf("JSON report omits package summary: %s", stdout.String())
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

func TestRunTreeAddsPackageTree(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--tree", "--format", "json", filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")}, &stdout, &stderr)
	if code != exitOK || stderr.Len() != 0 || !strings.Contains(stdout.String(), `"tree"`) || !strings.Contains(stdout.String(), `"name":"Doc_0"`) {
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

func TestRunWritesXLSXReport(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--format", "xlsx", input}, &stdout, &stderr)
	if code != exitOK || stderr.Len() != 0 || len(stdout.Bytes()) < 1000 {
		t.Fatalf("exit code = %d, xlsx bytes = %d, stderr = %s", code, len(stdout.Bytes()), stderr.String())
	}
	workbook, err := excelize.OpenReader(bytes.NewReader(stdout.Bytes()))
	if err != nil {
		t.Fatalf("invalid XLSX output: %v", err)
	}
	defer func() { _ = workbook.Close() }()
	if title, err := workbook.GetCellValue("汇总", "A1"); err != nil || title != "OFD 分析报告" {
		t.Fatalf("summary title = %q, err = %v", title, err)
	}
}

func readArchiveEntry(t *testing.T, filename, entryName string) []byte {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range reader.File {
		if entry.Name != entryName {
			continue
		}
		entryReader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(entryReader)
		_ = entryReader.Close()
		if err != nil {
			t.Fatal(err)
		}
		return content
	}
	t.Fatalf("archive entry %q not found", entryName)
	return nil
}
