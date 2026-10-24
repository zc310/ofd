//go:build unix

package fileutil

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestFindFirstFileInDirsSkipsFIFO(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "font.ttf"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := FindFirstFileInDirs([]string{dir}, "font.ttf"); err == nil {
		t.Fatalf("FIFO 不应作为文件返回: %q", got)
	}
	want := filepath.Join(dir, "nested", "font.ttf")
	if err := os.Mkdir(filepath.Dir(want), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("font"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := FindFirstFileInDirs([]string{dir}, "font.ttf"); err != nil || got != want {
		t.Fatalf("跳过 FIFO 后搜索 = %q, %v; want %q", got, err, want)
	}
}
