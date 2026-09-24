package backends_test

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"

	_ "github.com/zc310/ofd/internal/render/backends/draw2d"
	_ "github.com/zc310/ofd/internal/render/backends/fgg"
	_ "github.com/zc310/ofd/internal/render/backends/ftgg"
	_ "github.com/zc310/ofd/internal/render/backends/gg"
	_ "github.com/zc310/ofd/internal/render/backends/tinyskia"
)

const stress999Path = "../../../test/testdata/999.ofd"

// stress999Document 打开 999.ofd 并返回可复用的渲染文档（页面/字体一次到位）。
func stress999Document(t testing.TB, dpi geom.Resolution) (*render.Document, *parser.OFD) {
	t.Helper()
	ofd, err := parser.NewOFD(stress999Path)
	if err != nil {
		t.Fatalf("打开 999.ofd 失败: %v", err)
	}
	doc := render.NewDocumentWithDPI(color.White, ofd.Documents[0], dpi)
	page := doc.Pages[0]
	if _, err := doc.RasterizePage(page, render.BackendCanvas, dpi); err != nil {
		t.Fatalf("预热渲染失败: %v", err)
	}
	return doc, ofd
}

// TestCompareBackends999Page1 渲染 999.ofd 第 1 页在所有已注册后端下的输出，
// 以 canvas(A) 为基准与其余后端两两排版为 5 行 × 3 列（B=gg、C=ftgg、
// D=fgg、E=tinyskia、F=draw2d）：
//
//	原图A | 原图B | AB差异
//	原图A | 原图C | AC差异
//	原图A | 原图D | AD差异
//
// 差异图为白底红点（逐像素不一致处涂红）。结果写入 STRESS_PNG_DIR（默认
// /tmp/ofd_test/compare.png）。
func TestCompareBackends999Page1(t *testing.T) {
	doc, ofd := stress999Document(t, geom.DPI(150))
	defer ofd.Close()
	page := doc.Pages[0]

	base := render.BackendCanvas
	others := []string{drawing.BackendGG, drawing.BackendFTGG, drawing.BackendFGG, drawing.BackendTinySkia, drawing.BackendDraw2D}
	rendered := make(map[string]*image.RGBA)
	names := append([]string{base}, others...)
	for _, name := range names {
		img, err := doc.RasterizePage(page, name, geom.DPI(150))
		if err != nil {
			t.Fatalf("%s 渲染失败: %v", name, err)
		}
		rendered[name] = img
	}

	var rows []*image.RGBA
	for _, other := range others {
		a, b := rendered[base], rendered[other]
		n := countDiff(a, b)
		t.Logf("差异像素 %s vs %s: %d (%.2f%%)",
			backendTag(base), backendTag(other), n,
			100*float64(n)/float64(a.Bounds().Dx()*a.Bounds().Dy()))
		rows = append(rows, tripAt(a, b, backendTag(base), backendTag(other)))
	}

	grid := stackRows(rows, 4)

	dir := os.Getenv("STRESS_PNG_DIR")
	if dir == "" {
		dir = "/tmp/ofd_test"
	}
	_ = os.MkdirAll(dir, 0o755)
	out := filepath.Join(dir, "compare.png")
	f, err := os.Create(out)
	if err != nil {
		t.Fatalf("创建 %s 失败: %v", out, err)
	}
	if err := png.Encode(f, grid); err != nil {
		f.Close()
		t.Fatalf("写 PNG 失败: %v", err)
	}
	f.Close()
	t.Logf("已输出 %s（%dx%d）", out, grid.Bounds().Dx(), grid.Bounds().Dy())
}

