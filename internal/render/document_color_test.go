package render

import (
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
)

// 同一个颜色空间 ID 在不同 OFD 中指向不同的 Profile，资源适配与颜色缓存
// 都必须保持文档作用域，不能在拆包后丢失 Profile 或串用另一个文档的颜色。
func TestColorResourcesResolveProfilesWithinDocumentScope(t *testing.T) {
	source := &models.CTColor{ColorSpace: 30}
	for _, want := range []color.RGBA{{R: 255, A: 255}, {G: 255, A: 255}} {
		data, err := creator.Marshal(creator.Document{
			ID: "color-resources",
			ColorSpaces: []creator.ColorSpace{{
				ID: 30, Type: "RGB", ProfileData: []byte{want.R, want.G, want.B},
			}},
			Pages: []creator.Page{{}},
		})
		if err != nil {
			t.Fatal(err)
		}
		ofd, err := parser.NewOFD(data)
		if err != nil {
			t.Fatal(err)
		}
		doc := &Document{Document: ofd.Documents[0]}
		t.Cleanup(func() { doc.Close(); ofd.Close() })
		for range 2 {
			got := doc.updateCtColor(source)
			if got == nil || !got.HasValue || got.Value != want {
				t.Fatalf("Profile 颜色=%+v，期望%v", got, want)
			}
		}
		doc.Close()
		if got := doc.updateCtColor(source); got != nil {
			t.Fatal("关闭文档后仍返回颜色缓存结果")
		}
	}
}
