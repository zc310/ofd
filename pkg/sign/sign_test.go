package sign

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/pkg/merge"
)

func TestMain(m *testing.M) {
	if os.Getenv("OFD_SIGN_HELPER") == "1" {
		_, _ = io.Copy(os.Stdout, os.Stdin)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func testdataPath(name string) string {
	return filepath.Join("..", "..", "test", "testdata", name)
}

func TestSignAddsSignature(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("helper 进程命令依赖类 Unix 路径")
	}
	input, err := os.ReadFile(testdataPath("hello.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	if err := Sign(input, &buffer, Options{Command: os.Args[0], Environment: []string{"OFD_SIGN_HELPER=1"}}); err != nil {
		t.Fatalf("Sign 失败: %v", err)
	}

	pkg, err := core.OpenBytes(buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()
	for _, name := range []string{"Doc_0/Signatures.xml", "Doc_0/Signatures/Signature_sign-1.xml", "Doc_0/Signatures/Data/sign-1.dat"} {
		if !pkg.Has(name) {
			t.Fatalf("缺少签名文件 %s", name)
		}
	}
	root, err := pkg.Read("OFD.xml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(root, []byte("Doc_0/Signatures.xml")) {
		t.Fatalf("OFD.xml 未引用签名清单:\n%s", root)
	}

	statuses, err := merge.VerifySignatures(buffer.Bytes())
	if err != nil {
		t.Fatalf("VerifySignatures 失败: %v", err)
	}
	if len(statuses) != 1 || !statuses[0].DigestValid {
		t.Fatalf("签名摘要应有效: %+v", statuses)
	}
}

func TestSignRejectsEmptyCommand(t *testing.T) {
	if err := Sign([]byte("x"), &bytes.Buffer{}, Options{}); err == nil {
		t.Fatal("空命令应返回错误")
	}
}