// TestRenderImageOrientationAcrossBackends 回归：RenderImage 必须按页面矩阵把
// 源图原样（不翻转）落到设备坐标。历史上 tinyskia/draw2d 的 RenderImage 把
// y 轴符号写反，轴对齐图片被上下镜像（999.ofd 左上角二维码整体倒置），
// draw2d 甚至把下探内容画到页面外。这里用上半黑下半白的不对称图片验证：
// 设备上方应为黑、下方应为白，且各后端与 canvas 基准在两处探针一致。
func TestRenderImageOrientationAcrossBackends(t *testing.T) {
	const (
		imgW, imgH   = 12.0, 24.0
		boxX, boxY   = 2.0, 2.0
		pageW, pageH = 50.0, 50.0
	)
	// 12×24px 的对称轴对齐图片：上 12 行纯黑、下 12 行纯白（不透明）。
	img := image.NewRGBA(image.Rect(0, 0, 12, 24))
	for y := 0; y < 24; y++ {
		c := color.White
		if y < 12 {
			c = color.Black
		}
		for x := 0; x < 12; x++ {
			img.Set(x, y, c)
		}
	}

	// 复刻 render.imageMatrixWH 对轴对齐图片生成的矩阵（img 像素 y 向下、
	// 页面 y 向上），放置到页面左上角 12×24mm 区域。
	m := geom.Matrix{
		{1, 0, boxX},
		{0, 1, pageH - boxY - imgH},
	}

	res := geom.DPI(96)
	dpmm := res.DPMM()
	hpx := int(pageH*dpmm + 0.5)

	raster := func(name string) *image.RGBA {
		b, err := render.NewBackend(name, pageW, pageH, res)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b.RenderImage(img, m)
		return b.Raster()
	}

	// 源像素 (sx,sy) → 设备（与 canvas/gg 一致）：deviceY = hpx - dpmm·(m12 + m11·(imgH-sy))，
	// deviceX = dpmm·(m00·sx + m02)。探针取图片区域内、远离反锯齿边界的整数点。
	left := int(dpmm * boxX)
	topRow := int(float64(hpx) - dpmm*(m[1][2]+imgH))
	botRow := int(float64(hpx) - dpmm*(m[1][2]))
	blackProbe := image.Point{left + 10, topRow + 5}
	whiteProbe := image.Point{left + 10, botRow - 5}

	isDark := func(c color.Color) bool {
		r, g, b, _ := c.RGBA()
		return (r+g+b)/3 < 0x7f00
	}

	base := raster(render.BackendCanvas)
	if isDark(base.RGBAAt(blackProbe.X, blackProbe.Y)) == false || isDark(base.RGBAAt(whiteProbe.X, whiteProbe.Y)) == true {
		t.Fatalf("测试场景无效：canvas 基准应在 %v 为黑、%v 为白", blackProbe, whiteProbe)
	}
	for _, name := range []string{drawing.BackendGG, drawing.BackendFTGG, drawing.BackendFGG, drawing.BackendTinySkia, drawing.BackendDraw2D} {
		got := raster(name)
		if isDark(got.RGBAAt(blackProbe.X, blackProbe.Y)) == false {
			t.Errorf("%s: 图片顶部区域应为黑，实际在 %v 为白（图片被上下倒置）", name, blackProbe)
		}
		if isDark(got.RGBAAt(whiteProbe.X, whiteProbe.Y)) == true {
			t.Errorf("%s: 图片底部区域应为白，实际在 %v 为黑（图片被上下倒置）", name, whiteProbe)
		}
	}
}

// backendTag 返回后端的对比标签（对应 A=canvas、B=gg、C=ftgg、D=fgg、
// E=tinyskia、F=draw2d）。
func backendTag(name string) string {
	switch name {
	case render.BackendCanvas:
		return "A(canvas)"
	case drawing.BackendGG:
		return "B(gg)"
	case drawing.BackendFTGG:
		return "C(ftgg)"
	case drawing.BackendFGG:
		return "D(fgg)"
	case drawing.BackendTinySkia:
		return "E(tinyskia)"
	case drawing.BackendDraw2D:
		return "F(draw2d)"
	default:
		return name
	}
}

// tripAt 生成一行三格：原图A | 原图B | 两者差异图（白底红点），每格顶部带
// 标签，diff 格标签为 "diff A-B"。
func tripAt(a, b *image.RGBA, aTag, bTag string) *image.RGBA {
	const labelH = 16
	tw, th := a.Bounds().Dx(), a.Bounds().Dy()
	row := image.NewRGBA(image.Rect(0, 0, tw*3, labelH+th))
	draw.Draw(row, row.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(row, image.Rect(0, labelH, tw, labelH+th), a, image.Point{}, draw.Src)
	draw.Draw(row, image.Rect(tw, labelH, tw*2, labelH+th), b, image.Point{}, draw.Src)
	diff, _ := diffImage(a, b)
	draw.Draw(row, image.Rect(2*tw, labelH, tw*3, labelH+th), diff, image.Point{}, draw.Src)
	drawLabel(row, 4, aTag)
	drawLabel(row, tw+4, bTag)
	drawLabel(row, tw*2+4, "diff "+diffTag(aTag, bTag))
	return row
}

// diffTag 生成差异格标签的短名（如 "A-B"）。
func diffTag(a, b string) string {
	return a[:1] + "-" + b[:1]
}

// diffImage 返回 A/B 的逐像素差异图：一致全白，不一致的点涂红。
func diffImage(a, b *image.RGBA) (*image.RGBA, int) {
	ra, rb := a.Bounds(), b.Bounds()
	w, h := ra.Dx(), ra.Dy()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	n := 0
	for y := 0; y < h; y++ {
		ya, yb := ra.Min.Y+y, rb.Min.Y+y
		for x := 0; x < w; x++ {
			xa, xb := ra.Min.X+x, rb.Min.X+x
			pa, pb := a.RGBAAt(xa, ya), b.RGBAAt(xb, yb)
			if pa.R == pb.R && pa.G == pb.G && pa.B == pb.B && pa.A == pb.A {
				img.Set(x, y, color.White)
				continue
			}
			img.Set(x, y, color.RGBA{R: 255, A: 255})
			n++
		}
	}
	return img, n
}

// countDiff 统计 A/B 逐像素不一致的数量。
func countDiff(a, b *image.RGBA) int {
	_, n := diffImage(a, b)
	return n
}

// stackRows 把若干等宽行纵向叠成一列，行间留 gap 像素。
func stackRows(rows []*image.RGBA, gap int) *image.RGBA {
	w, h := rows[0].Bounds().Dx(), rows[0].Bounds().Dy()
	total := h*len(rows) + gap*(len(rows)-1)
	img := image.NewRGBA(image.Rect(0, 0, w, total))
	y := 0
	for _, row := range rows {
		draw.Draw(img, image.Rect(0, y, w, y+h), row, image.Point{}, draw.Src)
		y += h + gap
	}
	return img
}

// drawLabel 用 basicfont 在图中写一行 ASCII 标签。
func drawLabel(img *image.RGBA, x int, text string) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.RGBA{R: 0x20, G: 0x20, B: 0x20, A: 0xff}),
		Face: basicfont.Face7x13,
	}
	d.Dot = fixed.P(x, 13)
	d.DrawString(text)
}

