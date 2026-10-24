package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/internal/version"
	"github.com/zc310/ofd/pkg/integrity"
	"github.com/zc310/ofd/pkg/sign"
)

func integrityCLIFixture(t *testing.T) string {
	t.Helper()
	input, _ := cliFixture(t)
	addIntegrityCLIManifest(t, input)
	return input
}

func addIntegrityCLIManifest(t *testing.T, input string) {
	t.Helper()
	p, err := container.OpenFile(input)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	var data bytes.Buffer
	z := zip.NewWriter(&data)
	var manifest strings.Builder
	manifest.WriteString(`<OFDEntries xmlns="http://www.ofdspec.org/2016" ID="integrity1"><FileList>`)
	for i, entry := range p.Entries() {
		content, err := p.Read(entry.Path)
		if err != nil {
			t.Fatal(err)
		}
		w, err := z.Create(entry.Path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&manifest, `<File ID="f%d" FileLoc="/%s"/>`, i+1, entry.Path)
	}
	manifest.WriteString(`</FileList></OFDEntries>`)
	w, err := z.Create("OFDEntries.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, manifest.String()); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCLIIntegrityReportsAndVerificationExitCode(t *testing.T) {
	input := integrityCLIFixture(t)
	for _, format := range []string{"text", "markdown", "json"} {
		out, diag := call(t, []string{"integrity", "inspect", "--format", format, input}, "", 0)
		if diag != "" || out == "" {
			t.Fatalf("out=%s diag=%s", out, diag)
		}
		if format == "json" {
			var report integrity.Report
			if err := json.Unmarshal([]byte(out), &report); err != nil || report.Status != "incomplete" || !report.Present {
				t.Fatalf("report=%+v err=%v", report, err)
			}
		}
		out, _ = call(t, []string{"integrity", "verify", "--format", format, input}, "", 1)
		if format == "json" && !json.Valid([]byte(out)) {
			t.Fatal("验证诊断污染 JSON")
		}
	}
	for _, action := range []string{"", "inspect", "verify", "protect"} {
		for _, flag := range []string{"--version", "-v"} {
			args := []string{"integrity"}
			if action != "" {
				args = append(args, action)
			}
			out, diag := call(t, append(args, flag), "", 0)
			if strings.TrimSpace(out) != version.Version || diag != "" {
				t.Fatalf("版本输出错误: stdout=%q stderr=%q", out, diag)
			}
		}
	}
}

func TestCLIIntegrityProtectRequiresSignerAndProtectsEvidencePaths(t *testing.T) {
	input, evidence := cliFixture(t)
	call(t, []string{"integrity", "unknown"}, "", 2)
	help, diag := call(t, []string{"integrity"}, "", 0)
	if !strings.Contains(help, "Usage:") || diag != "" {
		t.Fatalf("完整性父命令帮助错误: stdout=%q stderr=%q", help, diag)
	}
	out := filepath.Join(filepath.Dir(input), "protected.ofd")
	call(t, []string{"integrity", "protect", "-o", out, input}, "", 2)
	call(t, []string{"integrity", "protect", "--sign-cmd", "unused", "--trust-root", evidence, "--force", "-o", evidence, input}, "", 2)
	call(t, []string{"integrity", "verify", "--max-entry-bytes", "-1", input}, "", 2)
	call(t, []string{"integrity", "inspect", "--format", "bad", input}, "", 2)
	call(t, []string{"integrity", "verify", "--password-file", "-", "--key", evidence, input}, "", 2)
}

func TestIntegritySignerOutputLimitCannotBeBypassedByReaderFrom(t *testing.T) {
	b := &limitedBuffer{max: 8}
	if _, err := io.Copy(b, bytes.NewBuffer(make([]byte, 100))); err == nil {
		t.Fatal("io.Copy 绕过输出预算")
	}
	if b.Len() > 8 {
		t.Fatal("输出超过预算")
	}
}

func TestIntegrityReportIncludesPhysicalSignatureEvidenceFailures(t *testing.T) {
	report := &integrity.Report{Present: true, Status: "incomplete", PhysicalSignatures: []sign.Verification{{ID: "outer-1", Scope: "physical", Result: &sign.SignatureVerificationResult{Validation: &sign.SignatureValidation{Status: "incomplete", Checks: []sign.VerificationCheck{{Code: "certificates.trust", Status: "incomplete", Message: "缺少证书信任根"}}}}}}}
	for _, format := range []string{"text", "markdown", "json"} {
		var output bytes.Buffer
		if err := renderIntegrity(&output, report, format); err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{"outer-1", "certificates.trust", "缺少证书信任根"} {
			if !strings.Contains(output.String(), text) {
				t.Fatalf("%s 缺少 %s: %s", format, text, &output)
			}
		}
		if format == "json" && !json.Valid(output.Bytes()) {
			t.Fatal("物理签名报告不是有效 JSON")
		}
	}
}
