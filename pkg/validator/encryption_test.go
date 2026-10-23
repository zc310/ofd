package validator

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/crypt"
	"github.com/zc310/ofd/pkg/spec"
)

func cryptoFiles(t *testing.T) map[string]string {
	t.Helper()
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
	return files
}

func TestLockedPackageReportsIncompleteInsteadOfMissingRootOrBadSignature(t *testing.T) {
	for _, opaque := range []bool{false, true} {
		files := cryptoFiles(t)
		if opaque {
			files["Security/map.dat"] = strings.Repeat("x", 32)
		}
		v, err := New(WithSignatureVerification(&SignatureVerificationOptions{}))
		if err != nil {
			t.Fatal(err)
		}
		report := v.ValidateReader(context.Background(), bytes.NewReader(makeArchive(t, files)), "encrypted.ofd")
		if report.Status != StatusPartial || report.Encryption == nil || report.Encryption.State != "locked" || report.ExitCode(false) != 1 || report.HasErrors() {
			t.Fatalf("report = %+v", report)
		}
		if !hasIssueCode(report, "encryption.locked") || hasIssueCode(report, "zip.missing_ofd_xml") || hasIssueCode(report, "reference.missing") || hasIssueCode(report, "digest.mismatch") {
			t.Fatalf("issues = %+v", report.Issues)
		}
		for _, name := range []string{"encryption", "references", "semantic", "digest", "signatures"} {
			found := false
			for _, check := range report.Checks {
				if check.Name == name && check.Status == "incomplete" {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s not incomplete: %+v", name, report.Checks)
			}
		}
		for _, render := range []func(*bytes.Buffer) error{
			func(w *bytes.Buffer) error { return RenderText(w, report) },
			func(w *bytes.Buffer) error { return RenderMarkdown(w, report) },
			func(w *bytes.Buffer) error { return RenderJSON(w, report, true) },
		} {
			var output bytes.Buffer
			if err := render(&output); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), "encryption") && !strings.Contains(output.String(), "未解锁") {
				t.Fatal("report lost encryption diagnostic")
			}
		}
	}
}

func TestLockedPackageDoesNotHideContainerOrVisibleXMLErrors(t *testing.T) {
	files := cryptoFiles(t)
	// 原始 OFD 路径中放置故意损坏的占位文件，校验时必须跳过；
	// 不相关的明文 XML 仍应报告实际语法错误。
	files["OFD.xml"], files["visible.xml"] = "not XML", "<broken"
	v, _ := New(WithMode(ModeStructural))
	report := v.ValidateReader(context.Background(), bytes.NewReader(makeArchive(t, files)), "locked.ofd")
	if report.Status != StatusInvalid || !hasIssueFileCode(report, "xml.malformed", "visible.xml") {
		t.Fatalf("issues = %+v", report.Issues)
	}
	for _, issue := range report.Issues {
		if issue.File == "OFD.xml" {
			t.Fatalf("placeholder was validated: %+v", issue)
		}
	}
	data := makeArchiveEntries(t, archiveEntry{"Encryptions.xml", files["Encryptions.xml"]}, archiveEntry{"../escape", "bad"})
	report = v.ValidateReader(context.Background(), bytes.NewReader(data), "bad.ofd")
	if !hasIssueCode(report, "zip.invalid_path") || report.ExitCode(false) != 1 {
		t.Fatalf("container issues = %+v", report.Issues)
	}
}

func TestEncryptedArchiveProfileRemainsInvalidWithOrWithoutPlaintextRoot(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		files := cryptoFiles(t)
		options := []Option{WithMode(ModeStructural)}
		if explicit {
			options = append(options, WithDocType(spec.DocTypeOFDA))
		} else {
			files["Security/map.dat"] = strings.ReplaceAll(files["Security/map.dat"], "/OFD.xml", "/Doc_0/Page.xml")
			files["OFD.xml"] = `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD-A"/>`
		}
		v, _ := New(options...)
		report := v.ValidateReader(context.Background(), bytes.NewReader(makeArchive(t, files)), "archive.ofd")
		if report.Status != StatusInvalid || !hasIssueCode(report, "profile.ofd_a.encrypted") {
			t.Fatalf("explicit=%v: %+v", explicit, report)
		}
	}
}

func TestReferenceClosureRejectsLockedPackageInsteadOfAuthorizingDeletion(t *testing.T) {
	index, err := PackageReferences(context.Background(), makeArchive(t, cryptoFiles(t)))
	if index != nil || !errors.Is(err, crypt.ErrLocked) {
		t.Fatalf("index=%+v, err=%v", index, err)
	}
}

func TestEncryptionMetadataBudgetAndDamageUseDistinctDiagnostics(t *testing.T) {
	files := cryptoFiles(t)
	v, _ := New(WithMaxXMLBytes(1))
	report := v.ValidateReader(context.Background(), bytes.NewReader(makeArchive(t, files)), "budget.ofd")
	if !hasIssueCode(report, "encryption.limit_exceeded") || hasIssueCode(report, "encryption.metadata_invalid") {
		t.Fatalf("budget diagnostics = %+v", report.Issues)
	}
	v, _ = New()
	files["Security/seed.xml"] = "<broken"
	report = v.ValidateReader(context.Background(), bytes.NewReader(makeArchive(t, files)), "damaged.ofd")
	if !hasIssueCode(report, "encryption.metadata_invalid") || report.Status != StatusInvalid {
		t.Fatalf("damage diagnostics = %+v", report.Issues)
	}
}
