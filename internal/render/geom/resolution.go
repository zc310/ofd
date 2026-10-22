package geom

// Resolution、DPI、DPMM及DefaultResolution复用canvas定义；本包保留OFD专用
// 的最小栅格描边宽度规则。

// MinimumRasterStrokeWidth 返回栅格化时极细正线宽的下限（毫米）。
// GB/T 33190—2016 表21以两个像素描述正线宽的设备下限；低分辨率预览
// 按300 DPI参考设备换算该下限，允许抗锯齿覆盖不足一个输出像素，
// 避免将字形轮廓扩成两个实心像素后挤住字腔。300 DPI及以上按实际设备计算。
func MinimumRasterStrokeWidth(resolution Resolution) float64 {
	return 2 / max(resolution.DPMM(), DPI(300).DPMM())
}
