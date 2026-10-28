package composite

import (
	"math"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

func TestCompositeContentExtent(t *testing.T) {
	ctm := models.CTM{0.3528, 0, 0, 0.3528, 0, 0}
	box := models.StBox{Width: 204.0008, Height: 264.0013}
	w, h, ok := ContentExtent(models.CompositeObject{CtComposite: models.CtComposite{
		CTGraphicUnit: models.CTGraphicUnit{CTM: &ctm, Boundary: box},
	}})
	if !ok || math.Abs(w-578.2) > 0.5 || math.Abs(h-748.3) > 0.5 {
		t.Fatalf("轴向 CTM 反推范围错误: %g,%g,%v", w, h, ok)
	}
	// 无 CTM 时以 Boundary 识别声明为整页的小印章内容。
	w, h, ok = ContentExtent(models.CompositeObject{CtComposite: models.CtComposite{CTGraphicUnit: models.CTGraphicUnit{Boundary: box}}})
	if !ok || w != box.Width || h != box.Height {
		t.Fatal("无 CTM 时应以 Boundary 作为内容范围")
	}
	for _, bad := range []models.CTM{
		{0.3528, 0.2, -0.2, 0.3528, 0, 0}, {}, {1, 0, 0, 1, math.NaN(), 0},
	} {
		if _, _, ok := ContentExtent(models.CompositeObject{CtComposite: models.CtComposite{CTGraphicUnit: models.CTGraphicUnit{CTM: &bad, Boundary: box}}}); ok {
			t.Fatalf("不应从矩阵 %v 反推内容范围", bad)
		}
	}
}

func TestCompositeExtentMismatch(t *testing.T) {
	for _, tc := range []struct {
		declared, extent float64
		want             bool
	}{
		{1639.19, 578.23, true}, // y.ofd 的声明尺寸约为反推范围的 2.83 倍。
		{2653.63, 936.08, true}, // intro.ofd 的同类兼容场景。
		{200, 200, false}, {200, 198, false}, {200, 150, true}, {0, 578, false},
	} {
		if got := ExtentMismatch(tc.declared, tc.extent); got != tc.want {
			t.Fatalf("尺寸误差判定错误: %+v, got=%v", tc, got)
		}
	}
}

func TestResamplePlanFlipsImageBoundsAndRetainsMargins(t *testing.T) {
	// 25.4 DPI 下像素与毫米一一对应：图像 Y=10..20 对应画布 Y=80..90。
	canvasBox, dpi, ok := Resample(geom.Rect{X0: 20, Y0: 10, X1: 30, Y1: 20}, 100, 100, 25.4, models.StBox{Width: 40}, 100)
	want := geom.Rect{X0: 19.3, Y0: 79.3, X1: 30.7, Y1: 90.7}
	if !ok || dpi != 400 || math.Abs(canvasBox.X0-want.X0) > 1e-9 || math.Abs(canvasBox.Y0-want.Y0) > 1e-9 || math.Abs(canvasBox.X1-want.X1) > 1e-9 || math.Abs(canvasBox.Y1-want.Y1) > 1e-9 {
		t.Fatalf("重采样计划错误: %+v DPI=%g ok=%v", canvasBox, dpi, ok)
	}
	_, dpi, ok = Resample(geom.Rect{X1: 1, Y1: 1}, 100, 100, 25.4, models.StBox{Width: 40}, 100)
	if !ok || dpi != 1200 {
		t.Fatal("重采样 DPI 未受 1200 上限约束")
	}
	for _, ink := range []geom.Rect{{}, {X1: 100, Y1: 100}} {
		if _, _, ok := Resample(ink, 100, 100, 25.4, models.StBox{Width: 40}, 100); ok {
			t.Fatal("空白或无收益内容不应重采样")
		}
	}
	if _, _, ok := Resample(geom.Rect{X1: 10, Y1: 10}, 100, 100, 100, models.StBox{Width: 1}, 100); ok {
		t.Fatal("分辨率无提升时不应重采样")
	}
}
