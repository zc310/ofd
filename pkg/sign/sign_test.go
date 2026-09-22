package sign

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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

func TestSignRemovesLegacySignsDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("helper 进程命令依赖类 Unix 路径")
	}
	input, err := os.ReadFile(testdataPath("999.ofd"))
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
	for _, item := range pkg.Entries() {
		if strings.Contains(item.Path, "Doc_0/Signs/") || strings.HasSuffix(item.Path, "Doc_0/Signs.xml") {
			t.Fatalf("旧签名目录文件未清理: %s", item.Path)
		}
		if strings.Contains(item.Path, "Signs/") && !strings.Contains(item.Path, "/Signatures/") {
			t.Fatalf("不应保留旧 Signs 目录文件: %s", item.Path)
		}
	}
	if !pkg.Has("Doc_0/Signatures/Data/sign-1.dat") {
		t.Fatal("应写入新签名文件")
	}
}

func TestSignRejectsEmptyCommand(t *testing.T) {
	if err := Sign([]byte("x"), &bytes.Buffer{}, Options{}); err == nil {
		t.Fatal("空命令应返回错误")
	}
}

func TestSignDeterministic(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("helper 进程命令依赖类 Unix 路径")
	}
	input, err := os.ReadFile(testdataPath("hello.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	options := Options{
		Command:       os.Args[0],
		Environment:   []string{"OFD_SIGN_HELPER=1"},
		Deterministic: true,
		Date:          time.Unix(0, 0).UTC(),
	}
	var first, second bytes.Buffer
	if err := Sign(input, &first, options); err != nil {
		t.Fatalf("首次 Sign 失败: %v", err)
	}
	if err := Sign(input, &second, options); err != nil {
		t.Fatalf("再次 Sign 失败: %v", err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("确定性签名两次输出应完全一致")
	}
}

func TestSignWritesStampAnnot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("helper 进程命令依赖类 Unix 路径")
	}
	input, err := os.ReadFile(testdataPath("hello.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	if err := Sign(input, &buffer, Options{
		Command:     os.Args[0],
		Environment: []string{"OFD_SIGN_HELPER=1"},
		Stamp:       &StampOptions{},
	}); err != nil {
		t.Fatalf("Sign 失败: %v", err)
	}

	pkg, err := core.OpenBytes(buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()
	signature, err := pkg.Read("Doc_0/Signatures/Signature_sign-1.xml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(signature, []byte("<StampAnnot")) {
		t.Fatalf("Signature.xml 应包含 StampAnnot:\n%s", signature)
	}
	if !bytes.Contains(signature, []byte(`Boundary="160 247 40 40"`)) {
		t.Fatalf("默认 Boundary 应为首页右下角 40mm 印章:\n%s", signature)
	}

	signatures, err := pkg.Read("Doc_0/Signatures.xml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(signatures, []byte(`Type="Seal"`)) {
		t.Fatalf("Signatures.xml 应声明 Seal 类型:\n%s", signatures)
	}
}

func TestSignStampNilWhenNotRequested(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("helper 进程命令依赖类 Unix 路径")
	}
	input, err := os.ReadFile(testdataPath("hello.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	if err := Sign(input, &buffer, Options{Command: os.Args[0], Environment: []string{"OFD_SIGN_HELPER=1"}}); err != nil {
		t.Fatal(err)
	}
	pkg, err := core.OpenBytes(buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()
	signature, err := pkg.Read("Doc_0/Signatures/Signature_sign-1.xml")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(signature, []byte("StampAnnot")) {
		t.Fatal("未请求签章时不应写入 StampAnnot")
	}
}

func TestSignReferenceSelection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("helper 进程命令依赖类 Unix 路径")
	}
	input, err := os.ReadFile(testdataPath("hello.ofd"))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		options *ReferenceOptions
		want    []string
		notWant []string
	}{
		{
			name:    "默认包含文档体全部文件",
			options: nil,
			want:    []string{"../Document.xml", "../DocumentRes.xml", "../Pages/Page_0/Content.xml"},
		},
		{
			name:    "Include 只保留 XML",
			options: &ReferenceOptions{Include: []string{"*.xml", "Pages/**"}},
			want:    []string{"../Document.xml", "../DocumentRes.xml", "../Pages/Page_0/Content.xml"},
		},
		{
			name:    "Exclude 排除页面内容",
			options: &ReferenceOptions{Exclude: []string{"Pages/**"}},
			want:    []string{"../Document.xml", "../DocumentRes.xml"},
			notWant: []string{"../Pages/Page_0/Content.xml"},
		},
		{
			name:    "包含根文件",
			options: &ReferenceOptions{RootDocument: true},
			want:    []string{"../../OFD.xml"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var buffer bytes.Buffer
			if err := Sign(input, &buffer, Options{
				Command:     os.Args[0],
				Environment: []string{"OFD_SIGN_HELPER=1"},
				References:  test.options,
			}); err != nil {
				t.Fatalf("Sign 失败: %v", err)
			}
			pkg, err := core.OpenBytes(buffer.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = pkg.Close() }()
			signature, err := pkg.Read("Doc_0/Signatures/Signature_sign-1.xml")
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range test.want {
				needle := []byte(`FileRef="` + want + `"`)
				if !bytes.Contains(signature, needle) {
					t.Fatalf("References 应包含 %s:\n%s", want, signature)
				}
			}
			for _, notWant := range test.notWant {
				needle := []byte(`FileRef="` + notWant + `"`)
				if bytes.Contains(signature, needle) {
					t.Fatalf("References 不应包含 %s:\n%s", notWant, signature)
				}
			}
		})
	}
}
