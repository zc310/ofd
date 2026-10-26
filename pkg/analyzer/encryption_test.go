package analyzer

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/crypt"
)

func TestAnalyzeLockedPackagePreservesMetadataAndIncompleteState(t *testing.T) {
	files := map[string]string{"Cipher/root.dat": strings.Repeat("x", 32)}
	for name, source := range map[string]string{"Encryptions.xml": "Encryptions.xml", "Security/seed.xml": "seed.xml", "Security/map.dat": "map.xml"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "crypt", source))
		if errors.Is(err, os.ErrNotExist) {
			t.Skipf("missing crypto fixture %s", source)
		}
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(data)
	}
	report, err := Analyze(bytes.NewReader(makeArchive(t, files)))
	if !errors.Is(err, crypt.ErrLocked) || report.Status != StatusPartial || report.Encryption == nil || report.Encryption.State != "locked" || len(report.Errors) != 0 {
		t.Fatalf("report=%+v, err=%v", report, err)
	}
	for _, render := range []func(*bytes.Buffer) error{
		func(w *bytes.Buffer) error { return RenderText(w, report) },
		func(w *bytes.Buffer) error { return RenderMarkdown(w, report) },
		func(w *bytes.Buffer) error { return RenderJSON(w, report, true) },
	} {
		var out bytes.Buffer
		if err := render(&out); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "未解锁") {
			t.Fatalf("encryption diagnostic lost: %s", &out)
		}
	}
	t.Run("flow", func(t *testing.T) {
		doc, err := BuildReport(t.Context(), report, DocumentOptions{Font: testReportFont(t)})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(analysisModelText(doc), "未解锁") {
			t.Fatal("报告缺少未解锁说明")
		}
	})
}
