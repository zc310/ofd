package flow

// pageGeometry 仅保存物理页面及页边距，不包含主题、字体或重排选项。
type pageGeometry struct {
	PageWidth, PageHeight                            float64
	MarginTop, MarginRight, MarginBottom, MarginLeft float64
}

// mmPerPoint 是 1 磅对应的毫米数。
const mmPerPoint = 25.4 / 72.0

func ptToMM(pt float64) float64 { return pt * mmPerPoint }

func (e *engine) computeArea() {
	o := e.geometry
	e.contentTop = o.PageHeight - o.MarginTop
	e.contentBottom = o.MarginBottom
	e.contentLeft = o.MarginLeft
	e.contentRight = o.PageWidth - o.MarginRight
	e.contentWidth = e.contentRight - e.contentLeft
	e.contentHeight = e.contentTop - e.contentBottom
}
