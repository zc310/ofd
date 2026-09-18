package browser

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFindChromeRejectsMissingExplicitPath(t *testing.T) {
	if _, err := FindChrome(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("不存在的显式路径应报错")
	}
}

func TestFindChromePrefersExplicitPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chrome")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	found, err := FindChrome(path)
	if err != nil {
		t.Fatalf("显式路径应被接受: %v", err)
	}
	if found != path {
		t.Fatalf("返回路径 = %q, 期望 %q", found, path)
	}
}

func TestFindChromeUsesEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chrome-env")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvChrome, path)
	found, err := FindChrome("")
	if err != nil {
		t.Fatalf("环境变量路径应被接受: %v", err)
	}
	if found != path {
		t.Fatalf("返回路径 = %q, 期望 %q", found, path)
	}
}

func TestFindChromeOnPath(t *testing.T) {
	found, err := FindChrome("")
	if err != nil {
		t.Fatalf("FindChrome 不应报错: %v", err)
	}
	if found == "" {
		t.Skip("PATH 中没有 Chrome/Chromium，跳过")
	}
}

// TestConvertFileToPDFUsesCustomTempDir 验证渲染临时目录落在指定目录下并在
// 渲染后清理。容器内可能需要禁用沙箱。
func TestConvertFileToPDFUsesCustomTempDir(t *testing.T) {
	if chrome, err := FindChrome(""); err != nil || chrome == "" {
		t.Skip("未安装 Chrome/Chromium，跳过")
	}
	tempRoot := t.TempDir()
	input := filepath.Join(t.TempDir(), "page.html")
	if err := os.WriteFile(input, []byte("<html><body><h1>临时目录测试</h1></body></html>"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := ConvertFileToPDF(context.Background(), input, Options{
		TempDir:         tempRoot,
		NoSandbox:       true,
		Width:           210,
		Height:          297,
		PrintBackground: true,
	})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	entries, err := os.ReadDir(tempRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("自定义临时目录应在渲染后清空，实际残留: %v", entries)
	}
}

// TestConvertFileToPDFRejectsUnreadableTempDir 验证临时目录不可用时返回明确错误。
func TestConvertFileToPDFRejectsUnreadableTempDir(t *testing.T) {
	if chrome, err := FindChrome(""); err != nil || chrome == "" {
		t.Skip("未安装 Chrome/Chromium，跳过")
	}
	input := filepath.Join(t.TempDir(), "page.html")
	if err := os.WriteFile(input, []byte("<html><body>x</body></html>"), 0600); err != nil {
		t.Fatal(err)
	}
	badDir := filepath.Join(t.TempDir(), "missing-parent", "child")
	_, err := ConvertFileToPDF(context.Background(), input, Options{TempDir: badDir, NoSandbox: true})
	if err == nil || !strings.Contains(err.Error(), "创建临时目录失败") {
		t.Fatalf("不存在的临时目录应报错，实际: %v", err)
	}
}

// TestConvertFileToPDFBlocksRemoteResources 验证默认会阻断外部资源请求，且
// AllowRemoteResources 能放行。这是对 CDP SetBlockedURLs 模式语法的回归保护：
// 旧写法 "http://*" 在新版 URLPattern 下不会命中任何请求。
func TestConvertFileToPDFBlocksRemoteResources(t *testing.T) {
	if chrome, err := FindChrome(""); err != nil || chrome == "" {
		t.Skip("未安装 Chrome/Chromium，跳过")
	}
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_ = png.Encode(w, solidPNG())
	}))
	defer server.Close()

	input := filepath.Join(t.TempDir(), "page.html")
	html := "<html><body><h1>remote</h1><img src=\"" + server.URL + "/img.png\" width=\"100\" height=\"100\"></body></html>"
	if err := os.WriteFile(input, []byte(html), 0600); err != nil {
		t.Fatal(err)
	}

	base := Options{NoSandbox: true, Width: 210, Height: 297, PrintBackground: true}

	requests.Store(0)
	if _, err := ConvertFileToPDF(context.Background(), input, base); err != nil {
		t.Fatalf("默认（阻断）渲染失败: %v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("默认应阻断外部资源，实际请求 %d 次", got)
	}

	allowed := base
	allowed.AllowRemoteResources = true
	requests.Store(0)
	if _, err := ConvertFileToPDF(context.Background(), input, allowed); err != nil {
		t.Fatalf("放行渲染失败: %v", err)
	}
	if got := requests.Load(); got == 0 {
		t.Fatalf("AllowRemoteResources 应放行外部资源，实际请求 0 次")
	}
}

func solidPNG() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 0xff, A: 0xff})
		}
	}
	return img
}
