package markdown

import (
	"strings"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
)

type alertStyle struct {
	title string
	color creator.Color
}

// GitHub Alerts 是引用块首行的独立标记，不将行内代码、转义文本或未知类型解释为提示。
func (p *mdParser) alert(quote *ast.Blockquote, indent int) ([]flow.Block, bool) {
	first, ok := quote.FirstChild().(*ast.Paragraph)
	if !ok || len(first.Source()) == 0 {
		return nil, false
	}
	line := first.Source()[0]
	marker := strings.TrimSpace(string(p.source[line.Start:line.Stop]))
	var style alertStyle
	switch marker {
	case "[!NOTE]":
		style = alertStyle{"Note", creator.Color{R: 9, G: 105, B: 218}}
	case "[!TIP]":
		style = alertStyle{"Tip", creator.Color{R: 26, G: 127, B: 55}}
	case "[!IMPORTANT]":
		style = alertStyle{"Important", creator.Color{R: 130, G: 80, B: 223}}
	case "[!WARNING]":
		style = alertStyle{"Warning", creator.Color{R: 154, G: 103, B: 0}}
	case "[!CAUTION]":
		style = alertStyle{"Caution", creator.Color{R: 207, G: 34, B: 46}}
	default:
		return nil, false
	}
	// 标记与正文可能属于同一段；只移除第一物理行，保留其后的行内样式与换行。
	if len(first.Source()) == 1 {
		quote.RemoveChild(first)
	} else {
		for child := first.FirstChild(); child != nil; {
			next := child.NextSibling()
			first.RemoveChild(child)
			if text, ok := child.(*ast.Text); ok && (text.SoftLineBreak() || text.HardLineBreak()) {
				break
			}
			child = next
		}
	}
	inner := p.blocks(quote, indent)
	icon := p.icons[style.title]
	blocks := make([]flow.Block, 0, 1+len(inner))
	blocks = append(blocks, flow.Block{Kind: "paragraph", Indent: float64(indent) * 6,
		Runs: []flow.Run{{Text: style.title, Bold: true}}, LeftBorderColor: &style.color, LeadingIcon: &icon,
		Style: &flow.TextStyle{Color: &style.color, SpaceBefore: 2, Set: flow.StyleFirstLineIndent}, KeepWithNext: len(inner) > 0})
	for i := range inner {
		if inner[i].LeftBorderColor == nil {
			inner[i].LeftBorderColor = &style.color
		}
	}
	return append(blocks, inner...), true
}
