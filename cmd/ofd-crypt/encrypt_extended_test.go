package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/pkg/crypt"
)

func TestCLIIndependentKeysAndAppendUseSeparateOldCredentials(t *testing.T) {
	input, password := cliFixture(t)
	dir := filepath.Dir(input)
	inner, outer, plain := filepath.Join(dir, "inner.ofd"), filepath.Join(dir, "outer.ofd"), filepath.Join(dir, "plain.ofd")
	call(t, []string{"encrypt", "--independent-keys", "--password-file", password, "-o", inner, input}, "", 0)
	for _, format := range []string{"text", "markdown", "json"} {
		out, _ := call(t, []string{"encrypt", "--append", "--input-password-file", password, "--password-file", "-", "--user", "new-user", "--force", "--format", format, "-o", outer, inner}, "new-password", 0)
		if format == "json" {
			var report crypt.EncryptReport
			if err := json.Unmarshal([]byte(out), &report); err != nil || report.OperationID != "enc2" || report.Relative != "enc1" {
				t.Fatalf("report=%+v err=%v", report, err)
			}
		}
	}
	call(t, []string{"decrypt", "--password-for", "enc1=" + password, "--password-for", "enc2=-", "-o", plain, outer}, "new-password", 0)
	before, err := container.OpenFile(input)
	if err != nil {
		t.Fatal(err)
	}
	defer before.Close()
	after, err := container.OpenFile(plain)
	if err != nil {
		t.Fatal(err)
	}
	defer after.Close()
	for _, entry := range before.Entries() {
		a, err := before.Read(entry.Path)
		if err != nil {
			t.Fatal(err)
		}
		b, err := after.Read(entry.Path)
		if err != nil || !bytes.Equal(a, b) {
			t.Fatalf("恢复条目改变: %s %v", entry.Path, err)
		}
	}
	// 操作选择器也可用于追加预检；新接收者名称不会覆盖旧接收者筛选。
	call(t, []string{"encrypt", "--append", "--password-for", "enc1=" + password, "--password-file", "-", "--force", "-o", outer, inner}, "another-new", 0)
	previous, err := os.ReadFile(outer)
	if err != nil {
		t.Fatal(err)
	}
	call(t, []string{"encrypt", "--append", "--input-password-file", "-", "--password-file", password, "--force", "-o", outer, inner}, "wrong-old", 1)
	now, err := os.ReadFile(outer)
	if err != nil || !bytes.Equal(previous, now) {
		t.Fatal("追加失败覆盖已有输出")
	}
	call(t, []string{"encrypt", "--append", "--input-password-file", password, "--password-file", password, "--force", "-o", password, inner}, "", 2)
}

func TestCLIRejectsInvalidAppendAndPageCombinationsBeforeReadingStdin(t *testing.T) {
	input, password := cliFixture(t)
	output := filepath.Join(filepath.Dir(input), "output.ofd")
	for _, flags := range [][]string{
		{"--append"},
		{"--input-password-file", password},
		{"--append", "--input-password-file", "-"},
		{"--append", "--input-password-file", password, "--input-key", password},
		{"--append", "--input-password-file", password, "--entry", "Encryptions.xml"},
		{"--page-id", "1"},
		{"--page-document", "Doc/Document.xml"},
		{"--page-document", "Doc/Document.xml", "--page-id", "0"},
		{"--page-document", "Doc/Document.xml", "--page-id", "1", "--page-id", "1"},
		{"--page-document", "Doc/Document.xml", "--page-id", "1", "--entry", "Doc/data.bin"},
		{"--append", "--input-password-file", password, "--page-document", "Doc/Document.xml", "--page-id", "1"},
	} {
		args := append([]string{"encrypt", "--password-file", "-", "-o", output}, flags...)
		call(t, append(args, input), "", 2)
	}
}

func TestCLIPageScopeReportsExpandedSelection(t *testing.T) {
	input, password := cliFixture(t)
	var data bytes.Buffer
	z := zip.NewWriter(&data)
	for name, content := range map[string]string{
		"OFD.xml":          `<OFD xmlns="http://www.ofdspec.org/2016"><DocBody><DocRoot>Doc/Document.xml</DocRoot></DocBody></OFD>`,
		"Doc/Document.xml": `<Document xmlns="http://www.ofdspec.org/2016"><Pages><Page ID="1" BaseLoc="one.xml"/><Page ID="2" BaseLoc="two.xml"/></Pages></Document>`,
		"Doc/one.xml":      `<Page xmlns="http://www.ofdspec.org/2016"><Content/></Page>`,
		"Doc/two.xml":      `<Page xmlns="http://www.ofdspec.org/2016"><Content/></Page>`,
	} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(filepath.Dir(input), "page.ofd")
	out, _ := call(t, []string{"encrypt", "--password-file", password, "--page-document", "Doc/Document.xml", "--page-id", "1", "--format", "json", "-o", output, input}, "", 0)
	var report crypt.EncryptReport
	if err := json.Unmarshal([]byte(out), &report); err != nil || len(report.SelectedEntries) != 1 || report.SelectedEntries[0] != "Doc/one.xml" {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}
