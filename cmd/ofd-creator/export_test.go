package main

import (
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
