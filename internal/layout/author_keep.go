package layout

// 只有逻辑上紧邻且没有强制边界的块才能构成跟随链。
func authorFollows(current, next *authorBlock) bool {
	return !current.block.PageBreakAfter &&
		(current.block.KeepWithNext || current.block.Kind == "heading") &&
		!next.block.PageBreakBefore && next.block.Kind != "page_break" &&
		next.block.Kind != "column_break" && next.section == nil
}

func (p *authorBlock) allNoteLines() []authorLine {
	if p.table != nil {
		return p.table.noteLines(0, len(p.table.rowOffsets)-1)
	}
	return p.lines
}

// 尾块要能真正开始排版：整块同页使用完整高度，其他块使用可满足孤行要求的开头。
func (p *authorBlock) keepTail(limit float64) (float64, []authorLine) {
	if p.block.KeepTogether && p.height() <= limit {
		return p.height(), p.allNoteLines()
	}
	return p.minimumHeight(), p.minimumNoteLines()
}

// 提前决定能够容纳的连续链，再统一换页。已规划的中间块不重复试算后面的整条链。
// 超长链采用能放入整栏的最长前缀；尾块仍可在下一轮继续跟随后面的内容。
func (e *engine) ensureAuthorKeepChain(blocks []authorBlock, start int) (int, error) {
	end := start
	if start+1 >= len(blocks) || !authorFollows(&blocks[start], &blocks[start+1]) {
		return end, nil
	}
	height := 0.0
	var lines, bestLines []authorLine
	bestHeight := 0.0
	for i := start; i+1 < len(blocks) && authorFollows(&blocks[i], &blocks[i+1]); i++ {
		if err := e.author.ctx.Err(); err != nil {
			return start, err
		}
		height += blocks[i].height()
		if height > e.contentHeight+0.001 {
			break
		}
		lines = append(lines, blocks[i].allNoteLines()...)
		tailHeight, tailLines := blocks[i+1].keepTail(e.contentHeight)
		need := height + tailHeight
		if need > e.contentHeight+0.001 {
			break
		}
		candidate := append(append([]authorLine(nil), lines...), tailLines...)
		fresh, err := e.planAuthorFootnotes(candidate, authorPageFootnotes{})
		if err != nil {
			return start, err
		}
		if need+fresh.height > e.contentHeight+0.001 {
			break
		}
		end, bestHeight, bestLines = i+1, need, candidate
	}
	if end > start {
		if err := e.ensureAuthorHeight(bestHeight, bestLines); err != nil {
			return start, err
		}
	}
	return end, nil
}

func (p *authorBlock) minimumLineCount() int {
	orphans, widows := p.block.Orphans, p.block.Widows
	if orphans == 0 {
		orphans = 2
	}
	if widows == 0 {
		widows = 2
	}
	n := min(orphans, len(p.lines))
	// 剩余不足尾页最少行数时，实际排版会整段移走；提前保留整个短段落。
	if len(p.lines)-n < widows {
		n = len(p.lines)
	}
	return n
}
