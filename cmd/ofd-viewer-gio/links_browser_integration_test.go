//go:build linux

package main

import (
	"bytes"
	"context"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"github.com/zc310/ofd/pkg/creator"
)

// 显式启用才启动真实浏览器；仅访问本地测试服务，不更改系统默认应用或用户配置。
func TestDesktopWebLinkRealBrowserIntegration(t *testing.T) {
	browser := os.Getenv("OFD_VIEWER_TEST_BROWSER")
	if browser == "" {
		t.Skip("设置 OFD_VIEWER_TEST_BROWSER 为 Chrome/Chromium 路径以验证真实浏览器链路")
	}
	if _, err := exec.LookPath(browser); err != nil {
		t.Fatalf("查找指定的测试浏览器失败: %v", err)
	}
	if _, err := exec.LookPath("xdg-open"); err != nil {
		t.Fatalf("查找系统链接启动器失败: %v", err)
	}
	dir := t.TempDir()
	opener := filepath.Join(dir, "browser")
	script := "#!/bin/sh\nexec \"$OFD_VIEWER_TEST_BROWSER\" --headless=new --no-sandbox --disable-gpu --disable-dev-shm-usage --no-first-run --no-default-browser-check --disable-background-networking --disable-component-update --disable-sync --user-data-dir=\"$OFD_VIEWER_TEST_PROFILE\" --timeout=10000 --dump-dom \"$@\"\n"
	if err := os.WriteFile(opener, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BROWSER", opener)
	t.Setenv("OFD_VIEWER_TEST_PROFILE", filepath.Join(dir, "profile"))
	for _, name := range []string{"DISPLAY", "WAYLAND_DISPLAY", "XDG_CURRENT_DESKTOP", "DESKTOP_SESSION", "DE"} {
		t.Setenv(name, "")
	}
	visited := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/clicked" {
			select {
			case visited <- r.URL.Query().Get("value"):
			default:
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><title>OFD 链接验证</title>链接已打开"))
	}))
	defer server.Close()
	data, err := creator.Marshal(creator.Document{ID: "gio-browser-link", PageSize: creator.A4, Pages: []creator.Page{
		{Items: []creator.Item{creator.Path{X: 10, Y: 10, Width: 40, Height: 40, Data: "M 0 0 L 40 0 L 40 40 C", Fill: true,
			Actions: []creator.Action{{URI: &creator.URIAction{URI: server.URL + "/clicked?value=a%26b"}}}}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	m, err := openDocumentInput("browser-link.ofd", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer m.close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	opened := make(chan error, 1)
	v := viewer{scene: m.scene, viewport: pageViewport{mode: fitManual, scale: 2}}
	v.openURL = func(uri string) { go func() { opened <- openWebURL(ctx, uri) }() }
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(240, 140)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	frame := func() {
		ops.Reset()
		v.layoutViewport(gtx)
		router.Frame(&ops)
	}
	frame()
	router.Queue(pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: f32.Pt(40, 40)})
	frame()
	router.Queue(pointer.Event{Kind: pointer.Release, Position: f32.Pt(40, 40)})
	frame()
	select {
	case err := <-opened:
		if err != nil {
			t.Fatalf("真实浏览器启动失败: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("等待真实浏览器打开链接超时: %v", ctx.Err())
	}
	select {
	case value := <-visited:
		if value != "a&b" {
			t.Fatalf("浏览器收到的查询参数被改变: %q", value)
		}
	default:
		t.Fatal("启动器返回成功，但真实浏览器没有访问点击目标")
	}
}
