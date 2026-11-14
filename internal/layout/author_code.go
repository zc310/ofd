package layout

import (
	"errors"
	"fmt"
	"strings"
)

var errFlowContent = errors.New("无效的流式内容")

const authorCodePaddingX = 2.0
const authorCodePaddingY = 1.6

// 代码按物理行保留空白，超宽行在面板内折行，不把标点解释为 Markdown。
// 面板分页复用 placeAuthorLines，背景使用 §9.1 表 36 的标准 OFD 路径。
func (e *engine) prepareAuthorCode(p authorBlock) (authorBlock, error) {
	if len(p.block.Runs) != 0 || p.block.Marker != "" {
		return p, fmt.Errorf("%w：code 块不接受 runs 或 marker：%d / %q", errFlowContent, len(p.block.Runs), p.block.Marker)
	}
	if !e.author.fontResources[p.style.Font].FixedWidth {
		return p, fmt.Errorf("%w：代码块需要等宽字体 code_font，实际 %q", errFlowContent, p.style.Font)
	}
	width := e.contentWidth - 2*authorCodePaddingX - p.markerWidth
	if width <= 0 {
		return p, fmt.Errorf("%w：代码面板内边距超过行宽 %g", errFlowContent, e.contentWidth)
	}
	lines, err := authorCodeSpans(e.author.ctx, p.block.Text, p.block.CodeLang)
	if err != nil {
		return p, err
	}
	for _, spans := range lines {
		inlines := make([]Inline, 0, len(spans))
		for _, span := range spans {
			inlines = append(inlines, Inline{Text: span.text, Color: span.color})
		}
		// 整个物理行统一寻找 UAX #14 断点，避免颜色边界改变换行位置。
		segments := e.segments(inlines, p.style.Size, metricKey{font: p.style.Font, mono: true})
		if e.author.err != nil {
			return p, e.author.err
		}
		wrapped, err := e.wrapAuthorCode(segments, width)
		if err != nil {
			return p, err
		}
		for _, atoms := range wrapped {
			line := authorLine{atoms: atoms, height: ptToMM(p.style.Size) * p.style.LineHeight}
			line.ascent, line.descent = e.author.metrics(p.style.Font, ptToMM(p.style.Size))
			for _, item := range line.atoms {
				up, down := e.author.metrics(item.key.font, item.size)
				line.ascent, line.descent = max(line.ascent, up), max(line.descent, down)
			}
			line.height = max(line.height, line.ascent+line.descent)
			if line.height+2*authorCodePaddingY > e.contentHeight {
				return p, fmt.Errorf("%w：代码行及面板内边距超过整页版心 %g", errFlowContent, e.contentHeight)
			}
			p.lines = append(p.lines, line)
		}
	}
	p.extraHeight = 2 * authorCodePaddingY
	if label := strings.TrimSpace(p.block.CodeLang); label != "" {
		style := p.style
		style.Size = 8
		p.codeLabel = e.authorLines([]Inline{{Text: label}}, style, width, 0)
		if len(p.codeLabel) != 1 || e.lineWidth(p.codeLabel[0].atoms) > width+0.001 {
			return p, fmt.Errorf("%w：代码语言标签 %q 无法放入一行", errFlowContent, label)
		}
		p.codeLabel[0].height += 1.2
		p.extraHeight += p.codeLabel[0].height
	}
	if len(p.lines) > 0 && p.lines[0].height+p.extraHeight > e.contentHeight {
		return p, fmt.Errorf("%w：代码标签和首行无法同时放入整页版心 %g", errFlowContent, e.contentHeight)
	}
	return p, e.author.err
}
