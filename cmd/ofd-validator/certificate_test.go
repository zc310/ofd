package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/internal/manifest"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/validator"
)

func TestCertificateMetadataFlagReportsLocalScopeInHeadlessFormats(t *testing.T) {
	input := filepath.Join("..", "ofd-creator", "examples", "certificate-tags.yaml")
	m, base, err := manifest.Load(input, "yaml")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := m.Build(base, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []bool{false, true} {
		if invalid {
			doc.CustomDatas = nil
		}
		data, err := creator.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		filename := filepath.Join(t.TempDir(), "certificate.ofd")
		if err := os.WriteFile(filename, data, 0o600); err != nil {
			t.Fatal(err)
		}
		for _, format := range []string{"text", "markdown", "json"} {
			for _, structural := range []bool{false, true} {
				var output, diagnostics bytes.Buffer
				args := []string{"--certificate-metadata", "--format", format, filename}
				if structural {
					args = append(args, "--mode", "structural")
				}
				want := 0
				if invalid {
					want = 1
				}
				if code := run(args, &output, &diagnostics); code != want || diagnostics.Len() != 0 {
					t.Fatalf("校验失败: format=%s, structural=%t, invalid=%t, code=%d, stderr=%s", format, structural, invalid, code, &diagnostics)
				}
				if format == "json" {
					var report validator.Report
					if err := json.Unmarshal(output.Bytes(), &report); err != nil {
						t.Fatal(err)
					}
					found := false
					for _, check := range report.Checks {
						if check.Name == "certificate_metadata" {
							found = true
							if check.Status == "passed" && invalid {
								t.Fatal("缺少元数据不能通过")
							}
						}
					}
					if !found || report.Summary.Infos == 0 {
						t.Fatalf("缺少范围说明或检查项: %+v", report)
					}
				}
			}
		}
	}
}
