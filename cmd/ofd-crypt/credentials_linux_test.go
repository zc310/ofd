package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCredentialSpecialFilesAreRejectedBeforeBlockingOpenOrRead(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "unopened.fifo")
	heldFIFO := filepath.Join(dir, "held.fifo")
	for _, path := range []string{fifo, heldFIFO} {
		if err := syscall.Mkfifo(path, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// 回归失败时也释放可能阻塞在 os.Open 的读取者。
	t.Cleanup(func() {
		file, err := os.OpenFile(fifo, os.O_RDWR|syscall.O_NONBLOCK, 0)
		if err == nil {
			file.Close()
		}
	})
	held, err := os.OpenFile(heldFIFO, os.O_RDWR|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { held.Close() })
	alias := filepath.Join(dir, "fifo-alias")
	if err := os.Symlink(fifo, alias); err != nil {
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close(); writer.Close() })
	for _, test := range []struct{ name, path string }{
		{"fifo without writer", fifo},
		{"fifo with writer kept open", heldFIFO},
		{"symlink to fifo", alias},
		{"fd pipe", fmt.Sprintf("/dev/fd/%d", reader.Fd())},
		{"device", "/dev/null"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				data, err := readCredential(ctx, test.path, nil, 64<<10)
				clear(data)
				done <- err
			}()
			select {
			case err := <-done:
				if err == nil || !strings.Contains(err.Error(), "普通文件") {
					t.Fatalf("特殊路径应在打开和读取前明确拒绝: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("特殊凭据路径发生阻塞")
			}
		})
	}
}

func TestCredentialRegularFileAliasesRemainReadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "password")
	if err := os.WriteFile(path, []byte("secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "password-alias")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, path := range []string{alias, fmt.Sprintf("/dev/fd/%d", file.Fd())} {
		data, err := readCredential(t.Context(), path, nil, 64<<10)
		if err != nil || string(data) != "secret\n" {
			t.Fatalf("指向普通文件的路径应正常读取: %q, %v", data, err)
		}
	}
}
