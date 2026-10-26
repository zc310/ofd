package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/manifest"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/converter"
	"github.com/zc310/ofd/pkg/validator"
)

func TestTemplateCLIInvoiceExampleBothFormatsAndReexport(t *testing.T) {
	yamlPath := filepath.Join("examples", "template-invoice.yaml")
	m, _, err := manifest.Load(yamlPath, "yaml")
	if err != nil {
		t.Fatal(err)
	}
	jsonData, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	jsonPath := filepath.Join(t.TempDir(), "template.json")
	if err := os.WriteFile(jsonPath, jsonData, 0600); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{yamlPath, jsonPath} {
		t.Run(filepath.Ext(input), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := []string{"template", "render", "-i", input, "--data", filepath.Join("examples", "template-invoice-data.json")}
			if code := run(append(args, "--check"), &stdout, &stderr); code != exitOK {
				t.Fatalf("check code %d: %s", code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatal("check 不应输出产物")
			}
			output := filepath.Join(t.TempDir(), "invoices.ofd")
			if code := run(append(args, "-o", output, "--validate", "--deterministic"), &stdout, &stderr); code != exitOK {
				t.Fatalf("render code %d: %s", code, stderr.String())
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			instance, err := validator.New()
			if err != nil {
				t.Fatal(err)
			}
			report := instance.ValidateReader(context.Background(), bytes.NewReader(data), output)
			if report.HasErrors() || report.HasWarnings() {
				_ = validator.RenderText(&stderr, report)
				t.Fatal(stderr.String())
			}
			ofd, err := parser.NewOFD(data)
			if err != nil {
				t.Fatal(err)
			}
			if len(ofd.Documents[0].Pages) != 2 {
				t.Fatal("示例必须生成两页")
			}
			_ = ofd.Close()
			var text bytes.Buffer
			if err := converter.Text(context.Background(), data, &text); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(text.String(), "1234.50") || !strings.Contains(text.String(), "5678.00") {
				t.Fatalf("生成文字不正确: %s", text.String())
			}
			exported := filepath.Join(t.TempDir(), "exported.yaml")
			if code := run([]string{"export", "-i", output, "-o", exported}, &stdout, &stderr); code != exitOK {
				t.Fatalf("export code %d: %s", code, stderr.String())
			}
			if code := run([]string{"-i", exported, "--check"}, &stdout, &stderr); code != exitOK {
				t.Fatalf("reload code %d: %s", code, stderr.String())
			}
		})
	}
}

func TestTemplateCLIRejectsInvalidArgumentsAndPreservesExistingOutput(t *testing.T) {
	for _, args := range [][]string{
		{"template"}, {"template", "unknown"}, {"template", "render", "-i", "input.yaml", "--check"},
		{"template", "render", "-i", "-", "--data", "-", "--check"},
		{"template", "render", "-i", "input.yaml", "--data", "data.json", "-o", "data.json"},
		{"template", "render", "-i", "input.yaml", "--data", "data.json", "--check", "--max-pages", "-1"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != exitUsage {
			t.Fatalf("%v code %d: %s", args, code, stderr.String())
		}
		if stdout.Len() != 0 {
			t.Fatal("错误不能污染标准输出")
		}
	}
	var stdout, stderr bytes.Buffer
	output := filepath.Join(t.TempDir(), "existing.ofd")
	if err := os.WriteFile(output, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	badData := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(badData, []byte(`{"invoices":[{}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"template", "render", "-i", "examples/template-invoice.yaml", "--data", badData, "-o", output}, &stdout, &stderr); code != exitResource {
		t.Fatalf("code %d: %s", code, stderr.String())
	}
	data, err := os.ReadFile(output)
	if err != nil || string(data) != "keep" {
		t.Fatalf("失败时覆盖了原输出: %s %v", data, err)
	}
}

func TestTemplateCLIHelpVersionAndDataShorthand(t *testing.T) {
	for _, args := range [][]string{{"template", "--help"}, {"template", "render", "--help"}, {"template", "render", "--version"}, {"-i", "examples/template-invoice.yaml", "--data", "examples/template-invoice-data.json", "--check"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != exitOK {
			t.Fatalf("%v code %d: %s", args, code, stderr.String())
		}
	}
}

func TestTemplateCLIDetailPaginationExampleAndExactTotals(t *testing.T) {
	yamlPath := filepath.Join("examples", "template-invoice-details.yaml")
	m, _, err := manifest.Load(yamlPath, "yaml")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	jsonPath := filepath.Join(t.TempDir(), "template.json")
	if err := os.WriteFile(jsonPath, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{yamlPath, jsonPath} {
		t.Run(filepath.Ext(input), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := []string{"template", "render", "-i", input, "--data", "examples/template-invoice-details-data.json"}
			if code := run(append(args, "--check"), &stdout, &stderr); code != exitOK {
				t.Fatalf("check %d: %s", code, stderr.String())
			}
			output := filepath.Join(t.TempDir(), "details.ofd")
			if code := run(append(args, "-o", output, "--validate", "--deterministic"), &stdout, &stderr); code != exitOK {
				t.Fatalf("render %d: %s", code, stderr.String())
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			ofd, err := parser.NewOFD(data)
			if err != nil {
				t.Fatal(err)
			}
			if len(ofd.Documents[0].Pages) != 4 {
				t.Fatalf("示例应为三页明细单据加一页独立单据，实际 %d 页", len(ofd.Documents[0].Pages))
			}
			_ = ofd.Close()
			instance, err := validator.New()
			if err != nil {
				t.Fatal(err)
			}
			report := instance.ValidateReader(context.Background(), bytes.NewReader(data), output)
			if report.HasErrors() || report.HasWarnings() {
				_ = validator.RenderText(&stderr, report)
				t.Fatal(stderr.String())
			}
			var text bytes.Buffer
			if err := converter.Text(context.Background(), data, &text); err != nil {
				t.Fatal(err)
			}
			for _, value := range []string{"FIRST PAGE", "CONTINUED", "LAST PAGE", "SINGLE PAGE", "7.80", "0.30", "Follow-up service"} {
				if !strings.Contains(text.String(), value) {
					t.Fatalf("产物文字缺少 %q: %s", value, text.String())
				}
			}
			if strings.Count(text.String(), "Grand total") != 2 {
				t.Fatalf("合计应仅在每张单据的末页显示: %s", text.String())
			}
			exported := filepath.Join(t.TempDir(), "exported.json")
			if code := run([]string{"export", "-i", output, "-o", exported}, &stdout, &stderr); code != exitOK {
				t.Fatalf("export %d: %s", code, stderr.String())
			}
			if code := run([]string{"-i", exported, "--check"}, &stdout, &stderr); code != exitOK {
				t.Fatalf("reload %d: %s", code, stderr.String())
			}
		})
	}
}
