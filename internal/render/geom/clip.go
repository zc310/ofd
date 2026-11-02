package geom

import "math"

// IntersectClips 合并两个只读裁剪，nil 表示无限制；矩形完全覆盖时复用原路径。
func IntersectClips(a, b *Path) *Path {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if ClipContainsRect(a, b.Bounds()) {
		return b
	}
	if ClipContainsRect(b, a.Bounds()) {
		return a
	}
	return a.And(b)
}

// ClipContainsRect 只对严格覆盖的轴对齐矩形走快路径，不能将带洞或近似矩形
// 的裁剪当成完整覆盖。仅容忍矩形重建时一个相邻浮点数的舍入误差，
// 不使用描边展开的毫米容差放宽真实裁剪。nil 表示无限制。
func ClipContainsRect(clip *Path, bounds Rect) bool {
	if clip == nil {
		return true
	}
	if !clip.IsAxisAlignedRect() {
		return false
	}
	box := clip.Bounds()
	return math.Nextafter(box.X0, math.Inf(-1)) <= bounds.X0 &&
		bounds.X1 <= math.Nextafter(box.X1, math.Inf(1)) &&
		math.Nextafter(box.Y0, math.Inf(-1)) <= bounds.Y0 &&
		bounds.Y1 <= math.Nextafter(box.Y1, math.Inf(1))
}
