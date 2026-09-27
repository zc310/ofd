package canvas

import (
	"os"
	"testing"

	"github.com/zc310/ofd/internal/models"
)

// 文字宽度缓存只影响性能，不应改变任何测量结果：同一字体面下缓存命中与
// 未命中必须给出完全相同的宽度。
func TestCachedTextWidthMatchesUncached(t *testing.T) {
	data, err := os.ReadFile("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf")
	if err != nil {
		t.Skipf("DejaVu Sans 不可用: %v", err)
	}
	family, err := loadCachedEmbeddedFont("WidthCache", data, FontRegular, nil)
	if err != nil {
		t.Fatalf("加载嵌入字体失败: %v", err)
	}
	fonts := NewFonts(nil)
	for _, value := range []string{"A", "Hello", "宽度", "A", "宽度"} {
		face := buildTextFace(family, models.TextObject{CtText: models.CtText{Size: 12}}, nil)
		if face == nil {
			t.Fatal("字体面构建失败")
		}
		want := face.TextWidth(value)
		// 第一次填充缓存，第二次应当命中缓存。
		if got := fonts.cachedTextWidth(face, value); got != want {
			t.Fatalf("%q 首次缓存宽度 %v, 未缓存宽度 %v", value, got, want)
		}
		if got := fonts.cachedTextWidth(face, value); got != want {
			t.Fatalf("%q 命中缓存的宽度 %v, 未缓存宽度 %v", value, got, want)
		}
	}
}

// 不同字号、不同文本不能共用同一个缓存条目。
func TestCachedTextWidthSeparatesSizeAndValue(t *testing.T) {
	data, err := os.ReadFile("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf")
	if err != nil {
		t.Skipf("DejaVu Sans 不可用: %v", err)
	}
	family, err := loadCachedEmbeddedFont("WidthCacheSize", data, FontRegular, nil)
	if err != nil {
		t.Fatalf("加载嵌入字体失败: %v", err)
	}
	fonts := NewFonts(nil)
	small := buildTextFace(family, models.TextObject{CtText: models.CtText{Size: 10}}, nil)
	large := buildTextFace(family, models.TextObject{CtText: models.CtText{Size: 40}}, nil)
	if small == nil || large == nil {
		t.Fatal("字体面构建失败")
	}
	wantSmall, wantLarge := small.TextWidth("Hello"), large.TextWidth("Hello")
	if wantSmall == wantLarge {
		t.Skip("该字体下两种字号宽度相同，无法区分")
	}
	if got := fonts.cachedTextWidth(small, "Hello"); got != wantSmall {
		t.Errorf("小字号宽度 %v, 期望 %v", got, wantSmall)
	}
	if got := fonts.cachedTextWidth(large, "Hello"); got != wantLarge {
		t.Errorf("大字号宽度 %v, 期望 %v", got, wantLarge)
	}
	if got := fonts.cachedTextWidth(small, "World"); got != small.TextWidth("World") {
		t.Errorf("不同文本的宽度串味: %v", got)
	}
}
