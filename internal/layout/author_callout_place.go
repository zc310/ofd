package layout

import (
	"context"
	"fmt"
)

func calloutCodeOverhead(lines []authorLine) float64 {
	height := 0.0
	var previous *authorCalloutCode
	for _, line := range lines {
		code := line.callout.code
		if code != nil && code != previous {
			height += 2 * authorCodePaddingY
		}
		previous = code
	}
	return height
}

func (p *authorBlock) calloutRepeatHeight(start int) float64 {
	if start == 0 || len(p.calloutRepeat) == 0 {
		return 0
	}
	height := 2.0
	for _, line := range p.calloutRepeat {
		height += line.height
	}
	return height
}

func (e *engine) fitCalloutLines(lines []authorLine, overhead float64) (int, error) {
	plan := e.currentAuthorFootnotes()
	height, count := overhead, 0
	var previous *authorCalloutCode
	for _, line := range lines {
		if err := e.author.ctx.Err(); err != nil {
			return 0, err
		}
		code := line.callout.code
		nextHeight := height + line.height
		if code != nil && code != previous {
			nextHeight += 2 * authorCodePaddingY
		}
		next, err := e.planAuthorFootnotes([]authorLine{line}, plan)
		if err != nil {
			return 0, err
		}
		if e.y-nextHeight < e.authorFlowBottom()+next.height-0.001 {
			break
		}
		height, plan, previous, count = nextHeight, next, code, count+1
	}
	return count, nil
}

func (e *engine) placeAuthorCallout(ctx context.Context, p *authorBlock) error {
	c := p.block.callout
	e.author.leftBorder = nil
	if c.Pagination == "keep" {
		fresh, err := e.planAuthorFootnotes(p.lines, authorPageFootnotes{})
		if err != nil {
			return err
		}
		// 栏首不占段前间距，框后的间距也不属于必须同栏的可见内容。
		height := p.height() - p.style.SpaceBefore - p.style.SpaceAfter
		if height+fresh.height > e.contentHeight+0.001 {
			return fmt.Errorf("严格同栏的提示框及脚注超过整栏高度: %w", errFlowContent)
		}
		if err := e.ensureAuthorHeight(height, p.lines); err != nil {
			return err
		}
	}
	before := p.style.SpaceBefore
	for start := 0; start < len(p.lines); {
		if err := ctx.Err(); err != nil {
			return err
		}
		gap := before
		if !e.authorHasContent() {
			gap = 0
		}
		overhead := 2*authorCalloutPadding + p.calloutRepeatHeight(start)
		count, err := e.fitCalloutLines(p.lines[start:], gap+overhead)
		if err != nil {
			return err
		}
		remaining := len(p.lines) - start
		if c.Pagination == "keep" && count < remaining {
			if e.authorHasContent() {
				e.nextAuthorColumn()
				continue
			}
			return fmt.Errorf("严格同栏的提示框无法放入当前栏高: %w", errFlowContent)
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
			if count < minimum && e.authorHasContent() {
				e.nextAuthorColumn()
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
			if e.authorHasContent() {
				e.nextAuthorColumn()
				continue
			}
			return fmt.Errorf("提示框标题与正文开头无法同时放入整栏: %w", errFlowContent)
		}
		if count == 0 {
			if e.authorHasContent() {
				e.nextAuthorColumn()
				continue
			}
			if e.authorFlowBottom() > e.opts.MarginBottom {
				return fmt.Errorf("提示框无法放入平衡栏高: %w", errFlowContent)
			}
			count, err = e.fitCalloutLines(p.lines[start:], gap+overhead)
			if err != nil {
				return err
			}
			if count == 0 || p.lines[start+count-1].callout.codeLabel {
				return fmt.Errorf("提示框标题、代码标签或文字行及脚注无法放入整栏: %w", errFlowContent)
			}
		}
		lines := p.lines[start : start+count]
		plan, err := e.planAuthorFootnotes(lines, e.currentAuthorFootnotes())
		if err != nil {
			return err
		}
		e.commitAuthorFootnotes(plan)
		e.y -= gap
		if start == 0 {
			e.recordAuthorAnchor(p.block.ID)
		}
		height := overhead + calloutCodeOverhead(lines)
		for _, line := range lines {
			height += line.height
		}
		e.drawAuthorCallout(p, height)
		e.y -= authorCalloutPadding
		if start > 0 {
			for _, line := range p.calloutRepeat {
				e.drawAuthorLine(line, p.calloutTitleStyle, e.contentLeft+p.indent+authorCalloutPadding, e.contentWidth-p.indent-2*authorCalloutPadding, e.y)
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
			end, codeHeight := i, 2*authorCodePaddingY
			for end < len(lines) && lines[end].callout.code == code {
				codeHeight += lines[end].height
				end++
			}
			e.fillRoundedPanel(e.contentLeft+p.indent+code.left, e.y-codeHeight, code.width, codeHeight, 1.2, colorCodeBG)
			e.y -= authorCodePaddingY
			for ; i < end; i++ {
				e.drawCalloutLine(p, lines[i])
			}
			e.y -= authorCodePaddingY
		}
		e.y -= authorCalloutPadding
		start += count
		before = 0
		if start < len(p.lines) {
			e.nextAuthorColumn()
		}
	}
	e.spaceAuthorLeftBorder(nil, p.indent, p.style.SpaceAfter)
	return e.author.err
}

func (e *engine) drawCalloutLine(p *authorBlock, line authorLine) {
	meta := line.callout
	left := e.contentLeft + p.indent + meta.left
	width := e.contentWidth - p.indent - meta.left - authorCalloutPadding
	if meta.code != nil {
		width -= authorCodePaddingX
	}
	top := e.y - meta.top
	if len(meta.marker) > 0 {
		e.drawAuthorLine(meta.marker[0], meta.style, left+meta.iconWidth, meta.markerWidth-meta.iconWidth, top)
	}
	draw := line
	draw.height = meta.height
	if meta.icon != nil {
		iconBlock := authorBlock{icon: meta.icon, style: meta.style, markerWidth: meta.iconWidth}
		e.drawAuthorIcon(&iconBlock, draw, left+meta.iconWidth, width-meta.iconWidth, top)
	}
	e.drawAuthorLine(draw, meta.style, left+meta.markerWidth, width-meta.markerWidth, top)
	e.y -= line.height
}
