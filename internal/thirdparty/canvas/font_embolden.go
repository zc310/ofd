package canvas

// emboldenGlyphPath 按 GB/T 33190—2016 §11.2 表45的 Weight 合成加粗轮廓。
// 沿用 Path.Filling 的 NonZero 填充判定，分别扩张外轮廓、收缩孔洞；
// 不依赖 TTF/CFF 格式或整字形面积，允许同字形的外轮廓方向相反。
func emboldenGlyphPath(path *Path, distance float64) *Path {
	contours := path.Split()
	bounds := make([]Rect, len(contours))
	for i, contour := range contours {
		bounds[i] = contour.FastBounds()
	}
	result := &Path{}
	for i, contour := range contours {
		ccw := contour.CCW()
		winding := -1
		if ccw {
			winding = 1
		}
		start := contour.StartPos()
		for j, other := range contours {
			// 起点不在其他轮廓的保守包围盒内时，绕数必为零。
			// 避免中文多笔画字形做全量曲线求交，也不需要 flatten 来计算面积。
			if i == j || !bounds[j].ContainsPoint(start) {
				continue
			}
			if n, boundary := glyphContourWindings(other.RayIntersections(start.X, start.Y)); !boundary {
				winding += n
			}
		}
		d := distance
		if ccw != NonZero.Fills(winding) {
			d = -d
		}
		// 直接对单条轮廓做局部快速偏移，不修改输入或全局 FastStroke。
		if expanded, _ := contour.offset(d, ButtCap, RoundJoin, false, Tolerance); expanded != nil {
			result = result.Append(expanded)
		}
	}
	return result
}

// glyphContourWindings 计算闭合字形轮廓的绕数：顶点交点各计半次，线段内部
// 交点计一次，水平重合段本身不计数。射线与台阶重合时，两端非水平线段仍各
// 贡献半次穿越；忽略整对顶点会漏掉宋体“变”字孔洞外侧的绕数。
func glyphContourWindings(intersections []Intersection) (int, bool) {
	twice := 0
	boundary := false
	for _, intersection := range intersections {
		if intersection.T[0] == 0 {
			boundary = true
			continue
		}
		if intersection.Same {
			continue
		}
		delta := 2
		if intersection.T[1] == 0 || intersection.T[1] == 1 {
			delta = 1
		}
		if intersection.Into() {
			delta = -delta
		}
		twice += delta
	}
	return twice / 2, boundary
}
