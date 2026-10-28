package paint

import (
	"image/color"
	"math"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

func TestTranslatedGradientPreservesPageCoordinates(t *testing.T) {
	gradient := NewLinearGradient(&models.CTAxialShd{
		StartPoint: models.StPos{X: 10, Y: 20},
		EndPoint:   models.StPos{X: 20, Y: 20},
		Segment: []models.Segment{
			{Position: 0, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}},
			{Position: 1, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}},
		},
	}, identityGradientTransform, nil)
	local := TranslateGradient(gradient, 10, 20)
	if got, want := local.At(5, 0), gradient.At(15, 20); got != want {
		t.Fatalf("局部渐变 = %v，期望页面坐标采样 %v", got, want)
	}
}

func TestNativeRadialTextGradientRejectsEllipticalAndInvalidTransforms(t *testing.T) {
	gradient := geom.NewRadialGradient(geom.Point{}, 0, geom.Point{}, 20)
	for _, matrix := range []geom.Matrix{
		geom.Identity.Scale(2, 1),
		{{1, 0.5, 0}, {0, 1, 0}},
		geom.Identity.Scale(0, 1),
		geom.Identity.Scale(math.Inf(1), 1),
	} {
		if mapped := MapNativeGradient(gradient, matrix); mapped != nil {
			t.Errorf("矩阵 %v 被错误地作为圆形径向渐变处理", matrix)
		}
	}
	for _, gradient := range []*geom.RadialGradient{
		geom.NewRadialGradient(geom.Point{}, 0, geom.Point{X: math.MaxFloat64}, 20),
		geom.NewRadialGradient(geom.Point{}, 0, geom.Point{}, math.MaxFloat64),
	} {
		if mapped := MapNativeGradient(gradient, geom.Identity.Scale(0.5, 0.5)); mapped != nil {
			t.Error("径向渐变换算溢出后仍被交给原生后端")
		}
	}
}

func TestRunAndDeviceGradientsShareBoundaryColors(t *testing.T) {
	gradient := geom.NewLinearGradient(geom.Point{}, geom.Point{X: 10, Y: 5})
	gradient.Grad = geom.Grad{{Offset: 0, Color: color.RGBA{R: 255, A: 255}}, {Offset: 1, Color: color.RGBA{B: 255, A: 255}}}
	want := gradient.At(4, 4)
	// 两个 run 的局部坐标不同，但均指向同一个 Boundary 点 (4,4)。
	first := TextRunGradient(gradient, geom.Point{X: 2, Y: 3}, 2)
	second := TextRunGradient(gradient, geom.Point{X: 0, Y: 6}, 0.5)
	if first.At(1, -1) != want || second.At(8, 2) != want {
		t.Fatal("run 原点或水平缩放导致 Boundary 渐变不连续")
	}
	matrix := geom.Matrix{{2, 0.5, 3}, {1, 3, -2}}
	device, ok := DeviceGradient(gradient, matrix)
	if !ok {
		t.Fatal("有效仿射变换无法用于设备空间采样")
	}
	point := matrix.Dot(geom.Point{X: 4, Y: 4})
	if got := device.At(point.X, point.Y); got != want {
		t.Fatalf("设备空间采样 = %v，期望 Boundary 颜色 %v", got, want)
	}
	for _, invalid := range []geom.Matrix{geom.Identity.Scale(0, 1), geom.Identity.Scale(math.Inf(1), 1)} {
		if _, ok := DeviceGradient(gradient, invalid); ok {
			t.Fatalf("接受了不可用于设备采样的矩阵 %v", invalid)
		}
	}
}

func TestDeviceGradientRejectsNonFiniteMatrixAndInverse(t *testing.T) {
	gradient := geom.NewLinearGradient(geom.Point{}, geom.Point{X: 10})
	// 平移不参与行列式，单独检查 det 无法发现其 NaN/Inf。
	for row := range 2 {
		for column := range 3 {
			for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
				matrix := geom.Identity
				matrix[row][column] = value
				if mapped, ok := DeviceGradient(gradient, matrix); ok || mapped != nil {
					t.Errorf("接受了非有限矩阵: row=%d column=%d value=%g", row, column, value)
				}
			}
		}
	}
	// 原矩阵与行列式有效，但逆变换的平移会溢出。
	matrix := geom.Matrix{{0.5, 0, math.MaxFloat64}, {0, 0.5, 0}}
	if mapped, ok := DeviceGradient(gradient, matrix); ok || mapped != nil {
		t.Error("逆矩阵溢出后仍返回设备空间渐变")
	}
}
