package text

import (
	"math"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

// HScale 返回文字的水平缩放比例，缺省值为 1。
func HScale(object models.TextObject) float64 {
	if object.HScale > 0 {
		return object.HScale
	}
	return 1
}

// advanceDiffers 判断显式步进是否无法用同一文本串的字体自然步进表达。
// 纵向位移和明显回退表示换行；子集字体的 hmtx 可能只是占位宽度，显式前进
// 与自然字宽相差过大时也必须断串，否则整串整形会忽略源 TextCode 的字距。
// 小幅字距仍合并，以保留整段绘制、避免 PDF 文字提取时插入空格。
func advanceDiffers(deltaX, deltaY, naturalX float64) bool {
	if math.Abs(deltaY) > 0.01 {
		return true
	}
	if deltaX < -math.Max(math.Abs(naturalX), 0.01) {
		return true
	}
	if deltaX > 0 && math.Abs(naturalX) > 0 {
		if tolerance := math.Max(0.2, math.Abs(naturalX)*0.05); math.Abs(deltaX-naturalX) > tolerance {
			return true
		}
	}
	return false
}

// NeedsRunBreak 判断连续字形是否必须断开绘制，以保留换行和连字的显式定位。
func NeedsRunBreak(object models.TextObject, deltaX, deltaY, naturalX float64) bool {
	if advanceDiffers(deltaX, deltaY, naturalX) {
		return true
	}
	if !hasCollapsedGlyphTransform(object.CGTransform) || math.Abs(deltaY) > 0.01 {
		return false
	}
	return math.Abs(deltaX-naturalX) > 0.01
}

func hasCollapsedGlyphTransform(transforms []models.CTCGTransform) bool {
	for _, transform := range transforms {
		codeCount, glyphCount := transform.CodeCount, transform.GlyphCount
		if codeCount <= 0 {
			codeCount = 1
		}
		if glyphCount <= 0 {
			glyphCount = len(transform.Glyphs)
		}
		if codeCount > glyphCount {
			return true
		}
	}
	return false
}

// normalizeDirection 将方向归一化为最接近的标准象限方向。
// OFD 使用 0、90、180、270 度，非标准输入采用邻近象限。
func normalizeDirection(direction int) int {
	direction %= 360
	if direction < 0 {
		direction += 360
	}
	switch {
	case direction >= 45 && direction < 135:
		return 90
	case direction >= 135 && direction < 225:
		return 180
	case direction >= 225 && direction < 315:
		return 270
	default:
		return 0
	}
}

// DirectionsZero 判断阅读方向和字符方向是否均为水平方向。
func DirectionsZero(object models.TextObject) bool {
	return normalizeDirection(object.ReadDirection) == 0 && normalizeDirection(object.CharDirection) == 0
}

// ReadAdvance 将字宽按阅读方向转换为 OFD 坐标增量。
func ReadAdvance(advance float64, direction int) (float64, float64) {
	switch normalizeDirection(direction) {
	case 90:
		return 0, advance
	case 180:
		return -advance, 0
	case 270:
		return 0, -advance
	default:
		return advance, 0
	}
}

// CharDirection 返回归一化的字符旋转角度。
func CharDirection(object models.TextObject) float64 {
	return float64(normalizeDirection(object.CharDirection))
}

// Advance 返回源字符到下一字符的增量。显式 DeltaX/DeltaY 优先，两者均未指定
// 时，按 ReadDirection 使用字体字宽和 HScale 计算。
func Advance(glyphWidth float64, object models.TextObject, code models.TextCode, index int) (float64, float64) {
	if len(code.DeltaX) > 0 || len(code.DeltaY) > 0 {
		return valueAt(code.DeltaX, index), valueAt(code.DeltaY, index)
	}
	return ReadAdvance(glyphWidth*HScale(object), object.ReadDirection)
}

func glyphAdvance(glyphWidth float64, object models.TextObject, code models.TextCode, previous, current Glyph) (float64, float64) {
	if current.CodePosition > previous.CodePosition && (len(code.DeltaX) > 0 || len(code.DeltaY) > 0) {
		var deltaX, deltaY float64
		for index := previous.CodePosition; index < current.CodePosition; index++ {
			if index < len(code.DeltaX) {
				deltaX += code.DeltaX[index]
			}
			if index < len(code.DeltaY) {
				deltaY += code.DeltaY[index]
			}
		}
		return deltaX, deltaY
	}
	return ReadAdvance(glyphWidth*HScale(object), object.ReadDirection)
}

// PositionedAdvance 按字符或字形定位队列返回相邻绘制字形之间的增量。
func PositionedAdvance(width float64, object models.TextObject, code models.TextCode, previous, current Glyph, glyphDeltas bool) (float64, float64) {
	if glyphDeltas {
		previous.CodePosition = previous.GlyphPosition
		current.CodePosition = current.GlyphPosition
	}
	return glyphAdvance(width, object, code, previous, current)
}

func valueAt(values models.StArrayF, index int) float64 {
	if index >= 0 && index < len(values) {
		return values[index]
	}
	return 0
}

// CTMLinearMatrix 返回图元 CTM 的线性部分，供 y 向上的字形变换使用。
// OFD 的 CTM 定义在 y 向下的对象空间，因此进行 F·L·F 共轭以保留倾斜与旋转方向。
// CTM 为 nil、单位变换或含非有限分量时返回 nil。
func CTMLinearMatrix(ctm *models.CTM) *geom.Matrix {
	if ctm == nil {
		return nil
	}
	a, b, c, d := (*ctm)[0], (*ctm)[1], (*ctm)[2], (*ctm)[3]
	if !finiteFloat(a) || !finiteFloat(b) || !finiteFloat(c) || !finiteFloat(d) {
		return nil
	}
	if a == 1 && b == 0 && c == 0 && d == 1 {
		return nil
	}
	matrix := geom.Matrix{{a, -c, 0}, {-b, d, 0}}
	return &matrix
}

func finiteFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
