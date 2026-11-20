package main

import (
	"math"
	"testing"
)

func TestContinuousGeometryMixedSizesAndVisibleGaps(t *testing.T) {
	g, ok := buildContinuousGeometry([]continuousPageSize{{100, 200}, {200, 100}, {50, 50}}, 400, 2, 10)
	if !ok || g.height != 720 || g.pages[0].x != 100 || g.pages[1].y != 410 || g.pages[2].y != 620 {
		t.Fatalf("页面几何错误: %+v", g)
	}
	for _, tc := range []struct {
		top, bottom float64
		first, end  int
	}{
		{-10, 1, 0, 1}, {399, 411, 0, 2}, {400, 410, 1, 1},
		{410, 610, 1, 2}, {610, 620, 2, 2}, {619, 621, 2, 3},
		{720, 800, 3, 3}, {-30, -10, 0, 0}, {10, 10, 0, 0},
	} {
		first, end := g.visible(tc.top, tc.bottom)
		if first != tc.first || end != tc.end {
			t.Fatalf("可见范围 [%v,%v) = [%d,%d)，期望 [%d,%d)", tc.top, tc.bottom, first, end, tc.first, tc.end)
		}
	}
}

func TestContinuousGeometryRejectsInvalidAndOverflow(t *testing.T) {
	for _, tc := range []struct {
		sizes             []continuousPageSize
		width, scale, gap float64
	}{
		{[]continuousPageSize{{0, 1}}, 100, 1, 0},
		{[]continuousPageSize{{1, math.NaN()}}, 100, 1, 0},
		{[]continuousPageSize{{math.MaxFloat64, 1}}, 100, 2, 0},
		{[]continuousPageSize{{1, math.MaxFloat64}, {1, math.MaxFloat64}}, 100, 1, 0},
		{[]continuousPageSize{{1, 1e20}, {1, 1}}, 100, 1, 0},
		{[]continuousPageSize{{1, 1}}, 0, 1, 0},
		{[]continuousPageSize{{1, 1}}, 100, 1, -1},
		{[]continuousPageSize{{1, 1}}, 100, 1, math.Inf(1)},
	} {
		if _, ok := buildContinuousGeometry(tc.sizes, tc.width, tc.scale, tc.gap); ok {
			t.Fatalf("无效页面或参数被接受: %+v", tc)
		}
	}
}

func TestContinuousGeometryLargeDocumentFindsVisiblePages(t *testing.T) {
	sizes := make([]continuousPageSize, 100000)
	for i := range sizes {
		sizes[i] = continuousPageSize{100, 200}
	}
	g, ok := buildContinuousGeometry(sizes, 500, 1, 10)
	if !ok {
		t.Fatal("无法建立长文档布局")
	}
	if first, end := g.visible(50000*210+20, 50000*210+450); first != 50000 || end != 50003 {
		t.Fatalf("长文档可见范围错误: [%d,%d)", first, end)
	}
}
