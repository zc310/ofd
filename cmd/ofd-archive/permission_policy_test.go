package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zc310/ofd/pkg/creator"
)

func TestPreserveCLIArchivesExpiredRestrictedInputWithoutPermissionBypassFlag(t *testing.T) {
	denied := false
	data, err := creator.Marshal(creator.Document{ID: "archival-evidence", Pages: []creator.Page{{}}, Permissions: &creator.Permissions{
		Edit: &denied, Export: &denied, ValidPeriod: &creator.ValidPeriod{End: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	input, output := filepath.Join(dir, "original.ofd"), filepath.Join(dir, "preserved.ofd")
	if err := os.WriteFile(input, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"preserve", "--doc-type", "OFD-A", "--dry-run", input},
		{"preserve", "--doc-type", "OFD-A", "-o", output, input},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != exitOK {
			t.Fatalf("归档被阅读权限拒绝：code=%d stderr=%s", code, &stderr)
		}
	}
	got, err := os.ReadFile(input)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("原件发生变化：%v", err)
	}
	if info, err := os.Stat(output); err != nil || info.Size() == 0 {
		t.Fatalf("没有归档副本：%v", err)
	}
}
