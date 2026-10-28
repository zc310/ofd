// Package pathbuild 将 OFD 路径命令及对象坐标转换为只读绘制路径。
// 路径构造只依赖模型与几何类型，不持有文档资源或绘制后端。
package pathbuild

import (
	"math"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

// objectPointTransform 先应用对象 CTM，再叠加 Boundary 偏移并翻转 Y 轴。
func objectPointTransform(object models.PathObject, pageHeight float64) func(models.StPos) geom.Point {
	return func(point models.StPos) geom.Point {
		if object.CTM != nil {
			point.X, point.Y = object.CTM.TransformPoint(point)
		}
		return geom.Point{X: point.X + object.Boundary.X, Y: pageHeight - (point.Y + object.Boundary.Y)}
	}
}

// Object 将路径对象映射到画布坐标，叠加可选的父级变换。
// 无效矩阵或边界产生空路径，组合矩阵只计算一次。
func Object(object models.PathObject, pageHeight float64, parentCTM *models.CTM) *geom.Path {
	if !object.CTM.IsFinite() || !parentCTM.IsFinite() || !object.Boundary.IsFinite() || !finiteFloat(pageHeight) {
		return &geom.Path{}
	}
	// Pattern 的 CellContent 边界属于父级图块的坐标系。
	// 父级与对象矩阵只组合一次，避免每个点重复执行矩阵乘法与分配。
	var ctm *models.CTM
	if parentCTM != nil {
		ctm = parentCTM
		if object.CTM != nil {
			ctm = parentCTM.Multiply(object.CTM)
			if !ctm.IsFinite() {
				return &geom.Path{}
			}
		}
	}
	box := object.Boundary
	objectTransform := objectPointTransform(object, pageHeight)
	transform := func(pt models.StPos) (float64, float64) {
		if ctm != nil {
			x, y := ctm.Transform(pt.X, pt.Y)
			return x + box.X, pageHeight - (y + box.Y)
		}
		point := objectTransform(pt)
		return point.X, point.Y
	}
	return New(&object.CtPath, transform)
}

// New 按给定仿射坐标变换构造路径，跳过参数缺失或坐标非有限的命令。
// 圆弧的半径、轴向及扫掠方向与端点一起变换。
func New(cp *models.CtPath, transform func(pt models.StPos) (float64, float64)) *geom.Path {
	pa := &geom.Path{}
	if cp == nil || transform == nil {
		return pa
	}
	var current, start models.StPos
	for _, cmd := range cp.AbbreviatedData {
		switch cmd.Type {
		case models.MoveTo, models.Start:
			if len(cmd.Points) < 1 {
				continue
			}
			x, y := transform(cmd.Points[0])
			if !finiteFloat(x) || !finiteFloat(y) {
				continue
			}
			pa.MoveTo(x, y)
			current, start = cmd.Points[0], cmd.Points[0]

		case models.LineTo:
			if len(cmd.Points) < 1 {
				continue
			}
			x, y := transform(cmd.Points[0])
			if !finiteFloat(x) || !finiteFloat(y) {
				continue
			}
			pa.LineTo(x, y)
			current = cmd.Points[0]

		case models.QuadTo:
			if len(cmd.Points) < 2 {
				continue
			}
			cpx, cpy := transform(cmd.Points[0])
			x, y := transform(cmd.Points[1])
			if !finiteFloat(cpx) || !finiteFloat(cpy) || !finiteFloat(x) || !finiteFloat(y) {
				continue
			}
			pa.QuadTo(cpx, cpy, x, y)
			current = cmd.Points[1]

		case models.CubicBezier:
			if len(cmd.Points) < 3 {
				continue
			}
			x1, y1 := transform(cmd.Points[0])
			x2, y2 := transform(cmd.Points[1])
			x3, y3 := transform(cmd.Points[2])
			if !finiteFloat(x1) || !finiteFloat(y1) || !finiteFloat(x2) || !finiteFloat(y2) || !finiteFloat(x3) || !finiteFloat(y3) {
				continue
			}
			pa.CubeTo(x1, y1, x2, y2, x3, y3)
			current = cmd.Points[2]

		case models.ArcTo:
			if cmd.Arc == nil {
				continue
			}
			endX, endY := transform(cmd.Arc.EndPoint)
			if !finiteFloat(cmd.Arc.RX) || !finiteFloat(cmd.Arc.RY) || !finiteFloat(cmd.Arc.XAxisRotation) || !finiteFloat(endX) || !finiteFloat(endY) {
				continue
			}
			// GB/T 33190—2016 §8.1.5、§9.5：CTM 作用于整条椭圆弧，
			// 不仅是终点。先在局部空间构弧，再变换半径、轴向与扫掠方向。
			arc := &geom.Path{}
			arc.MoveTo(current.X, current.Y)
			arc.ArcTo(cmd.Arc.RX, cmd.Arc.RY, cmd.Arc.XAxisRotation,
				cmd.Arc.LargeArcFlag, cmd.Arc.SweepFlag, cmd.Arc.EndPoint.X, cmd.Arc.EndPoint.Y)
			ox, oy := transform(models.StPos{})
			xx, xy := transform(models.StPos{X: 1})
			yx, yy := transform(models.StPos{Y: 1})
			matrix := geom.Matrix{{xx - ox, yx - ox, ox}, {xy - oy, yy - oy, oy}}
			if !finiteMatrix(matrix) {
				continue
			}
			if geom.Equal(matrix.Det(), 0) {
				arc = arc.ReplaceArcs()
			}
			arc.Transform(matrix)
			scanner := arc.Scanner()
			for scanner.Scan() {
				switch scanner.Cmd() {
				case geom.ArcToCmd:
					rx, ry, angle, large, sweep := scanner.Arc()
					if finiteFloat(rx) && finiteFloat(ry) && finiteFloat(angle) {
						pa.ArcTo(rx, ry, angle, large, sweep, endX, endY)
					}
				case geom.CubeToCmd:
					c1, c2, end := scanner.CP1(), scanner.CP2(), scanner.End()
					pa.CubeTo(c1.X, c1.Y, c2.X, c2.Y, end.X, end.Y)
				case geom.LineToCmd:
					pa.LineTo(endX, endY)
				}
			}
			current = cmd.Arc.EndPoint

		case models.Close:
			pa.Close()
			current = start
		}
	}
	return pa
}

func finiteFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func finiteMatrix(matrix geom.Matrix) bool {
	for _, row := range matrix {
		for _, value := range row {
			if !finiteFloat(value) {
				return false
			}
		}
	}
	return true
}
