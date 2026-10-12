package pdf2ofd

// pdfSoftMaskRow/Column 按最近邻把目标坐标映射到源坐标。乘法使用 64 位
// 整数，单图预算允许的长条图在 32 位平台上也不会因中间乘积溢出而错位。
func pdfSoftMaskRow(y, maskHeight, height int) int {
	maskY := int(int64(y) * int64(maskHeight) / int64(height))
	if maskY >= maskHeight {
		maskY = maskHeight - 1
	}
	return maskY
}

func pdfSoftMaskColumn(x, maskWidth, width int) int {
	maskX := int(int64(x) * int64(maskWidth) / int64(width))
	if maskX >= maskWidth {
		maskX = maskWidth - 1
	}
	return maskX
}
