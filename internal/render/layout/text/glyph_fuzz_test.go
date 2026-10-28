package text

import (
	"math"
	"testing"

	"github.com/zc310/ofd/internal/models"
)

func FuzzTextGlyphTransformCounts(f *testing.F) {
	f.Add("AB", 1, 2, 1)
	f.Add("中文", 0, 0, 65535)
	f.Add("fi", math.MaxInt, math.MaxInt, -1)
	f.Fuzz(func(t *testing.T, text string, codeCount, glyphCount, gid int) {
		if len(text) > 128 {
			return
		}
		runes := []rune(text)
		if len(runes) > 32 {
			return
		}
		transform := models.CTCGTransform{CodeCount: codeCount, GlyphCount: glyphCount, Glyphs: []int{gid, 1, 2}}
		glyphs := CodeGlyphs(nil, runes, []models.CTCGTransform{transform}, 0)
		if len(glyphs) > len(runes)+3 {
			t.Fatal("字符变换突破有界输入大小")
		}
		for _, glyph := range glyphs {
			if glyph.CodePosition < 0 || glyph.CodePosition >= len(runes) {
				t.Fatalf("字符索引越界：%+v", glyph)
			}
		}
	})
}
