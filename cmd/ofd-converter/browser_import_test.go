package main

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/convert/browser"
	"github.com/zc310/ofd/pkg/webreader"
)

func TestEMLAndSVGCLIRecognizeExternalImportsAndPreserveFailedOutput(t *testing.T) {
	for _, tc := range []struct{ format, content string }{
		{"eml", "Subject: Test\n\nEmail body"},
		{"svg", `<svg xmlns="http://www.w3.org/2000/svg"><text x="1" y="20">Test</text></svg>`},
	} {
		root := t.TempDir()
		input, output := filepath.Join(root, "input."+strings.ToUpper(tc.format)), filepath.Join(root, "output.ofd")
		if err := os.WriteFile(input, []byte(tc.content), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, []byte("existing"), 0600); err != nil {
			t.Fatal(err)
		}
		opts, err := parseArgs([]string{"--chrome", filepath.Join(root, "missing-chrome"), input, output})
		if err != nil {
			t.Fatal(err)
		}
		opts.ctx = t.Context()
		if resolveInputFormat(opts) != tc.format || !batchInputExtension(input) || !isExternalInput(opts) {
			t.Fatalf("%s 没有接入格式识别、批量和外部并发限制", tc.format)
		}
		if err := run(opts); err == nil || !strings.Contains(err.Error(), "Chrome") {
			t.Fatalf("没有明确报告缺少浏览器: %v", err)
		}
		data, err := os.ReadFile(output)
		if err != nil || string(data) != "existing" {
			t.Fatalf("失败时覆盖已有输出: %q error=%v", data, err)
		}
		opts.paper = "invalid"
		if err := run(opts); !errors.Is(err, ErrUsage) {
			t.Fatalf("无效纸张应返回参数错误: %v", err)
		}
	}
}

func TestEMLAndSVGCLIConvertWithBrowser(t *testing.T) {
	chrome, err := browser.FindChrome("")
	if err != nil {
		t.Fatal(err)
	}
	if chrome == "" {
		t.Skip("未安装 Chrome/Chromium")
	}
	for _, input := range []string{"../../testdata/eml/project-update.eml", "../../testdata/svg/workflow.svg"} {
		if _, err := os.Stat(input); errors.Is(err, os.ErrNotExist) {
			t.Skipf("缺少样本 %s", input)
		} else if err != nil {
			t.Fatal(err)
		}
		output := filepath.Join(t.TempDir(), "document.ofd")
		opts, err := parseArgs([]string{"--chrome", chrome, "--chrome-no-sandbox", "--paper", "A5", "--landscape", input, output})
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
		r, err := webreader.Open(data)
		if err != nil {
			t.Fatal(err)
		}
		page, err := r.Page(0)
		r.Close()
		if err != nil || math.Abs(page.Width-210) > 25.4/72 || math.Abs(page.Height-148) > 25.4/72 {
			t.Fatalf("纸张参数未传递到 %s: %+v error=%v", input, page, err)
		}
	}
}
