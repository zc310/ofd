// Package pattern 按 OFD 底纹属性计算平铺范围及图块变换。
// 本包只依赖模型与几何，不绘制 CellContent，也不持有文档资源或绘制预算。
package pattern

import (
	"math"
	"strings"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

// MaxTiles 限制一次平铺计划的图块数，防止无界展开。
const MaxTiles = 100000

// Status 区分有效计划、无效几何与图块数量超限。
type Status uint8

const (
	// Invalid 表示无法构造有效的坐标变换。
	Invalid Status = iota
	// Ready 表示计划可以用于平铺。
	Ready
	// LimitExceeded 表示索引或数量超限，调用方可记录预算诊断。
	LimitExceeded
)

// Plan 是只读平铺计划；Columns 与 Rows 方法也用于数量超限时的诊断。
// 零值与未就绪的计划不提供图块变换。
type Plan struct {
	columns, rows  int
	startX, startY int
	xStep, yStep   float64
	base           models.CTM
	reflections    [2][2]models.CTM
	ready          bool
}

// Columns 返回平铺计划的列数。
func (p Plan) Columns() int { return p.columns }

// Rows 返回平铺计划的行数。
func (p Plan) Rows() int { return p.rows }

// New 按 GB/T 33190—2016 §8.3.3 表28 计算底纹步长、原点、镜像与 CTM。
// bounds 使用画布 Y 向上坐标；图块矩阵使用 OFD Y 向下坐标。
func New(pattern *models.CtPattern, object models.CTGraphicUnit, pb models.StBox, bounds geom.Rect, parentCTM *models.CTM) (Plan, Status) {
	var plan Plan
	if pattern == nil || pattern.Width <= 0 || pattern.Height <= 0 || !finiteFloat(pattern.Width) || !finiteFloat(pattern.Height) ||
		!pb.IsFinite() {
		return plan, Invalid
	}

	xStep, yStep := patternSteps(pattern)
	if xStep <= 0 || yStep <= 0 {
		return plan, Invalid
	}

	patternTransform, ok := patternCTM(pattern)
	if !ok {
		return plan, Invalid
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
		if !base.IsFinite() {
			return plan, Invalid
		}
	}
	// 对于 CellContent，Boundary 位于对象 CTM 之外，因此要在组合父级变换和
	// 对象变换后再进行平移。
	base = *translationMatrix(originX, originY).Multiply(&base)
	base = *base.Multiply(&patternTransform)
	if !base.IsFinite() {
		return plan, Invalid
	}
	inverse, ok := invertCTM(base)
	if !ok {
		return plan, Invalid
	}

	// 将裁剪边界从画布坐标（Y 轴向上）转换为 OFD 坐标（Y 轴向下），然后在
	// 单元坐标系中计算所需的图块索引。
	if !finiteFloat(bounds.X0) || !finiteFloat(bounds.Y0) || !finiteFloat(bounds.X1) || !finiteFloat(bounds.Y1) {
		return plan, Invalid
	}
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
		if !finiteFloat(x) || !finiteFloat(y) {
			return plan, Invalid
		}
		minX = math.Min(minX, x)
		maxX = math.Max(maxX, x)
		minY = math.Min(minY, y)
		maxY = math.Max(maxY, y)
	}
	startX, countX, okX := patternTileRange(minX, maxX, xStep)
	startY, countY, okY := patternTileRange(minY, maxY, yStep)
	plan.columns, plan.rows = countX, countY
	if !okX || !okY || countX > MaxTiles/countY {
		return plan, LimitExceeded
	}
	plan.startX, plan.startY = startX, startY
	plan.xStep, plan.yStep, plan.base = xStep, yStep, base

	// 翻转只由行列的奇偶决定，四个矩阵预先构造，避免逐图块转小写和求乘积。
	for ix := range 2 {
		for iy := range 2 {
			plan.reflections[ix][iy] = patternReflection(pattern.ReflectMethod, pattern.Width, pattern.Height, ix, iy)
		}
	}
	plan.ready = true
	return plan, Ready
}

