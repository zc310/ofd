package flow

import (
	"errors"
	"fmt"
	"strings"
)

// splitCodeLines 保留行首空白，忽略块末尾的空行。
func splitCodeLines(code string) []string {
	raw := strings.Split(strings.ReplaceAll(code, "\r\n", "\n"), "\n")
	for len(raw) > 0 && strings.TrimSpace(raw[len(raw)-1]) == "" {
		raw = raw[:len(raw)-1]
	}
	return raw
}

// expandCodeTabs 将行首制表符展开为四个空格，其他位置展开为一个空格。
func expandCodeTabs(line string) string {
	if !strings.ContainsRune(line, '\t') {
		return line
	}
	var builder strings.Builder
	leading := true
	for _, r := range line {
		switch {
		case r == '\t' && leading:
			builder.WriteString("    ")
		case r == '\t':
			builder.WriteByte(' ')
		default:
			builder.WriteRune(r)
		}
		if r != ' ' && r != '\t' {
			leading = false
		}
	}
	return builder.String()
}

var errContent = errors.New("无效的流式内容")

const codePaddingX = 2.0
const codePaddingY = 1.6

// 代码按物理行保留空白，超宽行在面板内折行，不把标点解释为 Markdown。
// 面板分页复用 placeLines，背景使用 §9.1 表 36 的标准 OFD 路径。
func (e *engine) prepareCode(p preparedBlock) (preparedBlock, error) {
	if len(p.block.Runs) != 0 || p.block.Marker != "" {
		return p, fmt.Errorf("%w：code 块不接受 runs 或 marker：%d / %q", errContent, len(p.block.Runs), p.block.Marker)
	}
	if !e.state.fontResources[p.style.Font].FixedWidth {
		return p, fmt.Errorf("%w：代码块需要等宽字体 code_font，实际 %q", errContent, p.style.Font)
	}
	width := e.contentWidth - 2*codePaddingX - p.markerWidth
	if width <= 0 {
		return p, fmt.Errorf("%w：代码面板内边距超过行宽 %g", errContent, e.contentWidth)
	}
	lines, err := codeSpans(e.state.ctx, p.block.Text, p.block.CodeLang)
	if err != nil {
		return p, err
	}
	for _, spans := range lines {
		inlines := make([]Run, 0, len(spans))
		for _, span := range spans {
			inlines = append(inlines, Run{Text: span.text, Color: span.color})
		}
		// 整个物理行统一寻找 UAX #14 断点，避免颜色边界改变换行位置。
		segments := e.segments(inlines, p.style.Size, metricKey{font: p.style.Font, mono: true})
		if e.state.err != nil {
			return p, e.state.err
		}
		wrapped, err := e.wrapCode(segments, width)
		if err != nil {
			return p, err
		}
		for _, atoms := range wrapped {
			line := textLine{atoms: atoms, height: ptToMM(p.style.Size) * p.style.LineHeight}
			line.ascent, line.descent = e.state.metrics(p.style.Font, ptToMM(p.style.Size))
			for _, item := range line.atoms {
				up, down := e.state.metrics(item.key.font, item.size)
				line.ascent, line.descent = max(line.ascent, up), max(line.descent, down)
			}
			line.height = max(line.height, line.ascent+line.descent)
			if line.height+2*codePaddingY > e.contentHeight {
				return p, fmt.Errorf("%w：代码行及面板内边距超过整页版心 %g", errContent, e.contentHeight)
			}
			p.lines = append(p.lines, line)
		}
	}
	p.extraHeight = 2 * codePaddingY
	if label := strings.TrimSpace(p.block.CodeLang); label != "" {
		style := p.style
		style.Size = 8
		p.codeLabel = e.breakLines([]Run{{Text: label}}, style, width, 0)
		if len(p.codeLabel) != 1 || e.lineWidth(p.codeLabel[0].atoms) > width+0.001 {
			return p, fmt.Errorf("%w：代码语言标签 %q 无法放入一行", errContent, label)
		}
		p.codeLabel[0].height += 1.2
		p.extraHeight += p.codeLabel[0].height
	}
	if len(p.lines) > 0 && p.lines[0].height+p.extraHeight > e.contentHeight {
		return p, fmt.Errorf("%w：代码标签和首行无法同时放入整页版心 %g", errContent, e.contentHeight)
	}
	return p, e.state.err
}
