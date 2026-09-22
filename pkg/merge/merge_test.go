package merge

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/pkg/validator"
)

func testdataPath(names ...string) string {
	parts := append([]string{"..", "..", "test", "testdata"}, names...)
	return filepath.Join(parts...)
}

func mergeToBytes(t *testing.T, inputs []string, options Options) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := Files(inputs, &buffer, options); err != nil {
		t.Fatalf("Files(%v) 失败: %v", inputs, err)
	}
	return buffer.Bytes()
}

func openMerged(t *testing.T, data []byte) *core.Package {
	t.Helper()
	pkg, err := core.OpenBytes(data)
	if err != nil {
		t.Fatalf("打开合并结果失败: %v", err)
	}
	t.Cleanup(func() { _ = pkg.Close() })
	return pkg
}

func readEntry(t *testing.T, pkg *core.Package, name string) string {
	t.Helper()
	data, err := pkg.Read(name)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", name, err)
	}
	return string(data)
}

func TestMergeRenamesDocumentDirectories(t *testing.T) {
	data := mergeToBytes(t, []string{
		testdataPath("hello.ofd"),
		testdataPath("helloworld.ofd"),
	}, Options{})

	pkg := openMerged(t, data)
	root := readEntry(t, pkg, "OFD.xml")
	if count := strings.Count(root, "<DocRoot>Doc_0/Document.xml</DocRoot>"); count != 1 {
		t.Fatalf("期望一个 Doc_0 文档体，实际 OFD.xml:\n%s", root)
	}
	if count := strings.Count(root, "<DocRoot>Doc_1/Document.xml</DocRoot>"); count != 1 {
		t.Fatalf("期望一个 Doc_1 文档体，实际 OFD.xml:\n%s", root)
	}
	if !pkg.Has("Doc_0/Document.xml") || !pkg.Has("Doc_1/Document.xml") {
		t.Fatalf("合并结果缺少 Doc_0/Document.xml 或 Doc_1/Document.xml")
	}
}

func TestMergeRegeneratesDuplicateDocID(t *testing.T) {
	// hello.ofd 与 helloworld.ofd 的 DocID 都是 helloworld。
	data := mergeToBytes(t, []string{
		testdataPath("hello.ofd"),
		testdataPath("helloworld.ofd"),
	}, Options{})

	root := readEntry(t, openMerged(t, data), "OFD.xml")
	ids := findDocIDs(root)
	if len(ids) != 2 {
		t.Fatalf("期望 2 个 DocID，实际 %d 个: %v\n%s", len(ids), ids, root)
	}
	if ids[0] == "" || ids[1] == "" {
		t.Fatalf("合并后的 DocID 为空: %v", ids)
	}
	if ids[0] == ids[1] {
		t.Fatalf("合并后的 DocID 应保持唯一，实际都为 %q", ids[0])
	}
}

func TestMergeRewritesAbsoluteSignaturePaths(t *testing.T) {
	// 999.ofd 的签名清单使用 /Doc_0/... 绝对包内路径。
	data := mergeToBytes(t, []string{
		testdataPath("999.ofd"),
		testdataPath("999.ofd"),
	}, Options{Signatures: SignatureRewrite})

	pkg := openMerged(t, data)
	first := readEntry(t, pkg, "Doc_0/Signs/Sign_0/Signature.xml")
	second := readEntry(t, pkg, "Doc_1/Signs/Sign_0/Signature.xml")

	if !strings.Contains(first, `FileRef="/Doc_0/Pages/Page_0/Content.xml"`) {
		t.Fatalf("Doc_0 签名引用应保持不变，实际:\n%s", first)
	}
	if strings.Contains(second, "/Doc_0/") {
		t.Fatalf("Doc_1 签名引用不应再指向 /Doc_0: \n%s", second)
	}
	if !strings.Contains(second, `FileRef="/Doc_1/Pages/Page_0/Content.xml"`) {
		t.Fatalf("Doc_1 签名引用应重写为 /Doc_1: \n%s", second)
	}
}

func TestSignaturePreserveRejectsRenamedAbsolutePaths(t *testing.T) {
	err := Files([]string{testdataPath("999.ofd"), testdataPath("999.ofd")}, &bytes.Buffer{}, Options{Signatures: SignaturePreserve})
	if err == nil {
		t.Fatalf("preserve 模式下签名绝对路径无法保留时应返回错误")
	}
}

