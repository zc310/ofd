package xmlcodec

import (
	"math"
	"strconv"
)

// numberPrecision 对应 0.0001mm，远低于常见渲染分辨率。
const numberPrecision = 4

// AppendFloat 追加不使用科学计数法的 OFD 数值，四舍五入到四位小数，
// 去除末尾多余的零并归一化负零。非有限值按 creator 的既有约定写为零。
func AppendFloat(dst []byte, value float64) []byte {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		value = 0
	}
	// 上界不能包含 2^63：float64(MaxInt64) 会向上舍入为这个超出 int64 的值。
	if value == math.Trunc(value) && value >= -0x1p63 && value < 0x1p63 {
		return strconv.AppendInt(dst, int64(value), 10)
	}
	start := len(dst)
	dst = strconv.AppendFloat(dst, value, 'f', numberPrecision, 64)
	end := len(dst)
	for end > start+1 && dst[end-1] == '0' {
		end--
	}
	if end > start+1 && dst[end-1] == '.' {
		end--
	}
	dst = dst[:end]
	if end == start+2 && dst[start] == '-' && dst[start+1] == '0' {
		dst[start] = '0'
		dst = dst[:start+1]
	}
	return dst
}
