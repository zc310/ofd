package xps

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("超时错误未保留 DeadlineExceeded: %v", err)
	}
	time.Sleep(600 * time.Millisecond)
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("超时后进程组中的子进程仍在运行")
	} else if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal(statErr)
	}
}

func TestConvertToPDFReturnsCancellationUnwrapped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ConvertToPDF(ctx, "missing.xps", Options{})
	if err != context.Canceled {
		t.Fatalf("取消错误 = %v，期望原样返回 context.Canceled", err)
	}
}

func TestConvertToPDFRejectsSilentFailuresWithoutTrailingSeparator(t *testing.T) {
	if err := ensureUnixTestShell(); err != nil {
		t.Skip(err)
	}
	input := filepath.Join(t.TempDir(), "input.xps")
	if err := os.WriteFile(input, []byte("xps"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"exit 7", "printf '%%PDF-invalid' > \"$2\"\nexit 7", "exit 0"} {
		t.Run(body, func(t *testing.T) {
			_, err := ConvertToPDF(context.Background(), input, Options{Xpstopdf: writeFakeXpstopdf(t, body)})
			if err == nil || strings.HasSuffix(err.Error(), ": ") {
				t.Fatalf("错误 = %v", err)
			}
			if strings.Contains(body, "exit 7") {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
					t.Fatalf("退出错误未保留: %v", err)
				}
			}
		})
	}
}

func TestValidatePDFRejectsInvalidInfoWithoutRetry(t *testing.T) {
	for _, title := range []string{"(Valid title)", "42"} {
		t.Run(title, func(t *testing.T) {
			data := testutil.MinimalPDF(nil, 100, 100)
			var previousXRef int
			start := bytes.LastIndex(data, []byte("startxref\n"))
			if _, err := fmt.Sscanf(string(data[start:]), "startxref\n%d", &previousXRef); err != nil {
				t.Fatal(err)
			}
			// 用增量更新添加 Info，保留原有页面及其交叉引用偏移。
			var output bytes.Buffer
			output.Write(data)
			infoOffset := output.Len()
			fmt.Fprintf(&output, "6 0 obj\n<< /Title %s >>\nendobj\n", title)
			xrefOffset := output.Len()
			fmt.Fprintf(&output, "xref\n6 1\n%010d 00000 n \n", infoOffset)
			fmt.Fprintf(&output, "trailer\n<< /Size 7 /Root 1 0 R /Info 6 0 R /Prev %d >>\nstartxref\n%d\n%%%%EOF\n", previousXRef, xrefOffset)
			err := validatePDF(context.Background(), output.Bytes())
			if title == "42" {
				if err == nil || !strings.Contains(err.Error(), "Title") {
					t.Fatalf("无效 Info 的原始错误未保留: %v", err)
				}
			} else if err != nil {
				t.Fatalf("有效 Info 校验失败: %v", err)
			}
		})
	}
}

func TestCopyFileUsesOpenedInputAfterPathReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不允许替换仍打开的文件")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "input.xps")
	if err := os.WriteFile(source, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if err := os.Rename(source, filepath.Join(dir, "original.xps")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "copy.xps")
	if err := copyFile(input, target); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "original" {
		t.Fatalf("复制内容 = %q, error = %v", data, err)
	}
}

func ensureUnixTestShell() error {
	if runtime.GOOS == "windows" {
		return errors.New("脚本测试需要 Unix 执行环境")
	}
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
