package flow

import (
	"fmt"
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
	rise   float64
	color  *creator.Color
	glue   int
	strike bool
	code   bool
	href   string
	target string
}

// segments 按 Unicode UAX #14 断行规则把行内内容切成不可断开的段。
// 每个返回的段内部不允许换行，段与段之间可以换行；空段表示强制换行。
// base 提供整段统一的字族与样式基线，行内 Run 可在其上追加 Bold/Italic。
func (e *engine) segments(inlines []Run, sizePT float64, base metricKey) [][]atom {
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
		if inline.Code {
			if inline.Font == "" {
				inline.Font = e.state.opts.CodeFont
			}
			if resource, ok := e.state.fontResources[inline.Font]; !ok || !resource.FixedWidth {
				e.state.err = fmt.Errorf("%w：行内代码需要已注册的等宽字体 code_font，实际 %q", errContent, inline.Font)
				return nil
			}
		}
		glue := 0
		if inline.Code || inline.Footnote != "" {
			glueID++
			glue = glueID
		}
		style := styledRune{
			key: metricKey{
				font:   base.font,
				bold:   inline.Bold || base.bold,
				italic: inline.Italic || base.italic,
				mono:   inline.Code || base.mono,
				fam:    base.fam,
			},
			size:   size,
			color:  colorText,
			glue:   glue,
			strike: inline.Strike,
			code:   inline.Code,
			href:   inline.Href,
			target: inline.Target,
		}
		if inline.Color != nil {
			style.color = inline.Color
		}
		if inline.Footnote != "" {
			style.size = size * footnoteScale
			style.rise = size * footnoteRise
		}
		switch {
		case inline.Code:
			style.color = colorCode
		case inline.Link:
			style.color = colorLink
		}
		for _, r := range inline.Text {
			if err := e.state.ctx.Err(); err != nil {
				e.state.err = err
				return nil
			}
			style.key.font = e.state.resolveFont(inline, base.font, r)
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
		if err := e.state.ctx.Err(); err != nil {
			e.state.err = err
			return nil
		}
		line := iterator.Line()
		if segment := e.buildSegment(line.Text, line.Offset, styles); len(segment) > 0 {
			result = append(result, segment)
		}
		if line.IsMandatoryBreak {
			result = append(result, nil)
		}
	}
	return joinGlued(result)
}

// buildSegment 把一段文本按样式拆成可绘制的 atom，换行符不参与绘制。
func (e *engine) buildSegment(text []rune, offset int, styles []styledRune) []atom {
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
		width := e.state.width(chunk, style.size, style.key.font)
		atoms = append(atoms, atom{
			text:   chunk,
			key:    style.key,
			size:   style.size,
			rise:   style.rise,
			color:  style.color,
			width:  width,
			space:  isWhitespace(chunk),
			glue:   style.glue,
			strike: style.strike,
			code:   style.code,
			href:   style.href,
			target: style.target,
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
