package merge

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"image"
	"image/png"

	"github.com/klauspost/compress/zip"

	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
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
	}, Options{Signatures: creator.SignatureRewrite})

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
	err := Files([]string{testdataPath("999.ofd"), testdataPath("999.ofd")}, &bytes.Buffer{}, Options{Signatures: creator.SignaturePreserve})
	if err == nil {
		t.Fatalf("preserve 模式下签名绝对路径无法保留时应返回错误")
	}
}

func TestSignatureDropRemovesSignatureFiles(t *testing.T) {
	data := mergeToBytes(t, []string{
		testdataPath("999.ofd"),
		testdataPath("999.ofd"),
	}, Options{Signatures: creator.SignatureDrop})

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
		Signatures: creator.SignatureRewrite,
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
			options := Options{Signatures: creator.SignatureDrop, Orphans: OrphanIgnore}
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

func TestPagesMergesDocuments(t *testing.T) {
	inputs := []Source{{Path: testdataPath("hello.ofd")}, {Path: testdataPath("helloworld.ofd")}}
	var buffer bytes.Buffer
	if err := Pages(inputs, &buffer, PageOptions{}); err != nil {
		t.Fatalf("Pages 失败: %v", err)
	}

	instance, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	report := instance.ValidateReader(context.Background(), bytes.NewReader(buffer.Bytes()), "pages.ofd")
	if report.HasErrors() {
		var out strings.Builder
		_ = validator.RenderText(&out, report)
		t.Fatalf("页面合并结果未通过严格校验:\n%s", out.String())
	}
}

func TestPagesMergesSameFontName(t *testing.T) {
	inputs := []Source{{Path: testdataPath("hello.ofd")}, {Path: testdataPath("hello.ofd")}}
	var buffer bytes.Buffer
	if err := Pages(inputs, &buffer, PageOptions{}); err != nil {
		t.Fatalf("Pages 失败: %v", err)
	}
	instance, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	if report := instance.ValidateReader(context.Background(), bytes.NewReader(buffer.Bytes()), "pages.ofd"); report.HasErrors() {
		var out strings.Builder
		_ = validator.RenderText(&out, report)
		t.Fatalf("同名字体合并结果未通过严格校验:\n%s", out.String())
	}
}

func TestPagesMergesComplexDocuments(t *testing.T) {
	inputs := []Source{{Path: testdataPath("project-showcase.ofd")}, {Path: testdataPath("pattern-fill.ofd")}}
	var buffer bytes.Buffer
	if err := Pages(inputs, &buffer, PageOptions{}); err != nil {
		t.Fatalf("Pages 失败: %v", err)
	}
	instance, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	if report := instance.ValidateReader(context.Background(), bytes.NewReader(buffer.Bytes()), "pages.ofd"); report.HasErrors() {
		var out strings.Builder
		_ = validator.RenderText(&out, report)
		t.Fatalf("复杂文档页面合并未通过严格校验:\n%s", out.String())
	}
}

func TestPagesFlattensMultiDocumentBody(t *testing.T) {
	multi := mergeToBytes(t, []string{testdataPath("hello.ofd"), testdataPath("helloworld.ofd")}, Options{})
	var buffer bytes.Buffer
	if err := Pages([]Source{{Name: "multi", Data: multi}}, &buffer, PageOptions{}); err != nil {
		t.Fatalf("Pages 失败: %v", err)
	}
	instance, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	if report := instance.ValidateReader(context.Background(), bytes.NewReader(buffer.Bytes()), "pages.ofd"); report.HasErrors() {
		var out strings.Builder
		_ = validator.RenderText(&out, report)
		t.Fatalf("展平多文档体未通过严格校验:\n%s", out.String())
	}
}

func TestParsePages(t *testing.T) {
	got, err := ParsePages("1,3-5,2")
	if err != nil {
		t.Fatal(err)
	}
	want := []int{1, 3, 4, 5, 2}
	if len(got) != len(want) {
		t.Fatalf("ParsePages = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("ParsePages = %v, want %v", got, want)
		}
	}
	for _, spec := range []string{"0", "3-1", "a", "1,,2"} {
		if _, err := ParsePages(spec); err == nil {
			t.Fatalf("ParsePages(%q) 应返回错误", spec)
		}
	}
}

func TestPagesSelectsAndReorders(t *testing.T) {
	inputs := []Source{{Path: testdataPath("hello.ofd")}, {Path: testdataPath("helloworld.ofd")}}
	var buffer bytes.Buffer
	if err := Pages(inputs, &buffer, PageOptions{Pages: []int{2, 1}}); err != nil {
		t.Fatalf("Pages 失败: %v", err)
	}
	instance, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	if report := instance.ValidateReader(context.Background(), bytes.NewReader(buffer.Bytes()), "pages.ofd"); report.HasErrors() {
		var out strings.Builder
		_ = validator.RenderText(&out, report)
		t.Fatalf("选页结果未通过严格校验:\n%s", out.String())
	}
	ofd, err := parser.NewOFD(buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ofd.Close() }()
	if len(ofd.Documents) != 1 || len(ofd.Documents[0].Pages) != 2 {
		t.Fatalf("期望单文档 2 页，实际 %d 个文档体", len(ofd.Documents))
	}
}

func TestPagesRejectsOutOfRange(t *testing.T) {
	inputs := []Source{{Path: testdataPath("hello.ofd")}}
	var buffer bytes.Buffer
	if err := Pages(inputs, &buffer, PageOptions{Pages: []int{2}}); err == nil {
		t.Fatalf("越界页码应返回错误")
	}
}

func smallDocument(t *testing.T, id string) []byte {
	t.Helper()
	data, err := creator.Marshal(creator.Document{
		ID:       id,
		PageSize: creator.PageSize{Width: 100, Height: 100},
		Pages: []creator.Page{{Items: []creator.Item{
			creator.Path{X: 1, Y: 1, Width: 10, Height: 10, Data: "M 0 0 L 10 0 L 10 10 C", Stroke: true, LineWidth: 1},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPagesEnforcesLimits(t *testing.T) {
	first := smallDocument(t, "limit-a")
	second := smallDocument(t, "limit-b")
	inputs := []Source{{Data: first}, {Data: second}}
	cases := []struct {
		name   string
		limits PageLimits
	}{
		{"max input", PageLimits{MaxInputBytes: 8}},
		{"max total", PageLimits{MaxTotalBytes: int64(len(first)) + 1}},
		{"max pages", PageLimits{MaxPages: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buffer bytes.Buffer
			if err := Pages(inputs, &buffer, PageOptions{Limits: tc.limits}); err == nil {
				t.Fatalf("超过限制时应返回错误")
			}
		})
	}
}

func TestPagesRejectsNegativeLimits(t *testing.T) {
	var buffer bytes.Buffer
	if err := Pages([]Source{{Data: smallDocument(t, "limit-neg")}}, &buffer, PageOptions{Limits: PageLimits{MaxPages: -1}}); err == nil {
		t.Fatalf("负数限制应返回错误")
	}
}

func pngData(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func documentWithMedia(t *testing.T, id, mediaName string, mediaData []byte) []byte {
	t.Helper()
	data, err := creator.Marshal(creator.Document{
		ID:       id,
		PageSize: creator.PageSize{Width: 100, Height: 100},
		Media:    []creator.Media{{ID: 100, Type: "Image", Format: "PNG", Name: mediaName, Data: mediaData}},
		Pages: []creator.Page{{Items: []creator.Item{
			creator.Image{X: 1, Y: 1, Width: 10, Height: 10, ResourceID: 100},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPagesRemapsMediaConflicts(t *testing.T) {
	image := pngData(t)
	inputs := []Source{
		{Data: documentWithMedia(t, "media-a", "image.png", image)},
		{Data: documentWithMedia(t, "media-b", "image.png", image)},
	}
	var buffer bytes.Buffer
	if err := Pages(inputs, &buffer, PageOptions{}); err != nil {
		t.Fatalf("同名/同 ID 媒体合并失败: %v", err)
	}
	instance, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	if report := instance.ValidateReader(context.Background(), bytes.NewReader(buffer.Bytes()), "pages.ofd"); report.HasErrors() {
		var out strings.Builder
		_ = validator.RenderText(&out, report)
		t.Fatalf("媒体合并结果未通过严格校验:\n%s", out.String())
	}
}

func documentWithAttachment(t *testing.T, id string) []byte {
	t.Helper()
	data, err := creator.Marshal(creator.Document{
		ID:          id,
		PageSize:    creator.PageSize{Width: 100, Height: 100},
		Attachments: []creator.Attachment{{ID: "att-1", Name: "附件", FileName: "att-1.txt", Data: []byte("hello")}},
		Pages: []creator.Page{{Items: []creator.Item{
			creator.Path{X: 1, Y: 1, Width: 10, Height: 10, Data: "M 0 0 L 10 0 L 10 10 C", Stroke: true, LineWidth: 1},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPagesRemapsAttachmentConflicts(t *testing.T) {
	inputs := []Source{
		{Data: documentWithAttachment(t, "att-a")},
		{Data: documentWithAttachment(t, "att-b")},
	}
	var buffer bytes.Buffer
	if err := Pages(inputs, &buffer, PageOptions{}); err != nil {
		t.Fatalf("同名附件合并失败: %v", err)
	}
}

func TestParsePageSelection(t *testing.T) {
	selectors, err := ParsePageSelection("1,3-5")
	if err != nil || len(selectors) != 1 || selectors[0].Source != 0 {
		t.Fatalf("全局选择解析错误: %+v, err=%v", selectors, err)
	}
	if want := []int{1, 3, 4, 5}; !equalInts(selectors[0].Pages, want) {
		t.Fatalf("全局页码 = %v, want %v", selectors[0].Pages, want)
	}

	selectors, err = ParsePageSelection("s1:1,3-5;s2:2")
	if err != nil || len(selectors) != 2 {
		t.Fatalf("按源选择解析错误: %+v, err=%v", selectors, err)
	}
	if selectors[0].Source != 1 || !equalInts(selectors[0].Pages, []int{1, 3, 4, 5}) {
		t.Fatalf("s1 = %+v", selectors[0])
	}
	if selectors[1].Source != 2 || !equalInts(selectors[1].Pages, []int{2}) {
		t.Fatalf("s2 = %+v", selectors[1])
	}

	selectors, err = ParsePageSelection("s2")
	if err != nil || len(selectors) != 1 || selectors[0].Source != 2 || len(selectors[0].Pages) != 0 {
		t.Fatalf("整源选择解析错误: %+v, err=%v", selectors, err)
	}

	for _, spec := range []string{"s0:1", "s1:", "s1:0", "s1:3-1", "s1:1;;s2:2"} {
		if _, err := ParsePageSelection(spec); err == nil {
			t.Fatalf("ParsePageSelection(%q) 应返回错误", spec)
		}
	}
}

func equalInts(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range want {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func TestPagesSelectsBySource(t *testing.T) {
	// 只取第 2 个源的第 1 页。
	inputs := []Source{{Path: testdataPath("hello.ofd")}, {Path: testdataPath("helloworld.ofd")}}
	var buffer bytes.Buffer
	if err := Pages(inputs, &buffer, PageOptions{Selectors: []PageSelector{{Source: 2, Pages: []int{1}}}}); err != nil {
		t.Fatalf("Pages 失败: %v", err)
	}
	ofd, err := parser.NewOFD(buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ofd.Close() }()
	if len(ofd.Documents) != 1 || len(ofd.Documents[0].Pages) != 1 {
		t.Fatalf("期望单文档 1 页，实际文档体 %d", len(ofd.Documents))
	}
}

func TestPagesRejectsInvalidSource(t *testing.T) {
	inputs := []Source{{Path: testdataPath("hello.ofd")}}
	var buffer bytes.Buffer
	if err := Pages(inputs, &buffer, PageOptions{Selectors: []PageSelector{{Source: 2}}}); err == nil {
		t.Fatalf("来源越界应返回错误")
	}
}

func TestPagesOverridesMetadata(t *testing.T) {
	inputs := []Source{{Path: testdataPath("hello.ofd")}}
	var buffer bytes.Buffer
	if err := Pages(inputs, &buffer, PageOptions{ID: "custom-id", Title: "自定义标题", Author: "作者"}); err != nil {
		t.Fatalf("Pages 失败: %v", err)
	}
	ofd, err := parser.NewOFD(buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ofd.Close() }()
	if len(ofd.DocBodies) != 1 {
		t.Fatalf("期望 1 个文档体，实际 %d", len(ofd.DocBodies))
	}
	info := ofd.DocBodies[0].DocInfo
	if info.DocID != "custom-id" {
		t.Fatalf("DocID = %q", info.DocID)
	}
	if info.Title == nil || *info.Title != "自定义标题" {
		t.Fatalf("Title = %v", info.Title)
	}
	if info.Author == nil || *info.Author != "作者" {
		t.Fatalf("Author = %v", info.Author)
	}
}

func documentWithNavigation(t *testing.T, id string) []byte {
	t.Helper()
	path := creator.Path{X: 1, Y: 1, Width: 10, Height: 10, Data: "M 0 0 L 10 0 L 10 10 C", Stroke: true, LineWidth: 1}
	data, err := creator.Marshal(creator.Document{
		ID:       id,
		PageSize: creator.PageSize{Width: 100, Height: 100},
		Pages: []creator.Page{
			{Items: []creator.Item{path}, Actions: []creator.Action{{Goto: &creator.GotoAction{Page: 1}}}},
			{Items: []creator.Item{path}},
		},
		Bookmarks: []creator.Bookmark{{Name: "b-" + id, Goto: creator.GotoAction{Page: 1}}},
		Outlines:  []creator.Outline{{Title: "o-" + id, Actions: []creator.Action{{Goto: &creator.GotoAction{Page: 0}}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPagesRetainsNavigation(t *testing.T) {
	inputs := []Source{
		{Data: documentWithNavigation(t, "nav-a")},
		{Data: documentWithNavigation(t, "nav-b")},
	}
	var buffer bytes.Buffer
	// 先取第 2 个源，再取第 1 个源，页序整体重排。
	if err := Pages(inputs, &buffer, PageOptions{Selectors: []PageSelector{{Source: 2}, {Source: 1}}}); err != nil {
		t.Fatalf("Pages 失败: %v", err)
	}
	instance, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	if report := instance.ValidateReader(context.Background(), bytes.NewReader(buffer.Bytes()), "pages.ofd"); report.HasErrors() {
		var out strings.Builder
		_ = validator.RenderText(&out, report)
		t.Fatalf("导航保留结果未通过严格校验:\n%s", out.String())
	}
	pkg := openMerged(t, buffer.Bytes())
	root := readEntry(t, pkg, "Doc_0/Document.xml")
	if count := strings.Count(root, "<Bookmark "); count != 2 {
		t.Fatalf("期望保留 2 个书签，实际 %d:\n%s", count, root)
	}
	if count := strings.Count(root, "<OutlineElem"); count != 2 {
		t.Fatalf("期望保留 2 个大纲项，实际 %d", count)
	}
}

func TestPagesDropsGotoToUnselectedPage(t *testing.T) {
	inputs := []Source{{Data: documentWithNavigation(t, "nav-c")}}
	var buffer bytes.Buffer
	// 只保留第 1 页；指向第 2 页的书签与页面动作应被丢弃。
	if err := Pages(inputs, &buffer, PageOptions{Pages: []int{1}}); err != nil {
		t.Fatalf("Pages 失败: %v", err)
	}
	pkg := openMerged(t, buffer.Bytes())
	root := readEntry(t, pkg, "Doc_0/Document.xml")
	if count := strings.Count(root, "<Bookmark "); count != 0 {
		t.Fatalf("目标页未保留的书签应被丢弃，实际 %d", count)
	}
	if !strings.Contains(root, "<OutlineElem") {
		t.Fatalf("大纲项应保留（仅动作被丢弃）:\n%s", root)
	}
}

func documentWithAnnotation(t *testing.T, id string) []byte {
	t.Helper()
	path := creator.Path{X: 1, Y: 1, Width: 10, Height: 10, Data: "M 0 0 L 10 0 L 10 10 C", Stroke: true, LineWidth: 1}
	data, err := creator.Marshal(creator.Document{
		ID:       id,
		PageSize: creator.PageSize{Width: 100, Height: 100},
		Pages:    []creator.Page{{Items: []creator.Item{path}}, {Items: []creator.Item{path}}},
		Annotations: []creator.AnnotationPage{{Page: 1, Items: []creator.Annotation{{
			ID:          1,
			Type:        "Highlight",
			Creator:     "tester",
			LastModDate: time.Now(),
			Items:       []creator.Item{path},
		}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPagesRetainsAnnotations(t *testing.T) {
	inputs := []Source{
		{Data: documentWithAnnotation(t, "an-a")},
		{Data: documentWithAnnotation(t, "an-b")},
	}
	var buffer bytes.Buffer
	if err := Pages(inputs, &buffer, PageOptions{Selectors: []PageSelector{{Source: 2}, {Source: 1}}}); err != nil {
		t.Fatalf("Pages 失败: %v", err)
	}
	instance, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	if report := instance.ValidateReader(context.Background(), bytes.NewReader(buffer.Bytes()), "pages.ofd"); report.HasErrors() {
		var out strings.Builder
		_ = validator.RenderText(&out, report)
		t.Fatalf("注解保留结果未通过严格校验:\n%s", out.String())
	}
	pkg := openMerged(t, buffer.Bytes())
	root := readEntry(t, pkg, "Doc_0/Annotations.xml")
	if count := strings.Count(root, "<Page "); count != 2 {
		t.Fatalf("期望 2 个带注解的页面，实际 %d:\n%s", count, root)
	}
}

func TestPagesDoesNotDuplicateAnnotations(t *testing.T) {
	// 同一页被重复选取时，注解只应保留一份，且落在最终页索引上。
	data := documentWithAnnotation(t, "dup")
	var buffer bytes.Buffer
	if err := Pages([]Source{{Data: data}}, &buffer, PageOptions{Pages: []int{2, 2}}); err != nil {
		t.Fatalf("Pages 失败: %v", err)
	}
	pkg := openMerged(t, buffer.Bytes())
	root := readEntry(t, pkg, "Doc_0/Annotations.xml")
	if count := strings.Count(root, "<Page "); count != 1 {
		t.Fatalf("期望 1 个带注解的页面，实际 %d:\n%s", count, root)
	}
}

func TestPagesConcurrentMatchesSerial(t *testing.T) {
	paths := []string{testdataPath("project-showcase.ofd"), testdataPath("pattern-fill.ofd"), testdataPath("hello.ofd")}
	sources := make([]Source, 0, len(paths))
	for _, path := range paths {
		sources = append(sources, Source{Path: path})
	}
	var serial, concurrent bytes.Buffer
	if err := Pages(sources, &serial, PageOptions{Deterministic: true, Concurrency: 1}); err != nil {
		t.Fatalf("串行 Pages 失败: %v", err)
	}
	if err := Pages(sources, &concurrent, PageOptions{Deterministic: true, Concurrency: 3}); err != nil {
		t.Fatalf("并发 Pages 失败: %v", err)
	}
	if !bytes.Equal(serial.Bytes(), concurrent.Bytes()) {
		t.Fatalf("并发与串行结果不一致")
	}
}

func TestPagesRejectsNegativeConcurrency(t *testing.T) {
	var buffer bytes.Buffer
	if err := Pages([]Source{{Path: testdataPath("hello.ofd")}}, &buffer, PageOptions{Concurrency: -1}); err == nil {
		t.Fatalf("负数并发数应返回错误")
	}
}

func collectSignatureEvents(t *testing.T, run func(onSignature func(SignatureEvent)) error) []SignatureEvent {
	t.Helper()
	var events []SignatureEvent
	if err := run(func(event SignatureEvent) { events = append(events, event) }); err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	return events
}

func signatureActions(events []SignatureEvent) map[SignatureAction]int {
	counts := make(map[SignatureAction]int)
	for _, event := range events {
		counts[event.Action]++
	}
	return counts
}

func TestMergeSignatureEvents(t *testing.T) {
	preserved := collectSignatureEvents(t, func(on func(SignatureEvent)) error {
		var buffer bytes.Buffer
		return Files([]string{testdataPath("999.ofd")}, &buffer, Options{Signatures: creator.SignaturePreserve, OnSignature: on})
	})
	if signatureActions(preserved)[SignaturePreserved] != 1 {
		t.Fatalf("preserve 事件 = %+v", preserved)
	}

	rewritten := collectSignatureEvents(t, func(on func(SignatureEvent)) error {
		var buffer bytes.Buffer
		return Files([]string{testdataPath("999.ofd"), testdataPath("999.ofd")}, &buffer, Options{Signatures: creator.SignatureRewrite, OnSignature: on})
	})
	if signatureActions(rewritten)[SignatureRewritten] != 1 {
		t.Fatalf("rewrite 事件 = %+v", rewritten)
	}

	dropped := collectSignatureEvents(t, func(on func(SignatureEvent)) error {
		var buffer bytes.Buffer
		return Files([]string{testdataPath("999.ofd"), testdataPath("999.ofd")}, &buffer, Options{Signatures: creator.SignatureDrop, OnSignature: on})
	})
	if signatureActions(dropped)[SignatureDropped] != 2 {
		t.Fatalf("drop 事件 = %+v", dropped)
	}
}

func TestVerifySignatures(t *testing.T) {
	statuses, err := VerifySignatures(testdataPath("999.ofd"))
	if err != nil {
		t.Fatalf("VerifySignatures 失败: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("期望 1 个签名，实际 %d", len(statuses))
	}
	if !statuses[0].DigestValid {
		t.Fatalf("原始文件摘要应有效: %+v", statuses[0])
	}

	var buffer bytes.Buffer
	if err := Files([]string{testdataPath("999.ofd"), testdataPath("999.ofd")}, &buffer, Options{Signatures: creator.SignatureRewrite}); err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	merged, err := VerifySignatures(buffer.Bytes())
	if err != nil {
		t.Fatalf("VerifySignatures 失败: %v", err)
	}
	if len(merged) != 2 {
		t.Fatalf("期望 2 个签名，实际 %d", len(merged))
	}
}

func TestPagesSignatureEvents(t *testing.T) {
	var events []SignatureEvent
	var buffer bytes.Buffer
	err := Pages([]Source{{Path: testdataPath("999.ofd")}}, &buffer, PageOptions{
		Pages:       []int{1},
		OnSignature: func(event SignatureEvent) { events = append(events, event) },
	})
	if err != nil {
		t.Fatalf("Pages 失败: %v", err)
	}
	if signatureActions(events)[SignatureDropped] != 1 {
		t.Fatalf("模型级合并应丢弃并汇总签名，实际 %+v", events)
	}
}
