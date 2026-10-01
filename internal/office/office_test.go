package office

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/testutil"
)

func TestFindSofficeRejectsMissingExplicitPath(t *testing.T) {
	if _, err := FindSoffice(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("不存在的显式路径应报错")
	}
}

func TestFindSofficePrefersExplicitPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "soffice")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	found, err := FindSoffice(path)
	if err != nil {
		t.Fatalf("显式路径应被接受: %v", err)
	}
	if found != path {
		t.Fatalf("返回路径 = %q, 期望 %q", found, path)
	}
}

func TestFindSofficeUsesEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "soffice-env")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvSoffice, path)
	found, err := FindSoffice("")
	if err != nil {
		t.Fatalf("环境变量路径应被接受: %v", err)
	}
	if found != path {
		t.Fatalf("返回路径 = %q, 期望 %q", found, path)
	}
}

func TestFindSofficeOnPath(t *testing.T) {
	if _, err := exec.LookPath("soffice"); err != nil {
		t.Skip("PATH 中没有 soffice，跳过")
	}
	if _, err := FindSoffice(""); err != nil {
		t.Fatalf("应能在 PATH 中找到 soffice: %v", err)
	}
}

func TestFileURL(t *testing.T) {
	if got := fileURL("/tmp/a b"); got != "file:///tmp/a b" {
		t.Fatalf("fileURL = %q", got)
	}
}

// TestConvertToPDFUsesCustomTempDir 验证临时工作目录落在指定目录下，且转换后
// 被清理。
func TestConvertToPDFUsesCustomTempDir(t *testing.T) {
	if _, err := exec.LookPath("soffice"); err != nil {
		t.Skip("PATH 中没有 soffice，跳过")
	}
	tempRoot := t.TempDir()
	input := filepath.Join(t.TempDir(), "sample.fodt")
	if err := os.WriteFile(input, []byte(fodtSample), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ConvertToPDF(context.Background(), input, Options{TempDir: tempRoot}); err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	entries, err := os.ReadDir(tempRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("自定义临时目录应在转换后清空，实际残留: %v", entries)
	}
}

// TestConvertToPDFRejectsUnreadableTempDir 验证临时目录不可用时返回明确错误。
func TestConvertToPDFRejectsUnreadableTempDir(t *testing.T) {
	if _, err := exec.LookPath("soffice"); err != nil {
		t.Skip("PATH 中没有 soffice，跳过")
	}
	input := filepath.Join(t.TempDir(), "sample.fodt")
	if err := os.WriteFile(input, []byte(fodtSample), 0600); err != nil {
		t.Fatal(err)
	}
	badDir := filepath.Join(t.TempDir(), "missing-parent", "child")
	_, err := ConvertToPDF(context.Background(), input, Options{TempDir: badDir})
	if err == nil || !strings.Contains(err.Error(), "创建临时目录失败") {
		t.Fatalf("不存在的临时目录应报错，实际: %v", err)
	}
}

func TestConvertToPDFWithFakeSofficeRejectsFailures(t *testing.T) {
	input := writeOfficeTestInput(t)
	cases := []struct {
		name   string
		script string
		want   string
	}{
		{name: "命令失败", script: "printf '转换失败\\n' >&2\nexit 7", want: "LibreOffice 转换失败"},
		{name: "没有输出", script: "exit 0", want: "LibreOffice 未生成 PDF"},
		{name: "空输出", script: "out=\"\"; previous=\"\"; for arg in \"$@\"; do if [ \"$previous\" = \"--outdir\" ]; then out=\"$arg\"; fi; previous=\"$arg\"; done; : > \"$out/input.pdf\"", want: "LibreOffice 生成的 PDF 为空"},
		{name: "无效输出", script: "out=\"\"; previous=\"\"; for arg in \"$@\"; do if [ \"$previous\" = \"--outdir\" ]; then out=\"$arg\"; fi; previous=\"$arg\"; done; printf 'not a PDF' > \"$out/input.pdf\"", want: "LibreOffice 生成的 PDF 无效"},
		{name: "损坏的 PDF 结构", script: "out=\"\"; previous=\"\"; for arg in \"$@\"; do if [ \"$previous\" = \"--outdir\" ]; then out=\"$arg\"; fi; previous=\"$arg\"; done; printf '%%PDF-1.7\\nnot a valid PDF' > \"$out/input.pdf\"", want: "LibreOffice 生成的 PDF 无效"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			soffice := writeFakeSoffice(t, test.script)
			_, err := ConvertToPDF(context.Background(), input, Options{Soffice: soffice})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("错误 = %v，期望包含 %q", err, test.want)
			}
		})
	}
}

func TestConvertToPDFWithFakeSofficeHonorsTimeout(t *testing.T) {
	if err := ensureUnixTestShell(); err != nil {
		t.Skip(err)
	}
	input := writeOfficeTestInput(t)
	marker := filepath.Join(t.TempDir(), "child-finished")
	soffice := writeFakeSoffice(t, "(sleep 0.5; touch '"+marker+"') &\nwait")
	started := time.Now()
	_, err := ConvertToPDF(context.Background(), input, Options{Soffice: soffice, Timeout: 50 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "转换超时") {
		t.Fatalf("超时错误 = %v，期望转换超时", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("超时后返回过慢: %s", elapsed)
	}
	time.Sleep(600 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("超时后进程组中的子进程仍在运行")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestConvertToPDFWithFakeSofficeAcceptsPDF(t *testing.T) {
	input := writeOfficeTestInput(t)
	validPDF := filepath.Join(t.TempDir(), "valid.pdf")
	if err := os.WriteFile(validPDF, testutil.MinimalPDF(nil, 100, 100), 0600); err != nil {
		t.Fatal(err)
	}
	script := "out=\"\"; previous=\"\"; for arg in \"$@\"; do if [ \"$previous\" = \"--outdir\" ]; then out=\"$arg\"; fi; previous=\"$arg\"; done; cp " + shellQuote(validPDF) + " \"$out/input.pdf\""
	data, err := ConvertToPDF(context.Background(), input, Options{
		Soffice: writeFakeSoffice(t, script),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "%PDF-") {
		t.Fatalf("PDF 数据缺少文件头: %q", data[:min(len(data), 16)])
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func writeOfficeTestInput(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.fodt")
	if err := os.WriteFile(path, []byte(fodtSample), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeFakeSoffice(t *testing.T, body string) string {
	t.Helper()
	if err := ensureUnixTestShell(); err != nil {
		t.Skip(err)
	}
	path := filepath.Join(t.TempDir(), "soffice")
	script := "#!/bin/sh\nset -eu\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func ensureUnixTestShell() error {
	if _, err := exec.LookPath("sh"); err != nil {
		return errors.New("测试需要 sh")
	}
	return nil
}

const fodtSample = `<?xml version="1.0" encoding="UTF-8"?>
<office:document xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0" office:version="1.2">
<office:body><office:text><text:p>临时目录测试 TempDir</text:p></office:text></office:body>
</office:document>`
