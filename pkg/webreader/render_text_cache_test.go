package webreader

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// redInkTopThird 统计位图顶部三分之一内的红色像素数。sample0.ofd 第一页
// 顶部有一条红色大字标题。
func redInkTopThird(t *testing.T, data []byte) int {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("解码 PNG 失败: %v", err)
	}
	b := img.Bounds()
	n := 0
	for y := b.Min.Y; y < b.Min.Y+b.Dy()/3; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r>>8 > 180 && g>>8 < 90 && bl>>8 < 90 {
				n++
			}
		}
	}
	return n
}

// renderSequence 在同一个 Reader 上按顺序渲染各页，返回最后一页的位图。
func renderSequence(t *testing.T, reader *Reader, pages []int) []byte {
	t.Helper()
	var last []byte
	for _, page := range pages {
		data, err := reader.RenderPage(page, RenderOptions{Format: RenderPNG, DPI: 96})
		if err != nil {
			t.Fatalf("渲染第 %d 页失败: %v", page, err)
		}
		last = data
	}
	return last
}

// 渲染其它页面后再回到第一页，红色大字标题必须仍在原位。
//
// 回归保护：字形轮廓缓存的路径被 drawTextPath 原地 Transform 污染。
// geom.Path.Transform（以及底层 canvas.Path.Transform）是原地修改，
// 若直接变换缓存对象，同一字形在后续渲染时会把平移不断累积，最终整体移出
// 页面——表现为「刚打开能看到，滚动回来就没了」。
func TestRenderPageKeepsTextAfterVisitingOtherPages(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "other", "sample0.ofd")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("缺少测试文档 %s: %v", path, err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	pageCount := len(reader.pages)
	if pageCount < 2 {
		t.Skip("样例页数不足，无法验证跨页缓存")
	}

	first := redInkTopThird(t, renderSequence(t, reader, []int{0}))
	if first < 1000 {
		t.Fatalf("首次渲染第一页红色标题像素 = %d，期望 >= 1000", first)
	}

	// 访问其余页面后回到第一页。
	all := make([]int, 0, pageCount+1)
	for page := 1; page < pageCount; page++ {
		all = append(all, page)
	}
	all = append(all, 0)
	second := redInkTopThird(t, renderSequence(t, reader, all))

	if second < 1000 {
		t.Fatalf("访问其它页后重渲染第一页红色标题像素 = %d，期望 >= 1000（字形轮廓缓存被原地变换污染）", second)
	}
}
