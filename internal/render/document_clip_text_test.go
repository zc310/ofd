package render

import (
	"image"
	"math"
	"path/filepath"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
)

// inkBox 是图元 Boundary 区域内非背景像素的包围盒。
type inkBox struct {
	minX, minY int
	maxX, maxY int
	found      bool
}

// clipsInkBox 统计图元 Boundary 区域内的墨迹包围盒与像素数。Boundary 的 y 自页面
// 顶端向下，与位图行号同向。
func clipsInkBox(t *testing.T, img *image.RGBA, po *models.PathObject, dpi float64) (inkBox, int) {
	t.Helper()
	scale := dpi / 25.4
	b := po.Boundary
	x0, x1 := int(b.X*scale), int((b.X+b.Width)*scale)
	y0, y1 := int(b.Y*scale), int((b.Y+b.Height)*scale)
	box := inkBox{minX: 1 << 30, minY: 1 << 30, maxX: -1, maxY: -1}
	count, total := 0, 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
				continue
			}
			total++
			if r, g, bl, a := img.At(x, y).RGBA(); r != 0xffff || g != 0xffff || bl != 0xffff || a != 0xffff {
				count++
				if x < box.minX {
					box.minX = x
				}
				if y < box.minY {
					box.minY = y
				}
				if x > box.maxX {
					box.maxX = x
				}
				if y > box.maxY {
					box.maxY = y
				}
				box.found = true
			}
		}
	}
	if total == 0 {
		t.Fatalf("图元 %v 的 Boundary 区域为空", po.ID)
	}
	return box, count
}

// clipsTextClipObject 打开 clips.ofd 并渲染第 1 页，返回指定 ID 的裁剪图元、
// 其文字裁剪区、位图与页面高度。
func clipsTextClipObject(t *testing.T, id models.StID) (*models.PathObject, *models.CtText, *image.RGBA, float64) {
	t.Helper()
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "testdata", "clips.ofd"))
	if err != nil {
		t.Skipf("样例不可用: %v", err)
	}
	t.Cleanup(func() { _ = ofd.Close() })
	doc := ofd.Documents[0]
	page := doc.Pages[0]
	box, err := page.PhysicalBox()
	if err != nil {
		t.Fatal(err)
	}
	surface, err := NewDocument(canvas.White, doc).Page(page)
	if err != nil {
		t.Fatal(err)
	}
	const dpi = 150
	img := surface.Rasterize(geom.DPI(dpi))
	if img == nil {
		t.Fatal("页面栅格化为空")
	}
	content := page.Content()
	if content == nil {
		t.Fatal("页面内容为空")
	}
	for _, layer := range content.Layer {
		for _, item := range layer.Items {
			po := item.Path
			if po == nil || po.ID != id || po.Clips == nil {
				continue
			}
			for _, clip := range po.Clips.Clip {
				for _, area := range clip.Area {
					if area.Text != nil {
						return po, area.Text, img, box.Height
					}
				}
			}
			return po, nil, img, box.Height
		}
	}
	t.Fatalf("样例中缺少 ID=%v 的图元", id)
	return nil, nil, nil, 0
}

// TestPathClipAreaWithTextUsesGlyphOutlines 回归：Clip/Clip/Area 允许用 Path 或
// Text 定义裁剪区域，此前 buildClipRegion 只处理 Path，文字区域被静默跳过；
// 区域全部落空时 buildPathClip 返回 nil，裁剪整体失效，被裁图元被完整不裁剪地
// 画出（clips.ofd 第 1 页红色矩形的墨迹占比为 100%）。修复后矩形按字形轮廓填充。
func TestPathClipAreaWithTextUsesGlyphOutlines(t *testing.T) {
	po, text, img, _ := clipsTextClipObject(t, 8)
	if text == nil {
		t.Skip("ID=8 不再使用文字裁剪区，用例失效")
	}
	box, count := clipsInkBox(t, img, po, 150)
	if !box.found {
		t.Fatal("文字裁剪区没有产生任何墨迹，字形轮廓可能无法取得")
	}
	area := po.Boundary.Width * po.Boundary.Height
	ratio := float64(count) / (area * (150.0 / 25.4) * (150.0 / 25.4))
	if ratio > 0.5 {
		t.Errorf("文字裁剪未生效：墨迹占比 %.1f%%，接近不裁剪的 100%%", ratio*100)
	}

	// 路径裁剪必须仍然生效：椭圆裁剪后的墨迹应明显低于其 Boundary 区域。
	pathClipped, _, pathImg, _ := clipsTextClipObject(t, 6)
	pathBox, pathCount := clipsInkBox(t, pathImg, pathClipped, 150)
	if !pathBox.found {
		t.Fatal("路径裁剪图元没有产生墨迹")
	}
	pathArea := pathClipped.Boundary.Width * pathClipped.Boundary.Height
	pathRatio := float64(pathCount) / (pathArea * (150.0 / 25.4) * (150.0 / 25.4))
	if pathRatio > 0.9 {
		t.Errorf("路径裁剪结果异常：墨迹占比 %.1f%%", pathRatio*100)
	}
}

// TestPathClipAreaWithTextIsNotVerticallyMirrored 守住文字裁剪区的字形朝向。
//
// 字形轮廓的 y 轴向下，而 OFD 路径数据 y 轴向上；buildTextClipPath 必须像
// drawTextPath 那样只翻转基线原点、不翻转字形。若误用路径分支的 y 取反，
// 墨迹占比与正立时几乎相同（3.4% 对 3.3%），只有位置会暴露问题：正立时字形
// 主体在基线上方、底部贴着基线，颠倒时几乎整体落到基线下方。
func TestPathClipAreaWithTextIsNotVerticallyMirrored(t *testing.T) {
	po, text, img, _ := clipsTextClipObject(t, 8)
	if text == nil || len(text.TextCode) == 0 {
		t.Skip("ID=8 不再使用文字裁剪区，用例失效")
	}
	box, _ := clipsInkBox(t, img, po, 150)
	if !box.found {
		t.Fatal("文字裁剪区没有产生任何墨迹")
	}
	const dpi = 150
	scale := dpi / 25.4
	code := text.TextCode[0]
	// 基线在页面内自顶端向下的位置：图元 Boundary + 文字 Boundary + TextCode.Y。
	baseline := (po.Boundary.Y + text.Boundary.Y + code.Y) * scale
	em := text.Size * scale
	if em <= 0 {
		t.Fatalf("字号无效: %v", text.Size)
	}

	above := baseline - float64(box.minY) // 字形顶端在基线上方的距离
	below := float64(box.maxY) - baseline // 字形底端在基线下方的距离
	// 汉字字面通常占满 em 盒并坐在基线上：顶端应明显高于基线，底端贴着基线。
	// 阈值取 0.35em，兼顾不同字体的字面高度；颠倒时两项都会越界。
	const limit = 0.35
	if above < limit*em {
		t.Errorf("字形没有位于基线上方（顶端仅高出基线 %.1fpx，em=%.1fpx，阈值 %.1fpx）：字形可能被上下颠倒",
			above, em, limit*em)
	}
	if below > limit*em {
		t.Errorf("字形越过基线 %.1fpx（em=%.1fpx，阈值 %.1fpx）：字形可能被上下颠倒",
			below, em, limit*em)
	}
	if math.Abs(float64(box.maxY-box.minY)-0.93*em) > 0.4*em {
		t.Logf("注意：字形高度 %.1fpx 与预期 0.93em(%.1fpx) 相差较大", float64(box.maxY-box.minY), 0.93*em)
	}
}
