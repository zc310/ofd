package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/crypt"
	"github.com/zc310/ofd/pkg/integrity"
)

func TestCLIIntegrityVerifyHonorsDecryptionBudgets(t *testing.T) {
	input, password := cliFixture(t)
	dir := filepath.Dir(input)
	independent, layered := filepath.Join(dir, "independent.ofd"), filepath.Join(dir, "layered.ofd")
	call(t, []string{"encrypt", "--independent-keys", "--password-file", password, "-o", independent, input}, "", 0)
	call(t, []string{"encrypt", "--append", "--input-password-file", password, "--password-file", password, "-o", layered, independent}, "", 0)
	addIntegrityCLIManifest(t, independent)
	addIntegrityCLIManifest(t, layered)
	for _, test := range []struct{ flag, low, high, input string }{
		{"--max-recipients", "3", "4", independent},
		{"--max-credential-attempts", "3", "4", independent},
		{"--max-layers", "1", "2", layered},
		{"--max-processed-bytes", "1", "1048576", independent},
	} {
		t.Run(test.flag, func(t *testing.T) {
			args := []string{"integrity", "verify", "--format", "json", "--password-file", password, test.flag}
			_, diag := call(t, append(append([]string{}, args...), test.low, test.input), "", 1)
			if !strings.Contains(diag, crypt.ErrLimitExceeded.Error()) {
				t.Fatalf("低预算未传入验证流程: %s", diag)
			}
			out, diag := call(t, append(append([]string{}, args...), test.high, test.input), "", 1)
			if strings.Contains(diag, crypt.ErrLimitExceeded.Error()) {
				t.Fatalf("提高预算后仍超限: %s", diag)
			}
			var report integrity.Report
			if err := json.Unmarshal([]byte(out), &report); err != nil {
				t.Fatalf("缺少验证报告: %v, stdout=%s", err, out)
			}
			foundReferences := false
			for _, check := range report.Checks {
				if check.Code == "verification" {
					t.Fatalf("提高预算后验证仍中止: %+v", report.Checks)
				}
				foundReferences = foundReferences || check.Code == "references"
			}
			if !foundReferences || report.Status != "invalid" {
				t.Fatalf("应完成闭包检查并保留缺少清单签名的失败: %+v", report)
			}
		})
	}
	for _, flag := range []string{"--max-recipients", "--max-layers", "--max-processed-bytes"} {
		call(t, []string{"integrity", "verify", flag, "-1", independent}, "", 2)
	}
}

func TestCLIIntegrityVerifyPreservesPartialReportAndFailure(t *testing.T) {
	input, password := cliFixture(t)
	encrypted := filepath.Join(filepath.Dir(input), "encrypted.ofd")
	call(t, []string{"encrypt", "--password-file", password, "-o", encrypted, input}, "", 0)
	addIntegrityCLIManifest(t, encrypted)
	expected, err := integrity.Verify(t.Context(), encrypted, integrity.Options{Decrypt: &crypt.DecryptOptions{Password: []byte("wrong")}})
	if !errors.Is(err, crypt.ErrDecryptFailed) || expected == nil || expected.Status != "invalid" || len(expected.Checks) < 3 {
		t.Fatalf("未构造出带已有失败的部分报告: report=%+v err=%v", expected, err)
	}
	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	var expectedValue any
	if err := json.Unmarshal(expectedJSON, &expectedValue); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"text", "markdown", "json"} {
		for _, target := range []string{"stdout", "file"} {
			t.Run(format+"/"+target, func(t *testing.T) {
				args := []string{"integrity", "verify", "--format", format, "--password-file", "-"}
				output := filepath.Join(t.TempDir(), "report")
				if target == "file" {
					args = append(args, "-o", output)
				}
				out, diag := call(t, append(args, encrypted), "wrong", 1)
				if !strings.Contains(diag, crypt.ErrDecryptFailed.Error()) {
					t.Fatalf("原验证错误被丢弃: %s", diag)
				}
				if target == "file" {
					if out != "" {
						t.Fatalf("文件报告污染 stdout: %s", out)
					}
					data, err := os.ReadFile(output)
					if err != nil {
						t.Fatal(err)
					}
					out = string(data)
				}
				if format == "json" {
					var got any
					if err := json.Unmarshal([]byte(out), &got); err != nil || !reflect.DeepEqual(got, expectedValue) {
						t.Fatalf("CLI 未保留库返回的部分报告: report=%+v err=%v stdout=%s", got, err, out)
					}
				} else {
					for _, text := range []string{"invalid", "manifest.signature", "verification", crypt.ErrDecryptFailed.Error()} {
						if !strings.Contains(out, text) {
							t.Fatalf("部分报告缺少 %q: %s", text, out)
						}
					}
				}
			})
		}
	}
	writeErr := errors.New("报告输出失败")
	var diag bytes.Buffer
	if code := run(t.Context(), []string{"integrity", "verify", "--format", "json", "--password-file", "-", encrypted}, strings.NewReader("wrong"), integrityFailingWriter{writeErr}, &diag); code != 1 {
		t.Fatalf("报告写入失败退出码错误: %d", code)
	}
	for _, err := range []error{writeErr, crypt.ErrDecryptFailed} {
		if !strings.Contains(diag.String(), err.Error()) {
			t.Fatalf("报告写入失败后未保留错误 %v: %s", err, &diag)
		}
	}
}

type integrityFailingWriter struct{ err error }

func (w integrityFailingWriter) Write([]byte) (int, error) {
	return 0, fmt.Errorf("写入报告: %w", w.err)
}
