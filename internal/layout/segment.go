package layout

import (
	"strings"
	"unicode"

	"github.com/go-text/typesetting/segmenter"

	"github.com/zc310/ofd/pkg/creator"
)

// styledRune 记录每个字符使用的度量与颜色样式。
type styledRune struct {
	key   metricKey
	size  float64
	color *creator.Color
}

// segments 按 Unicode UAX #14 断行规则把行内内容切成不可断开的段。
// 每个返回的段内部不允许换行，段与段之间可以换行；空段表示强制换行。
// hardBold 用于标题等需要强制加粗的场景。
func (e *engine) segments(inlines []Inline, sizePT float64, hardBold bool) [][]atom {
	size := ptToMM(sizePT)
	var runes []rune
	var styles []styledRune
	for _, inline := range inlines {
		if inline.Text == "" {
			continue
		}
		bold := inline.Bold || hardBold
		style := styledRune{key: metricKey{bold: bold, italic: inline.Italic, mono: inline.Code}, size: size, color: colorText}
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
	return result
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
			text:  chunk,
			key:   style.key,
			size:  style.size,
			color: style.color,
			width: measureWidth(chunk, style.size, style.key),
			space: isWhitespace(chunk),
		})
		index = end
	}
	return atoms
}

// wrapSegments 以段为单位贪心装箱；单个段宽于整行时允许溢出，避免死循环。
func (e *engine) wrapSegments(segments [][]atom, width float64) [][]atom {
	if width <= 0 {
		width = e.contentWidth
	}
	var lines [][]atom
	var current []atom
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
