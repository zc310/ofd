package flow

import (
	"strings"

	"github.com/zc310/ofd/pkg/creator"
)

// atom 是 UAX #14 段内样式一致的最小绘制片段；尺寸和偏移均为毫米。
type atom struct {
	text   string
	key    metricKey
	size   float64
	rise   float64
	hscale float64
	color  *creator.Color
	width  float64
	space  bool
	// glue 相同且非零的相邻片段不允许断开，用于行内代码和脚注编号。
	glue   int
	strike bool
	code   bool
	href   string
	target string
}

// textRun 是一行中相邻且样式一致、可以合并为同一文字对象的片段。
type textRun struct {
	text   string
	key    metricKey
	size   float64
	rise   float64
	hscale float64
	color  *creator.Color
	x      float64
	width  float64
	strike bool
	code   bool
	href   string
	target string
}

func (e *engine) lineWidth(line []atom) float64 {
	width := 0.0
	for _, item := range line {
		width += item.width
	}
	return width
}

// mergeRuns 把空白并入前一片段；不同字体、样式或链接目标必须保留独立对象。
func mergeRuns(line []atom, left float64) []textRun {
	var runs []textRun
	x := left
	for _, item := range line {
		if item.space || strings.TrimSpace(item.text) == "" {
			if len(runs) > 0 {
				runs[len(runs)-1].text += item.text
				runs[len(runs)-1].width += item.width
			}
			x += item.width
			continue
		}
		if len(runs) > 0 {
			last := &runs[len(runs)-1]
			if last.key == item.key && last.size == item.size && last.rise == item.rise && last.hscale == item.hscale && last.color == item.color &&
				last.strike == item.strike && last.code == item.code && last.href == item.href && last.target == item.target {
				last.text += item.text
				last.width += item.width
				x += item.width
				continue
			}
		}
		runs = append(runs, textRun{text: item.text, key: item.key, size: item.size, rise: item.rise, hscale: item.hscale, color: item.color, x: x, width: item.width, strike: item.strike, code: item.code, href: item.href, target: item.target})
		x += item.width
	}
	return runs
}
