package layout

import (
	"math"
	"unicode/utf8"
)

// authorTextRun 将自然字宽与对齐后的字符推进量一起交给 OFD 写出。
type authorTextRun struct {
	textRun
	deltas []float64
}

func authorAtomScale(item atom) float64 {
	if item.hscale > 0 {
		return item.hscale
	}
	return 1
}

// 两端对齐只伸展 UAX #14 允许的段边界，不拆开英文单词、组合字符或脚注编号。
// 同样式文字仍合并成一个对象；跨字体或链接边界的间距计入下一个对象的位置。
func (e *engine) authorLineRuns(line authorLine, left, width float64, justify bool) []authorTextRun {
	extra := 0.0
	if justify && len(line.gaps) > 0 {
		extra = math.Max(0, width-e.lineWidth(line.atoms)) / float64(len(line.gaps))
	}
	runs := mergeRuns(line.atoms, left)
	// 空白会并入前一个文字对象，但其原字体的宽度必须保留。
	// 先按原始 atom 测量字符，再按合并对象分配 DeltaX，避免用相邻字体重新度量空格。
	var advances []float64
	for _, item := range line.atoms {
		for _, r := range item.text {
			advances = append(advances, e.author.width(string(r), item.size, item.key.font)*authorAtomScale(item))
		}
	}
	result := make([]authorTextRun, 0, len(runs))
	offset, gapIndex := 0, 0
	for _, run := range runs {
		for gapIndex < len(line.gaps) && line.gaps[gapIndex] <= offset {
			gapIndex++
		}
		run.x += float64(gapIndex) * extra
		count := utf8.RuneCountInString(run.text)
		deltas := make([]float64, count-1)
		// GB/T 33190—2016 §11.3：DeltaX 表示相邻字符绘制点的 X 偏移，单位毫米。
		// 固化真实字体推进量与伸展间距，阅读端无需重新计算对齐。
		for i := range deltas {
			advance := advances[offset+i]
			if gapIndex < len(line.gaps) && line.gaps[gapIndex] == offset+i+1 {
				advance += extra
				run.width += extra
				gapIndex++
			}
			deltas[i] = advance
		}
		offset += count
		result = append(result, authorTextRun{textRun: run, deltas: deltas})
	}
	return result
}
