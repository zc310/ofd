package pdf2ofd

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

// 重构前通过原始 Adobe-GB1-UCS2 解析器记录全部 16 位 CID 的结果。
// 每项序列化为 UTF-8 字节数（大端 uint32）及 UTF-8 内容；越界项为 U+FFFD。
func TestSharedGB1PreservesEveryPDFMapping(t *testing.T) {
	h := sha256.New()
	for cid := 0; cid <= 65535; cid++ {
		text := pdfPredefinedCIDUnicode("Adobe-GB1", uint16(cid))
		h.Write(binary.BigEndian.AppendUint32(nil, uint32(len(text))))
		h.Write([]byte(text))
	}
	const want = "371b4cf0c50dc00db8f6d0e941ee4ef73755d8f8f23133773680de88ba6ada03"
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		t.Fatalf("共用 GB1 表改变 PDF 文字映射: %s，期望 %s", got, want)
	}
}

func TestSharedGB1DoesNotDuplicateResourceOrLoseVariationSelectors(t *testing.T) {
	data, err := pdfCMapResource("Adobe-GB1-UCS2")
	if err != nil || data != nil {
		t.Fatalf("GB1 原始表不应重复存储: %d bytes, %v", len(data), err)
	}
	for cid, want := range map[uint16]string{580: "（", 22031: "郎\uFE00", 22032: "嗀\uFE00", 22038: "礼\uFE00", 22040: "蘒\uFE00", 22048: "𠂇", 22127: " "} {
		if got := pdfPredefinedCIDUnicode("Adobe-GB1", cid); got != want {
			t.Errorf("CID %d=%q，期望 %q", cid, got, want)
		}
	}
	if _, cached := pdfCIDUnicodeMaps.Load("Adobe-GB1"); cached {
		t.Fatal("共用 GB1 表不应另建一份 PDF 字符串映射缓存")
	}
}
