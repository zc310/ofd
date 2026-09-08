package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/pkg/validator"
)

func TestRunCreatesValidatedYAMLDocument(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "document.yaml")
	output := filepath.Join(directory, "result.ofd")
	manifest := []byte("version: 1\ndocument:\n  id: cli-test\n  title: CLI 测试\n  pageSize:\n    name: A4\npages:\n  - items:\n      - type: text\n        x: 20\n        y: 30\n        width: 100\n        height: 10\n        value: hello\n        font: SimSun\n")
	if err := os.WriteFile(input, manifest, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-i", input, "-o", output, "--validate"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("run exit code = %d, stderr = %s", code, stderr.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	if report := instance.ValidateReader(t.Context(), bytes.NewReader(data), output); report.HasErrors() {
		t.Fatalf("generated OFD failed validation: %+v", report.Issues)
	}
}

func TestRunRejectsUnknownManifestField(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "document.json")
	if err := os.WriteFile(input, []byte(`{"document":{"id":"test"},"pages":[{}],"unknown":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--input", input, "--output", filepath.Join(directory, "result.ofd")}, &stdout, &stderr); code != exitResource {
		t.Fatalf("run exit code = %d, stderr = %s", code, stderr.String())
	}
}

func TestRunCreatesTOMLDocument(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "document.toml")
	output := filepath.Join(directory, "result.ofd")
	manifest := []byte("version = 1\n\n[document]\nid = \"toml-cli-test\"\n\n[[pages]]\n")
	if err := os.WriteFile(input, manifest, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-i", input, "-o", output, "--check"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("run exit code = %d, stderr = %s", code, stderr.String())
	}
}

func TestRunCheckDoesNotRequireOutput(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "document.yaml")
	if err := os.WriteFile(input, []byte("version: 1\ndocument:\n  id: check-test\npages:\n  - items: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-i", input, "--check"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("run exit code = %d, stderr = %s", code, stderr.String())
	}
}

func TestWriteOutputUsesAtomicReplacement(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "result.ofd")
	if err := writeOutput(output, []byte("new"), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("output = %q, want new", data)
	}
}
