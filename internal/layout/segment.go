package layout

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-text/typesetting/segmenter"

	"github.com/zc310/ofd/pkg/creator"
)

// styledRune 记录每个字符使用的度量与颜色样式。
type styledRune struct {
	key    metricKey
	size   float64
	color  *creator.Color
	glue   int
	strike bool
}

// segments 按 Unicode UAX #14 断行规则把行内内容切成不可断开的段。
// 每个返回的段内部不允许换行，段与段之间可以换行；空段表示强制换行。
// base 提供整段统一的字族与样式基线，行内 Inline 可在其上追加 Bold/Italic。
func (e *engine) segments(inlines []Inline, sizePT float64, base metricKey) [][]atom {
	size := ptToMM(sizePT)
	count := 0
	for _, inline := range inlines {
		count += utf8.RuneCountInString(inline.Text)
	}
	runes := make([]rune, 0, count)
	styles := make([]styledRune, 0, count)
	glueID := 0
	for _, inline := range inlines {
		if inline.Text == "" {
			continue
		}
		glue := 0
		if inline.Code {
			glueID++
			glue = glueID
		}
		style := styledRune{
			key: metricKey{
				bold:   inline.Bold || base.bold,
				italic: inline.Italic || base.italic,
				mono:   inline.Code || base.mono,
				fam:    base.fam,
			},
			size:   size,
			color:  colorText,
			glue:   glue,
			strike: inline.Strike,
		}
		switch {
		case inline.Code:
			style.color = colorCode
		case inline.Link:
			style.color = colorLink
		}
		for _, r := range inline.Text {
			runes = append(runes, r)
			styles = append(styles, style)
		}
	}
	if len(runes) == 0 {
		return nil
	}
	var breaker segmenter.Segmenter
	breaker.Init(runes)
	var result [][]atom
	iterator := breaker.LineIterator()
	for iterator.Next() {
		line := iterator.Line()
		if segment := buildSegment(line.Text, line.Offset, styles); len(segment) > 0 {
			result = append(result, segment)
		}
		if line.IsMandatoryBreak {
			result = append(result, nil)
		}
	}
	return joinGlued(result)
}

// buildSegment 把一段文本按样式拆成可绘制的 atom，换行符不参与绘制。
func buildSegment(text []rune, offset int, styles []styledRune) []atom {
	var atoms []atom
	for index := 0; index < len(text); {
		if text[index] == '\n' {
			index++
			continue
		}
		style := styles[offset+index]
		end := index
		for end < len(text) && text[end] != '\n' && styles[offset+end] == style {
			end++
		}
		chunk := string(text[index:end])
		atoms = append(atoms, atom{
			text:   chunk,
			key:    style.key,
			size:   style.size,
			color:  style.color,
			width:  measureWidth(chunk, style.size, style.key),
			space:  isWhitespace(chunk),
			glue:   style.glue,
			strike: style.strike,
		})
		index = end
	}
	return atoms
}

// joinGlued 把同一行内代码切出的断点粘回去，避免 ".zip" 从句点拆开。
func joinGlued(segments [][]atom) [][]atom {
	if len(segments) < 2 {
		return segments
	}
	merged := make([][]atom, 0, len(segments))
	merged = append(merged, segments[0])
	for _, segment := range segments[1:] {
		previous := merged[len(merged)-1]
		if len(previous) > 0 && len(segment) > 0 && previous[0].glue > 0 && previous[0].glue == segment[0].glue {
			merged[len(merged)-1] = append(previous, segment...)
			continue
		}
		merged = append(merged, segment)
	}
	return merged
}

// wrapSegments 以段为单位贪心装箱；单个段宽于整行时允许溢出，避免死循环。
func (e *engine) wrapSegments(segments [][]atom, width float64) [][]atom {
	if width <= 0 {
		width = e.contentWidth
	}
	var lines [][]atom
	// lines 里的每一行都是 current 数组的子切片，flush 后不能复用该数组，
	// 只能给下一行一个新的数组；这里按整段 atom 总数给一个有界容量，避免每行
	// 都从零长度反复翻倍扩容。
	total := 0
	for _, segment := range segments {
		total += len(segment)
	}
	if total > wrapLineAtomHint {
		total = wrapLineAtomHint
	}
	current := make([]atom, 0, total)
	currentWidth := 0.0
	flush := func() {
		trimmed := current
		for len(trimmed) > 0 && trimmed[len(trimmed)-1].space {
			trimmed = trimmed[:len(trimmed)-1]
		}
		lines = append(lines, trimmed)
		current = nil
		currentWidth = 0
	}
	for _, segment := range segments {
		if len(segment) == 0 {
			flush()
			continue
		}
		if len(current) == 0 && segmentWhitespaceOnly(segment) {
			continue
		}
		segmentWidth := e.lineWidth(segment)
		if currentWidth+segmentWidth > width && len(current) > 0 {
			flush()
			if segmentWhitespaceOnly(segment) {
				continue
			}
		}
		current = append(current, segment...)
		currentWidth += segmentWidth
	}
	if len(current) > 0 {
		flush()
	} else if len(lines) == 0 {
		lines = append(lines, nil)
	}
	return lines
}

func isWhitespace(text string) bool {
	for _, r := range text {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return text != ""
}

func segmentWhitespaceOnly(segment []atom) bool {
	if len(segment) == 0 {
		return false
	}
	for _, item := range segment {
		if strings.TrimSpace(item.text) != "" {
			return false
		}
	}
	return true
}

// wrapLineAtomHint 是 wrapSegments 为单行预留的 atom 数量上限。行内 atom 数由
// 版心宽度决定而与文档长度无关，取值只需覆盖常见行长即可。
const wrapLineAtomHint = 64
