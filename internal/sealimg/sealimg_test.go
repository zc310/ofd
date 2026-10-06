package sealimg

import (
	"bytes"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/tdewolff/canvas"
)

// inkRows 统计印章图片里非透明像素在每行的分布。
func inkRows(t *testing.T, img image.Image) []int {
	t.Helper()
	bounds := img.Bounds()
	rows := make([]int, bounds.Dy())
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
				rows[y-bounds.Min.Y]++
			}
		}
	}
	return rows
}

// TestRenderIsTransparentOutsideStamp 印章之外必须完全透明。
//
// 印章图片要压在页面内容上，任何不透明的背景都会盖住正文——这类问题在浅色
// 页面上几乎看不出来，只在深色背景或图文混排时暴露。
func TestRenderIsTransparentOutsideStamp(t *testing.T) {
	img, err := Render(Options{Width: 256, Height: 256})
	if err != nil {
		t.Skipf("缺少可用的中文字体: %v", err)
	}
	bounds := img.Bounds()
	corner := 4
	for y := bounds.Min.Y; y < bounds.Min.Y+corner; y++ {
		for x := bounds.Min.X; x < bounds.Min.X+corner; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a != 0 {
				t.Fatalf("左上角应完全透明，(%.1f,%.1f) 处 alpha=%d", float64(x), float64(y), a>>8)
			}
		}
	}
	for y := bounds.Max.Y - corner; y < bounds.Max.Y; y++ {
		for x := bounds.Max.X - corner; x < bounds.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a != 0 {
				t.Fatalf("右下角应完全透明，(%.1f,%.1f) 处 alpha=%d", float64(x), float64(y), a>>8)
			}
		}
	}
}

// TestRenderPlacesTextsOnCorrectArcs 顶字在上、底字在下。
//
// 这是最容易被静默破坏的回归：坐标系或角度约定一旦写反，文字会上下颠倒，而
// 每一个汉字单独看仍然是合法字形，不比对整行就发现不了。
func TestRenderPlacesTextsOnCorrectArcs(t *testing.T) {
	img, err := Render(Options{Width: 512, Height: 512})
	if err != nil {
		t.Skipf("缺少可用的中文字体: %v", err)
	}
	rows := inkRows(t, img)
	middle := len(rows) / 2

	topInk, bottomInk := 0, 0
	for y, count := range rows {
		if y < middle/2 {
			topInk += count
		}
		if y > middle+middle/2 {
			bottomInk += count
		}
	}
	if topInk == 0 {
		t.Error("上半部没有墨迹，顶字缺失或落到了下半部")
	}
	if bottomInk == 0 {
		t.Error("下半部没有墨迹，底字缺失或落到了上半部")
	}
	// 圆框本身上下对称，所以上下两半的墨迹量应当同量级；偏差过大说明某一段文字
	// 跑到了另一半。
	if topInk > 3*bottomInk || bottomInk > 3*topInk {
		t.Errorf("上下墨迹量失衡：top=%d bottom=%d", topInk, bottomInk)
	}
}

// TestRenderKeepsCentreStar 中心五角星必须落在画面中部。
func TestRenderKeepsCentreStar(t *testing.T) {
	img, err := Render(Options{Width: 512, Height: 512})
	if err != nil {
		t.Skipf("缺少可用的中文字体: %v", err)
	}
	bounds := img.Bounds()
	// 取中心附近的小窗口，那里只有五角星：文字弧和边框都在更外圈。
	window := bounds.Dx() / 8
	cx, cy := bounds.Min.X+bounds.Dx()/2, bounds.Min.Y+bounds.Dy()/2
	for y := cy - window; y < cy+window; y++ {
		for x := cx - window; x < cx+window; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
				return
			}
		}
	}
	t.Error("画面中心应有用五角星")
}

// TestRenderShapes 使用指定形状时外框必须填满对应方向。
func TestRenderShapes(t *testing.T) {
	circle, err := Render(Options{Width: 400, Height: 400})
	if err != nil {
		t.Skipf("缺少可用的中文字体: %v", err)
	}
	ellipse, err := Render(Options{Width: 640, Height: 400, Shape: ShapeEllipse})
	if err != nil {
		t.Skipf("缺少可用的中文字体: %v", err)
	}
	if ellipse.Bounds().Dx() <= ellipse.Bounds().Dy() {
		t.Errorf("椭圆输出应为横向，实际 %v", ellipse.Bounds())
	}
	// 椭圆在左右两侧的墨迹应比圆形更靠边。
	if !hasInkNearEdge(t, ellipse, "left") {
		t.Error("椭圆左侧应接近画面边缘")
	}
	if !hasInkNearEdge(t, circle, "left") {
		t.Log("圆形左侧未贴近边缘（属正常，仅记录）")
	}
}

func hasInkNearEdge(t *testing.T, img image.Image, side string) bool {
	t.Helper()
	bounds := img.Bounds()
	limit := bounds.Dx() / 40
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for offset := range limit {
			var x int
			switch side {
			case "left":
				x = bounds.Min.X + offset
			case "right":
				x = bounds.Max.X - 1 - offset
			default:
				t.Fatalf("未知边 %q", side)
			}
			if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
				return true
			}
		}
	}
	return false
}

