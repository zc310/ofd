package replace

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/spec"
)

// zipEntryNames 列出包内全部条目名。
func zipEntryNames(t *testing.T, data []byte) []string {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("输出不是合法 ZIP: %v", err)
	}
	var out []string
	for _, f := range r.File {
		out = append(out, f.Name)
	}
	return out
}

// zipEntry 读出包内指定条目。
func zipEntry(t *testing.T, data []byte, name string) string {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("输出不是合法 ZIP: %v", err)
	}
	for _, f := range r.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", name, err)
		}
		defer func() { _ = rc.Close() }()
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			t.Fatalf("读取 %s 失败: %v", name, err)
		}
		return buf.String()
	}
	t.Fatalf("输出内找不到 %s", name)
	return ""
}

// signedFixture 是带签名列表的样例。
const signedFixture = "../../test/testdata/zsbk.ofd"

// runOnSigned 对签名样例执行一次条目替换。
func runOnSigned(t *testing.T, ops []Operation, options Options) ([]byte, []string) {
	t.Helper()
	pkg, err := core.OpenFile(signedFixture)
	if err != nil {
		t.Fatalf("打开样例失败: %v", err)
	}
	defer func() { _ = pkg.Close() }()
	doc, err := pkg.Read("Doc_0/Document.xml")
	if err != nil {
		t.Fatalf("读取 Document.xml 失败: %v", err)
	}
	ops = append([]Operation{{Kind: OpSet, Name: "Doc_0/Document.xml", Data: doc}}, ops...)
	var out bytes.Buffer
	if err := Files(pkg, ops, &out, options); err != nil {
		t.Fatalf("替换失败: %v", err)
	}
	return out.Bytes(), nil
}

// TestDropRemovesSignaturesWithoutDanglingReference 确认 SignatureDrop 产出的包
// 里签名条目与 OFD.xml 中的签名引用同时消失。此前只删条目、留下
// DocBody/Signatures 引用，输出指向已删除的路径，不再是合法 OFD。
func TestDropRemovesSignaturesWithoutDanglingReference(t *testing.T) {
	out, _ := runOnSigned(t, nil, Options{Signatures: creator.SignatureDrop})

	for _, name := range zipEntryNames(t, out) {
		if strings.Contains(name, "Signs") {
			t.Errorf("SignatureDrop 后仍残留签名条目 %s", name)
		}
	}
	root := zipEntry(t, out, "OFD.xml")
	if strings.Contains(root, "Signatures") {
		t.Errorf("OFD.xml 仍引用已删除的签名列表，输出不是合法 OFD:\n%s", root)
	}
}

// TestDropWarnsAboutRemovedSignatures 确认移除签名条目时会提示，不再静默。
func TestDropWarnsAboutRemovedSignatures(t *testing.T) {
	pkg, err := core.OpenFile(signedFixture)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()
	doc, _ := pkg.Read("Doc_0/Document.xml")
	var warnings []string
	var out bytes.Buffer
	err = Files(pkg, []Operation{{Kind: OpSet, Name: "Doc_0/Document.xml", Data: doc}}, &out, Options{
		OnWarning: func(msg string) { warnings = append(warnings, msg) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) == 0 {
		t.Fatal("移除签名条目时没有任何提示，用户不会知道签名已丢失")
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "签名条目") {
		t.Errorf("提示未说明签名条目被移除:\n%s", joined)
	}
}

// TestSetOnRootDocumentIsHonored 确认对 OFD.xml 的显式 Set 生效。此前主循环跳过
// OFD.xml 而单独写出原始字节，导致 Set 被静默忽略、用户拿到原文件。
func TestSetOnRootDocumentIsHonored(t *testing.T) {
	custom := []byte(`<?xml version="1.0" encoding="UTF-8"?>` +
		`<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="ZZZ-MARKER">` +
		`<DocBody><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`)
	out, _ := runOnSigned(t, []Operation{{Kind: OpSet, Name: "OFD.xml", Data: custom}},
		Options{Signatures: creator.SignaturePreserve})
	if got := zipEntry(t, out, "OFD.xml"); !strings.Contains(got, "ZZZ-MARKER") {
		t.Errorf("对 OFD.xml 的 Set 被忽略，输出仍是原始内容:\n%s", got)
	}
}

// TestSetOnRootDocumentIsStrippedUnderDrop 确认显式替换 OFD.xml 时，
// SignatureDrop 仍会摘掉签名引用——否则用户自己写的 OFD.xml 里的引用会悬空。
func TestSetOnRootDocumentIsStrippedUnderDrop(t *testing.T) {
	custom := []byte(`<?xml version="1.0" encoding="UTF-8"?>` +
		`<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0">` +
		`<DocBody><DocRoot>Doc_0/Document.xml</DocRoot>` +
		`<Signatures>Doc_0/Signs/Signatures.xml</Signatures></DocBody></OFD>`)
	out, _ := runOnSigned(t, []Operation{{Kind: OpSet, Name: "OFD.xml", Data: custom}},
		Options{Signatures: creator.SignatureDrop})
	if got := zipEntry(t, out, "OFD.xml"); strings.Contains(got, "Signatures") {
		t.Errorf("SignatureDrop 未摘掉用户写入的签名引用:\n%s", got)
	}
}

// TestDeleteRootDocumentRejected 确认拒绝删除 OFD.xml。它是包的入口，删除后
// 输出不是合法 OFD，而此前删除会被静默忽略、OFD.xml 照旧写出。
func TestDeleteRootDocumentRejected(t *testing.T) {
	pkg, err := core.OpenFile(signedFixture)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()
	var out bytes.Buffer
	err = Files(pkg, []Operation{{Kind: OpDelete, Name: "OFD.xml"}}, &out, Options{})
	if err == nil {
		t.Fatal("删除 OFD.xml 应报错")
	}
	if !strings.Contains(err.Error(), spec.RootDocument) {
		t.Errorf("错误信息未指明是包入口:\n%v", err)
	}
}

// TestPreserveKeepsSignatureEntriesAndRootReference 确认 preserve 语义完整：
// 签名条目与 OFD.xml 中的引用都在。
func TestPreserveKeepsSignatureEntriesAndRootReference(t *testing.T) {
	out, _ := runOnSigned(t, nil, Options{Signatures: creator.SignaturePreserve})
	found := false
	for _, name := range zipEntryNames(t, out) {
		if strings.Contains(name, "Signs") {
			found = true
			break
		}
	}
	if !found {
		t.Error("preserve 后签名条目全部丢失")
	}
	if root := zipEntry(t, out, "OFD.xml"); !strings.Contains(root, "Signatures") {
		t.Errorf("preserve 后 OFD.xml 丢失签名引用:\n%s", root)
	}
}

// TestZeroValueStillDropsAndDocuments 确认零值行为与包级文档一致：仍是 drop。
// 字段注释此前写「空值跳过签名处理」，与包级说明和实现都不符。
func TestZeroValueStillDropsAndDocuments(t *testing.T) {
	out, _ := runOnSigned(t, nil, Options{})
	for _, name := range zipEntryNames(t, out) {
		if strings.Contains(name, "Signs") {
			t.Errorf("零值应等价于 SignatureDrop，但仍残留签名条目 %s", name)
		}
	}
}
