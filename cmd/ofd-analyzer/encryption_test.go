package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/pkg/analyzer"
)

func TestLockedInputKeepsJSONCleanAndReportsPartial(t *testing.T) {
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	for name, source := range map[string]string{"Encryptions.xml": "Encryptions.xml", "Security/seed.xml": "seed.xml", "Security/map.dat": "map.xml", "Cipher/root.dat": ""} {
		content := []byte(strings.Repeat("x", 32))
		if source != "" {
			var err error
			content, err = os.ReadFile(filepath.Join("..", "..", "testdata", "crypt", source))
			if errors.Is(err, os.ErrNotExist) {
				t.Skipf("missing crypto fixture %s", source)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "encrypted.ofd")
	if err := os.WriteFile(input, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"text", "markdown", "json"} {
		var out, stderr bytes.Buffer
		if code := run([]string{"--format", format, input}, &out, &stderr); code != exitFailed || !strings.Contains(stderr.String(), "未解锁") {
			t.Fatalf("%s code=%d stderr=%s", format, code, &stderr)
		}
		if !strings.Contains(out.String(), "未解锁") {
			t.Fatalf("%s diagnostic missing", format)
		}
		if format == "json" {
			var report analyzer.Report
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			if report.Status != analyzer.StatusPartial || report.Encryption.State != "locked" {
				t.Fatalf("report=%+v", report)
			}
		}
	}
}
