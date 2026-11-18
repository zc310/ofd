package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/validator"
)

func TestRunCustomTagTypeIDFlagPreservesBaseModeAndReportFormats(t *testing.T) {
	data, err := creator.Marshal(creator.Document{
		ID: "cert-cli", Pages: []creator.Page{{}},
		CustomTags: []creator.CustomTag{{TypeID: "cert", Data: []byte("<Certificate/>")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "cert.ofd")
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var output, diagnostics bytes.Buffer
	if code := run([]string{"--format", "json", filename}, &output, &diagnostics); code != 1 || diagnostics.Len() != 0 {
		t.Fatalf("默认模式不应隐式放宽 XSD: code=%d, stderr=%s", code, &diagnostics)
	}
	for _, format := range []string{"text", "markdown", "json"} {
		t.Run(format, func(t *testing.T) {
			output.Reset()
			diagnostics.Reset()
			if code := run([]string{"--custom-tag-type-id", "--format", format, filename}, &output, &diagnostics); code != 0 || diagnostics.Len() != 0 {
				t.Fatalf("标引模式失败: code=%d, stderr=%s, stdout=%s", code, &diagnostics, &output)
			}
			if format == "json" {
				var report validator.Report
				if err := json.Unmarshal(output.Bytes(), &report); err != nil {
					t.Fatal(err)
				}
				if report.HasErrors() || report.Summary.Warnings != 0 {
					t.Fatalf("标引报告含问题: %+v", report.Issues)
				}
			}
		})
	}
}
