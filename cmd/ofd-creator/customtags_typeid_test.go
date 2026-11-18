package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/validator"
	"go.yaml.in/yaml/v4"
)

func TestCertificateTagExamplesCheckAndValidateBufferedAndStreamOutputs(t *testing.T) {
	for _, format := range []string{"yaml", "json"} {
		t.Run(format, func(t *testing.T) {
			input := filepath.Join("examples", "certificate-tags.yaml")
			if format == "json" {
				raw, err := os.ReadFile(input)
				if err != nil {
					t.Fatal(err)
				}
				var value map[string]any
				if err := yaml.Unmarshal(raw, &value); err != nil {
					t.Fatal(err)
				}
				raw, err = json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				input = filepath.Join(t.TempDir(), "certificate-tags.json")
				if err := os.WriteFile(input, raw, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var output, diagnostics bytes.Buffer
			if code := run([]string{"-i", input, "--check"}, &output, &diagnostics); code != exitOK {
				t.Fatalf("清单检查失败: code=%d, stderr=%s", code, &diagnostics)
			}
			for _, stream := range []bool{false, true} {
				output.Reset()
				diagnostics.Reset()
				filename := filepath.Join(t.TempDir(), "generated.ofd")
				args := []string{"-i", input, "-o", filename, "--validate"}
				if stream {
					args = append(args, "--stream")
				}
				if code := run(args, &output, &diagnostics); code != exitOK || diagnostics.Len() != 0 {
					t.Fatalf("创建失败: stream=%t, code=%d, stderr=%s", stream, code, &diagnostics)
				}
				data, err := os.ReadFile(filename)
				if err != nil {
					t.Fatal(err)
				}
				checkCertificateTag(t, data)
				diagnostics.Reset()
				if err := validateMerged(bytes.NewReader(data), "merged.ofd", &diagnostics); err != nil {
					t.Fatalf("合并校验不接受 TypeID 标引: %v, %s", err, &diagnostics)
				}
			}
		})
	}
}

func checkCertificateTag(t *testing.T, data []byte) {
	t.Helper()
	instance, err := validator.New(validator.WithCertificateMetadata(true))
	if err != nil {
		t.Fatal(err)
	}
	report := instance.ValidateReader(t.Context(), bytes.NewReader(data), "certificate.ofd")
	if report.HasErrors() || report.Summary.Warnings != 0 {
		t.Fatalf("结构示例不应产生校验问题: %+v", report.Issues)
	}
	ofd, err := parser.NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	tags, err := ofd.Documents[0].LoadCustomTags()
	if err != nil {
		t.Fatal(err)
	}
	if tags == nil || len(tags.CustomTags) != 1 || tags.CustomTags[0].TypeID != "cert" || tags.CustomTags[0].NameSpace != "" {
		t.Fatalf("证照标引入口未保留: %+v", tags)
	}
}
