package flow

import (
	"fmt"
	"math"

	"github.com/zc310/ofd/pkg/creator"
)

type textLine struct {
	rule    bool
	callout *calloutLine
	atoms   []atom
	// gaps 记录自动换行中可伸展的 UAX #14 段边界，以后一个字符的序号表示。
	// 段末与显式换行不设置边界，跨页或换栏后仍保留该行的对齐方式。
	gaps    []int
	indent  float64
	height  float64
	ascent  float64
	descent float64
}

type preparedBlock struct {
	block             Block
	style             TextStyle
	lines             []textLine
	image             *creator.Image
	table             *preparedTable
	marker            []textLine
	markerWidth       float64
	extraHeight       float64
	section           *sectionState
	tocPage           []textLine
	tocWidth          float64
	fixed             *Fixed
	indent            float64
	codeLabel         []textLine
	calloutWidth      float64
	calloutRepeat     []textLine
	calloutTitleStyle TextStyle
	calloutTitleLines int
	icon              *creator.Path
}

func (p *preparedBlock) height() float64 {
	h := p.style.SpaceBefore + p.style.SpaceAfter + p.extraHeight
	for _, l := range p.lines {
		h += l.height
	}
	if p.image != nil {
		h += p.image.Height
	}
	if p.fixed != nil {
		h += p.fixed.Height
	}
	if p.table != nil {
		h += p.table.height()
	}
	return h
}

func (p *preparedBlock) minimumHeight() float64 {
	if p.image != nil || p.fixed != nil {
		return p.height()
	}
	if p.table != nil {
		return p.style.SpaceBefore + p.table.minimumHeight()
	}
	n := p.minimumLineCount()
	h := p.style.SpaceBefore + p.extraHeight
	for i := 0; i < len(p.lines) && i < n; i++ {
		h += p.lines[i].height
	}
	return h
}

