package canvas

import (
	"bytes"
	"image"
	"math"
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
)

// 高DPI按设备下限绘制细线，低DPI按300 DPI参考设备换算。
// 用透明背景上的累计覆盖率测量宽度，避免依赖线条恰好落在哪一行像素上。
func TestRasterizerMinimumPositiveStrokeWidth(t *testing.T) {
	for _, dpi := range []float64{72, 96, 150, 300, 600} {
		for _, scale := range []float64{0.3528, 1, 9} {
			resolution := canvas.DPI(dpi)
			c := canvas.New(20, 10)
			ctx := canvas.NewContext(c)
			ctx.SetFillColor(canvas.Transparent)
			ctx.SetStrokeColor(canvas.Black)
			ctx.SetStrokeCapper(canvas.ButtCap)
			ctx.SetStrokeWidth(0.002)
			ctx.Scale(scale, scale)
			path := &canvas.Path{}
			path.MoveTo(2/scale, 5/scale)
			path.LineTo(18/scale, 5/scale)
			ctx.DrawPath(0, 0, path)
			img := rasterize(c, resolution, canvas.DefaultColorSpace)
			coverage := strokeColumnCoverage(img, int(10*resolution.DPMM()))
			want := min(2, dpi/150)
			if math.Abs(coverage-want) > 0.03 {
				t.Errorf("DPI=%g 缩放=%g：描边覆盖宽度 %.3f 像素，期望%.3f像素", dpi, scale, coverage, want)
			}
		}
	}
}

func strokeColumnCoverage(img *image.RGBA, x int) float64 {
	var alpha int
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		alpha += int(img.RGBAAt(x, y).A)
	}
	return float64(alpha) / 255
}

func TestMinimumStrokeKeepsDashLengthsAndSourceStyle(t *testing.T) {
	c := canvas.New(20, 10)
	ctx := canvas.NewContext(c)
	b := newCanvasBackend(ctx)
	b.ClearFill()
	b.SetStrokeColor(canvas.Black)
	ctx.SetStrokeCapper(canvas.ButtCap)
	b.SetStrokeWidth(0.002)
	b.SetDashes(0, 2, 2)
	path := &canvas.Path{}
	path.MoveTo(2, 5)
	path.LineTo(18, 5)
	// 直接记录样式，避免 canvas.Context 在入队时按未换算的比例虚线长度
	// 提前将短路径判断为完整实线；此处只验证栅格化调整不改变已有虚线。
	c.RenderPath(path, ctx.Style, canvas.Identity)
	var vectorBefore, vectorAfter bytes.Buffer
	surface := canvasVectorSurface{c: c}
	if err := surface.Write(&vectorBefore, "svg"); err != nil {
		t.Fatal(err)
	}
	resolution := canvas.DPMM(10)
	want := 254.0 / 150
	img := rasterize(c, resolution, canvas.DefaultColorSpace)
	for _, x := range []int{30, 70, 110, 150} {
		if got := strokeColumnCoverage(img, x); math.Abs(got-want) > 0.03 {
			t.Errorf("虚线实线段 x=%d：覆盖 %.3f 像素，期望%.3f像素", x, got, want)
		}
	}
	for _, x := range []int{50, 90, 130, 170} {
		if got := strokeColumnCoverage(img, x); got != 0 {
			t.Errorf("虚线间隙 x=%d 被描边覆盖：%.3f 像素", x, got)
		}
	}
	if ctx.Style.StrokeWidth != 0.002 || len(ctx.Style.Dashes) != 2 || ctx.Style.Dashes[0] != 1000 {
		t.Fatal("栅格化改写了共享画布样式")
	}
	if err := surface.Write(&vectorAfter, "svg"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(vectorBefore.Bytes(), vectorAfter.Bytes()) {
		t.Fatal("栅格化改写了后续矢量输出的线宽或虚线")
	}
	// 相位仍按原来的毫米长度偏移：偏移半个周期后，实线段与间隙交换。
	phaseCanvas := canvas.New(20, 10)
	phaseStyle := ctx.Style
	phaseStyle.DashOffset = 1000 // 对应原始线宽下的2毫米。
	phaseCanvas.RenderPath(path, phaseStyle, canvas.Identity)
	phaseImage := rasterize(phaseCanvas, resolution, canvas.DefaultColorSpace)
	if strokeColumnCoverage(phaseImage, 30) != 0 || math.Abs(strokeColumnCoverage(phaseImage, 50)-want) > 0.03 {
		t.Fatal("最小线宽改变了虚线相位")
	}
	// 大于两个像素的线宽保持实际尺寸。
	c = canvas.New(20, 10)
	ctx = canvas.NewContext(c)
	ctx.SetFillColor(canvas.Transparent)
	ctx.SetStrokeColor(canvas.Black)
	ctx.SetStrokeWidth(0.5)
	ctx.DrawPath(0, 0, path)
	if got := strokeColumnCoverage(rasterize(c, resolution, canvas.DefaultColorSpace), 100); math.Abs(got-5) > 0.03 {
		t.Errorf("正常线宽被改写：覆盖 %.3f 像素，期望5像素", got)
	}
}

// zsbk.ofd 首页的“建设单位”由普通宋体文字和相邻的字形轮廓描边叠加。
// 这些描边的源线宽为0.002，CTM再缩小到0.3528，不能在图片里几乎消失。
func TestZsbkConstructionUnitOutlineStaysVisible(t *testing.T) {
	ofd, err := parser.NewOFD("../../../../testdata/ofdrw/zsbk.ofd")
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := ofd.Documents[0]
	template, err := doc.LoadTemplate(2)
	if err != nil {
		t.Fatal(err)
	}
	if template == nil || template.Content == nil {
		t.Fatal("缺少首页背景模板")
	}
	resolution := geom.DPI(150)
	renderDoc := render.NewDocumentWithDPI(nil, doc, resolution)
	defer renderDoc.Close()
	wanted := map[models.StID]bool{175: true, 177: true, 179: true, 181: true}
	var visit func([]models.PageItem)
	visit = func(items []models.PageItem) {
		for _, item := range items {
			if item.Kind == models.PageItemBlock {
				visit(item.Block.Items)
				continue
			}
			if item.Kind != models.PageItemPath || !wanted[item.Path.ID] {
				continue
			}
			b := newCanvasRasterBackend(297, 210, resolution)
			renderDoc.Path(b, *item.Path, nil, models.StBox{Width: 297, Height: 210})
			img := b.Raster()
			var alpha uint64
			for i := 3; i < len(img.Pix); i += 4 {
				alpha += uint64(img.Pix[i])
			}
			if coverage := float64(alpha) / 255; coverage < 50 {
				t.Errorf("字形描边 %d 仅覆盖 %.2f 个不透明像素，应清晰可见", item.Path.ID, coverage)
			}
			delete(wanted, item.Path.ID)
		}
	}
	for _, layer := range template.Content.Layer {
		if layer != nil {
			visit(layer.Items)
		}
	}
	if len(wanted) > 0 {
		t.Fatalf("缺少待验证的字形描边：%v", wanted)
	}
}
