package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveExportAssetRootRejectsOutsideManifestDirectory(t *testing.T) {
	directory := t.TempDir()
	manifest := filepath.Join(directory, "export", "document.yaml")
	assetRoot := filepath.Join(directory, "assets")
	_, _, err := resolveExportAssetRoot(&exportOptions{output: manifest, assetRoot: assetRoot})
	if err == nil || !strings.Contains(err.Error(), "必须位于 manifest 输出目录内") {
		t.Fatalf("resolveExportAssetRoot error = %v", err)
	}
}

func TestExportCommandsRejectTOMLBeforeWritingFiles(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		path string
	}{
		{"export explicit", []string{"export", "--format", "toml"}, "document.yaml"},
		{"export uppercase", []string{"export", "--format", "TOML"}, "document.yaml"},
		{"export inferred", []string{"export"}, "document.toml"},
		{"export-all", []string{"export-all", "--format", "toml"}, "bundle"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			output := filepath.Join(root, test.path)
			args := append(test.args, "-i", "missing.ofd", "-o", output)
			var stdout, stderr bytes.Buffer
			if code := run(args, &stdout, &stderr); code != exitUsage || !strings.Contains(stderr.String(), "不支持的导出格式") {
				t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 || stdout.Len() != 0 {
				t.Fatalf("格式错误仍产生了输出: entries=%v stdout=%q err=%v", entries, stdout.String(), err)
			}
		})
	}
}
