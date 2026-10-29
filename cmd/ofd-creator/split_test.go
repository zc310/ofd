package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
)

func splitCLIInput(t *testing.T, dir string) string {
	t.Helper()
	data, err := creator.Marshal(creator.Document{ID: "cli-split", PageSize: creator.A4, Pages: []creator.Page{{}, {}, {}}})
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, "input.ofd")
	if err := os.WriteFile(name, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestSplitCLIRangesEveryAndDefault(t *testing.T) {
	for _, tc := range []struct {
		name   string
		flags  []string
		counts []int
	}{
		{"ranges", []string{"--ranges", "3,1;2"}, []int{2, 1}},
		{"every", []string{"--every", "2"}, []int{2, 1}},
		{"default", nil, []int{1, 1, 1}},
		{"document", []string{"--by-document"}, []int{3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			input := splitCLIInput(t, dir)
			output := filepath.Join(dir, "parts")
			args := append([]string{"split", input, "--output-dir", output, "--validate", "--deterministic"}, tc.flags...)
			var stdout, stderr bytes.Buffer
			if code := run(args, &stdout, &stderr); code != exitOK {
				t.Fatalf("退出码 %d: %s", code, stderr.String())
			}
			paths := strings.Fields(stdout.String())
			if len(paths) != len(tc.counts) {
				t.Fatalf("输出清单错误: %q", stdout.String())
			}
			for i, name := range paths {
				ofd, err := parser.NewOFD(name)
				if err != nil {
					t.Fatal(err)
				}
				if len(ofd.Documents) != 1 || len(ofd.Documents[0].Pages) != tc.counts[i] {
					t.Errorf("输出页数错误: %s", name)
				}
				_ = ofd.Close()
			}
			entries, err := os.ReadDir(output)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != len(tc.counts) {
				t.Fatal("留下临时文件")
			}
		})
	}
}

func TestSplitCLIRejectsAllConflictsBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	input := splitCLIInput(t, dir)
	output := filepath.Join(dir, "parts")
	if err := os.Mkdir(output, 0o755); err != nil {
		t.Fatal(err)
	}
	conflict := filepath.Join(output, "input-002.ofd")
	if err := os.WriteFile(conflict, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"split", input, "--output-dir", output}, &stdout, &stderr); code != exitOutput {
		t.Fatalf("退出码 %d: %s", code, stderr.String())
	}
	entries, err := os.ReadDir(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || stdout.Len() != 0 {
		t.Fatal("检查全部文件名之前已经写出分卷")
	}
	data, err := os.ReadFile(conflict)
	if err != nil || string(data) != "existing" {
		t.Fatal("覆盖了已有文件")
	}
}

func TestSplitCLIInvalidArgumentsDoNotCreateOutput(t *testing.T) {
	dir := t.TempDir()
	input := splitCLIInput(t, dir)
	for _, flags := range [][]string{
		{"--every", "0"}, {"--every", "1", "--ranges", "1"}, {"--ranges", "1;"}, {"--ranges", "1,1"}, {"--ranges", "4"},
		{"--max-pages", "0"}, {"--max-output-mb", "-1"}, {"--prefix", "../out"}, {"--signatures", "rewrite"}, {"--by-document", "--document", "1"}, {"--document", "2"},
	} {
		output := filepath.Join(t.TempDir(), "parts")
		args := append([]string{"split", input, "--output-dir", output}, flags...)
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != exitUsage {
			t.Errorf("%v: 退出码 %d: %s", flags, code, stderr.String())
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Errorf("非法参数创建了输出目录: %v", flags)
		}
	}
}

func TestSplitCLIHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{{"split", "--help"}, {"split", "--version"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != exitOK || stdout.Len() == 0 || stderr.Len() != 0 {
			t.Fatalf("帮助/版本输出错误: %v %d %q %q", args, code, stdout.String(), stderr.String())
		}
	}
}

func TestSplitCLIValidationUsesConfiguredEntryLimit(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "large.ofd")
	data, err := creator.Marshal(creator.Document{ID: "large-split", PageSize: creator.A4, Pages: make([]creator.Page, 10000)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"split", input, "--by-document", "--max-entries", "11000", "--validate", "--output-dir", filepath.Join(dir, "parts")}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("严格校验未沿用条目数配置，退出码 %d: %s", code, stderr.String())
	}
	output := strings.TrimSpace(stdout.String())
	info, err := os.Stat(output)
	if err != nil || info.Size() == 0 {
		t.Fatalf("万页分卷未写出有效文件: %v", err)
	}
	result, err := parser.NewOFD(output)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close()
	source, err := parser.NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	pages := result.Documents[0].Pages
	if len(pages) != 10000 || pages[0].ID != source.Documents[0].Pages[0].ID || pages[9999].ID != source.Documents[0].Pages[9999].ID {
		t.Fatal("万页分卷页数或首尾页顺序错误")
	}
}
