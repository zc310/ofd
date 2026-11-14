package layout

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/zc310/ofd/pkg/creator"
)

const authorCalloutPadding = 3.0

// 提示框仍使用标准文字和填充路径（GB/T 33190—2016 第8、9、11章及表46）。
// 先展开以复用样式、脚注与引用处理，再合并成一个参与分页的逻辑块。
func expandAuthorCallouts(ctx context.Context, source []FlowBlock, theme FlowCalloutTheme) ([]FlowBlock, error) {
	var result []FlowBlock
	for i, b := range source {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if b.Kind != "callout" {
			if b.Callout != nil {
				return nil, fmt.Errorf("blocks[%d]: callout 配置需要 type 为 callout: %w", i, errFlowContent)
			}
			b.sourceIndex = i
			result = append(result, b)
			continue
		}
		if b.Callout == nil {
			return nil, fmt.Errorf("blocks[%d]: 缺少 callout 内容: %w", i, errFlowContent)
		}
		if b.Text != "" || len(b.Runs) > 0 || b.Marker != "" || b.Level != 0 || b.Image != nil || b.Table != nil || b.Fixed != nil || b.TOC != nil || b.Section != nil || b.LeadingIcon != nil || b.LeftBorderColor != nil || b.CodeLang != "" {
			return nil, fmt.Errorf("blocks[%d]: callout 不能同时携带其他内容或装饰: %w", i, errFlowContent)
		}
		c := *b.Callout
		if err := resolveAuthorCalloutAppearance(&c, theme); err != nil {
			return nil, fmt.Errorf("blocks[%d].callout: %w", i, err)
		}
		if !validCalloutIndent(b.Indent) {
			return nil, fmt.Errorf("blocks[%d]: 整块缩进必须为有限非负数: %w", i, errFlowContent)
		}
		c.outerIndent, c.outerStyle = b.Indent, b.Style
		if err := ValidateFlowText(c.Title); err != nil {
			return nil, fmt.Errorf("blocks[%d].callout.title: %w", i, err)
		}
		if err := ValidateFlowText(c.ContinuationSuffix); err != nil {
			return nil, fmt.Errorf("blocks[%d].callout.continuation_suffix: %w", i, err)
		}
		if c.RepeatTitle && strings.TrimSpace(c.Title) == "" || !c.RepeatTitle && c.ContinuationSuffix != "" {
			return nil, fmt.Errorf("blocks[%d].callout: repeat_title 需要非空标题，continuation_suffix 需要 repeat_title: %w", i, errFlowContent)
		}
		children, err := authorCalloutChildren(c)
		if err != nil {
			return nil, fmt.Errorf("blocks[%d].callout: %w", i, err)
		}
		if c.Title != "" {
			children = append([]FlowBlock{{Runs: []Inline{{Text: c.Title, Bold: true}}, Style: &TextStyle{Color: c.AccentColor, SpaceAfter: 2, Set: StyleSpaceBefore | StyleFirstLineIndent}}}, children...)
		}
		for j, child := range children {
			if err := validateAuthorCalloutChild(child); err != nil {
				return nil, fmt.Errorf("blocks[%d].callout.blocks[%d]: %w", i, j, err)
			}
			style := TextStyle{}
			if b.Style != nil {
				style = *b.Style
			}
			if child.Kind == "code" {
				// 外层正文的字体和首行缩进不能把代码改为比例字体；局部代码样式仍可覆盖。
				style.Font, style.Align, style.FirstLineIndent = "", "", 0
				style.Set &^= StyleFirstLineIndent
			}
			if child.Style != nil {
				style = mergeTextStyle(style, *child.Style)
			}
			child.Style = &style
			child.Indent += b.Indent + authorCalloutPadding
			child.callout = &c
			child.sourceIndex = i
			if j == 0 {
				if c.ShowIcon {
					child.LeadingIcon = authorCalloutIcon(c.Variant)
					if child.Kind == "" {
						child.Kind = "paragraph"
					}
				}
				child.ID, child.PageBreakBefore = b.ID, b.PageBreakBefore
				child.Orphans, child.Widows = b.Orphans, b.Widows
			}
			if j == len(children)-1 {
				child.PageBreakAfter, child.KeepWithNext = b.PageBreakAfter, b.KeepWithNext
			}
			result = append(result, child)
		}
	}
	return result, nil
}

