package render

import (
	"fmt"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"golang.org/x/image/font/gofont/goregular"
)

func BenchmarkTextLayoutFaceReuse(b *testing.B) {
	name := b.Name()
	if err := RegisterFallbackFont(goregular.TTF, name, FontRegular); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = UnregisterFallbackFont(name) })
	doc := NewDocument(nil, nil)
	b.Cleanup(doc.Close)
	if err := doc.UseFallbackFont(name); err != nil {
		b.Fatal(err)
	}
	object := models.TextObject{CtText: models.CtText{Size: 4}}
	for i := range 16 {
		object.TextCode = append(object.TextCode, models.TextCode{Value: "Benchmark text", Y: float64(i) * 5})
	}
	for _, reuse := range []bool{false, true} {
		b.Run(fmt.Sprintf("reuse=%v", reuse), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var layouts []TextLayout
				position := 0
				if reuse {
					collectObjectTextLayouts(object, &layouts, doc, nil, &position)
				} else {
					for _, code := range object.TextCode {
						layouts = append(layouts, buildTextLayout(doc, object, code, position))
						position += len([]rune(code.Value))
					}
				}
				if len(layouts) != len(object.TextCode) {
					b.Fatal("文字布局缺失")
				}
			}
		})
	}
}

func BenchmarkSealCacheKey(b *testing.B) {
	for _, size := range []int{64 << 10, 1 << 20} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			data := make([]byte, size)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for b.Loop() {
				_ = sealCacheKey(data, []string{"fallback"})
			}
		})
	}
}