func TestSignatureDropRemovesSignatureFiles(t *testing.T) {
	data := mergeToBytes(t, []string{
		testdataPath("999.ofd"),
		testdataPath("999.ofd"),
	}, Options{Signatures: SignatureDrop})

	pkg := openMerged(t, data)
	for _, entry := range pkg.Entries() {
		if strings.Contains(entry.Path, "Signs/") {
			t.Fatalf("drop 模式不应保留签名条目: %s", entry.Path)
		}
	}
	root := readEntry(t, pkg, "OFD.xml")
	if strings.Contains(root, "Signatures") {
		t.Fatalf("drop 模式不应在 OFD.xml 中保留签名引用:\n%s", root)
	}
}

func TestMergeKeepsRelativeSignaturePaths(t *testing.T) {
	relative := []byte(`<?xml version="1.0" encoding="UTF-8"?><Signature xmlns="http://www.ofdspec.org/2016"><SignedInfo><References><Reference FileRef="Document.xml"/></References></SignedInfo><SignedValue>Signs/Sign_0/SignedValue.dat</SignedValue></Signature>`)
	got, changed := rewriteSignatureXML(relative, "Doc_0", "Doc_1")
	if changed {
		t.Fatalf("相对路径不应被重写: %s", got)
	}
	if !bytes.Equal(got, relative) {
		t.Fatalf("相对路径未改动时应返回原始字节")
	}
}

func TestMergedDocumentsPassStrictValidation(t *testing.T) {
	data := mergeToBytes(t, []string{
		testdataPath("hello.ofd"),
		testdataPath("helloworld.ofd"),
	}, Options{})

	instance, err := validator.New()
	if err != nil {
		t.Fatalf("创建校验器失败: %v", err)
	}
	report := instance.ValidateReader(context.Background(), bytes.NewReader(data), "merged.ofd")
	if report.HasErrors() {
		var out strings.Builder
		_ = validator.RenderText(&out, report)
		t.Fatalf("合并结果未通过严格校验:\n%s", out.String())
	}
}

func TestMergeDeterministic(t *testing.T) {
	inputs := []string{testdataPath("hello.ofd"), testdataPath("helloworld.ofd")}
	first := mergeToBytes(t, inputs, Options{Deterministic: true})
	second := mergeToBytes(t, inputs, Options{Deterministic: true})
	if !bytes.Equal(first, second) {
		t.Fatalf("确定性合并应产生相同字节")
	}
}

func TestBytesMatchesFiles(t *testing.T) {
	paths := []string{testdataPath("hello.ofd"), testdataPath("helloworld.ofd")}
	documents := make([][]byte, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", path, err)
		}
		documents = append(documents, data)
	}

	fromFiles := mergeToBytes(t, paths, Options{Deterministic: true})
	var buffer bytes.Buffer
	if err := Bytes(documents, &buffer, Options{Deterministic: true}); err != nil {
		t.Fatalf("Bytes 失败: %v", err)
	}
	if !bytes.Equal(fromFiles, buffer.Bytes()) {
		t.Fatalf("Bytes 与 Files 应产生相同结果")
	}
}

func TestBytesRejectsEmptyDocument(t *testing.T) {
	if err := Bytes([][]byte{nil}, &bytes.Buffer{}, Options{}); err == nil {
		t.Fatalf("空输入文档应返回错误")
	}
}

func TestSourcesWithReaderAt(t *testing.T) {
	paths := []string{testdataPath("hello.ofd"), testdataPath("helloworld.ofd")}
	documents := make([][]byte, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", path, err)
		}
		documents = append(documents, data)
	}

	want := mergeToBytes(t, paths, Options{Deterministic: true})
	var buffer bytes.Buffer
	sources := make([]Source, 0, len(documents))
	for index, data := range documents {
		sources = append(sources, Source{Name: paths[index], Reader: bytes.NewReader(data), Size: int64(len(data))})
	}
	if err := Sources(sources, &buffer, Options{Deterministic: true}); err != nil {
		t.Fatalf("Sources 失败: %v", err)
	}
	if !bytes.Equal(want, buffer.Bytes()) {
		t.Fatalf("Sources(io.ReaderAt) 与 Files 应产生相同结果")
	}
}

