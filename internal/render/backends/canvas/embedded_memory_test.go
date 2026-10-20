package canvas

import (
	"runtime"
	"testing"
	"weak"

	"github.com/zc310/canvas"
	"github.com/zc310/ofd/internal/fontrepair"
	"golang.org/x/image/font/gofont/goregular"
)

func TestEmbeddedMappingReplacementDropsOnlySupersededCaches(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	old, err := loadCachedEmbeddedFont(fonts, "Mapped", goregular.TTF, FontRegular, nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := loadCachedEmbeddedFont(fonts, "Other", goregular.TTF, FontRegular, nil)
	if err != nil {
		t.Fatal(err)
	}
	oldFace, otherFace := old.Face(12, canvas.Black), other.Face(12, canvas.Black)
	oldLock := fonts.RenderLock(old)
	oldGlyph := oldFace.Font.GlyphIndex('A')
	for _, face := range []*canvas.FontFace{oldFace, otherFace} {
		fonts.shapedTextPath(face, "A")
		fonts.shapedTextLine(face, "A")
		fonts.cachedTextWidth(face, "A")
	}
	mapping := []fontrepair.GlyphMapping{{Rune: '\uE000', Glyph: 1}}
	current, err := loadCachedEmbeddedFont(fonts, "Mapped", goregular.TTF, FontRegular, mapping)
	if err != nil {
		t.Fatal(err)
	}
	if len(fonts.embeddedFonts) != 2 || current == old {
		t.Fatal("同一字体的旧映射版本应被替换，其他字体应保留")
	}
	if fonts.pathCache.Len() != 1 || fonts.textLineCache.Len() != 1 || fonts.textWidthCache.Len() != 1 {
		t.Fatal("应只删除旧字体的轮廓、整形和宽度缓存")
	}
	if fonts.RenderLock(old) != oldLock || oldFace.Font.GlyphIndex('A') != oldGlyph {
		t.Fatal("正在使用的旧字体及其渲染锁不应被改写")
	}
	if got := current.Face(12, canvas.Black).Font.GlyphIndex('\uE000'); got != 1 {
		t.Fatalf("新映射的字形 = %d，期望 1", got)
	}
	if hit, err := loadCachedEmbeddedFont(fonts, "Mapped", goregular.TTF, FontRegular, mapping); err != nil || hit != current {
		t.Fatalf("最新映射应继续命中缓存：%p, %v", hit, err)
	}
}

func TestEmbeddedMappingReplacementAllowsPreviousFontCollection(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	// 单独的调用栈结束后，旧字体只能由引擎缓存持有。
	oldFamily, oldFont := cacheRetiredFont(t, fonts)
	if _, err := loadCachedEmbeddedFont(fonts, "Collectible", goregular.TTF, FontRegular,
		[]fontrepair.GlyphMapping{{Rune: '\uE000', Glyph: 1}}); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	if oldFamily.Value() != nil || oldFont.Value() != nil {
		t.Fatal("旧字体仍被内嵌字体表、整形缓存或渲染锁表保留")
	}
	runtime.KeepAlive(fonts)
}

func cacheRetiredFont(t *testing.T, fonts *Fonts) (weak.Pointer[canvas.FontFamily], weak.Pointer[canvas.Font]) {
	t.Helper()
	family, err := loadCachedEmbeddedFont(fonts, "Collectible", goregular.TTF, FontRegular, nil)
	if err != nil {
		t.Fatal(err)
	}
	face := family.Face(12, canvas.Black)
	fonts.RenderLock(family)
	fonts.shapedTextPath(face, "A")
	fonts.shapedTextLine(face, "A")
	fonts.cachedTextWidth(face, "A")
	return weak.Make(family), weak.Make(face.Font)
}

func TestRetiredFontLateShapingDoesNotRefillCaches(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	old := shapeRetiredFont(t, fonts)
	if fonts.pathCache.Len()+fonts.textLineCache.Len()+fonts.textWidthCache.Len() != 0 {
		t.Fatal("旧字体面的迟到整形重新填充了缓存")
	}
	runtime.GC()
	if old.Value() != nil {
		t.Fatal("迟到调用结束后旧字体仍被缓存保留")
	}
	runtime.KeepAlive(fonts)
}

func shapeRetiredFont(t *testing.T, fonts *Fonts) weak.Pointer[canvas.Font] {
	t.Helper()
	family, err := loadCachedEmbeddedFont(fonts, "RetiredLate", goregular.TTF, FontRegular, nil)
	if err != nil {
		t.Fatal(err)
	}
	face := family.Face(12, canvas.Black)
	fonts.shapedTextPath(face, "before")
	fonts.shapedTextLine(face, "before")
	fonts.cachedTextWidth(face, "before")
	if _, err := loadCachedEmbeddedFont(fonts, "RetiredLate", goregular.TTF, FontRegular,
		[]fontrepair.GlyphMapping{{Rune: '\uE000', Glyph: 1}}); err != nil {
		t.Fatal(err)
	}
	if fonts.shapedTextPath(face, "late") == nil || fonts.shapedTextLine(face, "late") == nil || fonts.cachedTextWidth(face, "late") <= 0 {
		t.Fatal("仍持有旧字体的绘制应能完成计算")
	}
	return weak.Make(face.Font)
}
