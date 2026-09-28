package render

import (
	"image"
	"path/filepath"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
)

// clipsTextClipInkRatio 在指定图元的 Boundary 区域内统计墨迹占比。
func clipsTextClipInkRatio(t *testing.T, img *image.RGBA, po *models.PathObject, box models.StBox, dpi float64) float64 {
	t.Helper()
	scale := dpi / 25.4
	b := po.Boundary
	// Boundary 的 y 自页面顶端向下，与位图行号同向。
	x0, x1 := int(b.X*scale), int((b.X+b.Width)*scale)
	y0, y1 := int(b.Y*scale), int((b.Y+b.Height)*scale)
	ink, total := 0, 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
				continue
			}
			total++
			if r, g, bl, a := img.At(x, y).RGBA(); r != 0xffff || g != 0xffff || bl != 0xffff || a != 0xffff {
				ink++
			}
		}
	}
	if total == 0 {
		t.Fatalf("图元 %v 的 Boundary 区域为空", po.ID)
	}
	return float64(ink) / float64(total)
}

// TestPathClipAreaWithTextUsesGlyphOutlines 回归：Clip/Clip/Area 允许用 Path 或
// Text 定义裁剪区域，此前 buildClipRegion 只处理 Path，文字区域被静默跳过；
// 区域全部落空时 buildPathClip 返回 nil，裁剪整体失效，被裁图元被完整不裁剪地
// 画出（clips.ofd 第 1 页的红色矩形墨迹占比为 100%）。修复后矩形按字形轮廓填充。
func TestPathClipAreaWithTextUsesGlyphOutlines(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "test", "testdata", "clips.ofd"))
	if err != nil {
		t.Skipf("样例不可用: %v", err)
	}
	defer ofd.Close()
	doc := ofd.Documents[0]
	page := doc.Pages[0]
	box, err := page.PhysicalBox()
	if err != nil {
		t.Fatal(err)
	}
	const dpi = 150
	surface, err := NewDocument(canvas.White, doc).Page(page)
	if err != nil {
		t.Fatal(err)
	}
	img := surface.Rasterize(geom.DPI(dpi))
	if img == nil {
		t.Fatal("页面栅格化为空")
	}

	// ID=8 是文字裁剪的那个矩形，ID=6 是路径裁剪的椭圆。
	byID := map[models.StID]*models.PathObject{}
	content := page.Content()
	if content == nil {
		t.Fatal("页面内容为空")
	}
	for _, layer := range content.Layer {
		for _, item := range layer.Items {
			if item.Path != nil && item.Path.Clips != nil {
				byID[item.Path.ID] = item.Path
			}
		}
	}
	textClipped, ok := byID[8]
	if !ok {
		t.Skip("样例中缺少 ID=8 的文字裁剪图元")
	}
	if !clipAreaUsesText(textClipped) {
		t.Skip("ID=8 不再使用文字裁剪区，用例失效")
	}

	ratio := clipsTextClipInkRatio(t, img, textClipped, box, dpi)
	// 字形轮廓只覆盖矩形内很小一部分；完全不裁剪时会填满整个区域。
	if ratio <= 0 {
		t.Errorf("文字裁剪区没有产生任何墨迹（占比 %.1f%%），字形轮廓可能无法取得", ratio*100)
	}
	if ratio > 0.5 {
		t.Errorf("文字裁剪未生效：墨迹占比 %.1f%%，接近不裁剪的 100%%", ratio*100)
	}

	// 路径裁剪必须仍然生效：椭圆裁剪后的墨迹应明显低于其 Boundary 区域。
	pathClipped, ok := byID[6]
	if !ok {
		t.Skip("样例中缺少 ID=6 的路径裁剪图元")
	}
	pathRatio := clipsTextClipInkRatio(t, img, pathClipped, box, dpi)
	if pathRatio <= 0 || pathRatio > 0.9 {
		t.Errorf("路径裁剪结果异常：墨迹占比 %.1f%%", pathRatio*100)
	}
}

// clipAreaUsesText 判断图元的某个裁剪区是否由文字定义。
func clipAreaUsesText(po *models.PathObject) bool {
	for _, clip := range po.Clips.Clip {
		for _, area := range clip.Area {
			if area.Text != nil {
				return true
			}
		}
	}
	return false
}
