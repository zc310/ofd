package render

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

// 字体性能优化不得让 ano 首页的两个内嵌中文文字对象丢失。
func TestAnoEmbeddedTextObjects92And97RemainVisible(t *testing.T) {
	const input = "../../testdata/ofdrw/ano.ofd"
	ofd, err := parser.NewOFD(input)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skipf("缺少样例 %s", input)
		}
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := NewDocument(canvas.Transparent, ofd.Documents[0], 0)
	defer doc.Close()
	page := doc.Pages[0]
	lease, err := page.AcquireLease()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	content := lease.Content()
	box, err := page.PhysicalBox()
	if err != nil {
		t.Fatal(err)
	}
	doc.fonts.RegisterPageGlyphs(doc.Document, page, content)
	found := 0
	for _, layer := range content.Content.Layer {
		for _, item := range layer.Items {
			if item.Text == nil || item.Text.ID != 92 && item.Text.ID != 97 {
				continue
			}
			found++
			t.Run(fmt.Sprint(item.Text.ID), func(t *testing.T) {
				scene := canvas.New(box.Width, box.Height)
				doc.Text(newCanvasBackend(canvas.NewContext(scene)), *item.Text, nil, box)
				if !doc.fonts.HasLoadedEmbeddedFont(item.Text.Font) {
					t.Fatal("文字对象未使用可用的内嵌字体")
				}
				img := rasterize(scene, canvas.DPI(144), canvas.DefaultColorSpace)
				visible := 0
				for i := 3; i < len(img.Pix); i += 4 {
					if img.Pix[i] > 32 {
						visible++
					}
				}
				if visible < 100 {
					t.Fatalf("内嵌文字只有 %d 个可见像素", visible)
				}
			})
		}
	}
	if found != 2 {
		t.Fatalf("应找到两个受保护文字对象，实际 %d 个", found)
	}
}
