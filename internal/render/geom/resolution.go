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
