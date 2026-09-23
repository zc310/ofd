package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindFirstFileInDirsSupportsMultipleTargetFiles(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "nested", "SimHei.TTF")
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("font"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := FindFirstFileInDirs([]string{dir}, "simsun.ttc", "simhei.ttf")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("FindFirstFileInDirs() = %q, want %q", got, want)
	}
}

func TestSamePathComparesCleanedAbsolutePaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b.ofd")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("ofd"), 0o644); err != nil {
		t.Fatal(err)
	}
	joined := filepath.Join(dir, "a", "x", "..", "b.ofd")
	if !SamePath(path, joined) {
		t.Errorf("SamePath(%q, %q) = false, want true", path, joined)
	}
	if SamePath(path, filepath.Join(dir, "other.ofd")) {
		t.Error("不同文件路径不应视为同一文件")
	}
}

func TestSamePathResolvesSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.ofd")
	if err := os.WriteFile(target, []byte("ofd"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.ofd")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}
	if !SamePath(target, link) {
		t.Errorf("SamePath(%q, %q) = false, 符号链接应视为同一文件", target, link)
	}
}
