package render

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
)

// TestSealOpaqueWhiteBackgroundBecomesTransparent 回归：不带透明通道的印章
// 位图必须把白色纸张背景置为透明，红色墨迹保留；已经带透明通道的印章不能
// 被改写。
func TestSealOpaqueWhiteBackgroundBecomesTransparent(t *testing.T) {
	pal := color.Palette{color.RGBA{R: 255, G: 255, B: 255, A: 255}, color.RGBA{R: 223, G: 25, B: 29, A: 255}}
	src := image.NewPaletted(image.Rect(0, 0, 4, 4), pal)
	for i, idx := range []uint8{0, 1, 0, 0, 1, 1, 1, 0, 0, 1, 1, 0, 0, 0, 1, 0} {
		src.Pix[i] = idx
	}
	got := sealTransparentBackground(src)
	if _, _, _, a := got.At(0, 0).RGBA(); a != 0 {
		t.Fatalf("白色背景像素 alpha = %d, want 0", a>>8)
	}
	if _, _, _, a := got.At(1, 0).RGBA(); a != 0xffff {
		t.Fatalf("红色墨迹像素 alpha = %d, want 255", a>>8)
	}
	if _, _, _, a := got.At(1, 1).RGBA(); a != 0xffff {
		t.Fatalf("红色墨迹像素 alpha = %d, want 255", a>>8)
	}

	transparent := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	transparent.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 128})
	if replaced := sealTransparentBackground(transparent); replaced != image.Image(transparent) {
		t.Fatal("带透明通道的印章不应被改写")
	}
}

// TestOpaqueSealFixtureBackgroundKeyedOut 用真实样例保护：签章内嵌的索引 PNG
// 没有透明通道、背景为纯白，按键出白色后左上角背景必须透明。
func TestOpaqueSealFixtureBackgroundKeyedOut(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "ofdrw", "不规范资源路径.ofd")
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Skipf("样例不可用: %v", err)
	}
	defer zr.Close()

	var data []byte
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, "SignValue.dat") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err = io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		break
	}
	if data == nil {
		t.Skip("样例不含签章数据")
	}
	seal, err := parser.ExtractSealData(data)
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(bytes.NewReader(seal.Data))
	if err != nil {
		t.Fatal(err)
	}
	if opaque, ok := img.(interface{ Opaque() bool }); !ok || !opaque.Opaque() {
		t.Skip("签章本身带透明通道，本用例不适用")
	}
	b := img.Bounds()
	if _, _, _, a := sealTransparentBackground(img).At(b.Min.X, b.Min.Y).RGBA(); a != 0 {
		t.Fatalf("样例签章左上角背景 alpha = %d, want 0", a>>8)
	}
}

// TestOFDSealDocumentCached 回归：OFD 印章文档在多次渲染同一页面时必须只
// 解析/缓存一次，避免每次渲染都重新解包印章并重新加载其字体。
func TestOFDSealDocumentCached(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "testdata", "ofdrw/999.ofd"))
	if err != nil {
		t.Skipf("999.ofd 不可用: %v", err)
	}
	defer ofd.Close()
	if len(ofd.Documents) == 0 || len(ofd.Documents[0].Pages) == 0 {
		t.Skip("文档无页面")
	}
	doc := NewDocumentWithDPI(canvas.White, ofd.Documents[0], geom.DPI(96))
	page := doc.Pages[0]
	for i := range 2 {
		if _, err := doc.RasterizePage(page, BackendCanvas, geom.DPI(96)); err != nil {
			t.Fatalf("第 %d 次渲染失败: %v", i+1, err)
		}
	}
	doc.sealMu.Lock()
	n := len(doc.sealDocs)
	doc.sealMu.Unlock()
	if n == 0 {
		t.Skip("该样例无 OFD 印章，缓存用例不适用")
	}
	if n != 1 {
		t.Fatalf("印章缓存条目 = %d, want 1", n)
	}
}