// TestRenderIsDeterministic 同一输入必须逐字节可复现。
//
// 印章图片会进测试 fixture 和文档截图。输出只要沾上随机性（例如做旧噪声或
// 依赖遍历顺序），基线就会周期性地失效，而且每次失败都不一样。
func TestRenderIsDeterministic(t *testing.T) {
	first, err := RenderPNG(Options{Width: 300, Height: 300})
	if err != nil {
		t.Skipf("缺少可用的中文字体: %v", err)
	}
	second, err := RenderPNG(Options{Width: 300, Height: 300})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Error("同一输入两次渲染结果不一致，输出不可复现")
	}
}

// TestRenderFailsOnUnknownFontName 显式指定了不存在的字体必须报错，不能静默换字体。
//
// 静默回退会让"我指定的字体没生效"变成查不出来的事：图照出，只是长得不对。
func TestRenderFailsOnUnknownFontName(t *testing.T) {
	_, err := Render(Options{Width: 128, Height: 128, FontName: "不存在的字体名 SealimgTest"})
	if err == nil {
		t.Fatal("显式指定不存在的字体时应报错")
	}
	if !strings.Contains(err.Error(), "不存在的字体名 SealimgTest") {
		t.Errorf("错误信息应指明是哪个字体失败，实际 %q", err.Error())
	}
}

// TestRenderFailsOnLatinOnlyFontName 字体不含中文时必须报错，不能画出一张方框图。
func TestRenderFailsOnLatinOnlyFontName(t *testing.T) {
	name := latinOnlyFont(t)
	if name == "" {
		t.Skip("系统没有可用的纯拉丁字体，跳过")
	}
	_, err := Render(Options{Width: 128, Height: 128, FontName: name})
	if err == nil {
		t.Fatalf("字体 %q 不含中文却渲染成功，底字会是方框", name)
	}
	if !strings.Contains(err.Error(), "中文字形") {
		t.Errorf("错误信息应说明缺中文字形，实际 %q", err.Error())
	}
}

// TestCJKProbeCoversAllFixedText 守住字体探测集合与印章文案同步：新增或修改
// 固定文案后如果忘记更新探针，缺字方框会静默混进印章。
func TestCJKProbeCoversAllFixedText(t *testing.T) {
	probe := map[rune]bool{}
	for _, r := range cjkProbe {
		probe[r] = true
	}
	for _, text := range []string{TopText, BottomText, CenterText} {
		for _, r := range text {
			if !probe[r] {
				t.Errorf("字体探针缺少固定文案字符 %q", string(r))
			}
		}
	}
}

// inkInRect 统计矩形（相对图片左上角）内的非透明像素数。
func inkInRect(img image.Image, x0, y0, x1, y1 int) int {
	b := img.Bounds()
	x0, y0 = max(x0, b.Min.X), max(y0, b.Min.Y)
	x1, y1 = min(x1, b.Max.X), min(y1, b.Max.Y)
	count := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
				count++
			}
		}
	}
	return count
}

// TestRenderShowsFixedTopAndCenterText 验证当前固定文案确实各自渲染出墨迹，
// 而不只是外框或五角星。窗口经过收紧，只覆盖对应文案：顶部弧字在中心列上方
// 的内框与五角星之间的空隙，来源小字在五角星下方、内框之内。文案被清空或新
// 字缺字形都会让窗口变空。
//
// 之所以要单独守这一步：上下半部都有墨迹、中心有五角星这两条旧断言，在顶字
// 整行消失或小字消失时依然成立（外框和五角星还在），旧文案换成新文案时最容易
// 漏测。
func TestRenderShowsFixedTopAndCenterText(t *testing.T) {
	const size = 512
	img, err := Render(Options{Width: size, Height: size})
	if err != nil {
		t.Skipf("缺少可用的中文字体: %v", err)
	}
	cx := size / 2

	topInk := inkInRect(img, cx-40, 52, cx+40, 100)
	if topInk == 0 {
		t.Errorf("顶部文案「%s」所在窗口没有墨迹", TopText)
	}
	centerInk := inkInRect(img, cx-110, 325, cx+110, 370)
	if centerInk == 0 {
		t.Errorf("来源小字「%s」所在窗口没有墨迹", CenterText)
	}
	t.Logf("顶部文案墨迹 %d，来源小字墨迹 %d", topInk, centerInk)
}

func TestLoadFaceUsesSyntheticBold(t *testing.T) {
	face, err := loadFace("", 32)
	if err != nil {
		t.Skipf("缺少可用的中文字体: %v", err)
	}
	if face.FauxBold < 0.02 {
		t.Errorf("印章字体应使用合成粗体，FauxBold = %.3f", face.FauxBold)
	}
}

// latinOnlyFont 找一个不含中文字形的系统字体。
func latinOnlyFont(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"DejaVu Sans", "Liberation Sans", "Helvetica", "Arial", "Noto Sans Mono"} {
		if _, err := canvas.LoadSystemFont(name, canvas.FontRegular); err != nil {
			continue
		}
		font, err := canvas.LoadSystemFont(name, canvas.FontRegular)
		if err != nil {
			continue
		}
		face := font.Face(40*pointsPerUnit, sealInk)
		if !supportsCJK(face) {
			return name
		}
	}
	return ""
}

// TestRenderPNGIsPNG 确认输出编码可读。
func TestRenderPNGIsPNG(t *testing.T) {
	data, err := RenderPNG(Options{Width: 128, Height: 128})
	if err != nil {
		t.Skipf("缺少可用的中文字体: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("输出不是合法 PNG: %v", err)
	}
	if decoded.Bounds().Dx() != 128 {
		t.Errorf("输出宽度 = %d，期望 128", decoded.Bounds().Dx())
	}
}
