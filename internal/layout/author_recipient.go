package layout

import (
	"fmt"
	"strings"

	"github.com/zc310/ofd/pkg/creator"
)

// 首轮确实没有首页正文时，携带已加载字体请求重排；不把调用方字体来源再读一次。
type authorRecipientOverflow struct {
	indices []int
	fonts   []creator.Font
}

func (*authorRecipientOverflow) Error() string {
	return "主送机关需要移至版记以保留首页正文"
}

// 只迁移首页正文之前的主送机关，不跨越调用方指定的换页、换栏或分节边界。
func authorRecipientCandidates(blocks []FlowBlock) []int {
	var indices []int
	started := false
	for i, b := range blocks {
		if b.Kind == "section" || b.Kind == "page_break" || b.Kind == "column_break" {
			if started {
				return nil
			}
			continue
		}
		if b.PageBreakBefore && started {
			return nil
		}
		started = true
		switch {
		case b.Kind == "recipient":
			indices = append(indices, i)
		case b.Kind == "heading" && b.Level == 1:
		default:
			return indices
		}
		if b.PageBreakAfter {
			return nil
		}
	}
	return nil
}

type authorColophonRecipients struct {
	block      FlowBlock
	label      authorLine
	lines      []authorLine
	labelWidth float64
}

// 主送机关由主体的“：”改为版记末尾的“。”；富文本的字体、链接及引用字段保持原样。
func authorRecipientRuns(block FlowBlock, last bool) ([]Inline, error) {
	runs := append([]Inline(nil), block.Runs...)
	if block.Text != "" {
		runs = []Inline{{Text: block.Text}}
	}
	for _, run := range runs {
		if run.Footnote != "" {
			return nil, fmt.Errorf("自动移至版记的主送机关不能包含脚注，请将脚注放入正文")
		}
	}
	ending := "，"
	if last {
		ending = "。"
	}
	for i := len(runs) - 1; i >= 0; i-- {
		if strings.TrimSpace(runs[i].Text) == "" {
			if runs[i].PageRef != "" || runs[i].TitleRef != "" || runs[i].CaptionRef != "" {
				runs = append(runs, Inline{Text: ending})
				return runs, nil
			}
			continue
		}
		text := strings.TrimRight(runs[i].Text, " \t\r\n")
		text = strings.TrimSuffix(strings.TrimSuffix(text, "："), ":")
		if !strings.HasSuffix(text, "。") && !strings.HasSuffix(text, ".") {
			text += ending
		}
		runs[i].Text = text
		return runs, nil
	}
	return nil, fmt.Errorf("自动移至版记的主送机关没有可显示文字")
}

// §7.4.2：主送在抄送之上、四号仿宋，续行与冒号后的首字对齐；其间没有分隔线。
func (e *engine) prepareColophonRecipients(style TextStyle, inner float64) ([]authorColophonRecipients, float64, error) {
	var entries []authorColophonRecipients
	height := 0.0
	for i, block := range e.author.opts.relocatedRecipients {
		if err := e.author.ctx.Err(); err != nil {
			return nil, 0, err
		}
		label := e.authorLines([]Inline{{Text: "主送："}}, style, inner, 0)
		if len(label) != 1 {
			return nil, 0, fmt.Errorf("版记主送标签无法放入一行")
		}
		width := e.lineWidth(label[0].atoms)
		runs, err := authorRecipientRuns(block, i == len(e.author.opts.relocatedRecipients)-1)
		if err != nil {
			return nil, 0, err
		}
		lines := e.authorLines(runs, style, inner-width, 0)
		if len(lines) == 0 {
			return nil, 0, fmt.Errorf("版记主送机关没有可显示文字")
		}
		for _, line := range lines {
			if e.lineWidth(line.atoms) > inner-width+0.001 {
				return nil, 0, fmt.Errorf("版记主送机关包含超宽不可断文本")
			}
			height += line.height
		}
		entry := authorColophonRecipients{block: block, lines: lines, labelWidth: width}
		if i == 0 {
			entry.label = label[0]
		}
		entries = append(entries, entry)
	}
	return entries, height, e.author.err
}