func Test999StampSealInheritsFallbackFont(t *testing.T) {
	sealDocument, err := parser.NewOFD(filepath.Join("..", "..", "testdata", "ofdrw/999.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer sealDocument.Close()

	fontData, err := os.ReadFile("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf")
	if err != nil {
		t.Skipf("DejaVu Sans is unavailable: %v", err)
	}

	page := sealDocument.Documents[0].Pages[0]
	if _, err := page.PhysicalBox(); err != nil {
		t.Fatal(err)
	}
	withFallback := NewDocument(canvas.White, sealDocument.Documents[0])
	if err := RegisterFallbackFont(fontData, "Noto-Regular-Test", FontRegular); err != nil {
		t.Fatal(err)
	}
	if err := withFallback.UseFallbackFont("Noto-Regular-Test"); err != nil {
		t.Fatal(err)
	}
	if _, err := withFallback.Page(page); err != nil {
		t.Fatal(err)
	}
	if len(withFallback.fallbackFontFamilies()) != 1 {
		t.Fatalf("fallback families = %d, want 1", len(withFallback.fallbackFontFamilies()))
	}
	if withFallback.fallbackFontFamilies()[0] != "Noto-Regular-Test" {
		t.Fatalf("fallback family = %q", withFallback.fallbackFontFamilies()[0])
	}
}

// sealQuadrantImage 生成四象限测试印章：左上红、右上绿、左下蓝、右下黄，
// 便于断言渲染出来的到底是印章位图的哪一块。
func sealQuadrantImage(size int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	half := size / 2
	for y := range size {
		for x := range size {
			var c color.NRGBA
			switch {
			case x < half && y < half:
				c = color.NRGBA{R: 255, A: 255}
			case x >= half && y < half:
				c = color.NRGBA{G: 255, A: 255}
			case x < half:
				c = color.NRGBA{B: 255, A: 255}
			default:
				c = color.NRGBA{R: 255, G: 255, A: 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

// sealInkBox 返回栅格图中不透明墨迹的包围盒（毫米，左上原点）。
func sealInkBox(r *image.RGBA) (x, y, w, h float64, ok bool) {
	b := r.Bounds()
	loX, hiX, loY, hiY := b.Max.X, b.Min.X, b.Max.Y, b.Min.Y
	for py := b.Min.Y; py < b.Max.Y; py++ {
		for px := b.Min.X; px < b.Max.X; px++ {
			if _, _, _, a := r.At(px, py).RGBA(); a == 0 {
				continue
			}
			loX, hiX = min(loX, px), max(hiX, px)
			loY, hiY = min(loY, py), max(hiY, py)
		}
	}
	if loX > hiX {
		return 0, 0, 0, 0, false
	}
	const mmPerPx = 25.4 / 96.0
	return float64(loX) * mmPerPx, float64(loY) * mmPerPx,
		float64(hiX-loX+1) * mmPerPx, float64(hiY-loY+1) * mmPerPx, true
}

// TestSealStraddleClipDrawsSliceOnly 回归：骑缝章用 StampAnnot.Clip 声明每页只
// 显示印章的一条切片，渲染必须只画该切片、贴在 Boundary+Clip 处，并且取的是
// 印章位图上对应的那一块（横向、纵向裁剪都必须生效）。
//
// StampAnnot.Boundary.Y 是印章顶边距页顶的距离（与页面对象一致），因此下面
// 的 wantTop 直接等于 Boundary.Y+Clip.Y。
func TestSealStraddleClipDrawsSliceOnly(t *testing.T) {
	pb := models.StBox{Width: 210, Height: 297}
	// h.ofd 的骑缝章：40mm 印章盒逐页左移 8mm，8mm 裁剪窗口逐页右移 8mm。
	straddle := models.StBox{X: 170, Y: 128.5, Width: 40, Height: 40}

	cases := []struct {
		name     string
		boundary models.StBox
		clip     models.StBox
		wantX    float64 // 墨迹左边界（距页左，毫米）
		wantTop  float64 // 墨迹上边界（距页顶，毫米）
		wantW    float64
		wantH    float64
		probe    [2]float64 // 取色点（占绘制盒的相对比例）
		wantTint [3]uint8
	}{
		{
			name: "无 Clip 时画整枚印章", boundary: straddle,
			wantX: 170, wantTop: 128.5, wantW: 40, wantH: 40,
			probe: [2]float64{0.25, 0.25}, wantTint: [3]uint8{255, 0, 0},
		},
		{
			name:     "h.ofd s002 贴右缘取印章最左 8mm",
			boundary: models.StBox{X: 202, Y: 128.5, Width: 40, Height: 40},
			clip:     models.StBox{X: 0, Y: 0, Width: 8, Height: 40},
			wantX:    202, wantTop: 128.5, wantW: 8, wantH: 40,
			probe: [2]float64{0.5, 0.25}, wantTint: [3]uint8{255, 0, 0},
		},
		{
			name:     "h.ofd s003 盒左移后仍贴右缘",
			boundary: models.StBox{X: 194, Y: 128.5, Width: 40, Height: 40},
			clip:     models.StBox{X: 8, Y: 0, Width: 8, Height: 40},
			wantX:    202, wantTop: 128.5, wantW: 8, wantH: 40,
			probe: [2]float64{0.5, 0.25}, wantTint: [3]uint8{255, 0, 0},
		},
		{
			name:     "h.ofd s004 盒左移 16mm 仍贴右缘",
			boundary: models.StBox{X: 186, Y: 128.5, Width: 40, Height: 40},
			clip:     models.StBox{X: 16, Y: 0, Width: 8, Height: 40},
			wantX:    202, wantTop: 128.5, wantW: 8, wantH: 40,
			// 该切片横跨源图中线，取靠左的取色点以避开象限边界。
			probe: [2]float64{0.2, 0.25}, wantTint: [3]uint8{255, 0, 0},
		},
		{
			// 末两片取的是印章右半，源图那一段是绿色：证明裁剪窗口选的
			// 是印章上对应的横向片段，而不是把整枚印章缩窄。
			name:     "h.ofd s005 取印章右半",
			boundary: models.StBox{X: 178, Y: 128.5, Width: 40, Height: 40},
			clip:     models.StBox{X: 24, Y: 0, Width: 8, Height: 40},
			wantX:    202, wantTop: 128.5, wantW: 8, wantH: 40,
			probe: [2]float64{0.5, 0.25}, wantTint: [3]uint8{0, 255, 0},
		},
		{
			name:     "h.ofd s006 取印章最右 8mm",
			boundary: models.StBox{X: 170, Y: 128.5, Width: 40, Height: 40},
			clip:     models.StBox{X: 32, Y: 0, Width: 8, Height: 40},
			wantX:    202, wantTop: 128.5, wantW: 8, wantH: 40,
			probe: [2]float64{0.5, 0.25}, wantTint: [3]uint8{0, 255, 0},
		},
		{
			name: "纵向裁剪取印章上半", boundary: models.StBox{X: 20, Y: 20, Width: 40, Height: 40},
			clip:  models.StBox{X: 0, Y: 0, Width: 40, Height: 20},
			wantX: 20, wantTop: 20, wantW: 40, wantH: 20,
			probe: [2]float64{0.25, 0.25}, wantTint: [3]uint8{255, 0, 0},
		},
		{
			name: "纵向裁剪取印章下半", boundary: models.StBox{X: 20, Y: 20, Width: 40, Height: 40},
			clip:  models.StBox{X: 0, Y: 20, Width: 40, Height: 20},
			wantX: 20, wantTop: 40, wantW: 40, wantH: 20,
			probe: [2]float64{0.25, 0.75}, wantTint: [3]uint8{0, 0, 255},
		},
		{
			name: "裁剪窗口取印章右上象限", boundary: models.StBox{X: 20, Y: 20, Width: 40, Height: 40},
			clip:  models.StBox{X: 20, Y: 0, Width: 20, Height: 20},
			wantX: 40, wantTop: 20, wantW: 20, wantH: 20,
			probe: [2]float64{0.5, 0.5}, wantTint: [3]uint8{0, 255, 0},
		},
		{
			name: "裁剪窗口取印章右下象限", boundary: models.StBox{X: 20, Y: 20, Width: 40, Height: 40},
			clip:  models.StBox{X: 20, Y: 20, Width: 20, Height: 20},
			wantX: 40, wantTop: 40, wantW: 20, wantH: 20,
			probe: [2]float64{0.5, 0.5}, wantTint: [3]uint8{255, 255, 0},
		},
		{
			name: "Clip 等于 Boundary 时仍是整枚印章", boundary: straddle,
			clip:  models.StBox{X: 0, Y: 0, Width: 40, Height: 40},
			wantX: 170, wantTop: 128.5, wantW: 40, wantH: 40,
			probe: [2]float64{0.25, 0.25}, wantTint: [3]uint8{255, 0, 0},
		},
	}

	doc := &Document{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			surface := newOffscreenSurface(pb.Width, pb.Height, geom.DPI(96))
			if surface == nil {
				t.Skip("离屏表面未注册，跳过")
			}
			info := &parser.SealInfo{StampAnnot: &models.StampAnnot{Boundary: tc.boundary, Clip: tc.clip}}
			if err := doc.drawRasterSeal(surface, info, pb, sealQuadrantImage(100)); err != nil {
				t.Fatalf("绘制失败: %v", err)
			}
			raster := surface.Raster()
			gotX, gotY, gotW, gotH, ok := sealInkBox(raster)
			if !ok {
				t.Fatal("页面上没有印章墨迹")
			}
			t.Logf("墨迹 x=%.1f..%.1fmm 距顶 %.1f..%.1fmm", gotX, gotX+gotW, gotY, gotY+gotH)

			const tol = 1.2 // 96dpi 下 1mm≈3.8px，容许抗锯齿边缘
			for _, c := range []struct {
				label     string
				got, want float64
			}{
				{"左边界", gotX, tc.wantX},
				{"上边界", gotY, tc.wantTop},
				{"宽度", gotW, tc.wantW},
				{"高度", gotH, tc.wantH},
			} {
				if abs(c.got-c.want) > tol {
					t.Errorf("墨迹%s = %.1fmm, want %.1fmm", c.label, c.got, c.want)
				}
			}

			// 取色确认画的是印章位图上对应的那一块。
			r, g, bl, _ := raster.At(
				raster.Bounds().Min.X+int((gotX+gotW*tc.probe[0])/25.4*96),
				raster.Bounds().Min.Y+int((gotY+gotH*tc.probe[1])/25.4*96),
			).RGBA()
			gotTint := [3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8)}
			if gotTint != tc.wantTint {
				t.Errorf("取色点颜色 = %v, want %v", gotTint, tc.wantTint)
			}
		})
	}
}

// TestSealClipBoxClamped 回归：Clip 越出 Boundary 时必须夹回印章盒，不能盖住
// 印章盒以外的页面内容；缺省或无效的 Clip 一律按整枚印章绘制。
func TestSealClipBoxClamped(t *testing.T) {
	boundary := models.StBox{X: 100, Y: 50, Width: 40, Height: 40}
	cases := []struct {
		name   string
		clip   models.StBox
		want   models.StBox
		wantOK bool
	}{
		{name: "缺省 Clip 按整枚印章", clip: models.StBox{}, wantOK: false},
		{name: "零宽 Clip 无效", clip: models.StBox{X: 0, Y: 0, Width: 0, Height: 40}, wantOK: false},
		{name: "零高 Clip 无效", clip: models.StBox{X: 0, Y: 0, Width: 8, Height: 0}, wantOK: false},
		{name: "完全在盒外无效", clip: models.StBox{X: 50, Y: 0, Width: 8, Height: 8}, wantOK: false},
		{
			name: "右越界被夹住", clip: models.StBox{X: 36, Y: 0, Width: 8, Height: 40},
			want: models.StBox{X: 136, Y: 50, Width: 4, Height: 40}, wantOK: true,
		},
		{
			name: "左越界被夹住", clip: models.StBox{X: -8, Y: 0, Width: 16, Height: 40},
			want: models.StBox{X: 100, Y: 50, Width: 8, Height: 40}, wantOK: true,
		},
		{
			name: "盒内原样", clip: models.StBox{X: 8, Y: 4, Width: 8, Height: 20},
			want: models.StBox{X: 108, Y: 54, Width: 8, Height: 20}, wantOK: true,
		},
		{
			name: "整枚印章", clip: models.StBox{X: 0, Y: 0, Width: 40, Height: 40},
			want: boundary, wantOK: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := sealClipBox(boundary, tc.clip)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (got %+v)", ok, tc.wantOK, got)
			}
			if !ok {
				return
			}
			if abs(got.X-tc.want.X) > 1e-6 || abs(got.Y-tc.want.Y) > 1e-6 ||
				abs(got.Width-tc.want.Width) > 1e-6 || abs(got.Height-tc.want.Height) > 1e-6 {
				t.Fatalf("目标框 = %+v, want %+v", got, tc.want)
			}
		})
	}

	if _, ok := sealClipBox(models.StBox{}, models.StBox{X: 0, Y: 0, Width: 8, Height: 8}); ok {
		t.Fatal("零尺寸 Boundary 不应产生裁剪")
	}
}

// TestCropSealImage 回归：裁剪必须按 Boundary 坐标比例取源图子矩形；整枚印章
// 直接复用原图，取不到有效区域时保持原图不变。
func TestCropSealImage(t *testing.T) {
	const size = 100
	src := sealQuadrantImage(size)
	boundary := models.StBox{X: 0, Y: 0, Width: 40, Height: 40}

	// 右上象限对应源图 x 50..100、y 0..50。
	got := cropSealImage(src, boundary, models.StBox{X: 20, Y: 0, Width: 20, Height: 20})
	if b := got.Bounds(); b.Dx() != 50 || b.Dy() != 50 {
		t.Fatalf("裁剪尺寸 = %dx%d, want 50x50", b.Dx(), b.Dy())
	}
	if r, g, _, _ := got.At(25, 25).RGBA(); r>>8 != 0 || g>>8 != 255 {
		t.Fatalf("右上象限中心 = (%d,%d), want 纯绿", r>>8, g>>8)
	}
	// 裁剪后的原点对应源图 (50,0)，同样落在右上象限。
	if r, g, _, _ := got.At(0, 0).RGBA(); r>>8 != 0 || g>>8 != 255 {
		t.Fatalf("裁剪原点像素 = (%d,%d), want 纯绿", r>>8, g>>8)
	}
	// 左下象限对应源图 x 0..50、y 50..100。
	bl := cropSealImage(src, boundary, models.StBox{X: 0, Y: 20, Width: 20, Height: 20})
	if r, g, b2, _ := bl.At(25, 25).RGBA(); r>>8 != 0 || g>>8 != 0 || b2>>8 != 255 {
		t.Fatalf("左下象限中心 = (%d,%d,%d), want 纯蓝", r>>8, g>>8, b2>>8)
	}

	if full := cropSealImage(src, boundary, boundary); full != image.Image(src) {
		t.Fatal("整枚印章应直接复用原图")
	}
	if none := cropSealImage(src, boundary, models.StBox{X: 100, Y: 0, Width: 8, Height: 8}); none != image.Image(src) {
		t.Fatal("空裁剪区域应保持原图不变")
	}
}

// TestOFDSealClipKeepsVectorOrientation 回归：带 Clip 的 OFD 矢量印章改走「先
// 栅格化印章页、再裁剪」的路径，结果必须与直接矢量绘制一致——印章不能被上下
// 翻转、缩放或错位。999.ofd 自带一枚 OFD 格式的印章。
func TestOFDSealClipKeepsVectorOrientation(t *testing.T) {
	const (
		dpi     = 96.0
		mmPerPx = 25.4 / dpi
	)

	// 统计指定页面区域内的墨迹包围盒和纵向重心（页面背景为不透明白色）。
	measure := func(img *image.RGBA, box models.StBox) (bx0, by0, bx1, by1, cy float64, ok bool) {
		b := img.Bounds()
		x0, y0 := int(box.X/mmPerPx), int(box.Y/mmPerPx)
		x1, y1 := int((box.X+box.Width)/mmPerPx), int((box.Y+box.Height)/mmPerPx)
		loX, hiX, loY, hiY := b.Max.X, b.Min.X, b.Max.Y, b.Min.Y
		var sum, n float64
		for py := max(y0, b.Min.Y); py < min(y1, b.Max.Y); py++ {
			for px := max(x0, b.Min.X); px < min(x1, b.Max.X); px++ {
				cr, cg, cb, ca := img.At(px, py).RGBA()
				if ca == 0 || (cr>>8 > 240 && cg>>8 > 240 && cb>>8 > 240) {
					continue
				}
				loX, hiX = min(loX, px), max(hiX, px)
				loY, hiY = min(loY, py), max(hiY, py)
				sum += float64(py)
				n++
			}
		}
		if n == 0 || loX > hiX {
			return 0, 0, 0, 0, 0, false
		}
		return float64(loX) * mmPerPx, float64(loY) * mmPerPx,
			float64(hiX+1) * mmPerPx, float64(hiY+1) * mmPerPx, sum / n * mmPerPx, true
	}

	ofd, err := parser.NewOFD(filepath.Join("..", "..", "testdata", "ofdrw/999.ofd"))
	if err != nil {
		t.Skipf("999.ofd 不可用: %v", err)
	}
	defer ofd.Close()
	if len(ofd.Documents) == 0 || len(ofd.Documents[0].Pages) == 0 {
		t.Skip("文档无页面")
	}
	page := ofd.Documents[0].Pages[0]
	seals := ofd.Documents[0].GetSeals(page.ID)
	if len(seals) == 0 {
		t.Skip("该样例页面无印章")
	}
	annot := seals[0].StampAnnot
	if annot == nil {
		t.Skip("印章缺少 StampAnnot")
	}
	if got := seals[0].SealData.FileType; got != "ofd" {
		t.Skipf("印章格式为 %q，本用例只覆盖 OFD 矢量印章", got)
	}
	box := annot.Boundary

	// 基准：直接矢量绘制。
	base, err := NewDocumentWithDPI(canvas.White, ofd.Documents[0], geom.DPI(dpi)).RasterizePage(page, BackendCanvas, geom.DPI(dpi))
	if err != nil {
		t.Fatal(err)
	}
	// 被测：Clip 等于 Boundary，强制走栅格化裁剪路径，显示内容应完全一致。
	annot.Clip = box
	defer func() { annot.Clip = models.StBox{} }()
	clipped, err := NewDocumentWithDPI(canvas.White, ofd.Documents[0], geom.DPI(dpi)).RasterizePage(page, BackendCanvas, geom.DPI(dpi))
	if err != nil {
		t.Fatal(err)
	}

	gotX, gotY, gotX2, gotY2, gotCY, ok := measure(clipped, box)
	if !ok {
		t.Fatal("栅格化路径没有画出印章")
	}
	wantX, wantY, wantX2, wantY2, wantCY, ok := measure(base, box)
	if !ok {
		t.Skip("基准渲染未画出印章，本用例不适用")
	}
	t.Logf("矢量路径   x=%.1f..%.1f y=%.1f..%.1f 墨迹重心 y=%.1fmm", wantX, wantX2, wantY, wantY2, wantCY)
	t.Logf("栅格化路径 x=%.1f..%.1f y=%.1f..%.1f 墨迹重心 y=%.1fmm", gotX, gotX2, gotY, gotY2, gotCY)

	const tol = 1.5
	for _, c := range []struct {
		label     string
		got, want float64
	}{
		{"左边界", gotX, wantX},
		{"右边界", gotX2, wantX2},
		{"上边界", gotY, wantY},
		{"下边界", gotY2, wantY2},
		{"墨迹纵向重心", gotCY, wantCY},
	} {
		if abs(c.got-c.want) > tol {
			t.Errorf("%s = %.1fmm, 矢量路径为 %.1fmm（印章方向或缩放不一致）", c.label, c.got, c.want)
		}
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