// Tile 返回相对计划起点的某列某行的变换。未就绪计划、负数或越界索引，
// 以及逐图块平移溢出时返回 false。
func (p *Plan) Tile(column, row int) (models.CTM, bool) {
	if !p.ready || column < 0 || column >= p.columns || row < 0 || row >= p.rows {
		return models.CTM{}, false
	}
	ix, iy := p.startX+column, p.startY+row
	tile := patternTileTransform(p.base, p.reflections[ix&1][iy&1], float64(ix)*p.xStep, float64(iy)*p.yStep)
	return tile, tile.IsFinite()
}

// 在浮点转 int 前校验商、索引及数量。只检查 countX*countY 太晚：Inf 转换
// 及索引加减可能已溢出；32位平台还必须避免末索引递增绕回负数。
func patternTileRange(minimum, maximum, step float64) (int, int, bool) {
	if !finiteFloat(minimum) || !finiteFloat(maximum) || !finiteFloat(step) || step <= 0 || minimum > maximum {
		return 0, 0, false
	}
	first, last := math.Floor(minimum/step), math.Ceil(maximum/step)
	limit := math.Min(float64(int(^uint(0)>>1)-2), float64(1<<53-2))
	if !finiteFloat(first) || !finiteFloat(last) || first < -limit || last > limit {
		return 0, 0, false
	}
	tiles := last - first + 3
	if tiles <= 0 || tiles > MaxTiles {
		return 0, 0, false
	}
	return int(first) - 1, int(tiles), true
}

func patternTileTransform(base, reflection models.CTM, x, y float64) models.CTM {
	// 平铺位移仍从索引直接计算，不累加步长，避免大量图块产生浮点漂移。
	tx := base[0]*x + base[2]*y + base[4]
	ty := base[1]*x + base[3]*y + base[5]
	// patternReflection 只有轴向翻转和平移，按值组合，避免 Multiply 返回指针分配。
	return models.CTM{
		base[0] * reflection[0], base[1] * reflection[0],
		base[2] * reflection[3], base[3] * reflection[3],
		base[0]*reflection[4] + base[2]*reflection[5] + tx,
		base[1]*reflection[4] + base[3]*reflection[5] + ty,
	}
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
		// 奇数行把单元上下镜像（y -> height-y），相邻两行内容互为上下翻转
		flipY = iy&1 != 0
	case "column":
		// 奇数列把单元左右镜像（x -> width-x），相邻两列内容互为左右翻转
		flipX = ix&1 != 0
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
	// CTM 由 models.CTM 在 XML 解析时完成有限性校验；nil 表示未指定，按单位矩阵。
	if pattern == nil || pattern.CTM == nil {
		return models.IdentityMatrix, true
	}
	ctm := *pattern.CTM
	if !ctm.IsInvertible() {
		return models.IdentityMatrix, false
	}
	return ctm, true
}

func invertCTM(matrix models.CTM) (models.CTM, bool) {
	if !matrix.IsFinite() {
		return models.IdentityMatrix, false
	}
	determinant := matrix[0]*matrix[3] - matrix[1]*matrix[2]
	if determinant == 0 || math.IsNaN(determinant) || math.IsInf(determinant, 0) {
		return models.IdentityMatrix, false
	}
	inverse := models.CTM{
		matrix[3] / determinant,
		-matrix[1] / determinant,
		-matrix[2] / determinant,
		matrix[0] / determinant,
		(matrix[2]*matrix[5] - matrix[3]*matrix[4]) / determinant,
		(matrix[1]*matrix[4] - matrix[0]*matrix[5]) / determinant,
	}
	if !inverse.IsFinite() {
		return models.IdentityMatrix, false
	}
	return inverse, true
}

func finiteFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
