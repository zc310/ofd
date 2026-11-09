package canvasfont

import (
	"errors"
	"os"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

// intro 的正文已内嵌 PingFang，登记页面字形后不得误用系统字体。
func TestIntroPage5KeepsEmbeddedPingFang(t *testing.T) {
	const input = "../../../../testdata/ofdrw/intro.ofd"
	ofd, err := parser.NewOFD(input)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skipf("缺少样例 %s", input)
		}
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := ofd.Documents[0]
	page, err := doc.GetPage(4)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := page.AcquireLease()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	fonts := NewFonts(doc)
	defer fonts.Close()
	content := lease.Content()
	fonts.RegisterPageGlyphs(doc, page, content)
	family, err := fonts.LoadFont(128)
	if err != nil {
		t.Fatal(err)
	}
	cf, ok := family.(*canvas.FontFamily)
	if !ok || !fonts.HasLoadedEmbeddedFont(128) {
		t.Fatal("字体 128 未加载为内嵌 canvas 字体")
	}
	base := cf.Face(1, canvas.Black)
	checked := 0
	for _, layer := range content.Content.Layer {
		for _, item := range layer.Items {
			if item.Text == nil || item.Text.Font != 128 {
				continue
			}
			checked++
			for _, code := range item.Text.TextCode {
				for _, r := range code.Value {
					if base.Font.GlyphIndex(r) == 0 {
						t.Errorf("对象 %d 的内嵌字体缺少 %c (%U)", item.Text.ID, r, r)
					}
				}
			}
			_, actual := fonts.FaceObject(family, *item.Text, nil)
			if actual != family {
				t.Errorf("对象 %d 错误回退到 %q", item.Text.ID, actual.Name())
			}
		}
	}
	if checked == 0 {
		t.Fatal("没有检查到使用字体 128 的文字对象")
	}
}
