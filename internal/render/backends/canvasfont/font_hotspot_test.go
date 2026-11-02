package canvasfont

import (
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"golang.org/x/image/font/gofont/goregular"
)

func TestOrdinaryEmbeddedFontRepairReusesRepairer(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	source := &embeddedFontSource{data: goregular.TTF, digest: sha256.Sum256(goregular.TTF)}
	for range 2 {
		if err := loadEmbeddedFont(fonts, canvas.NewFontFamily("ordinary"), source, FontRegular, nil); err != nil {
			t.Fatal(err)
		}
		if len(fonts.embeddedRepairers) != 1 || fonts.embeddedRepairers[source.digest] == nil {
			t.Fatal("普通修复绕过了按字体摘要缓存的修复器")
		}
	}
}

func TestShapingCacheWeightsIncludeLongTextAndGeometry(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	family := canvas.NewFontFamily("weighted")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	face := family.Face(12, canvas.Black)
	value := strings.Repeat("long text ", 100)
	path := fonts.shapedTextPath(face, value)
	fonts.shapedTextLine(face, value)
	fonts.cachedTextWidth(face, value)
	if fonts.pathCache.Weight() < int64(len(value)+len(path.Data())*8) ||
		fonts.textLineCache.Weight() < int64(len(value)*2) || fonts.textWidthCache.Weight() < int64(len(value)) {
		t.Fatal("字体缓存权重没有计入完整文本或字形数据")
	}
	fonts.Close()
	if fonts.pathCache.Weight() != 0 || fonts.textLineCache.Weight() != 0 || fonts.textWidthCache.Weight() != 0 {
		t.Fatal("关闭字体引擎后没有释放加权缓存")
	}
}
