// Package composite 计算复合图元的兼容内容范围、保守墨迹边界与重采样计划。
// 本包不加载资源或执行绘制，输入模型及共享路径均视为只读。
package composite

import (
	"math"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/pathbuild"
)

const (
	axisEpsilon     = 1e-9
	extentTolerance = 0.1
	inkMarginAbs    = 0.5
	inkMarginRatio  = 0.02
	inkMaxAreaRatio = 0.5
)

// ContentExtent 由 Boundary 与轴向 CTM 反推内容范围；旋转或错切时不做反推。
// GB/T 33190—2016 §13 表50 定义单元宽高；此函数用于兼容声明尺寸与实际
// 对象范围不符的文档，不改变模型中的声明尺寸。
func ContentExtent(object models.CompositeObject) (float64, float64, bool) {
	var width, height float64
	if object.CTM == nil {
		width = object.Boundary.Width
		height = object.Boundary.Height
	} else {
		ctm := *object.CTM
		if !ctm.IsFinite() {
			return 0, 0, false
		}
		if math.Abs(ctm[1]) > axisEpsilon || math.Abs(ctm[2]) > axisEpsilon {
			return 0, 0, false
		}
		if ctm[0] == 0 || ctm[3] == 0 {
			return 0, 0, false
		}
		width = object.Boundary.Width / math.Abs(ctm[0])
		height = object.Boundary.Height / math.Abs(ctm[3])
	}
	if !finiteFloat(width) || !finiteFloat(height) || width <= 0 || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}

// ExtentMismatch 判断声明尺寸与反推范围的相对误差是否超过既有兼容阈值。
func ExtentMismatch(declared, extent float64) bool {
	if declared <= 0 {
		return false
	}
	return math.Abs(extent/declared-1) > extentTolerance
}

// InkRect 保守估算单元内容的画布边界（毫米，Y 向上），并预留字形与取整边距。
// 描边、旋转、图片及嵌套内容无法可靠随画布平移时，返回 false 以沿用完整画布。
// 路径使用实际轮廓，文字使用 Boundary；结果只用于优化，空白重画回退由调用方处理。
func InkRect(unit *models.CompositeGraphicUnit, w, h float64) (models.StBox, bool) {
	if unit == nil || w <= 0 || h <= 0 {
		return models.StBox{}, false
	}
	axisAligned := func(ctm *models.CTM) bool {
		if ctm == nil {
			return true
		}
		return ctm.IsFinite() && math.Abs(ctm[1]) <= axisEpsilon && math.Abs(ctm[2]) <= axisEpsilon
	}
	var (
		x0, y0, x1, y1 float64
		seen           bool
	)
	add := func(cx0, cy0, cx1, cy1 float64) {
		if !seen {
			x0, y0, x1, y1 = cx0, cy0, cx1, cy1
			seen = true
			return
		}
		x0, y0 = math.Min(x0, cx0), math.Min(y0, cy0)
		x1, y1 = math.Max(x1, cx1), math.Max(y1, cy1)
	}
	for _, item := range unit.Content.Items {
		switch item.Kind {
		case models.PageItemText:
			if item.Text == nil || !axisAligned(item.Text.CTM) || item.Text.Stroke || !item.Text.Boundary.IsFinite() {
				return models.StBox{}, false
			}
			b := item.Text.Boundary
			add(b.X, h-b.Y-b.Height, b.X+b.Width, h-b.Y)
		case models.PageItemPath:
			if item.Path == nil || !axisAligned(item.Path.CTM) || item.Path.Stroke.Value(true) {
				return models.StBox{}, false
			}
			bounds := pathbuild.Object(*item.Path, h, nil).Bounds()
			if bounds.Empty() {
				return models.StBox{}, false
			}
			add(bounds.X0, bounds.Y0, bounds.X1, bounds.Y1)
		case models.PageItemImage, models.PageItemComposite, models.PageItemBlock:
			// 图片矩阵直接使用页面高度翻转 Y，不随离屏画布 Translate 移动。
			// 嵌套内容与图片均不能用这里的边界推断安全收缩。
			return models.StBox{}, false
		default:
			// 未知图元同样无法推断。
			return models.StBox{}, false
		}
	}
	if !seen {
		return models.StBox{}, false
	}
	// 保留既有顺序：右边距基于左侧扩展后的宽度，顶部边距同理。
	x0 -= inkMarginAbs + inkMarginRatio*(x1-x0)
	x1 += inkMarginAbs + inkMarginRatio*(x1-x0)
	y0 -= inkMarginAbs + inkMarginRatio*(y1-y0)
	y1 += inkMarginAbs + inkMarginRatio*(y1-y0)
	x0, y0 = math.Max(x0, 0), math.Max(y0, 0)
	x1, y1 = math.Min(x1, w), math.Min(y1, h)
	iw, ih := x1-x0, y1-y0
	if !finiteFloat(iw) || !finiteFloat(ih) || iw <= 0 || ih <= 0 {
		return models.StBox{}, false
	}
	if iw*ih > inkMaxAreaRatio*w*h {
		return models.StBox{}, false
	}
	return models.StBox{X: x0, Y: y0, Width: iw, Height: ih}, true
}

// Resample 根据首遍图像的墨迹像素边界计算重画画布（毫米，Y 向上）和 DPI。
// 仅当内容面积足够小且分辨率至少提升两成时返回计划，最高 1200 DPI。
// 调用方须在重画前计入预算，并在重画为空时保留首遍结果。
func Resample(ink geom.Rect, w, h, dpi float64, box models.StBox, outDPI float64) (geom.Rect, float64, bool) {
	if ink.X1 <= ink.X0 || ink.Y1 <= ink.Y0 {
		return geom.Rect{}, 0, false
	}
	mmPerPx := 25.4 / dpi
	inkWidth, inkHeight := (ink.X1-ink.X0)*mmPerPx, (ink.Y1-ink.Y0)*mmPerPx
	inkX, inkY := ink.X0*mmPerPx, h-ink.Y1*mmPerPx
	if inkWidth*inkHeight > inkMaxAreaRatio*w*h {
		return geom.Rect{}, 0, false
	}
	next := box.Width * outDPI / inkWidth
	if !finiteFloat(next) || next <= 0 {
		return geom.Rect{}, 0, false
	}
	next = math.Min(next, 1200)
	if next < dpi*1.2 {
		return geom.Rect{}, 0, false
	}
	pad := inkMarginAbs + inkMarginRatio*math.Max(inkWidth, inkHeight)
	x0, y0 := math.Max(inkX-pad, 0), math.Max(inkY-pad, 0)
	x1, y1 := math.Min(inkX+inkWidth+pad, w), math.Min(inkY+inkHeight+pad, h)
	iw, ih := x1-x0, y1-y0
	if iw <= 0 || ih <= 0 || !finiteFloat(iw) || !finiteFloat(ih) {
		return geom.Rect{}, 0, false
	}
	return geom.Rect{X0: x0, Y0: y0, X1: x1, Y1: y1}, next, true
}

func finiteFloat(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
