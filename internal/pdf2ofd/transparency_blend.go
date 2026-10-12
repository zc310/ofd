package pdf2ofd

import "math"

// 按 PDF 32000 在工作 RGB 空间计算可分离与不可分离混合函数；
// 不透明度由调用方独立处理，不参与颜色函数计算。
func supportedPDFBlend(mode string) bool {
	switch mode {
	case "", "Normal", "Compatible", "Multiply", "Screen", "Overlay", "Darken", "Lighten", "ColorDodge", "ColorBurn", "HardLight", "SoftLight", "Difference", "Exclusion", "Hue", "Saturation", "Color", "Luminosity":
		return true
	}
	return false
}

func pdfBlendColor(mode string, b, s [3]float64) [3]float64 {
	switch mode {
	case "Hue":
		return blendSetLum(blendSetSat(s, blendSat(b)), blendLum(b))
	case "Saturation":
		return blendSetLum(blendSetSat(b, blendSat(s)), blendLum(b))
	case "Color":
		return blendSetLum(s, blendLum(b))
	case "Luminosity":
		return blendSetLum(b, blendLum(s))
	}
	var result [3]float64
	for i := range result {
		x, y := b[i], s[i]
		switch mode {
		case "Multiply":
			result[i] = x * y
		case "Screen":
			result[i] = x + y - x*y
		case "Overlay":
			result[i] = blendHardLight(y, x)
		case "Darken":
			result[i] = math.Min(x, y)
		case "Lighten":
			result[i] = math.Max(x, y)
		case "ColorDodge":
			if x == 0 {
				result[i] = 0
			} else if y == 1 {
				result[i] = 1
			} else {
				result[i] = math.Min(1, x/(1-y))
			}
		case "ColorBurn":
			if x == 1 {
				result[i] = 1
			} else if y != 0 {
				result[i] = 1 - math.Min(1, (1-x)/y)
			}
		case "HardLight":
			result[i] = blendHardLight(x, y)
		case "SoftLight":
			if y <= .5 {
				result[i] = x - (1-2*y)*x*(1-x)
			} else {
				d := math.Sqrt(x)
				if x <= .25 {
					d = ((16*x-12)*x + 4) * x
				}
				result[i] = x + (2*y-1)*(d-x)
			}
		case "Difference":
			result[i] = math.Abs(x - y)
		case "Exclusion":
			result[i] = x + y - 2*x*y
		default:
			result[i] = y
		}
	}
	return result
}

func blendHardLight(b, s float64) float64 {
	if s <= .5 {
		return 2 * b * s
	}
	return 1 - 2*(1-b)*(1-s)
}

func blendLum(c [3]float64) float64 { return .3*c[0] + .59*c[1] + .11*c[2] }
func blendSat(c [3]float64) float64 {
	return math.Max(c[0], math.Max(c[1], c[2])) - math.Min(c[0], math.Min(c[1], c[2]))
}

func blendSetLum(c [3]float64, lum float64) [3]float64 {
	d := lum - blendLum(c)
	for i := range c {
		c[i] += d
	}
	l := blendLum(c)
	n, x := math.Min(c[0], math.Min(c[1], c[2])), math.Max(c[0], math.Max(c[1], c[2]))
	if n < 0 {
		for i := range c {
			c[i] = l + (c[i]-l)*l/(l-n)
		}
	}
	if x > 1 {
		for i := range c {
			c[i] = l + (c[i]-l)*(1-l)/(x-l)
		}
	}
	return c
}

func blendSetSat(c [3]float64, sat float64) [3]float64 {
	indices := [3]int{0, 1, 2}
	if c[indices[0]] > c[indices[1]] {
		indices[0], indices[1] = indices[1], indices[0]
	}
	if c[indices[1]] > c[indices[2]] {
		indices[1], indices[2] = indices[2], indices[1]
	}
	if c[indices[0]] > c[indices[1]] {
		indices[0], indices[1] = indices[1], indices[0]
	}
	lo, mid, hi := indices[0], indices[1], indices[2]
	if c[hi] > c[lo] {
		c[mid] = (c[mid] - c[lo]) * sat / (c[hi] - c[lo])
		c[hi] = sat
	} else {
		c[mid], c[hi] = 0, 0
	}
	c[lo] = 0
	return c
}
