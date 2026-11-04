package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/pkg/analyzer"
)

func TestAnalyzerDoesNotExposeDocumentValidationOption(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--validate", "strict", "input.ofd"}, &stdout, &stderr); code != exitUsage || stdout.Len() != 0 || !strings.Contains(stderr.String(), "unknown flag") {
		t.Fatalf("分析器不应接受规范校验选项：code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestRunReportsCompletedAnalysisWithSizeWarningAndHonorsFailOnWarning(t *testing.T) {
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for name, content := range map[string]string{
		"OFD.xml":         `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>x</DocID></DocInfo><DocRoot>Document.xml</DocRoot></DocBody></OFD>`,
		"Document.xml":    `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>4</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages><Attachments>Attachments.xml</Attachments></Document>`,
		"Page.xml":        `<Page xmlns="http://www.ofdspec.org/2016"><Actions><Action Event="PO"><URI URI="https://example.invalid/"/></Action></Actions></Page>`,
		"Attachments.xml": `<Attachments xmlns="http://www.ofdspec.org/2016"><Attachment ID="4" Name="sample" Size="1"><FileLoc>sample.txt</FileLoc></Attachment></Attachments>`,
		"sample.txt":      "short",
	} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "diagnostic.ofd")
	if err := os.WriteFile(input, archive.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	for _, fail := range []bool{false, true} {
		var stdout, stderr bytes.Buffer
		args := []string{"--format", "json", "--no-signatures"}
		want := exitOK
		if fail {
			args = append(args, "--fail-on-warning")
			want = exitFailed
		}
		args = append(args, input)
		if code := run(args, &stdout, &stderr); code != want || stderr.Len() != 0 {
			t.Fatalf("退出码或输出流错误：%d %s", code, stderr.String())
		}
		var report analyzer.Report
		if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		if report.Status != analyzer.StatusComplete || report.SchemaVersion != "1" || len(report.Actions) != 1 || len(report.Diagnostics) != 2 || len(report.Conclusions) == 0 {
			t.Fatalf("CLI 丢失诊断或错误标记完成度：%s", stdout.String())
		}
	}
}
