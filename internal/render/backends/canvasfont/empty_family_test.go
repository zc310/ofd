package canvasfont

import (
	"reflect"
	"testing"

	"github.com/zc310/canvas"
	"golang.org/x/image/font/gofont/goregular"
)

func TestFontProbesAndCacheCleanupRejectEmptyFamily(t *testing.T) {
	for _, tc := range []struct {
		name   string
		family *canvas.FontFamily
	}{
		{"nil", nil},
		{"empty", canvas.NewFontFamily("empty")},
		{"zero_value", &canvas.FontFamily{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fonts := NewFonts(nil)
			defer fonts.Close()
			// 清理空族不能误删其他字体的缓存项。
			fonts.pathCache.Add(fontPathKey{value: "kept"}, &canvas.Path{}, 2048)
			fonts.textLineCache.Add(textLineKey{value: "kept"}, &canvas.Text{}, 2048)
			fonts.textWidthCache.Add(textWidthKey{value: "kept"}, 1, maxTextWidthCacheEntries)
			fonts.forgetShapingCache(tc.family, FontRegular)
			if fonts.pathCache.Len() != 1 || fonts.textLineCache.Len() != 1 || fonts.textWidthCache.Len() != 1 {
				t.Fatal("清理不可用字体族误删了其他条目")
			}
			if familyCoversCJK(tc.family, FontRegular) || familyCoversText(tc.family, textObjectWith("ABC"), FontRegular) {
				t.Fatal("不可用字体族不能声明覆盖文字")
			}
		})
	}
}

func TestProbeFamilyFaceKeepsRequestedStyleAndSize(t *testing.T) {
	family := canvas.NewFontFamily("valid-probe")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	for _, style := range []FontStyle{FontRegular, FontBold, FontItalic} {
		got := probeFamilyFace(family, 12, style)
		want := family.Face(12, canvas.Black, canvasStyle(style), canvas.FontNormal)
		if got == nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("探测改变了样式 %v 的字体面", style)
		}
	}
	if !familyCoversText(family, textObjectWith("ABC"), FontRegular) {
		t.Fatal("正常字体应覆盖 ASCII")
	}
}