func authorCalloutChildren(c FlowCallout) ([]FlowBlock, error) {
	modes := 0
	if c.Text != "" {
		modes++
	}
	if len(c.Runs) > 0 {
		modes++
	}
	if len(c.Blocks) > 0 {
		modes++
	}
	if modes != 1 {
		return nil, fmt.Errorf("text、runs、blocks 必须且只能设置一个: %w", errFlowContent)
	}
	children := append([]FlowBlock(nil), c.Blocks...)
	if c.Text != "" || len(c.Runs) > 0 {
		children = []FlowBlock{{Text: c.Text, Runs: c.Runs}}
	}
	bodyPresent := false
	for _, child := range children {
		bodyPresent = bodyPresent || strings.TrimSpace(child.Text) != ""
		for _, run := range child.Runs {
			bodyPresent = bodyPresent || strings.TrimSpace(run.Text) != "" || run.Footnote != "" || run.Target != "" || run.PageRef != "" || run.TitleRef != "" || run.CaptionRef != ""
		}
	}
	if !bodyPresent {
		return nil, fmt.Errorf("正文不能为空: %w", errFlowContent)
	}
	return children, nil
}

func validateAuthorCalloutChild(child FlowBlock) error {
	if !validCalloutIndent(child.Indent) {
		return fmt.Errorf("缩进必须为有限非负数: %w", errFlowContent)
	}
	if child.Kind != "" && child.Kind != "paragraph" && child.Kind != "list_item" && child.Kind != "code" || child.ID != "" || child.Callout != nil || child.Image != nil || child.Fixed != nil || child.Table != nil || child.Section != nil || child.TOC != nil || child.LeadingIcon != nil || child.LeftBorderColor != nil || child.CodeLang != "" && child.Kind != "code" || child.Level != 0 || child.KeepTogether || child.KeepWithNext || child.PageBreakBefore || child.PageBreakAfter || child.Orphans != 0 || child.Widows != 0 {
		return fmt.Errorf("仅支持不带 ID、装饰及分页控制的段落、列表或代码: %w", errFlowContent)
	}
	for _, text := range append([]string{child.Text, child.Marker, child.CodeLang}, inlineTexts(child.Runs)...) {
		if err := ValidateFlowText(text); err != nil {
			return err
		}
	}
	return nil
}

