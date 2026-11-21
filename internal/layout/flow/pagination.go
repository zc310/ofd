package flow

import (
	"context"
	"fmt"
)

func (e *engine) placeLines(ctx context.Context, p *preparedBlock) error {
	if len(p.lines) == 0 {
		return nil
	}
	orphans, widows := p.block.Orphans, p.block.Widows
	if orphans == 0 {
		orphans = 2
	}
	if widows == 0 {
		widows = 2
	}
	before := p.style.SpaceBefore
	for start := 0; start < len(p.lines); {
		if err := ctx.Err(); err != nil {
			return err
		}
		gap := before
		if !e.hasContent() {
			gap = 0
		}
		overhead := 0.0
		if p.block.Kind == "code" {
			overhead = 2 * codePaddingY
			if start == 0 {
				for _, line := range p.codeLabel {
					overhead += line.height
				}
			}
		}
		count, err := e.fitLines(p.lines[start:], gap+overhead)
		if err != nil {
			return err
		}
		remaining := len(p.lines) - start
		if count < remaining {
			if remaining-count < widows {
				count = remaining - widows
			}
			if count < orphans && start == 0 && e.hasContent() {
				e.nextColumn()
				continue
			}
			if e.flowBottom() > e.geometry.MarginBottom && count < orphans && start == 0 {
				return fmt.Errorf("平衡栏高无法满足段首孤行约束")
			}
		}
		if count <= 0 {
			if e.hasContent() {
				e.nextColumn()
				continue
			}
			// 空页无法同时满足孤行约束时优先保证内容向前推进。
			if e.flowBottom() > e.geometry.MarginBottom {
				return fmt.Errorf("平衡栏高无法满足段尾孤行约束")
			}
			count, err = e.fitLines(p.lines[start:], gap+overhead)
			if err != nil {
				return err
			}
			if count == 0 {
				return fmt.Errorf("文字行及其脚注无法同时放入整页版心，请减小字号或缩短脚注")
			}
		}
		plan, err := e.planFootnotes(p.lines[start:start+count], e.currentFootnotes())
		if err != nil {
			return err
		}
		e.commitFootnotes(plan)
		e.y -= gap
		if start == 0 {
			e.recordAnchor(p.block.ID)
		}
		if start == 0 && p.block.Kind == "heading" && e.state.opts.GenerateOutlines {
			if title := headingTitle(p.block); title != "" {
				e.state.headings = append(e.state.headings, heading{level: p.block.Level, title: title, destination: e.destination(e.geometry.PageHeight - e.y)})
			}
		}
		height := overhead
		for _, line := range p.lines[start : start+count] {
			height += line.height
		}
		border := p.block.LeftBorderColor
		if border == nil && p.block.Kind == "quote" {
			border = colorRule
		}
		e.drawLeftBorder(border, p.indent, e.y, height)
		if p.block.Kind == "code" {
			e.fillRoundedPanel(e.contentLeft+p.indent, e.y-height, e.contentWidth-p.indent, height, 1.2, colorCodeBG)
			e.y -= codePaddingY
			if start == 0 {
				style := p.style
				style.Size, style.Color = 8, colorCodeLabel
				for _, line := range p.codeLabel {
					e.drawLine(line, style, e.contentLeft+p.indent+codePaddingX, e.contentWidth-p.indent-2*codePaddingX, e.y)
					e.y -= line.height
				}
			}
		}
		for j := 0; j < count; j++ {
			line := p.lines[start+j]
			if e.state.opts.Official != nil && e.pageIndex == 0 && len(line.atoms) > 0 && (p.block.Kind == "paragraph" || p.block.Kind == "list_item" || p.block.Kind == "heading" && p.block.Level > 1) {
				e.state.officialFirstBody = true
			}
			if start+j == 0 && len(p.marker) > 0 {
				e.drawLine(p.marker[0], p.style, e.contentLeft+p.indent, p.markerWidth, e.y)
			}
			left, width := e.contentLeft+p.indent+p.markerWidth, e.contentWidth-p.indent-p.markerWidth-p.tocWidth
			if start+j == 0 && p.icon != nil {
				e.drawIcon(p, line, left, width, e.y)
			}
			if p.block.Kind == "code" {
				left += codePaddingX
				width -= 2 * codePaddingX
			}
			if p.block.Kind == "thematic_break" {
				e.fillRect(left, e.y-1, width, 0.3, colorRule)
			}
			e.drawLine(line, p.style, left, width, e.y)
			if start+j == len(p.lines)-1 && len(p.tocPage) > 0 {
				s := p.style
				s.Align = "right"
				e.drawLine(p.tocPage[0], s, e.contentLeft, e.contentWidth, e.y)
				left := e.contentLeft + p.markerWidth + e.lineWidth(line.atoms) + ptToMM(s.Size)*0.5
				right := e.contentRight - e.lineWidth(p.tocPage[0].atoms) - ptToMM(s.Size)*0.5
				if right > left {
					e.fillRect(left, e.y-line.height*0.75, right-left, 0.12, e.state.opts.Theme.BorderColor)
				}
			}
			e.y -= line.height
		}
		if p.block.Kind == "code" {
			e.y -= codePaddingY
		}
		start += count
		before = 0
		if start < len(p.lines) {
			e.nextColumn()
		}
	}
	e.spaceLeftBorder(p.block.LeftBorderColor, p.indent, p.style.SpaceAfter)
	return e.state.err
}
