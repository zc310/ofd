package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
)

func TestMergeAndSplitCLIDenyExportWithoutPublishingContent(t *testing.T) {
	denied := false
	data, err := creator.Marshal(creator.Document{ID: "restricted", Pages: []creator.Page{{}}, Permissions: &creator.Permissions{Export: &denied}})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "restricted.ofd")
	if err := os.WriteFile(input, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"merge", "-i", input, "-o", "-"},
		{"merge", "-i", input, "--pages", "1", "-o", "-"},
		{"export", "-i", input, "-o", filepath.Join(dir, "manifest.yaml"), "--asset-root", "assets"},
		{"export-all", "-i", input, "-o", filepath.Join(dir, "bundle")},
		{"split", input, "--by-document", "--output-dir", filepath.Join(dir, "parts")},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != exitResource || stdout.Len() != 0 || !strings.Contains(stderr.String(), "权限") {
			t.Fatalf("导出拒绝未保留处理失败契约：args=%v code=%d stdout=%s stderr=%s", args, code, &stdout, &stderr)
		}
	}
	files, err := filepath.Glob(filepath.Join(dir, "parts", "*.ofd"))
	if err != nil || len(files) != 0 {
		t.Fatalf("拆分拒绝仍发布OFD：%v %v", files, err)
	}
	for _, name := range []string{"assets", "bundle", "manifest.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("权限拒绝仍发布%s：%v", name, err)
		}
	}
}
