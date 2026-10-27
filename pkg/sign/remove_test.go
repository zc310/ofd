package sign

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/container"
	protocol "github.com/zc310/ofd/internal/crypt"
	"github.com/zc310/ofd/pkg/spec"
)

func removalFixture() map[string]string {
	xml := func(root, body string) string {
		return fmt.Sprintf(`<%s xmlns="%s">%s</%s>`, root, spec.Namespace, body, root)
	}
	description := func(value string) string {
		return xml("Signature", `<SignedInfo><References><Reference FileRef="/Work/Document.xml"/></References><Seal><BaseLoc>shared.esl</BaseLoc><ImageLoc>shared.png</ImageLoc></Seal></SignedInfo><SignedValue>`+value+`</SignedValue>`)
	}
	return map[string]string{
		"OFD.xml":                  xml("OFD", `<DocBody><DocInfo><DocID>original</DocID></DocInfo><DocRoot>Work/Document.xml</DocRoot><Signatures>Work/custom-list.xml</Signatures></DocBody>`),
		"Work/Document.xml":        "unchanged document",
		"Work/Pages/page.xml":      "unchanged page",
		"Work/attachment.bin":      "attachment bytes",
		"Work/custom-list.xml":     xml("Signatures", `<Signature ID="s1" BaseLoc="custom/a.xml"/><Signature ID="s2" BaseLoc="custom/b.xml"/>`),
		"Work/custom/a.xml":        description("a.dat"),
		"Work/custom/b.xml":        description("b.dat"),
		"Work/custom/a.dat":        "value a",
		"Work/custom/b.dat":        "value b",
		"Work/custom/shared.esl":   "shared seal",
		"Work/custom/shared.png":   "shared image",
		"Work/custom/ordinary.txt": "unrelated file",
		"Work/Signs/orphan.dat":    "legacy orphan",
	}
}

func removalZIP(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for name, data := range files {
		file, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(file, data); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func removalResult(t *testing.T, data []byte) *container.Package {
	t.Helper()
	pkg, err := container.OpenBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pkg.Close() })
	return pkg
}

