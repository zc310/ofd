package flow

import (
	"fmt"
	"math"

	"github.com/zc310/ofd/pkg/creator"
)

func normalizeColumns(columns Columns, available float64) (Columns, error) {
	if columns.Count == 0 {
		columns.Count = 1
	}
	if columns.Count < 1 || columns.Count > 8 {
		return columns, fmt.Errorf("columns.count 必须为 1 到 8，零表示单栏")
	}
	if columns.Gap < 0 || math.IsNaN(columns.Gap) || math.IsInf(columns.Gap, 0) {
		return columns, fmt.Errorf("columns.gap 必须为有限非负数")
	}
	if available-float64(columns.Count-1)*columns.Gap <= 0 {
		return columns, fmt.Errorf("分栏间距使栏宽不大于零")
	}
	return columns, nil
}

// 分栏属于创作布局，输出仍遵循 GB/T 33190—2016 §8.1 的页面毫米坐标。
func (e *engine) setSectionArea(section sectionState, column int) {
	e.setGeometry(section.geometry)
	width := (e.contentWidth - float64(section.columns.Count-1)*section.columns.Gap) / float64(section.columns.Count)
	e.contentLeft += float64(column) * (width + section.columns.Gap)
	e.contentWidth = width
	e.contentRight = e.contentLeft + width
	e.contentBottom = e.flowBottom()
}

func (e *engine) hasContent() bool {
	// 空白行不生成图元，但已经占用当前栏的高度。
	return len(e.cur().Items) > e.state.columnBase || e.y < e.contentTop
}

func (e *engine) pageHasContent() bool {
	return e.state.columnIndex > 0 || e.hasContent()
}

func (e *engine) nextColumn() {
	a := e.state
	e.recordColumnHeight()
	if a.columnIndex+1 >= a.section.columns.Count {
		e.newPage()
		return
	}
	a.columnIndex++
	a.columnBase = len(e.cur().Items)
	e.setSectionArea(a.section, a.columnIndex)
	e.contentBottom += e.currentFootnotes().height
	e.y = e.contentTop
}

// 多栏目的地用 XYZ 同时记录栏的左边界和内容顶部（标准 §14.2 表 54）。
func (e *engine) destination(top float64) creator.GotoAction {
	destination := creator.GotoAction{Page: e.pageIndex, Type: "FitH", Top: &top}
	columns := e.state.section.columns.Count
	if e.pageIndex >= 0 && e.pageIndex < len(e.state.pageSettings) {
		columns = e.state.pageSettings[e.pageIndex].section.columns.Count
	}
	if columns > 1 {
		left := e.contentLeft
		destination.Type, destination.Left = "XYZ", &left
	}
	return destination
}
