//go:build linux && !android && !flatpak

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultReaderInstallReplacesFyneDesktopEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "applications", defaultReaderDesktopID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("Exec=old-fyne %f\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	commands := defaultReaderCommands{
		lookPath: func(name string) (string, error) {
			if name == "xdg-mime" {
				return name, nil
			}
			return "", errors.New("未安装")
		},
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			return nil, nil
		},
	}
	exe := `/tmp/OFD 100% "viewer"`
	if err := installDefaultReader(context.Background(), dir, exe, commands); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `Exec="/tmp/OFD 100%% \"viewer\"" %f`) {
		t.Fatalf("desktop 启动路径未正确转义: %s", data)
	}
	want := [][]string{{"xdg-mime", "default", defaultReaderDesktopID, defaultReaderMIME}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("设置命令错误: %v", calls)
	}
}

func TestDefaultReaderQueryAndFailure(t *testing.T) {
	commands := defaultReaderCommands{
		lookPath: func(name string) (string, error) { return name, nil },
		run: func(_ context.Context, name string, _ ...string) ([]byte, error) {
			if name == "xdg-mime" {
				return nil, errors.New("查询失败")
			}
			return []byte("application/ofd: " + defaultReaderDesktopID + "\n其他内容"), nil
		},
	}
	if got := queryDefaultReader(context.Background(), commands); got != defaultReaderIsDefault {
		t.Fatalf("gio 回退查询失败: %v", got)
	}
	commands.run = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("权限不足"), errors.New("退出失败")
	}
	if err := installDefaultReader(context.Background(), t.TempDir(), "/tmp/viewer", commands); err == nil || !strings.Contains(err.Error(), "权限不足") {
		t.Fatalf("应报告命令错误: %v", err)
	}
}
