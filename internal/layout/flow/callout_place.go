package flow

import (
	"context"
	"fmt"
)

func calloutCodeOverhead(lines []textLine) float64 {
	height := 0.0
	var previous *calloutCode
	for _, line := range lines {
		code := line.callout.code
		if code != nil && code != previous {
			height += 2 * codePaddingY
		}
		previous = code
	}
	return height
}

func (p *preparedBlock) calloutRepeatHeight(start int) float64 {
	if start == 0 || len(p.calloutRepeat) == 0 {
		return 0
	}
	height := 2.0
	for _, line := range p.calloutRepeat {
		height += line.height
	}
	return height
}

func (e *engine) fitCalloutLines(lines []textLine, overhead float64) (int, error) {
	plan := e.currentFootnotes()
	height, count := overhead, 0
	var previous *calloutCode
	for _, line := range lines {
		if err := e.state.ctx.Err(); err != nil {
			return 0, err
		}
		code := line.callout.code
		nextHeight := height + line.height
		if code != nil && code != previous {
			nextHeight += 2 * codePaddingY
		}
		next, err := e.planFootnotes([]textLine{line}, plan)
		if err != nil {
			return 0, err
		}
		if e.y-nextHeight < e.flowBottom()+next.height-0.001 {
			break
		}
		height, plan, previous, count = nextHeight, next, code, count+1
	}
	return count, nil
}

func (e *engine) placeCallout(ctx context.Context, p *preparedBlock) error {
	c := p.block.callout
	e.state.leftBorder = nil
	if c.Pagination == "keep" {
		fresh, err := e.planFootnotes(p.lines, pageFootnotes{})
		if err != nil {
			return err
		}
		// 栏首不占段前间距，框后的间距也不属于必须同栏的可见内容。
		height := p.height() - p.style.SpaceBefore - p.style.SpaceAfter
		if height+fresh.height > e.contentHeight+0.001 {
			return fmt.Errorf("严格同栏的提示框及脚注超过整栏高度: %w", errContent)
		}
		if err := e.ensureHeight(height, p.lines); err != nil {
			return err
		}
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
		overhead := 2*calloutPadding + p.calloutRepeatHeight(start)
		count, err := e.fitCalloutLines(p.lines[start:], gap+overhead)
		if err != nil {
			return err
		}
		remaining := len(p.lines) - start
		if c.Pagination == "keep" && count < remaining {
			if e.hasContent() {
				e.nextColumn()
				continue
			}
			return fmt.Errorf("严格同栏的提示框无法放入当前栏高: %w", errContent)
		}
		if count < remaining {
			widows := p.block.Widows
			if widows == 0 {
				widows = 2
			}
			if remaining-count < widows {
				count = max(0, remaining-widows)
			}
			minimum := 1
			if start == 0 {
				minimum = p.minimumLineCount()
			}
			if count < minimum && e.hasContent() {
				e.nextColumn()
				continue
			}
		}
		// 语言标签不能单独留在代码片段末尾。
		if count > 0 && start+count < len(p.lines) && p.lines[start+count-1].callout.codeLabel {
			count--
		}
		titleMinimum := p.calloutTitleLines + 1
		if p.calloutTitleLines < len(p.lines) && p.lines[p.calloutTitleLines].callout.codeLabel {
			titleMinimum++
		}
		if start == 0 && p.calloutTitleLines > 0 && count < titleMinimum && count < remaining {
			if e.hasContent() {
				e.nextColumn()
				continue
			}
			return fmt.Errorf("提示框标题与正文开头无法同时放入整栏: %w", errContent)
		}
		if count == 0 {
			if e.hasContent() {
				e.nextColumn()
				continue
			}
			if e.flowBottom() > e.geometry.MarginBottom {
				return fmt.Errorf("提示框无法放入平衡栏高: %w", errContent)
			}
			count, err = e.fitCalloutLines(p.lines[start:], gap+overhead)
			if err != nil {
				return err
			}
			if count == 0 || p.lines[start+count-1].callout.codeLabel {
				return fmt.Errorf("提示框标题、代码标签或文字行及脚注无法放入整栏: %w", errContent)
			}
		}
		lines := p.lines[start : start+count]
		plan, err := e.planFootnotes(lines, e.currentFootnotes())
		if err != nil {
			return err
		}
		e.commitFootnotes(plan)
		e.y -= gap
		if start == 0 {
			e.recordAnchor(p.block.ID)
		}
		height := overhead + calloutCodeOverhead(lines)
		for _, line := range lines {
			height += line.height
		}
		e.drawCallout(p, height)
		e.y -= calloutPadding
		if start > 0 {
			for _, line := range p.calloutRepeat {
				e.drawLine(line, p.calloutTitleStyle, e.contentLeft+p.indent+calloutPadding, e.contentWidth-p.indent-2*calloutPadding, e.y)
				e.y -= line.height
			}
			if len(p.calloutRepeat) > 0 {
				e.y -= 2
			}
		}
		for i := 0; i < len(lines); {
			code := lines[i].callout.code
			if code == nil {
				e.drawCalloutLine(p, lines[i])
				i++
				continue
			}
			end, codeHeight := i, 2*codePaddingY
			for end < len(lines) && lines[end].callout.code == code {
				codeHeight += lines[end].height
				end++
			}
			e.fillRoundedPanel(e.contentLeft+p.indent+code.left, e.y-codeHeight, code.width, codeHeight, 1.2, colorCodeBG)
			e.y -= codePaddingY
			for ; i < end; i++ {
				e.drawCalloutLine(p, lines[i])
			}
			e.y -= codePaddingY
		}
		e.y -= calloutPadding
		start += count
		before = 0
		if start < len(p.lines) {
			e.nextColumn()
		}
	}
	e.spaceLeftBorder(nil, p.indent, p.style.SpaceAfter)
	return e.state.err
}

func (e *engine) drawCalloutLine(p *preparedBlock, line textLine) {
	meta := line.callout
	left := e.contentLeft + p.indent + meta.left
	width := e.contentWidth - p.indent - meta.left - calloutPadding
	if meta.code != nil {
		width -= codePaddingX
	}
	top := e.y - meta.top
	if len(meta.marker) > 0 {
		e.drawLine(meta.marker[0], meta.style, left+meta.iconWidth, meta.markerWidth-meta.iconWidth, top)
	}
	draw := line
	draw.height = meta.height
	if meta.icon != nil {
		iconBlock := preparedBlock{icon: meta.icon, style: meta.style, markerWidth: meta.iconWidth}
		e.drawIcon(&iconBlock, draw, left+meta.iconWidth, width-meta.iconWidth, top)
	}
	e.drawLine(draw, meta.style, left+meta.markerWidth, width-meta.markerWidth, top)
	e.y -= line.height
}