func validCalloutIndent(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func inlineTexts(runs []Inline) []string {
	texts := make([]string, 0, len(runs))
	for _, r := range runs {
		texts = append(texts, r.Text)
	}
	return texts
}

func authorCalloutAccent(variant string) (creator.Color, error) {
	switch variant {
	case "":
		return creator.Color{R: 108, G: 117, B: 125}, nil
	case "info":
		return creator.Color{R: 13, G: 110, B: 253}, nil
	case "warning":
		return creator.Color{R: 153, G: 112, B: 0}, nil
	case "danger":
		return creator.Color{R: 220, G: 53, B: 69}, nil
	case "success":
		return creator.Color{R: 25, G: 135, B: 84}, nil
	default:
		return creator.Color{}, fmt.Errorf("未知提示框变体 %q: %w", variant, errFlowContent)
	}
}

type authorCalloutLine struct {
	style                          TextStyle
	left, markerWidth, top, height float64
	marker                         []authorLine
	icon                           *creator.Path
	iconWidth                      float64
	code                           *authorCalloutCode
	codeLabel                      bool
}

type authorCalloutCode struct {
	left, width float64
}

func (e *engine) mergeAuthorCallouts(blocks []authorBlock, body TextStyle) ([]authorBlock, error) {
	var result []authorBlock
	for i := 0; i < len(blocks); {
		first := blocks[i]
		if first.block.callout == nil {
			result = append(result, first)
			i++
			continue
		}
		c := first.block.callout
		p := first
		p.lines = nil
		p.marker, p.markerWidth = nil, 0
		p.icon = nil
		// 子块缩进不改变面板左边界；面板使用外层缩进。
		p.indent = c.outerIndent
		p.style = body
		if c.outerStyle != nil {
			p.style = mergeTextStyle(p.style, *c.outerStyle)
		}
		p.extraHeight = 2 * authorCalloutPadding
		p.block.Kind, p.block.KeepTogether = "callout", true
		p.calloutRepeat = nil
		if c.RepeatTitle {
			suffix := c.ContinuationSuffix
			if suffix == "" {
				suffix = " (continued)"
			}
			p.calloutRepeat = e.authorLines([]Inline{{Text: c.Title + suffix, Bold: true}}, first.style, first.calloutWidth, 0)
			for _, line := range p.calloutRepeat {
				if e.lineWidth(line.atoms) > first.calloutWidth+0.001 {
					return nil, fmt.Errorf("blocks[%d].callout: 续页标题超过栏宽: %w", p.block.sourceIndex, errFlowContent)
				}
			}
			p.calloutTitleStyle = first.style
		}
		titleLines := 0
		for i < len(blocks) && blocks[i].block.callout == c {
			child := blocks[i]
			var code *authorCalloutCode
			labelLines := 0
			if child.block.Kind == "code" {
				code = &authorCalloutCode{left: child.indent - p.indent, width: child.calloutWidth - (child.indent - p.indent - authorCalloutPadding)}
				labelLines = len(child.codeLabel)
				child.lines = append(append([]authorLine(nil), child.codeLabel...), child.lines...)
				if child.icon != nil && len(child.lines) > 0 {
					child.lines[0].height = max(child.lines[0].height, child.icon.Height)
				}
			}
			for j, line := range child.lines {
				meta := &authorCalloutLine{style: child.style, left: child.indent - p.indent, markerWidth: child.markerWidth, height: line.height}
				meta.code = code
				if code != nil {
					meta.left += authorCodePaddingX
					if j < labelLines {
						meta.style.Size, meta.style.Color, meta.codeLabel = 8, colorCodeLabel, true
					}
				}
				if j == 0 {
					meta.top = child.style.SpaceBefore
					if len(p.lines) == 0 {
						meta.top = 0
					}
					meta.marker = child.marker
					meta.icon = child.icon
					if child.icon != nil {
						meta.iconWidth = child.icon.Width + child.icon.Height*0.35
					}
				}
				line.callout = meta
				line.height += meta.top
				if j == len(child.lines)-1 && i+1 < len(blocks) && blocks[i+1].block.callout == c {
					line.height += child.style.SpaceAfter
				}
				p.lines = append(p.lines, line)
			}
			if c.Title != "" && titleLines == 0 {
				titleLines = len(child.lines)
			}
			p.block.PageBreakAfter, p.block.KeepWithNext = child.block.PageBreakAfter, child.block.KeepWithNext
			i++
		}
		if len(p.lines) == 0 {
			return nil, fmt.Errorf("blocks[%d].callout: 正文不能为空: %w", p.block.sourceIndex, errFlowContent)
		}
		p.block.Orphans = max(p.block.Orphans, 2, titleLines+1)
		p.calloutTitleLines = titleLines
		if titleLines < len(p.lines) && p.lines[titleLines].callout.codeLabel {
			p.block.Orphans = max(p.block.Orphans, titleLines+2)
		}
		p.extraHeight += calloutCodeOverhead(p.lines)
		if e.author.err != nil {
			return nil, e.author.err
		}
		result = append(result, p)
	}
	return result, nil
}

func (e *engine) drawAuthorCallout(p *authorBlock, height float64) {
	x, bottom, width := e.contentLeft+p.indent, e.y-height, e.contentWidth-p.indent
	c := p.block.callout
	if c.Appearance == "minimal" {
		e.fillRect(x, bottom, 1, height, c.AccentColor)
		return
	}
	fill := c.Fill
	if fill == nil {
		fill = &creator.Color{R: 255, G: 255, B: 255}
	}
	e.fillRect(x, bottom, width, height, fill)
	border := c.BorderColor
	e.fillRect(x, bottom, width, 0.2, border)
	e.fillRect(x, e.y-0.2, width, 0.2, border)
	e.fillRect(x+width-0.2, bottom, 0.2, height, border)
	e.fillRect(x, bottom, 1, height, c.AccentColor)
}
