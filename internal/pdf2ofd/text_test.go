package pdf2ofd

import (
	"testing"
	"time"
)

func TestParsePDFToUnicodeRangeEndingAtMaxTerminates(t *testing.T) {
	// bfrange 的终点为 0xFFFF 时，code 是 uint16，递增会回绕成 0，
	// 循环条件恒真——basicapi.pdf 因此永远转不完，且 SIGTERM 无效。
	// 这里必须能返回，且区间内每个码位都有映射。
	cmap := "begincmap\n1 begincodespacerange <0000> <FFFF> endcodespacerange\n" +
		"1 beginbfrange\n<0020> <FFFF> <0041>\nendbfrange\nendcmap\n"

	type outcome struct {
		mapping map[uint16]string
	}
	done := make(chan outcome, 1)
	go func() { done <- outcome{parsePDFToUnicode([]byte(cmap))} }()

	var mapping map[uint16]string
	select {
	case got := <-done:
		mapping = got.mapping
	case <-time.After(10 * time.Second):
		t.Fatal("parsePDFToUnicode did not terminate: uint16 wraparound in the bfrange loop")
	}

	if got := mapping[0x20]; got != "A" {
		t.Fatalf("code 0x20 = %q, want %q", got, "A")
	}
	if got := mapping[0xFFFF]; got == "" {
		t.Fatal("code 0xFFFF has no mapping")
	}
	// 上界不可超过码位空间，否则畸形 CMap 能把内存撑到 GB 级。
	if len(mapping) > 1<<16 {
		t.Fatalf("expanded %d codes, want <= %d", len(mapping), 1<<16)
	}
}
