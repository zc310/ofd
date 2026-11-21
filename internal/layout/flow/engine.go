package flow

import (
	"context"
	"fmt"

	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/image/font/sfnt"
)

// engine 在单轮排版中维护页面、纵向游标和流式内容状态。
type engine struct {
	geometry pageGeometry
	state    *layoutState

	pages     []creator.Page
	pageIndex int

	y             float64
	contentTop    float64
	contentBottom float64
	contentLeft   float64
	contentRight  float64
	contentWidth  float64
	contentHeight float64
}

func (e *engine) newPage() {
	e.recordColumnHeight()
	e.computeArea()
	e.pages = append(e.pages, creator.Page{
		Area: &creator.PageArea{
			PhysicalBox: &creator.Box{Width: e.geometry.PageWidth, Height: e.geometry.PageHeight},
			ContentBox:  &creator.Box{X: e.contentLeft, Y: e.geometry.MarginTop, Width: e.contentWidth, Height: e.contentHeight},
		},
	})
	e.pageIndex = len(e.pages) - 1
	e.recordPage()
	e.state.columnIndex = 0
	e.state.columnBase = 0
	e.setSectionArea(e.state.section, 0)
	e.y = e.contentTop
}

func (e *engine) ensurePage() {
	if e.pageIndex < 0 {
		e.newPage()
	}
}

func (e *engine) cur() *creator.Page { return &e.pages[e.pageIndex] }

func (e *engine) add(item creator.Item) {
	e.ensurePage()
	e.cur().Items = append(e.cur().Items, item)
}

// ensureSpace 为整体图形预留高度；空栏不重复换栏，避免生成空白页。
func (e *engine) ensureSpace(height float64) {
	if e.pageIndex < 0 {
		e.newPage()
		return
	}
	if e.y-height < e.contentBottom-0.01 && e.hasContent() {
		e.nextColumn()
	}
	if e.flowBottom() > e.geometry.MarginBottom && e.y-height < e.contentBottom-0.01 && e.state.err == nil {
		e.state.err = fmt.Errorf("整体块超过当前栏的可用高度")
	}
}

func (e *engine) space(height float64) {
	if e.pageIndex < 0 || height <= 0 {
		return
	}
	e.y -= height
	if e.y < e.contentBottom {
		e.y = e.contentBottom
	}
}

// layoutState 的字体在单次排版内固定，度量和写出使用相同的数据快照。
type layoutState struct {
	ctx                     context.Context
	opts                    Options
	faces                   map[string]*sfnt.Font
	fonts                   []creator.Font
	err                     error
	headings                []heading
	anchors                 map[string]creator.GotoAction
	pendingLinks            []pendingLink
	section                 sectionState
	sectionIndex            int
	sectionPage             int
	nextNumber              int
	pageSettings            []pageSettings
	fontResources           map[string]creator.Font
	fontFamilies            map[string]FontFamily
	footnoteCache           map[footnoteKey]*preparedFootnote
	pageFootnotes           [][]pageFootnotes
	columnIndex, columnBase int
	officialFirstBody       bool
	balanceHardPages        map[int]bool
	columnHeights           [][]float64
	leftBorder              *leftBorder
}
