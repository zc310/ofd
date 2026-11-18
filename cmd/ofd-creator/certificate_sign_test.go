package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/zc310/ofd/pkg/sign"
)

func TestCertificateTagCLISignAndRemoveValidateAcceptsMetadataCollections(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("回归使用类 Unix 外部签名器路径")
	}
	dir := t.TempDir()
	signer := filepath.Join(dir, "ofd-signer-demo")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", signer, "../ofd-signer-demo")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("构建演示签名器失败: %v\n%s", err, output)
	}
	plain, signed, removed := filepath.Join(dir, "plain.ofd"), filepath.Join(dir, "signed.ofd"), filepath.Join(dir, "removed.ofd")
	for _, args := range [][]string{
		{"-i", "examples/certificate-tags.yaml", "-o", plain, "--validate"},
		{"sign", "--append", "--lock-signatures", "--sign-id", "issuer-lock", "--sign-cmd", signer, "--validate", "-o", signed, plain},
		{"remove-signatures", "--id", "issuer-lock", "--validate", "-o", removed, signed},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != exitOK || stdout.Len() != 0 || stderr.Len() != 0 {
			t.Fatalf("执行失败: %v code=%d stdout=%s stderr=%s", args, code, &stdout, &stderr)
		}
	}
	data, err := os.ReadFile(signed)
	if err != nil {
		t.Fatal(err)
	}
	results, err := sign.Verify(t.Context(), data, sign.VerificationOptions{})
	if err != nil || len(results) != 1 || !results[0].DigestValid || results[0].Result == nil || !results[0].Result.Valid || results[0].Result.FullyValid() {
		t.Fatalf("演示产物只能说明数学与摘要有效，不能代替机构授权: %+v %v", results, err)
	}
}
