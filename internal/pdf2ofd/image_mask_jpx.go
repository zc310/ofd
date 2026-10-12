package pdf2ofd

import (
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// applyJPXColorKey 在 cdef 确定的逻辑颜色分量中匹配原采样，不能从 PNG 的
// RGB 反推：高位深相邻采样或不同 CMYK/sYCC 分量可能转换为相同显示颜色。
// JPX 忽略 PDF Decode 和字典位深，额外原分量解码及 PNG 合成均计入预算。
func (p *pdfInterpreter) applyJPXColorKey(data []byte, stream *types.StreamDict, ranges types.Array) ([]byte, error) {
	info, err := p.jpxHeader(stream, true)
	if err != nil {
		return nil, err
	}
	m := info.metadata
	limits, err := p.imageColorKeyLimits(ranges, info.colours, m.BitsPerComponent[0])
	if err != nil {
		return nil, err
	}
	decoded, err := p.jpxImagePlanes(stream, info, m.Width, m.Height, true)
	if err != nil {
		return nil, err
	}
	maximum := float64(uint32(1)<<uint(m.BitsPerComponent[0]) - 1)
	return p.encodeImageWithMask(data, m.Width, m.Height, info.colours, func(x, y int) bool {
		index := y*m.Width + x
		for c := 0; c < info.colours; c++ {
			// 有损逆变换可返回小数；按整数码流位深舍入并截断到有效范围。
			sample := uint32(math.Round(clamp01(float64(decoded.Components[c][index])/maximum) * maximum))
			if sample < limits[c*2] || sample > limits[c*2+1] {
				return true
			}
		}
		return false
	})
}
