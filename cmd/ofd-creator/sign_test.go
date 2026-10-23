package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/crypt"
	"github.com/zc310/ofd/pkg/sign"
)

func TestSignCLIAppendsWithExternalSignerAndKeepsFailedOutputUntouched(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("外部命令按空白拆分的回归使用类 Unix 可执行文件路径")
	}
	dir := t.TempDir()
	signer := filepath.Join(dir, "ofd-signer-demo")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", signer, "../ofd-signer-demo")
	if data, err := build.CombinedOutput(); err != nil {
		t.Fatalf("构建签名器: %v\n%s", err, data)
	}
	input := filepath.Join("..", "..", "testdata", "ofd-creator", "hello.ofd")
	if _, err := os.Stat(input); errors.Is(err, os.ErrNotExist) {
		t.Skipf("缺少测试样本 %s", input)
	} else if err != nil {
		t.Fatal(err)
	}
	first, final := filepath.Join(dir, "first.ofd"), filepath.Join(dir, "final.ofd")
	var stdout, stderr bytes.Buffer
	for _, args := range [][]string{
		{"sign", "--append", "--sign-cmd", signer, "--validate", "-o", first, input},
		{"sign", "--append", "--relative", "sign-1", "--lock-signatures", "--sign-cmd", signer, "--validate", "-o", final, first},
	} {
		if code := run(args, &stdout, &stderr); code != exitOK || stdout.Len() != 0 || stderr.Len() != 0 {
			t.Fatalf("run(%v)=%d stdout=%s stderr=%s", args, code, &stdout, &stderr)
		}
	}
	results, err := sign.Verify(t.Context(), final, sign.VerificationOptions{})
	if err != nil || len(results) != 2 {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	for _, result := range results {
		if !result.DigestValid || result.Error != "" || !result.Result.Valid || result.Result.FullyValid() {
			t.Fatalf("演示签名应保持数学有效、信任未完成: %+v", result)
		}
	}
	destination := filepath.Join(dir, "existing.ofd")
	if err := os.WriteFile(destination, []byte("keep this output"), 0600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"sign", "--append", "--sign-cmd", signer, "-o", destination, final}, &stdout, &stderr); code != exitResource || stdout.Len() != 0 {
		t.Fatalf("锁定列表未阻止追加: code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "keep this output" {
		t.Fatalf("失败改写了输出: %q %v", data, err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"sign", "--append", "--sign-cmd", signer, "-o", "-", first}, &stdout, &stderr); code != exitOK || stderr.Len() != 0 {
		t.Fatalf("标准输出签名失败: %d %s", code, &stderr)
	}
	if results, err := sign.Verify(t.Context(), stdout.Bytes(), sign.VerificationOptions{}); err != nil || len(results) != 2 {
		t.Fatalf("二进制输出被文本污染: %+v %v", results, err)
	}
	var encrypted bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), input, &encrypted, crypt.EncryptOptions{KeepRoot: true, Recipients: []crypt.EncryptRecipient{{UserName: "test", Password: []byte("secret")}}}); err != nil {
		t.Fatal(err)
	}
	encryptedPath, physicalPath := filepath.Join(dir, "encrypted.ofd"), filepath.Join(dir, "physical.ofd")
	if err := os.WriteFile(encryptedPath, encrypted.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"sign", "--physical", "--validate", "--sign-cmd", signer, "-o", physicalPath, encryptedPath}, &stdout, &stderr); code != exitOK || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("物理签署 CLI 失败: %d %s", code, &stderr)
	}
	results, err = sign.VerifyPhysical(t.Context(), physicalPath, sign.VerificationOptions{})
	if err != nil || len(results) != 1 || !results[0].DigestValid || !results[0].Result.Valid || results[0].Result.FullyValid() {
		t.Fatalf("物理签名结果错误: %+v %v", results, err)
	}
}

func TestSignCLIRejectsInvalidArgumentsBeforeOpeningInput(t *testing.T) {
	for _, args := range [][]string{
		{"--relative", "base"}, {"--lock-signatures"}, {"--append", "--sign-type", "Seal"},
		{"--sign-id", "../bad"}, {"--append", "--relative", "../bad"}, {"--sign-check-method", "SHA1"},
		{"--physical", "--append"}, {"--physical", "--sign-type", "Seal"}, {"--physical", "--sign-exclude", "**"},
		{"--input", "missing.ofd", "other.ofd"}, {"extra.ofd", "extra2.ofd"},
	} {
		var stdout, stderr bytes.Buffer
		full := append([]string{"sign", "--sign-cmd", "missing-signer", "-o", "unused.ofd"}, args...)
		if code := run(full, &stdout, &stderr); code != exitUsage || stdout.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf("args=%v code=%d stdout=%s stderr=%s", args, code, &stdout, &stderr)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"sign", "--help"}, &stdout, &stderr); code != exitOK || stderr.Len() != 0 {
		t.Fatalf("帮助失败: %d %s", code, &stderr)
	}
	for _, flag := range []string{"--append", "--relative", "--lock-signatures", "--sign-cmd"} {
		if !strings.Contains(stdout.String(), flag) {
			t.Fatalf("帮助缺少 %s: %s", flag, &stdout)
		}
	}
}
