package flow

import (
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/zc310/ofd/pkg/creator"
)

// breakLines 复用 UAX #14 分段；首行缩进仅影响首行的可用宽度。
func (e *engine) breakLines(runs []Run, style TextStyle, width, indent float64) []textLine {
	copyRuns := append([]Run(nil), runs...)
	previousCR := false
	for i := range copyRuns {
		if copyRuns[i].TitleRef != "" {
			copyRuns[i].Text = e.state.opts.titleLabels[copyRuns[i].TitleRef]
			copyRuns[i].Target = copyRuns[i].TitleRef
		}
		if copyRuns[i].Footnote != "" {
			number := e.state.opts.noteNumbers[copyRuns[i].Footnote]
			copyRuns[i].Text = fmt.Sprintf("[%d]", number)
			copyRuns[i].Target = footnoteTarget(number)
		}
		if copyRuns[i].CaptionRef != "" {
			copyRuns[i].Text = e.state.opts.captionLabels[copyRuns[i].CaptionRef]
			copyRuns[i].Target = copyRuns[i].CaptionRef
		}
		if copyRuns[i].PageRef != "" {
			copyRuns[i].Text = e.state.referenceLabel(copyRuns[i].PageRef)
			copyRuns[i].Target = copyRuns[i].PageRef
		}
		text := copyRuns[i].Text
		if text != "" {
			// CRLF 可以跨越样式或空片段；按完整文字流识别一次换行。
			// 引用先展开，可见的引用文字会截断跨片段的 CRLF 配对。
			endsCR := strings.HasSuffix(text, "\r")
			if previousCR && strings.HasPrefix(text, "\n") {
				text = text[1:]
			}
			previousCR = endsCR
		}
		copyRuns[i].Text = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\t", "    ")
	}
	segments := e.segments(copyRuns, style.Size, metricKey{font: style.Font})
	if e.state.opts.Official != nil {
		for _, segment := range segments {
			for i := range segment {
				if math.Abs(segment[i].size-ptToMM(16)) < 0.000001 {
					// 保留三号的纵向字号，用标准 HScale 将 28 个全角字撑满 156mm（§5.2.3）。
					segment[i].hscale = officialCharWidth / ptToMM(16)
					segment[i].width *= segment[i].hscale
				}
			}
		}
	}
	var raw []textLine
	var current []atom
	var gaps []int
	runeCount := 0
	used := indent
	flush := func(wrapped bool) {
		for len(current) > 0 {
			last := &current[len(current)-1]
			trimmed := strings.TrimRightFunc(last.text, unicode.IsSpace)
			if trimmed == last.text {
				break
			}
			runeCount -= utf8.RuneCountInString(last.text) - utf8.RuneCountInString(trimmed)
			if trimmed == "" {
				current = current[:len(current)-1]
				continue
			}
			last.text = trimmed
			last.width = e.state.width(trimmed, last.size, last.key.font) * atomScale(*last)
			break
		}
		for len(gaps) > 0 && gaps[len(gaps)-1] >= runeCount {
			gaps = gaps[:len(gaps)-1]
		}
		line := textLine{atoms: current}
		if wrapped {
			line.gaps = gaps
		}
		raw = append(raw, line)
		current, gaps, runeCount, used = nil, nil, 0, 0
	}
	for _, segment := range segments {
		if len(segment) == 0 {
			flush(false)
			continue
		}
		whitespace := segmentWhitespaceOnly(segment)
		if len(current) == 0 && whitespace {
			continue
		}
		w := e.lineWidth(segment)
		// 尾空白只在后续确有文字时占位；不能因空格越界提前换行或产生空白行。
		if len(current) > 0 && !whitespace && used+e.widthWithoutTrailingSpace(segment) > width+0.001 {
			flush(true)
		}
		if style.Align == "justify" && len(current) > 0 {
			gaps = append(gaps, runeCount)
		}
		current = append(current, segment...)
		for _, item := range segment {
			runeCount += utf8.RuneCountInString(item.text)
		}
		used += w
	}
	if len(current) > 0 {
		flush(false)
	}
	var result []textLine
	for i, line := range raw {
		line.height = ptToMM(style.Size) * style.LineHeight
		if i == 0 {
			line.indent = indent
		}
		line.ascent, line.descent = e.state.metrics(style.Font, ptToMM(style.Size))
		upper, lower := line.ascent, line.descent
		for _, item := range line.atoms {
			ascent, descent := e.state.metrics(item.key.font, item.size)
			if item.rise == 0 {
				line.ascent = math.Max(line.ascent, ascent)
				line.descent = math.Max(line.descent, descent)
			}
			upper = math.Max(upper, ascent+item.rise)
			lower = math.Max(lower, descent-item.rise)
		}
		line.height = math.Max(line.height, line.ascent+line.descent)
		// 优先使用正文既有的基线和行距；上标超出行框时才增加必要的上下留白。
		baseline := line.ascent + (line.height-line.ascent-line.descent)/2
		above, below := math.Max(0, upper-baseline), math.Max(0, lower-(line.height-baseline))
		line.ascent += above
		line.descent += below
		line.height += above + below
		result = append(result, line)
	}
	return result
}

