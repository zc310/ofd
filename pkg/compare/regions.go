package compare

import (
	"container/heap"
	"context"
	"math"
	"sort"
)

const maxVisualRegions = 200

// 每个网格边长约为当前分辨率下的 1 毫米。按八邻域聚合相近笔画，
// 无需为每个变化像素单独保存连通区域标签。
type regionGrid struct {
	width, height, cell, cols, rows int
	counts                          []uint32
}

func newRegionGrid(width, height int, dpi float64) *regionGrid {
	cell := max(1, int(math.Ceil(dpi/25.4)))
	cols, rows := (width-1)/cell+1, (height-1)/cell+1
	return &regionGrid{width: width, height: height, cell: cell, cols: cols, rows: rows, counts: make([]uint32, cols*rows)}
}

func (g *regionGrid) add(x, y int) { g.counts[(y/g.cell)*g.cols+x/g.cell]++ }

func (g *regionGrid) regions(ctx context.Context, minimum int) ([]VisualRegion, int, int64, error) {
	selected := &regionHeap{}
	count := 0
	var ignored int64
	queue := []int{}
	for seed, value := range g.counts {
		if seed%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, 0, 0, err
			}
		}
		if value == 0 {
			continue
		}
		queue = append(queue[:0], seed)
		g.counts[seed] = 0
		x0, y0 := seed%g.cols, seed/g.cols
		x1, y1 := x0+1, y0+1
		pixels := int64(value)
		for i := 0; i < len(queue); i++ {
			if i%4096 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, 0, 0, err
				}
			}
			cx, cy := queue[i]%g.cols, queue[i]/g.cols
			for y := max(0, cy-1); y <= min(g.rows-1, cy+1); y++ {
				for x := max(0, cx-1); x <= min(g.cols-1, cx+1); x++ {
					index := y*g.cols + x
					if g.counts[index] == 0 {
						continue
					}
					pixels += int64(g.counts[index])
					g.counts[index] = 0
					queue = append(queue, index)
					x0 = min(x0, x)
					y0 = min(y0, y)
					x1 = max(x1, x+1)
					y1 = max(y1, y+1)
				}
			}
		}
		if pixels < int64(max(1, minimum)) {
			ignored += pixels
			continue
		}
		count++
		region := VisualRegion{Bounds: [4]int{x0 * g.cell, y0 * g.cell, min(g.width, x1*g.cell), min(g.height, y1*g.cell)}, ChangedPixels: pixels}
		if selected.Len() < maxVisualRegions {
			heap.Push(selected, region)
		} else if regionPriority(region, (*selected)[0]) {
			heap.Pop(selected)
			heap.Push(selected, region)
		}
	}
	regions := []VisualRegion(*selected)
	sort.Slice(regions, func(i, j int) bool {
		a, b := regions[i].Bounds, regions[j].Bounds
		if a[1] != b[1] {
			return a[1] < b[1]
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		if a[3] != b[3] {
			return a[3] < b[3]
		}
		return a[2] < b[2]
	})
	return regions, count, ignored, nil
}

func regionPriority(a, b VisualRegion) bool {
	if a.ChangedPixels != b.ChangedPixels {
		return a.ChangedPixels > b.ChangedPixels
	}
	for _, i := range []int{1, 0, 3, 2} {
		if a.Bounds[i] != b.Bounds[i] {
			return a.Bounds[i] < b.Bounds[i]
		}
	}
	return false
}

type regionHeap []VisualRegion

func (h regionHeap) Len() int           { return len(h) }
func (h regionHeap) Less(i, j int) bool { return regionPriority(h[j], h[i]) }
func (h regionHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *regionHeap) Push(v any)        { *h = append(*h, v.(VisualRegion)) }
func (h *regionHeap) Pop() any {
	old := *h
	last := old[len(old)-1]
	*h = old[:len(old)-1]
	return last
}
