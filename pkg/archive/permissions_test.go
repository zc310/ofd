package archive

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/validator"
)

func TestArchivePrepareKeepsRestrictedExpiredEvidenceByteForByte(t *testing.T) {
	denied := false
	data, err := creator.Marshal(creator.Document{ID: "archival-evidence", Pages: []creator.Page{{}}, Permissions: &creator.Permissions{
		Edit: &denied, Export: &denied, ValidPeriod: &creator.ValidPeriod{End: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	input, output := filepath.Join(dir, "input.ofd"), filepath.Join(dir, "archive")
	if err := os.WriteFile(input, data, 0600); err != nil {
		t.Fatal(err)
	}
	options := DefaultOptions()
	options.ValidatorOptions = []validator.Option{validator.WithMode(validator.ModeStructural)}
	if _, err := Prepare(t.Context(), input, output, Metadata{}, "", options); err != nil {
		t.Fatalf("权限/有效期阻止证据保存：%v", err)
	}
	for _, path := range []string{input, filepath.Join(output, "original", "document.ofd")} {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("原始证据发生改变：%s %v", path, err)
		}
	}
	verified, err := Verify(output)
	if err != nil || verified.Status != StatusPassed {
		t.Fatalf("受限证据固定性校验失败：%+v %v", verified, err)
	}
}
