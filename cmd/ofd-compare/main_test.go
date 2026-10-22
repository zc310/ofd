package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/version"
	"github.com/zc310/ofd/pkg/compare"
	"github.com/zc310/ofd/pkg/creator"
)

func inputFile(t *testing.T, width float64) string {
	t.Helper()
	data, err := creator.Marshal(creator.Document{ID: "cli-compare", PageSize: creator.PageSize{Width: width, Height: 20}, Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(t.TempDir(), "input.ofd")
	if err := os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestHelpAndVersionUseStdoutWithoutInputs(t *testing.T) {
	for _, arg := range []string{"--help", "-h", "--version", "-v"} {
		var out, errout bytes.Buffer
		if code := run([]string{arg}, &out, &errout); code != 0 || out.Len() == 0 || errout.Len() != 0 {
			t.Fatalf("%s: %d %s %s", arg, code, &out, &errout)
		}
		if (arg == "-v" || arg == "--version") && strings.TrimSpace(out.String()) != version.Version {
			t.Fatal(out.String())
		}
	}
}

func TestCLIJSONBaselineAndDifferenceExit(t *testing.T) {
	a, b := inputFile(t, 30), inputFile(t, 40)
	var out, errout bytes.Buffer
	code := run([]string{"--visual=false", "--format", "json", "--baseline", "2", "--fail-on-difference", a, b}, &out, &errout)
	if code != 1 || errout.Len() != 0 {
		t.Fatalf("退出 %d: %s", code, &errout)
	}
	var report compare.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Baseline != 2 || report.Comparisons[0].Target != 1 || report.Comparisons[0].Equal {
		t.Fatal("基准或差异结果错误")
	}
	out.Reset()
	errout.Reset()
	if code := run([]string{"--visual=false", "--fail-on-difference", a, a}, &out, &errout); code != 0 {
		t.Fatalf("相同文件退出 %d: %s", code, &errout)
	}
}

func TestCLIPageSelectionAndRegionOptions(t *testing.T) {
	a := inputFile(t, 30)
	var out, errout bytes.Buffer
	if code := run([]string{"--format", "json", "--pages", "1-2", "--min-region-pixels", "4", a, a}, &out, &errout); code != 0 {
		t.Fatalf("%d %s", code, &errout)
	}
	var report compare.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Options.Pages != "1-2" || report.Options.MinRegionPixels != 4 || report.Options.MaxTotalPixels != 1_000_000_000 || len(report.Comparisons[0].Pages) != 1 || !report.Comparisons[0].PagesEqual {
		t.Fatalf("选项未传递: %+v", report.Options)
	}
	for _, args := range [][]string{{"--pages", "0", a, a}, {"--pages", "3-1", a, a}, {"--min-region-pixels", "0", a, a}} {
		out.Reset()
		errout.Reset()
		if code := run(args, &out, &errout); code != 2 {
			t.Fatalf("参数错误未返回 2: %v %d %s", args, code, &errout)
		}
	}
	out.Reset()
	errout.Reset()
	if code := run([]string{"--format", "json", "--page-map", "1:1", "--baseline-cache-bytes", "-1", a, a}, &out, &errout); code != 0 {
		t.Fatalf("%d %s", code, &errout)
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.Options.PageMap != "1:1" || report.Options.BaselineCacheBytes != -1 {
		t.Fatalf("新选项未传递: %+v %v", report.Options, err)
	}
}

func TestCLILanguageSelectsLocalizedReportText(t *testing.T) {
	a := inputFile(t, 30)
	var out, errout bytes.Buffer
	if code := run([]string{"--visual=false", "--lang", "en", "--format", "text", a, a}, &out, &errout); code != 0 {
		t.Fatalf("%d %s", code, &errout)
	}
	if !strings.Contains(out.String(), "Page content is identical") {
		t.Fatal("--lang did not localize page matching explanation")
	}
}

func TestOutputFormatInferenceAndInputProtection(t *testing.T) {
	a := inputFile(t, 30)
	for _, ext := range []string{".html", ".json", ".md", ".txt"} {
		var out, errout bytes.Buffer
		name := filepath.Join(t.TempDir(), "report"+ext)
		if code := run([]string{"--visual=false", "-o", name, a, a}, &out, &errout); code != 0 {
			t.Fatalf("%s: %d %s", ext, code, &errout)
		}
		data, err := os.ReadFile(name)
		if err != nil || len(data) == 0 || out.Len() != 0 {
			t.Fatalf("报告写入失败: %v", err)
		}
		if ext == ".html" && !bytes.Contains(data, []byte("<!doctype html>")) {
			t.Fatal("未推断 HTML")
		}
	}
	for _, name := range []string{a, filepath.Join(t.TempDir(), "hardlink.ofd")} {
		if name != a {
			if err := os.Link(a, name); err != nil {
				t.Skip(err)
			}
		}
		var out, errout bytes.Buffer
		if code := run([]string{"--visual=false", "-o", name, a, a}, &out, &errout); code != 2 {
			t.Fatalf("覆盖输入未拒绝: %d %s", code, &errout)
		}
	}
}

func TestUsageFailuresAndRuntimeErrorsAreDistinct(t *testing.T) {
	for _, args := range [][]string{{}, {"one.ofd"}, {"--unknown"}, {"--baseline", "9", "a.ofd", "b.ofd"}, {"--format", "pdf", "a.ofd", "b.ofd"}, {"--dpi", "NaN", "a.ofd", "b.ofd"}, {"--pixel-tolerance", "256", "a.ofd", "b.ofd"}, {"--page-match", "guess", "a.ofd", "b.ofd"}} {
		var out, errout bytes.Buffer
		if code := run(args, &out, &errout); code != 2 || out.Len() != 0 || errout.Len() == 0 {
			t.Fatalf("参数 %v: %d stdout=%s stderr=%s", args, code, &out, &errout)
		}
	}
	var out, errout bytes.Buffer
	if code := run([]string{"missing.ofd", "also-missing.ofd"}, &out, &errout); code != 3 || out.Len() != 0 {
		t.Fatalf("输入错误: %d %s", code, &errout)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if code := runContext(ctx, []string{"a.ofd", "b.ofd"}, &out, &errout); code != 3 {
		t.Fatalf("取消退出 %d", code)
	}
}

func TestExternalHTMLImagesAreWrittenAndFailureCleansNewDirectory(t *testing.T) {
	a, b := inputFile(t, 30), inputFile(t, 40)
	dir := t.TempDir()
	output := filepath.Join(dir, "report.html")
	var out, errout bytes.Buffer
	if code := run([]string{"--external-images", "--fail-on-difference", "-o", output, a, b}, &out, &errout); code != 1 || errout.Len() != 0 {
		t.Fatalf("%d %s", code, &errout)
	}
	html, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`src="(ofd-compare-images-[^"/]+/[a-f0-9]+\.png)"`).FindAllSubmatch(html, -1)
	if len(matches) == 0 {
		t.Fatal("缺少外置图片引用")
	}
	for _, m := range matches {
		if _, err := os.Stat(filepath.Join(dir, string(m[1]))); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	before := len(entries)
	out.Reset()
	errout.Reset()
	if code := run([]string{"--external-images", "--max-report-bytes", "10000", "-o", output, a, b}, &out, &errout); code != 3 {
		t.Fatalf("%d %s", code, &errout)
	}
	entries, err = os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != before {
		t.Fatal("失败后遗留图片目录")
	}
	current, err := os.ReadFile(output)
	if err != nil || !bytes.Equal(current, html) {
		t.Fatal("失败覆盖了已有报告", err)
	}
	out.Reset()
	errout.Reset()
	if code := run([]string{"--external-images", "-o", filepath.Join(dir, "equal.html"), a, a}, &out, &errout); code != 0 {
		t.Fatalf("%d %s", code, &errout)
	}
	entries, err = os.ReadDir(dir)
	if err != nil || len(entries) != before+1 {
		t.Fatalf("无图片时遗留空目录: %v %v", entries, err)
	}
	for _, args := range [][]string{{"--external-images", a, b}, {"--external-images", "--format", "json", "-o", output, a, b}, {"--max-preview-bytes", "0", a, b}, {"--max-report-bytes", "-1", a, b}, {"--max-xml-structure-bytes", "0", a, b}, {"--page-map", "1:1,1:2", a, b}, {"--page-map", "1:1", "--page-match", "number", a, b}} {
		out.Reset()
		errout.Reset()
		if code := run(args, &out, &errout); code != 2 {
			t.Fatalf("%v: %d %s", args, code, &errout)
		}
	}
	out.Reset()
	errout.Reset()
	if code := run([]string{"--baseline-cache-bytes", "-2", a, b}, &out, &errout); code != 2 {
		t.Fatalf("%d %s", code, &errout)
	}
}
