package flow

import (
	"fmt"

	"github.com/go-text/typesetting/segmenter"
)

// 代码优先使用 Unicode 断点，但不裁剪首尾空白或丢弃空行。
// 超过整行的长命令、URL、标识符按字素折行，不拆开组合字符，也不改变原文或词元颜色。
func (e *engine) wrapCode(segments [][]atom, width float64) ([][]atom, error) {
	var lines [][]atom
	var current []atom
	used := 0.0
	flush := func() {
		lines = append(lines, current)
		current, used = nil, 0
	}
	for _, segment := range segments {
		if err := e.state.ctx.Err(); err != nil {
			return nil, err
		}
		segmentWidth := e.lineWidth(segment)
		if segmentWidth <= width+0.001 {
			if used+segmentWidth > width+0.001 && len(current) > 0 {
				flush()
			}
			current = append(current, segment...)
			used += segmentWidth
			continue
		}
		// 只有不可断片段自身超过整行时才细分字素；普通词仍整体移到下一行。
		for _, item := range segment {
			var breaker segmenter.Segmenter
			breaker.Init([]rune(item.text))
			iterator := breaker.GraphemeIterator()
			for iterator.Next() {
				if err := e.state.ctx.Err(); err != nil {
					return nil, err
				}
				part := item
				part.text = string(iterator.Grapheme().Text)
				part.width = e.state.width(part.text, part.size, part.key.font)
				part.space = isWhitespace(part.text)
				partWidth := part.width * atomScale(part)
				if partWidth > width+0.001 {
					return nil, fmt.Errorf("%w：代码字素 %q 超过面板宽度 %g，请调整字号或版心", errContent, part.text, width)
				}
				if used+partWidth > width+0.001 && len(current) > 0 {
					flush()
				}
				current = append(current, part)
				used += partWidth
			}
		}
	}
	if len(current) > 0 || len(lines) == 0 {
		flush()
	}
	return lines, e.state.err
}
