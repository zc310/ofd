package layout

import (
	"strings"

	"github.com/zc310/ofd/pkg/creator"
)

type authorHeading struct {
	level       int
	title       string
	destination creator.GotoAction
}

func authorHeadingTitle(block FlowBlock) string {
	var text strings.Builder
	text.WriteString(block.Text)
	for _, run := range block.Runs {
		text.WriteString(run.Text)
	}
	return strings.Join(strings.Fields(text.String()), " ")
}

// authorOutlines 依据 GB/T 33190—2016 §7.8 建立层级树，§14.2 的 FitH/XYZ
// 目的地使用最终页下标和栏内位置，由 creator 转换成标准页面 ID 引用。
func authorOutlines(headings []authorHeading) []creator.Outline {
	index := 0
	var build func(level int) []creator.Outline
	build = func(level int) []creator.Outline {
		var result []creator.Outline
		for index < len(headings) && headings[index].level > level {
			heading := headings[index]
			index++
			destination := heading.destination
			node := creator.Outline{Title: heading.title, Actions: []creator.Action{{Event: creator.ActionEventClick, Goto: &destination}}}
			node.Children = build(heading.level)
			result = append(result, node)
		}
		return result
	}
	return build(0)
}
