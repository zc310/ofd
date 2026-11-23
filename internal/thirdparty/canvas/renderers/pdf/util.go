package pdf

import (
	"bytes"
	"math"
	"strconv"

	"github.com/tdewolff/minify/v2"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

const mmPerPt = 25.4 / 72.0
const ptPerMm = 72 / 25.4

////////////////////////////////////////////////////////////////

func float64sEqual(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i, f := range a {
		if f != b[i] {
			return false
		}
	}
	return true
}

type dec float64

func (f dec) String() string {
	// 常见坐标远短于 32 字节，使用栈上缓冲区避免为每个数字单独分配切片。
	var buf [32]byte
	b := strconv.AppendFloat(buf[:0], float64(f), 'f', canvas.Precision, 64)
	b = minify.Decimal(b, canvas.Precision)
	if dec(math.MaxInt32) < f || f < dec(math.MinInt32) {
		if bytes.IndexByte(b, '.') == -1 {
			b = append(b, '.', '0')
		}
	}
	return string(b)
}
