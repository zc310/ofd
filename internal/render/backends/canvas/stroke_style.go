package canvas

import (
	"math"

	"github.com/zc310/canvas"
	"github.com/zc310/ofd/internal/render/geom"
)

// minimumStrokeStyle 将细线的显示下限换算回路径坐标，返回独立样式。
// 图片按实际分辨率计算，SVG按默认屏幕预览使用相同的参考线宽。
// 虚线采用canvas的比例值，需要随线宽反向折算，保持原来的绝对长度和相位。
func minimumStrokeStyle(style canvas.Style, matrix canvas.Matrix, resolution geom.Resolution) canvas.Style {
	if !style.HasStroke() {
		return style
	}
	scale := math.Sqrt(math.Abs(matrix.Det()))
	if !(scale > 0) || math.IsInf(scale, 0) {
		return style
	}
	minimum := geom.MinimumRasterStrokeWidth(resolution) / scale
	if style.StrokeWidth >= minimum {
		return style
	}
	ratio := style.StrokeWidth / minimum
	if len(style.Dashes) > 0 {
		dashes := make([]float64, len(style.Dashes))
		for i, dash := range style.Dashes {
			dashes[i] = dash * ratio
		}
		style.Dashes = dashes
		style.DashOffset *= ratio
	}
	style.StrokeWidth = minimum
	return style
}
