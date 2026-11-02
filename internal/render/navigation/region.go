package navigation

import (
	"math"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

// Region 按 §14.1 表51、§9.3将启动区域转换为所在坐标系的路径。
// 缺省返回 nil（使用对象边界），显式空区域返回空路径，不能扩大为整个对象。
func Region(region *models.CtRegion) *geom.Path {
	if region == nil {
		return nil
	}
	result := &geom.Path{}
	// 启动区域不是绘制主体，超出预算时禁用热区，不能退回整页可点击。
	if len(region.Areas) > 128 {
		return result
	}
	for _, area := range region.Areas {
		if !validRegionArea(area) {
			return &geom.Path{}
		}
		path := &geom.Path{}
		path.MoveTo(area.Start.X, area.Start.Y)
		for _, cmd := range area.Paths {
			switch cmd.XMLName.Local {
			case "Move":
				if cmd.Point1 != nil {
					path.MoveTo(cmd.Point1.X, cmd.Point1.Y)
				}
			case "Line":
				if cmd.Point1 != nil {
					path.LineTo(cmd.Point1.X, cmd.Point1.Y)
				}
			case "QuadraticBezier":
				if cmd.Point1 != nil && cmd.Point2 != nil {
					path.QuadTo(cmd.Point1.X, cmd.Point1.Y, cmd.Point2.X, cmd.Point2.Y)
				}
			case "CubicBezier":
				if cmd.Point1 != nil && cmd.Point2 != nil && cmd.Point3 != nil {
					path.CubeTo(cmd.Point1.X, cmd.Point1.Y, cmd.Point2.X, cmd.Point2.Y, cmd.Point3.X, cmd.Point3.Y)
				}
			case "Arc":
				if cmd.EllipseSize == nil || len(*cmd.EllipseSize) != 2 || cmd.EndPoint == nil {
					continue
				}
				rx, ry := (*cmd.EllipseSize)[0], (*cmd.EllipseSize)[1]
				if !finiteRegionValue(rx) || !finiteRegionValue(ry) || rx < 0 || ry < 0 {
					return &geom.Path{}
				}
				angle := 0.0
				if cmd.RotationAngle != nil {
					angle = *cmd.RotationAngle
				}
				path.ArcTo(rx, ry, angle, cmd.LargeArc != nil && *cmd.LargeArc, cmd.SweepDirection != nil && *cmd.SweepDirection, cmd.EndPoint.X, cmd.EndPoint.Y)
			case "Close":
				path.Close()
			}
		}
		path.Close()
		result = result.Or(path)
	}
	return result
}

func finiteRegionValue(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func validRegionArea(area models.Area) bool {
	if len(area.Paths) > 4096 || !finiteRegionValue(area.Start.X) || !finiteRegionValue(area.Start.Y) {
		return false
	}
	for _, command := range area.Paths {
		for _, point := range []*models.StPos{command.Point1, command.Point2, command.Point3, command.EndPoint} {
			if point != nil && (!finiteRegionValue(point.X) || !finiteRegionValue(point.Y)) {
				return false
			}
		}
		if command.RotationAngle != nil && !finiteRegionValue(*command.RotationAngle) {
			return false
		}
	}
	return true
}

// ActionRegion 返回页面坐标的启动区域；Region 位于图元局部空间，先 CTM 再加 Boundary。
func ActionRegion(action models.CtAction, unit models.CTGraphicUnit) *geom.Path {
	path := Region(action.Region)
	if path == nil {
		box := unit.Boundary
		return geom.Rectangle(abs(box.Width), abs(box.Height)).Translate(min(box.X, box.X+box.Width), min(box.Y, box.Y+box.Height))
	}
	matrix := geom.Identity.Translate(unit.Boundary.X, unit.Boundary.Y)
	if unit.CTM != nil {
		if !unit.CTM.IsFinite() {
			return &geom.Path{}
		}
		m := *unit.CTM
		matrix = matrix.Mul(geom.Matrix{{m[0], m[2], m[4]}, {m[1], m[3], m[5]}})
	}
	return path.Transform(matrix)
}

func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

// NormalizedRegion 将启动路径换算为热区外接矩形内的 SVG 路径，供浏览器精确命中。
func NormalizedRegion(path *geom.Path) string {
	if path == nil || path.Empty() {
		return ""
	}
	box := path.Bounds()
	if box.W() <= 0 || box.H() <= 0 {
		return ""
	}
	// 普通矩形不生成额外 SVG 定义，避免大文档每个链接多建一组 DOM。
	if geom.ClipContainsRect(path, box) {
		return ""
	}
	return path.Copy().Transform(geom.Identity.Scale(1/box.W(), 1/box.H()).Translate(-box.X0, -box.Y0)).ToSVG()
}
