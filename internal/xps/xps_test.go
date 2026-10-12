package xps

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

func TestFindXpstopdfRejectsMissingExplicitPath(t *testing.T) {
	if _, err := FindXpstopdf(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("不存在的显式路径应报错")
	}
}

func TestFindXpstopdfPrefersExplicitPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "xpstopdf")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	found, err := FindXpstopdf(path)
	if err != nil || found != path {
		t.Fatalf("显式路径 = %q, error = %v", found, err)
	}
}

func TestFindXpstopdfUsesEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "xpstopdf-env")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvXpstopdf, path)
	found, err := FindXpstopdf("")
	if err != nil || found != path {
		t.Fatalf("环境变量路径 = %q, error = %v", found, err)
	}
}

func TestConvertToPDFWithFakeXpstopdf(t *testing.T) {
	if err := ensureUnixTestShell(); err != nil {
		t.Skip(err)
	}
	input := filepath.Join(t.TempDir(), "input.xps")
	if err := os.WriteFile(input, []byte("xps"), 0600); err != nil {
		t.Fatal(err)
	}
	validPDF := filepath.Join(t.TempDir(), "valid.pdf")
	if err := os.WriteFile(validPDF, testutil.MinimalPDF(nil, 100, 100), 0600); err != nil {
		t.Fatal(err)
	}
	script := "cp " + shellQuote(validPDF) + " \"$2\""
	data, err := ConvertToPDF(context.Background(), input, Options{Xpstopdf: writeFakeXpstopdf(t, script)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "%PDF-") {
		t.Fatalf("PDF 数据缺少文件头: %q", data[:min(len(data), 16)])
	}
}

func TestConvertToPDFWithFakeXpstopdfRejectsFailures(t *testing.T) {
	if err := ensureUnixTestShell(); err != nil {
		t.Skip(err)
	}
	input := filepath.Join(t.TempDir(), "input.xps")
	if err := os.WriteFile(input, []byte("xps"), 0600); err != nil {
		t.Fatal(err)
	}
	tool := writeFakeXpstopdf(t, "printf '转换失败\\n' >&2\nexit 7")
	_, err := ConvertToPDF(context.Background(), input, Options{Xpstopdf: tool})
	if err == nil || !strings.Contains(err.Error(), "xpstopdf 转换失败") {
		t.Fatalf("错误 = %v", err)
	}
}

func TestConvertToPDFWithFakeXpstopdfHonorsTimeout(t *testing.T) {
	if err := ensureUnixTestShell(); err != nil {
		t.Skip(err)
	}
	input := filepath.Join(t.TempDir(), "input.xps")
	if err := os.WriteFile(input, []byte("xps"), 0600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "child-finished")
	tool := writeFakeXpstopdf(t, "(sleep 0.5; touch '"+marker+"') &\nwait")
	_, err := ConvertToPDF(context.Background(), input, Options{Xpstopdf: tool, Timeout: 50 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "转换超时") {
		t.Fatalf("超时错误 = %v", err)
	}
	time.Sleep(600 * time.Millisecond)
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("超时后进程组中的子进程仍在运行")
	} else if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal(statErr)
	}
}

func ensureUnixTestShell() error {
	if _, err := exec.LookPath("sh"); err != nil {
		return errors.New("测试需要 sh")
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func writeFakeXpstopdf(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "xpstopdf")
	script := "#!/bin/sh\nset -eu\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}
