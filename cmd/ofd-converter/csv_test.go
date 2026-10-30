package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/reader"
)

func TestCSVAndTSVCLIImportAndBatch(t *testing.T) {
	root := t.TempDir()
	for _, format := range []string{"csv", "tsv"} {
		input := filepath.Join(root, "input."+format)
		separator := ","
		if format == "tsv" {
			separator = "\t"
		}
		if err := os.WriteFile(input, []byte("0001"+separator+"Paper\n0002"+separator+"Ink\n"), 0600); err != nil {
			t.Fatal(err)
		}
		output := filepath.Join(root, format+".ofd")
		// 单短横线长参数沿用现有 CLI 兼容行为。
		opts, err := parseArgs([]string{"-csv-no-header", "-csv-title", "Inventory", "--csv-encoding", "utf8", "--paper", "A5", "--landscape", input, output})
		if err != nil {
			t.Fatal(err)
		}
		opts.ctx = t.Context()
		if err := run(opts); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		r, err := reader.Open(data)
		if err != nil {
			t.Fatal(err)
		}
		page, err := r.Page(0)
		if err != nil || page.Width != 210 || page.Height != 148 {
			t.Fatalf("纸张不正确: %+v error=%v", page, err)
		}
		runs, err := r.Text(0)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		var text strings.Builder
		for _, run := range runs {
			text.WriteString(run.Text)
		}
		for _, value := range []string{"Inventory", "0001", "Paper", "0002", "Ink"} {
			if !strings.Contains(text.String(), value) {
				t.Fatalf("缺少 %q: %q", value, text.String())
			}
		}
		if !batchInputExtension(input) {
			t.Fatalf("批量入口不识别 %s", format)
		}
	}
	outputDir := t.TempDir()
	opts, err := parseArgs([]string{"--input-dir", root, "--output-dir", outputDir, "--format", "ofd"})
	if err != nil {
		t.Fatal(err)
	}
	// 单独的批量目录避免两个同名输入映射到同一输出。
	if err := os.Rename(filepath.Join(root, "input.tsv"), filepath.Join(root, "other.tsv")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"csv.ofd", "tsv.ofd"} {
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	opts.ctx = t.Context()
	if err := run(opts); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"input.ofd", "other.ofd"} {
		if _, err := os.Stat(filepath.Join(outputDir, name)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCSVCLIInvalidOptionsAndMalformedInputKeepExistingOutput(t *testing.T) {
	root := t.TempDir()
	input, output := filepath.Join(root, "bad.csv"), filepath.Join(root, "existing.ofd")
	if err := os.WriteFile(input, []byte("a,b\n1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args  []string
		usage bool
	}{
		{[]string{"--csv-encoding", "unknown", input, output}, true},
		{[]string{"--paper", "unknown", input, output}, true},
		{[]string{input, output}, false},
	} {
		opts, err := parseArgs(tc.args)
		if err != nil {
			t.Fatal(err)
		}
		opts.ctx = t.Context()
		err = run(opts)
		if err == nil || errors.Is(err, ErrUsage) != tc.usage {
			t.Fatalf("error=%v，期望 usage=%t", err, tc.usage)
		}
		data, err := os.ReadFile(output)
		if err != nil || string(data) != "existing" {
			t.Fatalf("失败时修改已有输出: %q error=%v", data, err)
		}
	}
}
