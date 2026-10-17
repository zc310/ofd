package ses_test

import (
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/internal/parser"
)

func TestSM2SignaturesVerifyWithIndependentOpenSSL(t *testing.T) {
	tool, err := exec.LookPath("openssl")
	if err != nil {
		t.Skip("缺少 OpenSSL")
	}
	version, err := exec.CommandContext(t.Context(), tool, "version").Output()
	if err != nil || !strings.HasPrefix(string(version), "OpenSSL 3.") {
		t.Skip("此交叉检查需要 OpenSSL 3")
	}
	f := newSigningFixture(t, false)
	generated, err := parser.ParseSignedValue(f.value)
	if err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD("../../testdata/ofdrw/999.ofd")
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	for _, value := range []*parser.SignedValue{generated, ofd.Documents[0].GetSignedValue("1")} {
		if value == nil || value.SES == nil {
			t.Fatal("缺少可验证的 SES 数据")
		}
		for _, component := range []struct{ certificate, data, signature []byte }{
			{value.SES.Certificate, value.SES.TBS.Raw, value.SES.Signature.Bytes},
			{value.SES.TBS.Seal.Certificate, value.SES.TBS.Seal.SealInfo.Raw, value.SES.TBS.Seal.Signature.Bytes},
		} {
			certificate, err := smx509.ParseCertificate(component.certificate)
			if err != nil {
				t.Fatal(err)
			}
			pub, err := smx509.MarshalPKIXPublicKey(certificate.PublicKey)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			keyPath, dataPath, sigPath := filepath.Join(dir, "public.pem"), filepath.Join(dir, "data.der"), filepath.Join(dir, "signature.der")
			for name, data := range map[string][]byte{keyPath: pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub}), dataPath: component.data, sigPath: component.signature} {
				if err := os.WriteFile(name, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			output, err := exec.CommandContext(t.Context(), tool, "pkeyutl", "-verify", "-pubin", "-inkey", keyPath, "-in", dataPath, "-sigfile", sigPath, "-rawin", "-digest", "sm3", "-pkeyopt", "distid:1234567812345678").CombinedOutput()
			if err != nil {
				t.Fatalf("独立 OpenSSL 验签失败: %v %s", err, output)
			}
		}
	}
}