func TestRemoveAllKeepsContentPathsAndUnrelatedFiles(t *testing.T) {
	files := removalFixture()
	var output bytes.Buffer
	if err := Remove(t.Context(), bytes.NewReader(removalZIP(t, files)), &output, RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	pkg := removalResult(t, output.Bytes())
	for _, name := range []string{"Work/custom-list.xml", "Work/custom/a.xml", "Work/custom/b.xml", "Work/custom/a.dat", "Work/custom/b.dat", "Work/custom/shared.esl", "Work/custom/shared.png"} {
		if pkg.Has(name) {
			t.Errorf("签名文件未移除: %s", name)
		}
	}
	for _, name := range []string{"Work/Document.xml", "Work/Pages/page.xml", "Work/attachment.bin", "Work/custom/ordinary.txt", "Work/Signs/orphan.dat"} {
		data, err := pkg.Read(name)
		if err != nil || string(data) != files[name] {
			t.Errorf("非签名文件发生变化: %s %q %v", name, data, err)
		}
	}
	root, err := pkg.Read("OFD.xml")
	if err != nil || bytes.Contains(root, []byte("Signatures")) || !bytes.Contains(root, []byte("Work/Document.xml")) {
		t.Fatalf("入口清理错误: %s %v", root, err)
	}
	if results, err := Verify(t.Context(), output.Bytes(), VerificationOptions{}); err != nil || len(results) != 0 {
		t.Fatalf("移除后仍存在签名: %+v %v", results, err)
	}
}

func TestRemoveByIDKeepsSharedPayloadAndUnchangedRoot(t *testing.T) {
	files := removalFixture()
	var output bytes.Buffer
	if err := Remove(t.Context(), removalZIP(t, files), &output, RemoveOptions{IDs: []string{"s2"}}); err != nil {
		t.Fatal(err)
	}
	pkg := removalResult(t, output.Bytes())
	for _, name := range []string{"OFD.xml", "Work/custom/a.xml", "Work/custom/a.dat", "Work/custom/shared.esl", "Work/custom/shared.png"} {
		data, err := pkg.Read(name)
		if err != nil || string(data) != files[name] {
			t.Errorf("保留签名的原字节变化: %s %v", name, err)
		}
	}
	if pkg.Has("Work/custom/b.xml") || pkg.Has("Work/custom/b.dat") {
		t.Fatal("所选签名未删除")
	}
	list, err := pkg.Read("Work/custom-list.xml")
	if err != nil || bytes.Contains(list, []byte(`ID="s2"`)) || !bytes.Contains(list, []byte(`ID="s1"`)) {
		t.Fatalf("签名列表错误: %s %v", list, err)
	}
}

func TestRemoveRejectsBreakingRetainedSignature(t *testing.T) {
	for _, test := range []struct{ name, reference, relative string }{
		{"list", "/Work/custom-list.xml", ""},
		{"description", "/Work/custom/b.xml", ""},
		{"value", "/Work/custom/b.dat", ""},
		{"relative", "/Work/Document.xml", ` Relative="s2"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := removalFixture()
			files["Work/custom/a.xml"] = fmt.Sprintf(`<Signature xmlns="%s"><SignedInfo><References><Reference FileRef="%s"/></References></SignedInfo><SignedValue>a.dat</SignedValue></Signature>`, spec.Namespace, test.reference)
			files["Work/custom-list.xml"] = fmt.Sprintf(`<Signatures xmlns="%s"><Signature ID="s1" BaseLoc="custom/a.xml"%s/><Signature ID="s2" BaseLoc="custom/b.xml"/></Signatures>`, spec.Namespace, test.relative)
			var output bytes.Buffer
			if err := Remove(t.Context(), removalZIP(t, files), &output, RemoveOptions{IDs: []string{"s2"}}); !errors.Is(err, protocol.ErrProtectedContent) || output.Len() != 0 {
				t.Fatalf("未在写出前拒绝破坏保留签名: %v len=%d", err, output.Len())
			}
		})
	}
}

func TestRemoveCleanupHandlesBrokenAndMissingMetadata(t *testing.T) {
	for _, mode := range []string{"broken-list", "missing-list", "broken-description", "missing-description", "valid"} {
		t.Run(mode, func(t *testing.T) {
			files := removalFixture()
			// 损坏 XML 对应的残留只能根据明确的惯用目录清理。
			files["Work/Signatures/old.xml"] = "broken old signature"
			switch mode {
			case "broken-list":
				files["Work/custom-list.xml"] = "broken"
			case "missing-list":
				delete(files, "Work/custom-list.xml")
			case "broken-description":
				files["Work/custom/a.xml"] = "broken"
			case "missing-description":
				delete(files, "Work/custom/a.xml")
			}
			input := removalZIP(t, files)
			var output bytes.Buffer
			if mode != "valid" {
				if err := Remove(t.Context(), input, &output, RemoveOptions{}); err == nil || output.Len() != 0 {
					t.Fatal("默认清理应拒绝损坏元数据")
				}
			}
			if err := Remove(t.Context(), input, &output, RemoveOptions{Cleanup: true}); err != nil {
				t.Fatal(err)
			}
			pkg := removalResult(t, output.Bytes())
			for _, name := range []string{"Work/custom-list.xml", "Work/Signs/orphan.dat", "Work/Signatures/old.xml"} {
				if pkg.Has(name) {
					t.Errorf("残留未清理: %s", name)
				}
			}
			if !pkg.Has("Work/custom/ordinary.txt") {
				t.Fatal("误删非签名目录文件")
			}
		})
	}
}

func TestRemoveValidatesSelectionAndProtectedContainers(t *testing.T) {
	for _, test := range []struct {
		name    string
		options RemoveOptions
		entry   string
	}{
		{name: "unknown", options: RemoveOptions{IDs: []string{"missing"}}},
		{name: "invalid-id", options: RemoveOptions{IDs: []string{"../bad"}}},
		{name: "cleanup-selection", options: RemoveOptions{IDs: []string{"s1"}, Cleanup: true}},
		{name: "encrypted", entry: protocol.EntryPoint},
		{name: "integrity", entry: spec.IntegrityEntryPoint},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := removalFixture()
			if test.entry != "" {
				files[test.entry] = "protected"
			}
			var output bytes.Buffer
			err := Remove(t.Context(), removalZIP(t, files), &output, test.options)
			if err == nil || output.Len() != 0 {
				t.Fatalf("无效操作未拒绝: %v", err)
			}
			if test.entry != "" && !errors.Is(err, protocol.ErrProtectedContent) {
				t.Fatalf("保护错误不可识别: %v", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Remove(ctx, "missing.ofd", &bytes.Buffer{}, RemoveOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误未保留: %v", err)
	}
}

func TestRemoveRealLegacySignatureAndDeterministicOutput(t *testing.T) {
	input := testdataPath("ofdrw/999.ofd")
	if _, err := os.Stat(input); errors.Is(err, os.ErrNotExist) {
		t.Skipf("缺少测试样本 %s", input)
	} else if err != nil {
		t.Fatal(err)
	}
	var first, second bytes.Buffer
	for _, output := range []*bytes.Buffer{&first, &second} {
		if err := Remove(t.Context(), input, output, RemoveOptions{Deterministic: true}); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("确定性输出不一致")
	}
	if results, err := Verify(t.Context(), first.Bytes(), VerificationOptions{}); err != nil || len(results) != 0 {
		t.Fatalf("真实旧签名未移除: %+v %v", results, err)
	}
	original, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	before, after := removalResult(t, original), removalResult(t, first.Bytes())
	for _, item := range before.Entries() {
		if item.IsDir || item.Path == spec.RootDocument || isSignatureEntry(item.Path) {
			continue
		}
		oldData, err := before.Read(item.Path)
		if err != nil {
			t.Fatal(err)
		}
		newData, err := after.Read(item.Path)
		if err != nil || !bytes.Equal(oldData, newData) {
			t.Errorf("真实文档正文或资源变化: %s %v", item.Path, err)
		}
	}
}

func TestRemoveHandlesMultipleBodiesAndUnsignedInput(t *testing.T) {
	files := removalFixture()
	body := `<DocBody><DocRoot>Other/Document.xml</DocRoot><Signatures>Other/list.xml</Signatures></DocBody>`
	files["OFD.xml"] = strings.Replace(files["OFD.xml"], "</OFD>", body+"</OFD>", 1)
	files["Other/Document.xml"] = "second document"
	files["Other/list.xml"] = fmt.Sprintf(`<Signatures xmlns="%s"><Signature ID="s1" BaseLoc="/Work/custom/a.xml"/></Signatures>`, spec.Namespace)
	var output bytes.Buffer
	if err := Remove(t.Context(), removalZIP(t, files), &output, RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	pkg := removalResult(t, output.Bytes())
	if !pkg.Has("Other/Document.xml") || pkg.Has("Other/list.xml") {
		t.Fatal("多文档体清理错误")
	}
	var again bytes.Buffer
	if err := Remove(t.Context(), output.Bytes(), &again, RemoveOptions{}); err != nil {
		t.Fatalf("无签名文档应可重复清理: %v", err)
	}
	if results, err := Verify(t.Context(), again.Bytes(), VerificationOptions{}); err != nil || len(results) != 0 {
		t.Fatalf("重复清理错误: %+v %v", results, err)
	}
}

func TestRemoveRejectsUnsafePathsEvenDuringCleanup(t *testing.T) {
	files := removalFixture()
	files["Work/custom-list.xml"] = fmt.Sprintf(`<Signatures xmlns="%s"><Signature ID="s1" BaseLoc="../../outside.xml"/></Signatures>`, spec.Namespace)
	for _, cleanup := range []bool{false, true} {
		var output bytes.Buffer
		if err := Remove(t.Context(), removalZIP(t, files), &output, RemoveOptions{Cleanup: cleanup}); err == nil || output.Len() != 0 {
			t.Fatalf("未拒绝越界路径: cleanup=%v err=%v", cleanup, err)
		}
	}
}

func TestRemoveCleanupStillEnforcesMetadataBudgets(t *testing.T) {
	files := removalFixture()
	files["Work/custom-list.xml"] = strings.Repeat("x", (4<<20)+1)
	var output bytes.Buffer
	if err := Remove(t.Context(), removalZIP(t, files), &output, RemoveOptions{Cleanup: true}); !errors.Is(err, protocol.ErrLimitExceeded) || output.Len() != 0 {
		t.Fatalf("清理模式绕过元数据预算: %v len=%d", err, output.Len())
	}
}

func TestRemoveRejectsChangingRootProtectedByAnotherBody(t *testing.T) {
	files := removalFixture()
	files["Work/custom/a.xml"] = fmt.Sprintf(`<Signature xmlns="%s"><SignedInfo><References><Reference FileRef="/OFD.xml"/></References></SignedInfo><SignedValue>a.dat</SignedValue></Signature>`, spec.Namespace)
	body := `<DocBody><DocRoot>Other/Document.xml</DocRoot><Signatures>Other/list.xml</Signatures></DocBody>`
	files["OFD.xml"] = strings.Replace(files["OFD.xml"], "</OFD>", body+"</OFD>", 1)
	files["Other/Document.xml"] = "second document"
	files["Other/list.xml"] = fmt.Sprintf(`<Signatures xmlns="%s"><Signature ID="s3" BaseLoc="/Work/custom/b.xml"/></Signatures>`, spec.Namespace)
	var output bytes.Buffer
	if err := Remove(t.Context(), removalZIP(t, files), &output, RemoveOptions{IDs: []string{"s3"}}); !errors.Is(err, protocol.ErrProtectedContent) || output.Len() != 0 {
		t.Fatalf("清理其他文档体时破坏了根入口保护: %v len=%d", err, output.Len())
	}
}
