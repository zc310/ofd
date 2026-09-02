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
