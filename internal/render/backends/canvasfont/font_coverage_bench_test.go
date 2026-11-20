package canvasfont

import (
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"golang.org/x/image/font/gofont/goregular"
)

// 重复绘制和文字层度量会对相同文字调用 FaceObject，分别观察短对象与长对象的成本。
func BenchmarkFaceObjectCoverage(b *testing.B) {
	family := canvas.NewFontFamily("coverage-benchmark")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		b.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		value string
	}{
		{"short", "Invoice 123456789"},
		{"long", strings.Repeat("The quick brown fox jumps over the lazy dog. ", 32)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			fonts := NewFonts(nil)
			b.Cleanup(fonts.Close)
			object := textObjectWith(tc.value)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if face, got := fonts.FaceObject(family, object, nil); face == nil || got != family {
					b.Fatal("已覆盖的文字意外更换字体")
				}
			}
		})
	}
}