func TestSourcesValidatesInputs(t *testing.T) {
	hello, err := os.ReadFile(testdataPath("hello.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		sources []Source
	}{
		{"empty", nil},
		{"no source", []Source{{Name: "x"}}},
		{"multiple sources", []Source{{Name: "x", Data: hello, Reader: bytes.NewReader(hello), Size: int64(len(hello))}}},
		{"reader without size", []Source{{Name: "x", Reader: bytes.NewReader(hello)}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := Sources(tc.sources, &bytes.Buffer{}, Options{}); err == nil {
				t.Fatalf("应返回错误")
			}
		})
	}
}

func TestMarshal(t *testing.T) {
	data, err := Marshal([]string{testdataPath("hello.ofd"), testdataPath("helloworld.ofd")}, Options{})
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	if _, err := core.OpenBytes(data); err != nil {
		t.Fatalf("Marshal 结果不是有效 ZIP: %v", err)
	}
}

func TestMergeRejectsEntryOutsideDocumentDirectory(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "orphan.ofd")
	writeOrphanOFD(t, input)

	if err := Files([]string{input}, &bytes.Buffer{}, Options{}); err == nil {
		t.Fatalf("存在文档目录外的条目时应返回错误")
	}
}

func findDocIDs(root string) []string {
	var ids []string
	rest := root
	for {
		start := strings.Index(rest, "<DocID>")
		if start < 0 {
			break
		}
		start += len("<DocID>")
		end := strings.Index(rest[start:], "</DocID>")
		if end < 0 {
			break
		}
		ids = append(ids, strings.TrimSpace(rest[start:start+end]))
		rest = rest[start+end:]
	}
	return ids
}