func (e *engine) widthWithoutTrailingSpace(atoms []atom) float64 {
	width := e.lineWidth(atoms)
	for i := len(atoms) - 1; i >= 0; i-- {
		item := atoms[i]
		trimmed := strings.TrimRightFunc(item.text, unicode.IsSpace)
		if trimmed == "" {
			width -= item.width
			continue
		}
		if trimmed != item.text {
			width -= e.state.width(item.text[len(trimmed):], item.size, item.key.font) * atomScale(item)
		}
		break
	}
	return width
}

func (e *engine) drawLine(line textLine, style TextStyle, left, width, top float64) {
	x := left + line.indent
	w := e.lineWidth(line.atoms)
	switch style.Align {
	case "center":
		x += (width - line.indent - w) / 2
	case "right":
		x = left + width - w
	}
	baseline := e.geometry.PageHeight - top + line.ascent + (line.height-line.ascent-line.descent)/2
	for _, positioned := range e.lineRuns(line, x, width-line.indent, style.Align == "justify") {
		run := positioned.textRun
		if strings.TrimSpace(run.text) == "" {
			continue
		}
		size := run.size
		runAscent, runDescent := e.state.metrics(run.key.font, size)
		runBaseline := baseline - run.rise
		runTop := runBaseline - runAscent
		if _, note := e.state.opts.noteTargets[run.target]; note {
			back := footnoteBackTarget(run.target, e.pageIndex, e.state.columnIndex)
			if _, exists := e.state.anchors[back]; !exists {
				e.state.anchors[back] = e.destination(runTop)
			}
			run.target = footnotePageTarget(run.target, e.pageIndex, e.state.columnIndex)
		}
		x0, y0 := 0.0, runAscent
		color := style.Color
		// 代码词元的颜色只改变文字填充，字号、字体、位置和可提取文本保持不变。
		if run.color != nil && run.color != colorText && run.color != colorLink {
			color = run.color
		}
		if (run.href != "" || run.target != "" || run.color == colorLink) && e.state.opts.Official == nil {
			color = colorLink
		}
		if run.code {
			color = colorCode
			e.fillRoundedPanel(run.x, e.geometry.PageHeight-runTop-runAscent-runDescent, run.width, runAscent+runDescent, 0.5, colorInlineCodeBG)
		}
		fill := true
		resource := e.state.fontResources[run.key.font]
		// 字体资源与文字对象使用一致样式（GB/T 33190—2016 §11.1、§11.2），
		// 使阅读端直接选择所嵌入的字体面，避免再次反向合成斜体或字重。
		weight := 400
		if resource.Bold {
			weight = 700
		}
		if run.target != "" {
			e.state.pendingLinks = append(e.state.pendingLinks, pendingLink{page: e.pageIndex, item: len(e.cur().Items), target: run.target})
		}
		e.add(creator.Text{X: run.x, Y: runTop, Width: run.width, Height: runAscent + runDescent,
			Font: run.key.font, Size: size, HScale: run.hscale, Weight: weight, Italic: resource.Italic, Fill: &fill, FillColor: color,
			TextCodes: []creator.TextCode{{Value: run.text, X: &x0, Y: &y0, DeltaX: positioned.deltas}}, Actions: linkActions(run.href)})
		if run.strike {
			e.fillRect(run.x, e.geometry.PageHeight-runBaseline+size*0.3, run.width, size*0.05, color)
		}
	}
}
