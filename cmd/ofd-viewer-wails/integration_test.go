package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func prepareAssociationTest(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("仅 Linux 提供直接设置默认应用的能力")
	}
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_DATA_DIRS", t.TempDir())
	t.Setenv("FLATPAK_ID", "")
	if err := os.Mkdir(filepath.Join(dir, "applications"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "applications", desktopFileID), []byte("[Desktop Entry]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := integrationCommand
	t.Cleanup(func() { integrationCommand = previous })
}

func TestDefaultReaderQueriesAndVerifiesExplicitSetting(t *testing.T) {
	prepareAssociationTest(t)
	current := "another-reader.desktop"
	calls := 0
	integrationCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls++
		if _, ok := ctx.Deadline(); !ok || name != "xdg-mime" {
			t.Fatal("默认应用命令必须有超时并使用固定程序")
		}
		switch strings.Join(args, " ") {
		case "query default application/ofd":
			return []byte(current + "\n"), nil
		case "default " + desktopFileID + " application/ofd":
			current = desktopFileID
			return nil, nil
		default:
			t.Fatalf("意外系统命令: %v", args)
			return nil, nil
		}
	}
	d := &Desktop{}
	if state := d.DefaultReader(t.Context()); state.State != "other" || calls != 1 {
		t.Fatalf("初始状态 = %+v, commands=%d", state, calls)
	}
	if err := d.SetDefaultReader(t.Context()); err != nil || calls != 3 {
		t.Fatalf("设置结果 = %v, commands=%d", err, calls)
	}
	if state := d.DefaultReader(t.Context()); state.State != "default" {
		t.Fatalf("设置后状态 = %+v", state)
	}
}

func TestDefaultReaderFailureAndUnavailableDoNotClaimSuccess(t *testing.T) {
	prepareAssociationTest(t)
	d := &Desktop{}
	integrationCommand = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("permission denied"), errors.New("failed")
	}
	if state := d.DefaultReader(t.Context()); state.State != "unknown" {
		t.Fatalf("探测失败 = %+v", state)
	}
	if err := d.SetDefaultReader(t.Context()); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("丢失系统错误输出: %v", err)
	}
	integrationCommand = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	if err := d.SetDefaultReader(t.Context()); err == nil {
		t.Fatal("设置后未确认关联，不能声称成功")
	}
	t.Setenv("FLATPAK_ID", "sandbox")
	integrationCommand = func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("沙盒环境不应尝试改动宿主默认应用")
		return nil, nil
	}
	if state := d.DefaultReader(t.Context()); state.State != "unavailable" {
		t.Fatalf("沙盒状态 = %+v", state)
	}
	if err := d.SetDefaultReader(t.Context()); err == nil {
		t.Fatal("沙盒设置应明确失败")
	}
}

func TestDefaultReaderSettingPreservesCancellation(t *testing.T) {
	prepareAssociationTest(t)
	integrationCommand = func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	d := &Desktop{}
	if err := d.SetDefaultReader(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("默认阅读器设置丢失取消原因: %v", err)
	}
	if err := d.OpenDefaultReaderSettings(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("默认应用设置入口丢失取消原因: %v", err)
	}
}
