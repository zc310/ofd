package paint

import (
	"math"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/spec"
)

// DefaultLineWidth 是未指定线宽时的毫米值，见 GB/T 33190—2016 §8.2 表21。
const DefaultLineWidth = 0.353

const (
	defaultMiterLimit = 3.528
	maxStrokeValue    = 1000000
	maxDashElements   = 1024
	roundCapDotDash   = 0.001
)

// ResolvePathStyle 按 GB/T 33190—2016 §8.2 的就近原则解析路径绘制属性，
// 并将描边长度换算到页面坐标。inherited 应已完成 DrawParam.Relative 解析。
// 保留现有零值回退规则；输入模型及数组不被修改，返回值中的共享资源只读。
func ResolvePathStyle(object models.CtPath, inherited *models.DrawParam, strokeScale float64) models.CtPath {
	if !finiteFloat(strokeScale) || strokeScale <= 0 {
		strokeScale = 1
	}
	var fillSource, strokeSource *models.CTColor
	if inherited != nil {
		fillSource, strokeSource = inherited.FillColor, inherited.StrokeColor
	}
	effective := object
	if effective.LineWidth == 0 {
		if inherited != nil && inherited.LineWidth != 0 {
			effective.LineWidth = inherited.LineWidth
		} else {
			effective.LineWidth = DefaultLineWidth
		}
	}
	if inherited != nil {
		if effective.Cap == "" {
			effective.Cap = inherited.Cap
		}
		if effective.Join == "" {
			effective.Join = inherited.Join
		}
		if effective.MiterLimit == 0 {
			effective.MiterLimit = inherited.MiterLimit
		}
		if effective.DashPattern == nil {
			effective.DashPattern = inherited.DashPattern
			effective.DashOffset = inherited.DashOffset
		}
	}
	if HasColorValue(object.FillColor) {
		fillSource = object.FillColor
	}
	if HasColorValue(object.StrokeColor) {
		strokeSource = object.StrokeColor
	}
	effective.FillColor, effective.StrokeColor = fillSource, strokeSource
	if object.Stroke.Value(true) {
		effective.LineWidth = NormalizeLineWidth(effective.LineWidth * strokeScale)
		// MiterLimit 与 LineWidth 同属对象坐标系的绝对长度，同比缩放后
		// StrokeJoiner 中的斜接倍率保持不变。
		effective.MiterLimit = normalizedMiterLimit(effective.MiterLimit * strokeScale)
		effective.DashPattern = PrepareDashPattern(effective.DashPattern, effective.Cap, strokeScale)
		effective.DashOffset *= strokeScale
	}
	return effective
}

// HasColorValue 判断颜色元素是否携带实际颜色信息。空元素等价于未指定，
// 应回退到 DrawParam 的颜色。
func HasColorValue(source *models.CTColor) bool {
	return source != nil && (source.Value != nil || source.ColorSpace != 0 || source.Index != 0 ||
		source.GouraudShd != nil || source.LaGourandShd != nil || source.LaGouraudShd != nil ||
		source.AxialShd != nil || source.RadialShd != nil || source.Pattern != nil)
}

// LineCapper 将 OFD 端点样式转换为几何端点，缺省为 Butt。
func LineCapper(capStr string) geom.Capper {
	switch capStr {
	case spec.LineCapRound:
		return geom.RoundCap
	case spec.LineCapSquare:
		return geom.SquareCap
	default:
		return geom.ButtCap
	}
}

// LineJoiner 将 OFD 连接样式转换为几何连接，缺省为 Miter。
func LineJoiner(joinStr string) geom.Joiner {
	switch joinStr {
	case spec.LineJoinRound:
		return geom.RoundJoin
	case spec.LineJoinBevel:
		return geom.BevelJoin
	default:
		return geom.MiterJoin
	}
}

// StrokeJoiner 将绝对毫米的 MiterLimit 换算成后端使用的半线宽倍率。
func StrokeJoiner(join string, miterLimit, lineWidth float64) geom.Joiner {
	joiner := LineJoiner(join)
	if _, isMiter := joiner.(geom.MiterJoiner); isMiter {
		miterLimit = normalizedMiterLimit(miterLimit)
		if lineWidth > 0 {
			miterLimit /= lineWidth / 2
		}
		joiner = geom.MiterJoiner{GapJoiner: geom.BevelJoin, Limit: miterLimit}
	}
	return joiner
}

// NormalizeLineWidth 保留现有回退规则：零值、负数、非有限值及过大值使用默认线宽。
func NormalizeLineWidth(value float64) float64 {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) || value > maxStrokeValue {
		return DefaultLineWidth
	}
	return value
}

func normalizedMiterLimit(value float64) float64 {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) || value > maxStrokeValue {
		return defaultMiterLimit
	}
	return value
}

// PrepareDashPattern 按对象 CTM 缩放虚线数组，并保留圆端点的零长度点划。
// 无需调整时返回原数组；调整时复制，返回数组按只读使用。
func PrepareDashPattern(pattern *models.StArrayF, lineCap string, scale float64) *models.StArrayF {
	return preserveRoundCapDots(scaledDashPattern(pattern, scale), lineCap)
}

func scaledDashPattern(pattern *models.StArrayF, scale float64) *models.StArrayF {
	if pattern == nil || scale == 1 {
		return pattern
	}
	scaled := make(models.StArrayF, len(*pattern))
	for i, value := range *pattern {
		scaled[i] = value * scale
	}
	return &scaled
}

func preserveRoundCapDots(pattern *models.StArrayF, lineCap string) *models.StArrayF {
	if pattern == nil || lineCap != spec.LineCapRound {
		return pattern
	}
	var adjusted models.StArrayF
	for index := 0; index < len(*pattern); index += 2 {
		if (*pattern)[index] == 0 {
			if adjusted == nil {
				adjusted = append(models.StArrayF(nil), *pattern...)
			}
			adjusted[index] = roundCapDotDash
		}
	}
	if adjusted == nil {
		return pattern
	}
	return &adjusted
}

// ValidDashPattern 校验虚线偏移、长度及元素数量，并要求至少一段有实际长度。
func ValidDashPattern(offset float64, pattern *models.StArrayF) bool {
	if pattern == nil || len(*pattern) == 0 || len(*pattern) > maxDashElements ||
		offset < 0 || math.IsNaN(offset) || math.IsInf(offset, 0) || offset > maxStrokeValue {
		return false
	}
	hasLength := false
	for _, value := range *pattern {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) || value > maxStrokeValue {
			return false
		}
		if value > 0 {
			hasLength = true
		}
	}
	return hasLength
}

// StrokeScale 返回对象与父级 CTM 对描边长度的缩放系数 sqrt(|det|)。
// 路径坐标展平后，线宽、虚线与斜接限制仍需按此系数换算。
func StrokeScale(objectCTM, parentCTM *models.CTM) float64 {
	var ctm *models.CTM
	if parentCTM != nil {
		ctm = parentCTM
		if objectCTM != nil {
			ctm = parentCTM.Multiply(objectCTM)
			if !ctm.IsFinite() {
				return 1
			}
		}
	} else if objectCTM != nil {
		ctm = objectCTM
	}
	if ctm == nil || !ctm.IsFinite() {
		return 1
	}
	scale := math.Sqrt(math.Abs(ctm[0]*ctm[3] - ctm[1]*ctm[2]))
	if !finiteFloat(scale) || scale <= 0 {
		return 1
	}
	return scale
}
