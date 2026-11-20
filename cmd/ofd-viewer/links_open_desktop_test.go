//go:build !js && !windows

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDesktopWebLinkPassesAddressAsSingleArgumentAndReportsFailure(t *testing.T) {
	dir := t.TempDir()
	command := "xdg-open"
	if runtime.GOOS == "darwin" {
		command = "open"
	}
	output := filepath.Join(dir, "arguments.txt")
	if err := os.WriteFile(filepath.Join(dir, command), []byte("#!/bin/sh\nprintf '%s\\n' \"$#\" \"$@\" > \"$OFD_TEST_OPEN_ARGUMENTS\"\nexit \"$OFD_TEST_OPEN_EXIT\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("OFD_TEST_OPEN_ARGUMENTS", output)
	t.Setenv("OFD_TEST_OPEN_EXIT", "0")
	uri := "https://example.test/path?q=a&next=$(echo%20unsafe)#part"
	if err := openWebURL(t.Context(), uri); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "1\n"+uri+"\n" {
		t.Fatalf("地址未作为单个参数传递: %q", data)
	}
	t.Setenv("OFD_TEST_OPEN_EXIT", "1")
	if err := openWebURL(t.Context(), uri); err == nil {
		t.Fatal("启动器失败应返回错误")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := openWebURL(ctx, uri); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消后应保留 context.Canceled: %v", err)
	}
}
