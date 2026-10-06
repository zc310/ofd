package sign

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/emmansun/gmsm/sm3"
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

func testdataPath(names ...string) string {
	return filepath.Join(append([]string{"..", "..", "testdata"}, names...)...)
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

func TestSignatureDigestSupportsDeclaredMethods(t *testing.T) {
	data := []byte("signature digest test")
	md5Sum := md5.Sum(data)
	sha1Sum := sha1.Sum(data)
	sm3Hash := sm3.New()
	_, _ = sm3Hash.Write(data)
	sm3Sum := sm3Hash.Sum(nil)
	for _, test := range []struct {
		method string
		want   []byte
	}{
		{method: "MD5", want: md5Sum[:]},
		{method: "SHA1", want: sha1Sum[:]},
		{method: "SM3", want: sm3Sum},
		{method: "1.2.156.10197.1.401", want: sm3Sum},
	} {
		got, err := signatureDigest(test.method, data)
		if err != nil {
			t.Errorf("method=%s: unexpected error: %v", test.method, err)
			continue
		}
		if !bytes.Equal(got, test.want) {
			t.Errorf("method=%s: digest = %x, want %x", test.method, got, test.want)
		}
	}
	if _, err := signatureDigest("SHA256", data); err == nil {
		t.Fatal("不支持的摘要算法应返回错误")
	}
}

func TestSignSHA1CheckMethodVerifiesDigest(t *testing.T) {
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
		CheckMethod: "SHA1",
		Environment: []string{"OFD_SIGN_HELPER=1"},
	}); err != nil {
		t.Fatal(err)
	}
	statuses, err := merge.VerifySignatures(buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || !statuses[0].DigestValid {
		t.Fatalf("SHA1 签名摘要应有效: %+v", statuses)
	}
}

func TestSignRemovesLegacySignsDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("helper 进程命令依赖类 Unix 路径")
	}
	input, err := os.ReadFile(testdataPath("ofdrw/999.ofd"))
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

func TestSignWritesSeamStampAnnotations(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("helper 进程命令依赖类 Unix 路径")
	}
	input, err := os.ReadFile(testdataPath("ofdrw/999.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	if err := Sign(input, &buffer, Options{
		Command:     os.Args[0],
		Environment: []string{"OFD_SIGN_HELPER=1"},
		StampSeam: &StampSeamOptions{
			Size: 40,
			Y:    -1,
		},
	}); err != nil {
		t.Fatalf("Sign 骑缝章失败: %v", err)
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
	if got := bytes.Count(signature, []byte("<StampAnnot")); got != 5 {
		t.Fatalf("5 页文档应写入 5 个骑缝 StampAnnot，实际 %d:\n%s", got, signature)
	}
	for _, want := range []string{
		`PageRef="10" Boundary="202 50 40 40" Clip="0 0 8 40"`,
		`PageRef="92" Boundary="194 128.5 40 40" Clip="8 0 8 40"`,
		`PageRef="629" Boundary="170 128.5 40 40" Clip="32 0 8 40"`,
	} {
		if !bytes.Contains(signature, []byte(want)) {
			t.Errorf("骑缝 StampAnnot 缺少 %s:\n%s", want, signature)
		}
	}
	statuses, err := merge.VerifySignatures(buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || !statuses[0].DigestValid {
		t.Fatalf("骑缝章签名摘要应有效: %+v", statuses)
	}
}

func TestSignWritesLeftSeamStampAnnotations(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("helper 进程命令依赖类 Unix 路径")
	}
	input, err := os.ReadFile(testdataPath("ofdrw/999.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	if err := Sign(input, &buffer, Options{
		Command:     os.Args[0],
		Environment: []string{"OFD_SIGN_HELPER=1"},
		StampSeam: &StampSeamOptions{
			Edge: "left",
			Size: 40,
			Y:    -1,
		},
	}); err != nil {
		t.Fatalf("Sign 左边缘骑缝章失败: %v", err)
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
	for _, want := range []string{
		`PageRef="10" Boundary="0 50 40 40" Clip="0 0 8 40"`,
		`PageRef="92" Boundary="-8 128.5 40 40" Clip="8 0 8 40"`,
	} {
		if !bytes.Contains(signature, []byte(want)) {
			t.Errorf("左边缘骑缝 StampAnnot 缺少 %s:\n%s", want, signature)
		}
	}
}

func TestSelectSeamPages(t *testing.T) {
	pages := []pageGeometry{{id: "1"}, {id: "2"}, {id: "3"}, {id: "4"}, {id: "5"}}
	tests := []struct {
		mode string
		want []string
	}{
		{mode: "", want: []string{"1", "2", "3", "4", "5"}},
		{mode: "all", want: []string{"1", "2", "3", "4", "5"}},
		{mode: "odd", want: []string{"1", "3", "5"}},
		{mode: "even", want: []string{"2", "4"}},
		{mode: "1,3-4", want: []string{"1", "3", "4"}},
		{mode: "4-", want: []string{"4", "5"}},
		{mode: "-2", want: []string{"1", "2"}},
	}
	for _, test := range tests {
		got, err := selectSeamPages(pages, test.mode)
		if err != nil {
			t.Errorf("mode=%q: unexpected error: %v", test.mode, err)
			continue
		}
		if len(got) != len(test.want) {
			t.Errorf("mode=%q: got %d pages, want %d", test.mode, len(got), len(test.want))
			continue
		}
		for index, page := range got {
			if page.id != test.want[index] {
				t.Errorf("mode=%q page[%d] = %q, want %q", test.mode, index, page.id, test.want[index])
			}
		}
	}
	if _, err := selectSeamPages(pages, "middle"); err == nil {
		t.Fatal("不支持的骑缝页面选择应返回错误")
	}
}

func TestSignWritesSealBaseLoc(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("helper 进程命令依赖类 Unix 路径")
	}
	input, err := os.ReadFile(testdataPath("hello.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	sealPath := filepath.Join(t.TempDir(), "Seal.esl")
	sealBytes := []byte("fake-seal-der-bytes")
	if err := os.WriteFile(sealPath, sealBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	if err := Sign(input, &buffer, Options{
		Command:     os.Args[0],
		Environment: []string{"OFD_SIGN_HELPER=1"},
		Seal:        sealPath,
	}); err != nil {
		t.Fatalf("Sign 失败: %v", err)
	}

	pkg, err := core.OpenBytes(buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()
	if !pkg.Has("Doc_0/Signatures/Seal.esl") {
		t.Fatal("电子印章文件应被打包进签名目录")
	}
	gotSeal, err := pkg.Read("Doc_0/Signatures/Seal.esl")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotSeal, sealBytes) {
		t.Fatal("打包后的 Seal.esl 内容应与输入一致")
	}
	signature, err := pkg.Read("Doc_0/Signatures/Signature_sign-1.xml")
	if err != nil {
		t.Fatal(err)
	}
	// Seal 的 BaseLoc 是子元素而非属性：写成属性时阅读器解析出的 BaseLoc 为空，
	// 会把空路径解析成签名目录本身并报"打开文件失败: Doc_0/Signatures"。
	if !bytes.Contains(signature, []byte(`<Seal><BaseLoc>Seal.esl</BaseLoc></Seal>`)) {
		t.Fatalf("Signature.xml 应以子元素形式声明 Seal/BaseLoc:\n%s", signature)
	}
	// 独立印章文件必须被签名引用覆盖，否则它会被替换而不会被发现。
	if !bytes.Contains(signature, []byte(`FileRef="Seal.esl"`)) {
		t.Fatalf("签名引用应包含 Seal.esl:\n%s", signature)
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