func (e *engine) prepareBlock(b Block) (preparedBlock, error) {
	p := preparedBlock{block: b, style: e.state.opts.Theme.Body, indent: b.Indent}
	if math.IsNaN(b.Indent) || math.IsInf(b.Indent, 0) || b.Indent < 0 {
		return p, fmt.Errorf("%w：整块缩进必须为有限非负数，实际 %g", errContent, b.Indent)
	}
	if b.Kind == "quote" || b.LeftBorderColor != nil {
		p.indent += 6
	}
	originalWidth := e.contentWidth
	if b.callout != nil {
		p.calloutWidth = originalWidth - b.callout.outerIndent - 2*calloutPadding
	}
	e.contentWidth -= p.indent
	if b.callout != nil {
		e.contentWidth -= calloutPadding
	}
	defer func() { e.contentWidth = originalWidth }()
	if e.contentWidth <= 0 {
		return p, fmt.Errorf("%w：整块缩进 %g 超过可用行宽 %g", errContent, p.indent, originalWidth)
	}
	if b.Orphans < 0 || b.Widows < 0 {
		return p, fmt.Errorf("孤行控制的行数不能为负数")
	}
	if b.Text != "" && len(b.Runs) != 0 {
		return p, fmt.Errorf("text 与 runs 不能同时设置")
	}
	if b.Kind == "" {
		p.block.Kind = "paragraph"
	}
	switch p.block.Kind {
	case "heading":
		if b.Level < 1 || b.Level > 6 {
			return p, fmt.Errorf("标题 level 必须为 1 到 6")
		}
		p.style = mergeTextStyle(p.style, e.state.opts.Theme.Headings[b.Level-1])
	case "image":
		if b.Image == nil {
			return p, fmt.Errorf("缺少 image 内容")
		}
	case "table":
		if b.Table == nil {
			return p, fmt.Errorf("缺少 table 内容")
		}
		p.style = mergeTextStyle(p.style, e.state.opts.Theme.Table)
	case "fixed":
		if b.Fixed == nil {
			return p, fmt.Errorf("缺少 fixed 内容")
		}
	case "recipient":
		if e.state.opts.Official == nil {
			return p, fmt.Errorf("recipient 仅用于 gbt9704 公文配置")
		}
		p.style.FirstLineIndent = 0
		p.style.Align = "left"
		p.block.KeepWithNext = true
	case "quote":
		p.style.Color = colorQuote
		p.style.FirstLineIndent = 0
	case "code":
		p.style.Font = e.state.opts.CodeFont
		p.style.Size, p.style.LineHeight = 10.5, 1.1
		p.style.FirstLineIndent, p.style.Align, p.style.Color = 0, "left", colorCode
	case "thematic_break":
	case "paragraph", "list_item", "page_break", "column_break":
	case "toc_entry":
	default:
		return p, fmt.Errorf("未知内容类型 %q", b.Kind)
	}
	if b.Style != nil {
		p.style = mergeTextStyle(p.style, *b.Style)
	}
	if b.LeadingIcon != nil {
		var err error
		p, err = e.prepareIcon(p)
		if err != nil {
			return p, err
		}
	}
	if err := validateTextStyle(p.style); err != nil {
		return p, err
	}
	if e.state.faces[p.style.Font] == nil {
		return p, fmt.Errorf("字体 %q 未注册", p.style.Font)
	}
	if b.Image != nil && p.block.Kind != "image" || b.Table != nil && p.block.Kind != "table" {
		return p, fmt.Errorf("image/table 内容与块类型不一致")
	}
	if b.Fixed != nil && p.block.Kind != "fixed" {
		return p, fmt.Errorf("fixed 内容与块类型不一致")
	}
	if p.block.Kind == "page_break" || p.block.Kind == "column_break" {
		if b.Text != "" || len(b.Runs) > 0 || b.Marker != "" {
			return p, fmt.Errorf("page_break/column_break 不能携带文字")
		}
		return p, nil
	}
	if b.CodeLang != "" && p.block.Kind != "code" {
		return p, fmt.Errorf("%w：code_lang 只能用于 code 块，实际类型 %q", errContent, p.block.Kind)
	}
	if (p.block.Kind == "image" || p.block.Kind == "table" || p.block.Kind == "fixed") && (b.Text != "" || len(b.Runs) > 0) {
		return p, fmt.Errorf("图片、表格和固定块不能同时设置 text 或 runs")
	}
	if p.block.Kind == "image" {
		return e.prepareImage(p)
	}
	if p.block.Kind == "table" {
		return e.prepareTable(p)
	}
	if p.block.Kind == "fixed" {
		return e.prepareFixed(p)
	}
	if p.block.Kind == "code" {
		return e.prepareCode(p)
	}
	if p.block.Kind == "thematic_break" {
		if b.Text != "" || len(b.Runs) > 0 || b.Marker != "" {
			return p, fmt.Errorf("%w：thematic_break 不能携带文字、片段或标记：%q / %d / %q", errContent, b.Text, len(b.Runs), b.Marker)
		}
		p.lines = []textLine{{height: 2}}
		return p, nil
	}
	runs := b.Runs
	if b.Text != "" {
		runs = []Run{{Text: b.Text}}
	}
	width := e.contentWidth - p.markerWidth
	if p.block.Kind == "toc_entry" {
		p.style.Align = "left"
		p.markerWidth = float64(b.Level-1) * ptToMM(p.style.Size)
		p.tocPage = e.breakLines([]Run{{Text: e.state.referenceLabel(b.tocTarget), Target: b.tocTarget}}, p.style, width, 0)
		if len(p.tocPage) != 1 {
			return p, fmt.Errorf("目录页码无法排入一行")
		}
		p.tocWidth = e.lineWidth(p.tocPage[0].atoms) + ptToMM(p.style.Size)*2
		width -= p.markerWidth + p.tocWidth
		p.block.KeepTogether = true
		for i := range p.tocPage[0].atoms {
			p.tocPage[0].atoms[i].target = b.tocTarget
		}
		runs = []Run{{Text: b.Text, Target: b.tocTarget}}
	}
	if p.block.Kind == "list_item" {
		marker := b.Marker
		if marker == "" {
			marker = "•"
		}
		p.marker = e.breakLines([]Run{{Text: marker}}, p.style, width, 0)
		if len(p.marker) != 1 {
			return p, fmt.Errorf("列表标记必须能放入一行")
		}
		listWidth := e.lineWidth(p.marker[0].atoms) + ptToMM(p.style.Size)
		p.markerWidth += listWidth
		width -= listWidth
	}
	if width <= p.style.FirstLineIndent {
		return p, fmt.Errorf("缩进超过可用行宽")
	}
	p.lines = e.breakLines(runs, p.style, width, p.style.FirstLineIndent)
	if p.icon != nil {
		if len(p.lines) == 0 {
			return p, fmt.Errorf("%w：leading_icon 需要同一块中的文字", errContent)
		}
		p.lines[0].height = math.Max(p.lines[0].height, p.icon.Height)
	}
	for _, line := range p.lines {
		if e.lineWidth(line.atoms)+line.indent > width+0.001 {
			return p, fmt.Errorf("不可断开的文本超过行宽，请增加版心或插入换行")
		}
		if line.height > e.contentHeight {
			return p, fmt.Errorf("文字行高超过整页版心")
		}
	}
	return p, nil
}
