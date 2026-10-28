package geom

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
// 的裁剪当成完整覆盖。nil 表示无限制。
func ClipContainsRect(clip *Path, bounds Rect) bool {
	if clip == nil {
		return true
	}
	if clip.Empty() || !clip.Bounds().Contains(bounds) {
		return false
	}
	scanner := clip.Scanner()
	var start, previous Point
	edges, moves := 0, 0
	closed := false
	for scanner.Scan() {
		end := scanner.End()
		switch scanner.Cmd() {
		case MoveToCmd:
			moves++
			start = end
		case LineToCmd, CloseCmd:
			if previous.X != end.X && previous.Y != end.Y {
				return false
			}
			if previous != end {
				edges++
			}
			closed = scanner.Cmd() == CloseCmd && end == start
		default:
			return false
		}
		previous = end
	}
	return moves == 1 && edges == 4 && closed
}
