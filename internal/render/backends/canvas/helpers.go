package canvas

import (
	"math"

	"github.com/zc310/ofd/internal/render/geom"
)

// finiteFloat 判断浮点数是否既非 NaN 也非 Inf。
func finiteFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

// finiteMatrix 判断矩阵所有分量是否有限。
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

// containsPrivateGlyphRune 判断文本是否包含私有区字形引用。
func containsPrivateGlyphRune(value string) bool {
	for _, r := range value {
		if r >= 0xF0000 && r <= 0xFFFFD {
			return true
		}
	}
	return false
}
