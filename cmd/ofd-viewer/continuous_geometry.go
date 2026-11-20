package main

import (
	"math"
	"sort"
)

// continuousPageSize 仅保存页面物理尺寸，不持有页面对象或文档资源。
type continuousPageSize struct {
	width, height float64
}

// continuousGeometry 以同一物理比例竖向排列页面。布局建立为 O(n)，
// 可见范围查询为 O(log n)，滚动时不重新遍历所有页面。
type continuousGeometry struct {
	width, height, scale, gap float64
	pages                     []continuousPageGeometry
}

type continuousPageGeometry struct {
	x, y, width, height float64
}

// buildContinuousGeometry 保留输入页面顺序与全局下标，拒绝无效或溢出的尺寸。
// 调用者应在建立布局之前明确选择无效页面的占位策略。
func buildContinuousGeometry(sizes []continuousPageSize, width, scale, gap float64) (continuousGeometry, bool) {
	if !validPositive(width) || !validPositive(scale) || !finiteGeometry(gap) || gap < 0 {
		return continuousGeometry{}, false
	}
	g := continuousGeometry{width: width, scale: scale, gap: gap, pages: make([]continuousPageGeometry, len(sizes))}
	for i, size := range sizes {
		w, h := size.width*scale, size.height*scale
		if !validPositive(size.width) || !validPositive(size.height) || !validPositive(w) || !validPositive(h) {
			return continuousGeometry{}, false
		}
		x := (width - w) / 2
		if !finiteGeometry(x) || !finiteGeometry(g.height+h) || g.height+h <= g.height {
			return continuousGeometry{}, false
		}
		g.pages[i] = continuousPageGeometry{x: x, y: g.height, width: w, height: h}
		g.height += h
		if i+1 < len(sizes) {
			g.height += gap
			if !finiteGeometry(g.height) {
				return continuousGeometry{}, false
			}
		}
	}
	return g, true
}

func finiteGeometry(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
func validPositive(value float64) bool  { return finiteGeometry(value) && value > 0 }

// visible 返回与 [top, bottom) 相交的连续页码范围 [first, end)。
// 只落在页间空隙的视口返回空区间，不将不可见页面误报为可见。
func (g continuousGeometry) visible(top, bottom float64) (int, int) {
	if !finiteGeometry(top) || !finiteGeometry(bottom) || bottom <= top {
		return 0, 0
	}
	first := sort.Search(len(g.pages), func(i int) bool {
		p := g.pages[i]
		return p.y+p.height > top
	})
	end := sort.Search(len(g.pages), func(i int) bool { return g.pages[i].y >= bottom })
	return first, max(first, end)
}
