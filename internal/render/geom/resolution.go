package geom

// 每英寸对应的毫米数。
const mmPerInch = 25.4

// Resolution 是栅格化输出分辨率，内部以「点/毫米」存储（与 canvas.Resolution
// 语义一致）。DPI 用于对外表达「点/英寸」。
type Resolution float64

// DPMM 以点/毫米构造分辨率。
func DPMM(dpmm float64) Resolution { return Resolution(dpmm) }

// DPI 以点/英寸构造分辨率。
func DPI(dpi float64) Resolution { return Resolution(dpi / mmPerInch) }

// DPMM 返回点/毫米。
func (r Resolution) DPMM() float64 { return float64(r) }

// DPI 返回点/英寸。
func (r Resolution) DPI() float64 { return float64(r) * mmPerInch }

// DefaultResolution 是默认分辨率，等于 96 DPI。
const DefaultResolution = Resolution(96.0 / mmPerInch)

// MinimumRasterStrokeWidth 返回栅格化时极细正线宽的下限（毫米）。
// GB/T 33190—2016 表21以两个像素描述正线宽的设备下限；低分辨率预览
// 按300 DPI参考设备换算该下限，允许抗锯齿覆盖不足一个输出像素，
// 避免将字形轮廓扩成两个实心像素后挤住字腔。300 DPI及以上按实际设备计算。
func MinimumRasterStrokeWidth(resolution Resolution) float64 {
	return 2 / max(resolution.DPMM(), DPI(300).DPMM())
}
