package watermark

import (
	"archive/zip"
	"bytes"
	"testing"

	"github.com/zc310/ofd/internal/spec"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/replace"
)

// signedFixture 是带签名列表的样例。
const signedFixture = "../../test/testdata/999.ofd"

// countSignatureEntries 统计输出包内的签名条目数。
func countSignatureEntries(t *testing.T, data []byte) int {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("输出不是合法 ZIP: %v", err)
	}
	count := 0
	for _, f := range r.File {
		if bytes.Contains([]byte(f.Name), []byte("Signs")) {
			count++
		}
	}
	return count
}

// watermarkWithSignature 按指定签名方式给样例加水印，返回输出字节。
func watermarkWithSignature(t *testing.T, mode creator.SignatureMode) ([]byte, []string) {
	t.Helper()
	var warnings []string
	options := Options{SkipPermissionsCheck: true, SkipReadOnlyCheck: true}
	options.Options = replace.Options{
		Signatures: mode,
		OnWarning:  func(msg string) { warnings = append(warnings, msg) },
	}
	var out bytes.Buffer
	if err := Add(signedFixture, Target{Document: 0, Pages: []int{0}},
		Watermark{Creator: "test", Subtype: "Watermark"}, &out, options); err != nil {
		t.Fatalf("加水印失败: %v", err)
	}
	return out.Bytes(), warnings
}

// TestWatermarkPassesSignatureModeToReplace 确认签名处理方式能穿过内嵌的
// replace.Options 传到 pkg/replace。包级文档依赖这一行为，必须固定下来：
// 一旦透传断掉，零值会退回 SignatureDrop 而调用方毫不知情。
func TestWatermarkPassesSignatureModeToReplace(t *testing.T) {
	kept, _ := watermarkWithSignature(t, creator.SignaturePreserve)
	if n := countSignatureEntries(t, kept); n == 0 {
		t.Error("SignaturePreserve 下签名条目全部丢失，透传可能已断")
	}

	dropped, warnings := watermarkWithSignature(t, creator.SignatureDrop)
	if n := countSignatureEntries(t, dropped); n != 0 {
		t.Errorf("SignatureDrop 下仍残留 %d 个签名条目", n)
	}
	if len(warnings) == 0 {
		t.Error("移除签名条目时没有提示，调用方不会知道签名已丢失")
	}
}

// TestWatermarkZeroSignatureModeDropsEntries 记录零值行为。replace 的零值等价于
// SignatureDrop，因此默认会移除签名——这是既有契约，此处固定以防被无意改动。
func TestWatermarkZeroSignatureModeDropsEntries(t *testing.T) {
	out, _ := watermarkWithSignature(t, "")
	if n := countSignatureEntries(t, out); n != 0 {
		t.Errorf("零值应等价于 SignatureDrop，但仍残留 %d 个签名条目", n)
	}
}

// TestWatermarkPreserveKeepsRootReferenceIntact 确认保留签名时 OFD.xml 的
// Signatures 引用也还在，不会指向不存在的路径。
func TestWatermarkPreserveKeepsRootReferenceIntact(t *testing.T) {
	out, _ := watermarkWithSignature(t, creator.SignaturePreserve)
	r, err := zip.NewReader(bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.File {
		if f.Name != spec.RootDocument {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			t.Fatal(err)
		}
		_ = rc.Close()
		if !bytes.Contains(buf.Bytes(), []byte("Signatures")) {
			t.Errorf("保留了签名条目却丢了 %s 中的引用:\n%s", spec.RootDocument, buf.String())
		}
		return
	}
	t.Fatalf("输出缺少 %s", spec.RootDocument)
}
