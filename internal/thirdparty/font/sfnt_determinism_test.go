package font

import (
	"bytes"
	"testing"
	"time"

	"golang.org/x/image/font/gofont/goregular"
)

// Subsetting must not depend on the wall clock. The subset writer used to stamp
// the head table's modified timestamp with time.Now(), which changed the head
// table checksum and the font's checkSumAdjustment on every call. Any caller
// embedding the font program in an output document then got different bytes for
// the same input, so reproducible output and content-addressed caching were
// both impossible.
func TestSubsetWriteIsDeterministic(t *testing.T) {
	for _, name := range []string{"GoRegular", "resources/EBGaramond12-Regular.otf"} {
		t.Run(name, func(t *testing.T) {
			b := append([]byte(nil), goregular.TTF...)
			if name != "GoRegular" {
				b, _ = readFontFixture(t, name)
			}
			sfnt, err := ParseSFNT(b, 0)
			if err != nil {
				t.Fatalf("无法解析 %s: %v", name, err)
			}
			glyphIDs := []uint16{0, 1, 2, 3}

			var first []byte
			for i := 0; i < 5; i++ {
				// A wall-clock stamp has one-second resolution, so writes that
				// all land in the same second look identical even with the
				// timestamp bug. Space the first two writes more than a second
				// apart so the comparison can actually observe a clock change.
				if i == 1 && name == "GoRegular" {
					time.Sleep(1100 * time.Millisecond)
				}
				subset, err := sfnt.Subset(glyphIDs, SubsetOptions{Tables: KeepPDFTables})
				if err != nil {
					t.Fatalf("第 %d 次子集化失败: %v", i, err)
				}
				program := subset.Write()
				if first == nil {
					first = program
					continue
				}
				if !bytes.Equal(first, program) {
					t.Fatalf("第 %d 次子集化结果与首次不同: %d != %d 字节", i, len(first), len(program))
				}
			}
		})
	}
}

// The head table timestamps must be carried over from the source font:
// subsetting removes unused glyphs, it does not change the font design.
func TestSubsetWritePreservesHeadTimestamps(t *testing.T) {
	b := append([]byte(nil), goregular.TTF...)
	sfnt, err := ParseSFNT(b, 0)
	if err != nil {
		t.Fatalf("无法解析测试字体: %v", err)
	}
	subset, err := sfnt.Subset([]uint16{0, 1, 2}, SubsetOptions{Tables: KeepAllTables})
	if err != nil {
		t.Fatalf("子集化失败: %v", err)
	}
	got, err := ParseSFNT(subset.Write(), 0)
	if err != nil {
		t.Fatalf("无法解析子集字体: %v", err)
	}
	if !got.Head.Created.Equal(sfnt.Head.Created) {
		t.Errorf("created 被改动: %v != %v", got.Head.Created, sfnt.Head.Created)
	}
	if !got.Head.Modified.Equal(sfnt.Head.Modified) {
		t.Errorf("modified 被改动: %v != %v", got.Head.Modified, sfnt.Head.Modified)
	}
}