// TestCopyStrokeToFillAcrossBackends 回归 drawClippedPath 依赖的
// CopyStrokeToFill：它必须把当前描边画笔（尤其是纯色）真正复制成填充画笔。
// 历史上 gg/ftgg/tinyskia 只复制了开关与渐变，纯色裁剪描边被填充成透明而
// 整体丢失。这里对每个后端做相同的“复制描边→禁用描边→填充描边几何”操作，
// 要求红色覆盖像素数与 canvas 基准接近。
func TestCopyStrokeToFillAcrossBackends(t *testing.T) {
	ww, hh := 40.0, 40.0
	res := geom.DPI(96)

	redFillPixels := func(name string) int {
		b, err := render.NewBackend(name, ww, hh, res)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b.SetFillColor(color.White)
		b.DrawPath(0, 0, geom.Rectangle(ww, hh))
		b.ClearFill()
		b.SetStrokeColor(color.RGBA{R: 0xd3, G: 0x2f, B: 0x2f, A: 0xff})
		b.SetStrokeWidth(6)
		b.CopyStrokeToFill()
		b.SetStrokeColor(color.Transparent)
		p := &geom.Path{}
		p.MoveTo(5, 5)
		p.LineTo(35, 35)
		b.DrawPath(0, 0, p.Stroke(6, geom.ButtCap, geom.MiterJoin, geom.Tolerance))

		img := b.Raster()
		n := 0
		for i := 0; i+3 < len(img.Pix); i += 4 {
			if img.Pix[i] > 200 && img.Pix[i+1] < 100 && img.Pix[i+2] < 100 {
				n++
			}
		}
		return n
	}

	base := redFillPixels(render.BackendCanvas)
	if base == 0 {
		t.Fatal("canvas 基准没有填充像素，测试无意义")
	}
	for _, name := range []string{drawing.BackendGG, drawing.BackendFTGG, drawing.BackendFGG, drawing.BackendTinySkia, drawing.BackendDraw2D} {
		got := redFillPixels(name)
		t.Logf("%s 填充像素 %d（canvas 基准 %d）", name, got, base)
		if got == 0 {
			t.Errorf("%s: CopyStrokeToFill 后填充缺失（红色像素为 0）", name)
			continue
		}
		diff := got - base
		if diff < 0 {
			diff = -diff
		}
		if diff*100 > base*10 {
			t.Errorf("%s: 填充像素 %d 与 canvas %d 相差过大", name, got, base)
		}
	}
}

// TestNewBackendRejectsZeroPixelSize 回归：页面在给定分辨率下取整为 0 像素时
// 各后端必须返回错误，而不是 panic 或产出空图。
func TestNewBackendRejectsZeroPixelSize(t *testing.T) {
	for _, name := range []string{render.BackendCanvas, drawing.BackendGG, drawing.BackendFTGG, drawing.BackendFGG, drawing.BackendTinySkia, drawing.BackendDraw2D} {
		if _, err := render.NewBackend(name, 0.001, 0.001, geom.DPI(1)); err == nil {
			t.Errorf("%s: 0 像素页面未返回错误", name)
		}
	}
}
