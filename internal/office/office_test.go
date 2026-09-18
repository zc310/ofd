package office

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

const fodtSample = `<?xml version="1.0" encoding="UTF-8"?>
<office:document xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0" office:version="1.2">
<office:body><office:text><text:p>临时目录测试 TempDir</text:p></office:text></office:body>
</office:document>`
