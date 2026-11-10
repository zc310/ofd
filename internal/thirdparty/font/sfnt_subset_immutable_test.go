package font

import (
	"bytes"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

// 多页 SVG 可并行共享同一字体；子集化不得改写源字体的 OS/2 元数据。
func TestSubsetKeepsSharedSourceFontImmutable(t *testing.T) {
	t.Parallel()

	source, err := ParseSFNT(goregular.TTF, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := *source.OS2
	program := source.Write()
	for _, value := range []string{"ABC", "éÁ", "ΩΔ"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			glyphs := []uint16{0}
			for _, r := range value {
				glyphs = append(glyphs, source.GlyphIndex(r))
			}
			subset, err := source.Subset(glyphs, SubsetOptions{Tables: KeepMinTables})
			if err != nil {
				t.Fatal(err)
			}
			if subset.OS2 == source.OS2 || *source.OS2 != want || !bytes.Equal(source.Write(), program) {
				t.Fatal("子集化与源字体共享或修改了 OS/2 元数据")
			}
		})
	}
}
