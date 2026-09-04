package render

import (
	"math"
	"strconv"
	"strings"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/models"
)

const maxPatternTiles = 100000

// drawPatternPath 将 CellContent 直接作为矢量对象绘制。图案单元使用自身的
// 左上角坐标系，而目标画布使用左下角坐标系；应用图块 CTM 后，页面高度转换
// 由对象绘制函数完成。
func (p *Document) drawPatternPath(ctx *canvas.Context, path *canvas.Path, pattern *models.CtPattern, object models.PathObject, pb models.StBox, parentCTM *models.CTM) bool {
	if pattern == nil || pattern.Width <= 0 || pattern.Height <= 0 || path == nil || len(pattern.CellContent.Items) == 0 {
		return false
	}

	xStep, yStep := patternSteps(pattern)
	if xStep <= 0 || yStep <= 0 {
		return false
	}

	patternTransform, ok := patternCTM(pattern)
	if !ok {
		return false
	}

	originX, originY := object.Boundary.X, object.Boundary.Y
	if strings.EqualFold(pattern.RelativeTo, "Page") {
		originX, originY = 0, 0
	}
	base := models.IdentityMatrix
	if parentCTM != nil {
		base = *parentCTM
	}
	if object.CTM != nil {
		base = *base.Multiply(object.CTM)
	}
	// 对于 CellContent，Boundary 位于对象 CTM 之外，因此要在组合父级变换和
	// 对象变换后再进行平移。
	base = *translationMatrix(originX, originY).Multiply(&base)
	base = *base.Multiply(&patternTransform)
	inverse, ok := invertCTM(base)
	if !ok {
		return false
	}

	// 将裁剪边界从画布坐标（Y 轴向上）转换为 OFD 坐标（Y 轴向下），然后在
	// 单元坐标系中计算所需的图块索引。
	bounds := path.FastBounds()
	points := []models.StPos{
		{X: bounds.X0, Y: pb.Height - bounds.Y0},
		{X: bounds.X1, Y: pb.Height - bounds.Y0},
		{X: bounds.X1, Y: pb.Height - bounds.Y1},
		{X: bounds.X0, Y: pb.Height - bounds.Y1},
	}
	minX, maxX := math.Inf(1), math.Inf(-1)
	minY, maxY := math.Inf(1), math.Inf(-1)
	for _, point := range points {
		x, y := inverse.TransformPoint(point)
		minX = math.Min(minX, x)
		maxX = math.Max(maxX, x)
		minY = math.Min(minY, y)
		maxY = math.Max(maxY, y)
	}
	startX := int(math.Floor(minX/xStep)) - 1
	endX := int(math.Ceil(maxX/xStep)) + 1
	startY := int(math.Floor(minY/yStep)) - 1
	endY := int(math.Ceil(maxY/yStep)) + 1
	countX, countY := endX-startX+1, endY-startY+1
	if countX <= 0 || countY <= 0 || countX > maxPatternTiles || countY > maxPatternTiles || countX > maxPatternTiles/countY {
		return false
	}

	for ix := startX; ix <= endX; ix++ {
		for iy := startY; iy <= endY; iy++ {
			tile := *base.Multiply(&models.CTM{
				1, 0, 0, 1,
				float64(ix) * xStep,
				float64(iy) * yStep,
			})
			reflection := patternReflection(pattern.ReflectMethod, pattern.Width, pattern.Height, ix, iy)
			tile = *tile.Multiply(&reflection)
			p.drawItemsWithTransform(ctx, pattern.CellContent.Items, nil, pb, &tile, path)
		}
	}
	return true
}

func patternSteps(pattern *models.CtPattern) (float64, float64) {
	if pattern == nil {
		return 0, 0
	}
	xStep, yStep := pattern.XStep, pattern.YStep
	if xStep <= 0 || xStep < pattern.Width {
		xStep = pattern.Width
	}
	if yStep <= 0 || yStep < pattern.Height {
		yStep = pattern.Height
	}
	return xStep, yStep
}

func patternReflection(method string, width, height float64, ix, iy int) models.CTM {
	flipX, flipY := false, false
	switch strings.ToLower(method) {

	case "row":
		flipX = iy&1 != 0 // 只在奇数行水平翻转
	case "column":
		flipX = ix&1 != 0 // 只在奇数列垂直翻转
	case "rowandcolumn":
		// 两个方向都翻转，产生四象限对称图案
		flipX = ix&1 != 0
		flipY = iy&1 != 0
	}
	reflection := models.IdentityMatrix
	if flipX {
		reflection = *reflection.Multiply(&models.CTM{-1, 0, 0, 1, width, 0})
	}
	if flipY {
		reflection = *reflection.Multiply(&models.CTM{1, 0, 0, -1, 0, height})
	}
	return reflection
}

func translationMatrix(x, y float64) *models.CTM {
	return &models.CTM{1, 0, 0, 1, x, y}
}

func patternCTM(pattern *models.CtPattern) (models.CTM, bool) {
	if pattern == nil || len(pattern.CTM) == 0 {
		return models.IdentityMatrix, true
	}
	values := make([]float64, 0, len(pattern.CTM))
	for _, value := range pattern.CTM {
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return models.IdentityMatrix, false
		}
		values = append(values, v)
	}
	if len(values) != 6 || values[0]*values[3]-values[1]*values[2] == 0 {
		return models.IdentityMatrix, false
	}
	return models.CTM{values[0], values[1], values[2], values[3], values[4], values[5]}, true
}

func invertCTM(matrix models.CTM) (models.CTM, bool) {
	determinant := matrix[0]*matrix[3] - matrix[1]*matrix[2]
	if determinant == 0 || math.IsNaN(determinant) || math.IsInf(determinant, 0) {
		return models.IdentityMatrix, false
	}
	return models.CTM{
		matrix[3] / determinant,
		-matrix[1] / determinant,
		-matrix[2] / determinant,
		matrix[0] / determinant,
		(matrix[2]*matrix[5] - matrix[3]*matrix[4]) / determinant,
		(matrix[1]*matrix[4] - matrix[0]*matrix[5]) / determinant,
	}, true
}

// 保留 patternMatrix，供测试以及需要将 OFD CTM 表示为 canvas 矩阵的调用方使用。
func patternMatrix(pattern *models.CtPattern) (canvas.Matrix, bool) {
	ctm, ok := patternCTM(pattern)
	if !ok {
		return canvas.Identity, false
	}
	return canvas.Matrix{{ctm[0], ctm[2], ctm[4]}, {ctm[1], ctm[3], ctm[5]}}, true
}
