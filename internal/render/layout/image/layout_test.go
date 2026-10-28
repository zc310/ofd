package image

import (
	"math"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

func TestImageMatrixWHVectorEmbedMatchesRasterPlacement(t *testing.T) {
	box := models.StBox{X: 20, Y: 30, Width: 100, Height: 80}
	ctm := models.CTM{2, 0.1, -0.05, 1.5, 5, -3}
	pageHeight := 297.0
	svgW := 100.0
	svgH := 75.0
	dpmm := geom.DPI(96).DPMM()
	imgW := svgW * dpmm
	imgH := svgH * dpmm
	flip := geom.Matrix{{1, 0, 0}, {0, -1, svgH}}

	mRaster := MatrixWH(box, imgW, imgH, ctm, pageHeight)
	mVector := MatrixWH(box, svgW, svgH, ctm, pageHeight).Mul(flip)

	for _, corner := range [][2]float64{{0, 0}, {svgW, 0}, {0, svgH}, {svgW, svgH}} {
		canvasX, canvasY := corner[0], corner[1]
		pixelX := canvasX * dpmm
		pixelY := imgH - canvasY*dpmm
		fromRaster := mRaster.Dot(geom.Point{X: pixelX, Y: pixelY})
		fromVector := mVector.Dot(geom.Point{X: canvasX, Y: canvasY})
		if math.Abs(fromRaster.X-fromVector.X) > 1e-9 || math.Abs(fromRaster.Y-fromVector.Y) > 1e-9 {
			t.Fatalf("画布点 %v 的位图与矢量放置不同: %v, %v", corner, fromRaster, fromVector)
		}
	}
}

// 圆弧应位于弦朝向矩形角点的一侧；凹圆角会落在相反一侧。
func roundedBorderCornerBulge(t *testing.T, width, height, rx, ry float64) float64 {
	t.Helper()
	path := Border(width, height, rx, ry)
	if path.Empty() {
		t.Fatal("圆角边框为空")
	}
	norm := math.Hypot(rx, ry)
	if norm == 0 {
		t.Fatal("零半径不应生成圆弧")
	}
	bulge := 0.0
	for _, point := range path.Flatten(0.01).Coords() {
		if point.X > rx || point.Y < height-ry {
			continue
		}
		dist := (-ry*point.X + rx*(point.Y-(height-ry))) / norm
		bulge = math.Max(bulge, dist)
	}
	return bulge
}

func TestRoundedImageBorderCornersBulgeOutward(t *testing.T) {
	for _, tc := range []struct {
		width, height, rx, ry float64
	}{
		{20, 10, 2, 2}, {70, 46, 12, 12}, {40, 20, 6, 3}, {10, 10, 5, 5},
	} {
		bulge := roundedBorderCornerBulge(t, tc.width, tc.height, tc.rx, tc.ry)
		r := math.Min(math.Min(tc.rx, tc.width/2), math.Min(tc.ry, tc.height/2))
		if bulge < 0.05*r {
			t.Fatalf("圆角 %+v 的凸出量 %g 过小，圆弧可能凹进矩形", tc, bulge)
		}
	}
}

func TestRoundedImageBorderNoRadiusUsesRectangle(t *testing.T) {
	if got := len(Border(20, 10, 0, 0).Flatten(0.01).Coords()); got != 5 {
		t.Fatalf("无圆角边框顶点数=%d，期望 5", got)
	}
}

func TestImageBorderDashesStartAtTopLeftAndRunClockwise(t *testing.T) {
	coords := Border(20, 10, 0, 0).Coords()
	if coords[0] != (geom.Point{Y: 10}) || coords[1] != (geom.Point{X: 20, Y: 10}) {
		t.Fatalf("矩形边框没有从左上角顺时针绕行: %v", coords)
	}
}

func TestImageCTMUsesBoundaryOnlyWhenMissing(t *testing.T) {
	object := models.ImageObject{CtImage: models.CtImage{CTGraphicUnit: models.CTGraphicUnit{
		Boundary: models.StBox{X: 7, Y: 9, Width: 20, Height: 30},
	}}}
	if got := CTM(object); got != (models.CTM{20, 0, 0, 30, 0, 0}) {
		t.Fatalf("缺省图片矩阵错误: %v", got)
	}
	explicit := models.CTM{2, 0.1, 0.3, -3, 4, 5}
	object.CTM = &explicit
	if got := CTM(object); got != explicit {
		t.Fatalf("显式 CTM 被 Boundary 替换: %v", got)
	}
}

func TestClipMapsToPixelsWithoutChangingSharedPath(t *testing.T) {
	matrix := geom.Matrix{{0.2, 0.04, 10}, {0.03, -0.3, 80}}
	want := geom.Rectangle(40, 50).Translate(10, 20)
	shared := want.Copy().Transform(matrix)
	before := shared.Copy()
	got := Clip(shared, matrix, 200, 200)
	if got == nil || !got.Equals(want) {
		t.Fatalf("局部裁剪映射错误: %v", got)
	}
	if !shared.Equals(before) {
		t.Fatal("共享页面裁剪被修改")
	}
	got.Translate(1, 2)
	if !shared.Equals(before) {
		t.Fatal("局部裁剪与共享路径复用了可变数据")
	}
}

func TestClipHandlesInvalidMatricesWithoutInvertingSingularMatrix(t *testing.T) {
	path := geom.Rectangle(10, 10)
	for _, matrix := range []geom.Matrix{
		{}, {{1, 0, math.NaN()}, {0, 1, 0}}, {{1, 0, math.Inf(1)}, {0, 1, 0}},
	} {
		if got := Clip(path, matrix, 20, 20); got != nil {
			t.Fatal("无效或退化放置矩阵应沿用既有跳过裁剪行为")
		}
	}
	matrix := geom.Matrix{{0.1, 0, math.MaxFloat64}, {0, 0.1, math.MaxFloat64}}
	if got := Clip(path, matrix, 20, 20); got == nil || !got.Empty() {
		t.Fatal("逆矩阵溢出时应返回空裁剪，避免泄露原图")
	}
}
