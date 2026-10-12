package pdf2ofd

import jpeg2000 "github.com/mrjoshuak/go-jpeg2000"

// jpxColourToRGB 在 cdef 重排之后转换逻辑颜色分量。色度中心是码流的
// 2^(precision-1)，不是归一化的 .5，后者会在 8 位样本上引入半个等级偏移。
// sYCC 使用 BT.601 的 YCbCr 矩阵；高位深计算直到 PNG 输出才量化。
func jpxColourToRGB(values [4]float64, space jpeg2000.ColorSpace, bits int) [4]float64 {
	maximum := float64(uint32(1)<<uint(bits) - 1)
	center := float64(uint32(1)<<uint(bits-1)) / maximum
	y, cb, cr := values[0], values[1]-center, values[2]-center
	var r, g, b float64
	switch space {
	case jpeg2000.ColorSpaceSYCC, jpeg2000.ColorSpaceYCbCr2, jpeg2000.ColorSpaceYCbCr3:
		r, g, b = y+1.402*cr, y-.344136*cb-.714136*cr, y+1.772*cb
	case jpeg2000.ColorSpaceEYCC:
		// OpenJPEG color_esycc_to_rgb 的无符号 e-sYCC 逆矩阵。
		// eYCC 不能复用 BT.709 简化式；保持浮点到输出量化，避免提前舍入。
		r = y - .0000368*cb + 1.40199*cr
		g = 1.0003*y - .344125*cb - .7141128*cr
		b = .999823*y + 1.77204*cb - .000008*cr
	case jpeg2000.ColorSpacePhotoYCC:
		cb, cr = values[1]-156.0/255, values[2]-156.0/255
		r, g, b = y+1.3584*cr, y-.4302*cb-.7915*cr, y+2.2179*cb
	case jpeg2000.ColorSpaceCMY:
		r, g, b = 1-values[0], 1-values[1], 1-values[2]
	default:
		return values
	}
	values[0], values[1], values[2] = clamp01(r), clamp01(g), clamp01(b)
	return values
}
