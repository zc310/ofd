package canvas

import (
	"image"
	"math"
	"testing"

	"github.com/tdewolff/canvas"

	"github.com/zc310/ofd/internal/render/testscene"
)

// TestCanvasBackendParity 验证 canvasBackend 对 canvas.Context 的逐方法
// 转发与直接绘制输出逐像素一致：同一场景分别用 *canvas.Context（现有
// 调用方式）和 DrawContext 绘制，栅格化后必须字节相等。
func TestCanvasBackendParity(t *testing.T) {
	fontPath := testscene.PickTestFont()
	if fontPath == "" {
		t.Skip("未找到可用测试字体")
	}

	direct := drawPageDirect(t, fontPath)
	viaBackend := drawPageBackend(t, fontPath)

	if len(direct.Pix) != len(viaBackend.Pix) {
		t.Fatalf("尺寸不一致: %d vs %d", len(direct.Pix), len(viaBackend.Pix))
	}
	ink := 0
	for i := 0; i < len(direct.Pix); i += 4 {
		if direct.Pix[i] != 255 || direct.Pix[i+1] != 255 || direct.Pix[i+2] != 255 {
			ink++
		}
	}
	if ink == 0 {
		t.Fatal("场景内容为空，对比无意义")
	}
	diff := 0
	for i := range direct.Pix {
		if direct.Pix[i] != viaBackend.Pix[i] {
			diff++
		}
	}
	if diff != 0 {
		t.Fatalf("canvasBackend 与直接绘制存在 %d 字节差异", diff)
	}
}

func drawPageDirect(t *testing.T, fontPath string) *image.RGBA {
	c := canvas.New(200, 140)
	ctx := canvas.NewContext(c)
	ctx.SetCoordSystem(canvas.CartesianIV)
	testscene.SceneDirect(t, ctx, fontPath)
	return rasterize(c, canvas.DPI(96), canvas.DefaultColorSpace)
}

func drawPageBackend(t *testing.T, fontPath string) *image.RGBA {
	c := canvas.New(200, 140)
	ctx := canvas.NewContext(c)
	ctx.SetCoordSystem(canvas.CartesianIV)
	testscene.SceneBackend(t, newCanvasBackend(ctx), fontPath)
	return rasterize(c, canvas.DPI(96), canvas.DefaultColorSpace)
}

// TestCanvasBackendSetDashesCompensatesStrokeWidth 保护 canvasBackend 对
// canvas 比例虚线语义的折算：OFD 的 DashPattern/DashOffset 以毫米为单位、与
// LineWidth 无关，而 canvas 各渲染器输出前会按线宽缩放虚线，因此 SetDashes
// 必须先除以线宽，否则线宽 4 配 DashPattern="6 3" 会被放大成 24/12。
func TestCanvasBackendSetDashesCompensatesStrokeWidth(t *testing.T) {
	c := canvas.New(200, 20)
	ctx := canvas.NewContext(c)
	b := newCanvasBackend(ctx)

	ctx.SetStrokeWidth(4)
	b.SetDashes(2, 6, 3)

	if got, want := ctx.Style.DashOffset, 0.5; !canvas.Equal(got, want) {
		t.Errorf("相位未按线宽折算: got %v, want %v", got, want)
	}
	want := []float64{1.5, 0.75}
	if got := ctx.Style.Dashes; len(got) != len(want) ||
		!canvas.Equal(got[0], want[0]) || !canvas.Equal(got[1], want[1]) {
		t.Errorf("虚线数组未按线宽折算: got %v, want %v", got, want)
	}
}

// TestCanvasBackendSetDashesRendersAbsolutePeriod 保护折算后的实际绘制结果：
// 画布上的虚线周期应等于 OFD 指定的 DashPattern 之和（毫米），而不是被
// canvas 乘上线宽后的长度。用细线加 Butt 端点，避免圆头把空隙盖住。
func TestCanvasBackendSetDashesRendersAbsolutePeriod(t *testing.T) {
	const (
		lineWidth  = 0.5
		dash       = 6.0
		gap        = 3.0
		startX     = 10.0
		endX       = 190.0
		centerY    = 10.0
		wantPeriod = dash + gap
	)
	c := canvas.New(200, 20)
	ctx := canvas.NewContext(c)
	b := newCanvasBackend(ctx)

	ctx.SetStrokeColor(canvas.RGBA(0, 0, 1, 1))
	ctx.SetStrokeWidth(lineWidth)
	ctx.SetStrokeCapper(canvas.ButtCap)
	b.SetDashes(0, dash, gap)

	p := &canvas.Path{}
	p.MoveTo(startX, centerY)
	p.LineTo(endX, centerY)
	ctx.DrawPath(0, 0, p)

	res := canvas.DPI(96)
	img := rasterize(c, res, canvas.DefaultColorSpace)

	// 找墨水最多的行作为扫描线，再取该行上各段墨水的起点求周期。
	best, bestCount := 0, 0
	for y := 0; y < img.Bounds().Dy(); y++ {
		count := 0
		for x := 0; x < img.Bounds().Dx(); x++ {
			if inkPixel(img, x, y) {
				count++
			}
		}
		if count > bestCount {
			best, bestCount = y, count
		}
	}
	if bestCount == 0 {
		t.Fatal("未扫描到墨水，场景无效")
	}

	var starts []float64
	for x := 0; x < img.Bounds().Dx(); x++ {
		if inkPixel(img, x, best) && (x == 0 || !inkPixel(img, x-1, best)) {
			starts = append(starts, mmPerPixel*float64(x))
		}
	}
	if len(starts) < 3 {
		t.Fatalf("虚线段数过少: %d，无法测量周期", len(starts))
	}

	var sum float64
	for i := 1; i < len(starts); i++ {
		sum += starts[i] - starts[i-1]
	}
	period := sum / float64(len(starts)-1)

	if math.Abs(period-wantPeriod) > 0.3 {
		t.Errorf("虚线周期 %.2fmm，期望 %.2fmm（canvas 若未折算会得到 %.2fmm）",
			period, wantPeriod, wantPeriod*lineWidth*4)
	}
}

const mmPerPixel = 25.4 / 96.0

func inkPixel(img *image.RGBA, x, y int) bool {
	if !(image.Point{X: x, Y: y}).In(img.Bounds()) {
		return false
	}
	r, g, b, _ := img.At(x, y).RGBA()
	return b > 30000 && r < 60000 && g < 60000
}