// writeOrphanOFD 生成一个在文档目录外包含多余条目的 OFD，用于验证合并拒绝。
func writeOrphanOFD(t *testing.T, name string) {
	t.Helper()
	file, err := os.Create(name)
	if err != nil {
		t.Fatalf("创建测试 OFD 失败: %v", err)
	}
	defer func() { _ = file.Close() }()

	archive := zip.NewWriter(file)
	entries := map[string]string{
		"OFD.xml":            `<?xml version="1.0" encoding="UTF-8"?><OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>orphan</DocID></DocInfo><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`,
		"Doc_0/Document.xml": `<?xml version="1.0" encoding="UTF-8"?><Document xmlns="http://www.ofdspec.org/2016"/>`,
		"shared.txt":         "orphan",
	}
	for name, content := range entries {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatalf("创建条目 %s 失败: %v", name, err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatalf("写入条目 %s 失败: %v", name, err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("关闭测试 OFD 失败: %v", err)
	}
}

type ofdEntry struct {
	name    string
	content string
}

func writeTestOFD(t *testing.T, path string, entries []ofdEntry) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建测试 OFD 失败: %v", err)
	}
	defer func() { _ = file.Close() }()

	archive := zip.NewWriter(file)
	for _, entry := range entries {
		writer, err := archive.Create(entry.name)
		if err != nil {
			t.Fatalf("创建条目 %s 失败: %v", entry.name, err)
		}
		if _, err := writer.Write([]byte(entry.content)); err != nil {
			t.Fatalf("写入条目 %s 失败: %v", entry.name, err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("关闭测试 OFD 失败: %v", err)
	}
}

const testDocumentXML = `<?xml version="1.0" encoding="UTF-8"?><Document xmlns="http://www.ofdspec.org/2016"/>`

func testOFDXML(docRoot string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>test</DocID></DocInfo><DocRoot>` + docRoot + `</DocRoot></DocBody></OFD>`
}

func TestMergeRejectsUnsafeEntryName(t *testing.T) {
	input := filepath.Join(t.TempDir(), "unsafe.ofd")
	writeTestOFD(t, input, []ofdEntry{
		{"OFD.xml", testOFDXML("Doc_0/Document.xml")},
		{"Doc_0/Document.xml", testDocumentXML},
		{"Doc_0/../../../escape.txt", "escape"},
	})

	if err := Files([]string{input}, &bytes.Buffer{}, Options{}); err == nil {
		t.Fatalf("条目名越出包根目录时应返回错误")
	}
}

func TestMergeRejectsDuplicateEntryName(t *testing.T) {
	input := filepath.Join(t.TempDir(), "duplicate.ofd")
	writeTestOFD(t, input, []ofdEntry{
		{"OFD.xml", testOFDXML("Doc_0/Document.xml")},
		{"Doc_0/Document.xml", testDocumentXML},
		{"Doc_0/Document.xml", testDocumentXML},
	})

	if err := Files([]string{input}, &bytes.Buffer{}, Options{}); err == nil {
		t.Fatalf("重复条目名应返回错误")
	}
}

func TestMergeRejectsUnsafeDocRoot(t *testing.T) {
	input := filepath.Join(t.TempDir(), "docroot.ofd")
	writeTestOFD(t, input, []ofdEntry{
		{"OFD.xml", testOFDXML("../Doc_0/Document.xml")},
		{"Doc_0/Document.xml", testDocumentXML},
	})

	if err := Files([]string{input}, &bytes.Buffer{}, Options{}); err == nil {
		t.Fatalf("DocRoot 越出包根目录时应返回错误")
	}
}

func TestSignatureRewriteEmitsWarning(t *testing.T) {
	var warnings []string
	data := mergeToBytes(t, []string{testdataPath("999.ofd"), testdataPath("999.ofd")}, Options{
		Signatures: SignatureRewrite,
		OnWarning:  func(message string) { warnings = append(warnings, message) },
	})
	if len(warnings) == 0 {
		t.Fatalf("重写签名路径时应产生警告")
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "Doc_1") {
		t.Fatalf("警告应指明被重写的签名文件，实际: %v", warnings)
	}
	if _, err := core.OpenBytes(data); err != nil {
		t.Fatalf("结果不是有效 ZIP: %v", err)
	}
}

func TestOrphanModes(t *testing.T) {
	input := filepath.Join(t.TempDir(), "orphan.ofd")
	writeTestOFD(t, input, []ofdEntry{
		{"OFD.xml", testOFDXML("Doc_0/Document.xml")},
		{"Doc_0/Document.xml", testDocumentXML},
		{"shared.txt", "shared"},
	})

	if err := Files([]string{input}, &bytes.Buffer{}, Options{Orphans: OrphanError}); err == nil {
		t.Fatalf("error 模式应返回错误")
	}

	var buffer bytes.Buffer
	if err := Files([]string{input}, &buffer, Options{Orphans: OrphanIgnore}); err != nil {
		t.Fatalf("ignore 模式失败: %v", err)
	}
	pkg := openMerged(t, buffer.Bytes())
	if pkg.Has("shared.txt") {
		t.Fatalf("ignore 模式不应保留目录外条目")
	}
	if !pkg.Has("Doc_0/Document.xml") {
		t.Fatalf("ignore 模式应保留文档目录内条目")
	}

	buffer.Reset()
	if err := Files([]string{input}, &buffer, Options{Orphans: OrphanPreserve}); err != nil {
		t.Fatalf("preserve 模式失败: %v", err)
	}
	if !openMerged(t, buffer.Bytes()).Has("shared.txt") {
		t.Fatalf("preserve 模式应保留目录外条目")
	}
}

func TestMergeSmokeOverTestdata(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "test", "testdata", "*.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	hello := testdataPath("hello.ofd")
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > 500<<10 {
			continue
		}
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			var buffer bytes.Buffer
			options := Options{Signatures: SignatureDrop, Orphans: OrphanIgnore}
			if err := Files([]string{path, hello}, &buffer, options); err != nil {
				t.Skipf("跳过无法合并的样例: %v", err)
			}
			pkg := openMerged(t, buffer.Bytes())
			root := readEntry(t, pkg, "OFD.xml")
			if count := strings.Count(root, "DocBody"); count < 2 {
				t.Fatalf("合并结果应至少包含 2 个文档体，实际 %d:\n%s", count, root)
			}
		})
	}
}

func TestMergeEnforcesLimits(t *testing.T) {
	input := []string{testdataPath("hello.ofd")}
	cases := []struct {
		name   string
		limits Limits
	}{
		{"max entries", Limits{MaxEntries: 1}},
		{"max entry bytes", Limits{MaxEntryBytes: 8}},
		{"max total bytes", Limits{MaxTotalBytes: 16}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buffer bytes.Buffer
			if err := Files(input, &buffer, Options{Limits: tc.limits}); err == nil {
				t.Fatalf("超过限制时应返回错误")
			}
		})
	}
}

func TestMergeRejectsNegativeLimits(t *testing.T) {
	var buffer bytes.Buffer
	if err := Files([]string{testdataPath("hello.ofd")}, &buffer, Options{Limits: Limits{MaxEntryBytes: -1}}); err == nil {
		t.Fatalf("负数限制应返回错误")
	}
}
