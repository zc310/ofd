package geom

// Resolution、DPI、DPMM及DefaultResolution复用canvas定义；本包保留OFD专用
// 的最小栅格描边宽度规则。

// MinimumRasterStrokeWidth 返回栅格化时极细正线宽的下限（毫米）。
// GB/T 33190—2016 表21以两个像素描述正线宽的设备下限；低分辨率预览
// 为兼顾预览保真和极细描边可见性，低分辨率预览按600 DPI参考设备换算，
// 允许抗锯齿覆盖不足一个输出像素。此为预览策略，不将表21的设备下限直接
// 套用到屏幕像素；参考下限约0.0847mm，避免加粗0.1mm左右的表格线。
// 600 DPI及以上按实际设备计算。
func MinimumRasterStrokeWidth(resolution Resolution) float64 {
	return 2 / max(resolution.DPMM(), DPI(600).DPMM())
}
