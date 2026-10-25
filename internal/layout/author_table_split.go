package layout

import (
	"context"
	"fmt"
	"math"
)

// 片段只引用已断行的文字，不修改原单元格。每栏重画完整边框与内边距，
// 已排完的短单元格在后续片段保留空背景，文字不重复。
type authorRowFragment struct {
	cells []authorCell
	ends  []int
}

func (f authorRowFragment) height() float64 {
	// 与普通网格相同，空行仍保留最小可见高度。
	h := 2 * authorCellPadding
	for _, cell := range f.cells {
		h = math.Max(h, cell.height)
	}
	return h
}

func (f authorRowFragment) noteLines() []authorLine {
	var lines []authorLine
	for _, cell := range f.cells {
		lines = append(lines, cell.lines...)
	}
	return lines
}

func authorFragmentCell(cell authorCell, start, end int) authorCell {
	cell.lines = cell.lines[start:end]
	p := cell.appearance.Padding
	cell.height = p.Top + p.Bottom + cell.style.SpaceBefore + cell.style.SpaceAfter
	for _, line := range cell.lines {
		cell.height += line.height
	}
	return cell
}

func (t *authorTable) minimumRowFragment(row int) authorRowFragment {
	f := authorRowFragment{}
	for _, cell := range t.cells[t.rowCells[row]:t.rowCells[row+1]] {
		f.cells = append(f.cells, authorFragmentCell(cell, 0, min(1, len(cell.lines))))
	}
	return f
}

// 在当前栏试算同一逻辑行的各列；新增一行文字时同时计算其脚注，
// 后续片段中的脚注不会挤占当前栏。各列可以容纳不同数量的完整文字行。
func (e *engine) fitAuthorRowFragment(t *authorTable, row int, starts []int, available float64, existing authorPageFootnotes) (authorRowFragment, authorPageFootnotes, bool, error) {
	cells := t.cells[t.rowCells[row]:t.rowCells[row+1]]
	f := authorRowFragment{ends: append([]int(nil), starts...)}
	for i, cell := range cells {
		f.cells = append(f.cells, authorFragmentCell(cell, starts[i], starts[i]))
	}
	plan := existing
	progress := false
	for i, cell := range cells {
		for end := starts[i] + 1; end <= len(cell.lines); end++ {
			if err := e.author.ctx.Err(); err != nil {
				return f, plan, false, err
			}
			previous := f.cells[i]
			f.cells[i] = authorFragmentCell(cell, starts[i], end)
			next, err := e.planAuthorFootnotes(f.cells[i].lines[end-starts[i]-1:], plan)
			if err != nil {
				return f, plan, false, err
			}
			if f.height()+next.height > available+0.001 {
				f.cells[i] = previous
				break
			}
			f.ends[i], plan, progress = end, next, true
		}
	}
	// 空单元格也必须输出有高度的边框；有文字的列必须全部排完才算完成。
	done := true
	for i, cell := range cells {
		done = done && f.ends[i] == len(cell.lines)
	}
	fits := f.height()+plan.height <= available+0.001 && (progress || done)
	return f, plan, fits, nil
}

func (e *engine) drawAuthorRowFragment(t *authorTable, f authorRowFragment) {
	h := f.height()
	for _, cell := range f.cells {
		x := e.contentLeft + t.colOffsets[cell.col]
		width := t.colOffsets[cell.col+cell.colSpan] - t.colOffsets[cell.col]
		e.drawAuthorTableCell(cell, x, e.y, width, h)
	}
	e.y -= h
}

func (e *engine) placeAuthorSplitTable(ctx context.Context, p *authorBlock) error {
	t := p.table
	first, newSegment := true, true
	// 无正文的表格仍走既有路径。
	if len(t.groups) == 0 {
		copyTable := *t
		copyTable.splitRows = false
		copyBlock := *p
		copyBlock.table = &copyTable
		return e.placeAuthorTable(ctx, &copyBlock)
	}
	for _, group := range t.groups {
		row := group.start // split_rows 的正文没有 RowSpan，每个组恰好一行。
		starts := make([]int, t.rowCells[row+1]-t.rowCells[row])
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			gap, overhead := 0.0, 0.0
			plan := e.currentAuthorFootnotes()
			if newSegment {
				if first && e.authorHasContent() {
					gap = p.style.SpaceBefore
				}
				overhead = gap + t.captionHeight(!first) + t.rowOffsets[t.headerRows]
				var err error
				plan, err = e.planAuthorFootnotes(t.noteLines(0, t.headerRows), plan)
				if err != nil {
					return err
				}
			}
			f, next, fits, err := e.fitAuthorRowFragment(t, row, starts, e.y-e.authorFlowBottom()-overhead, plan)
			if err != nil {
				return err
			}
			if !fits {
				if !e.authorHasContent() {
					return fmt.Errorf("grid[%d]: 表题、表头、单元格文字行与脚注无法同时放入整栏版心", row)
				}
				e.nextAuthorColumn()
				newSegment = true
				continue
			}
			e.commitAuthorFootnotes(next)
			if newSegment {
				e.y -= gap
				if first {
					e.recordAuthorAnchor(p.block.ID)
					if e.author.opts.Official != nil && e.pageIndex == 0 {
						e.author.officialFirstBody = true
					}
				}
				e.drawTableCaption(t, !first)
				if t.headerRows > 0 {
					e.drawAuthorTableRows(t, 0, t.headerRows)
				}
				first, newSegment = false, false
			}
			e.drawAuthorRowFragment(t, f)
			starts = f.ends
			done := true
			for i, cell := range t.cells[t.rowCells[row]:t.rowCells[row+1]] {
				done = done && starts[i] == len(cell.lines)
			}
			if done {
				break
			}
			e.nextAuthorColumn()
			newSegment = true
		}
	}
	e.space(p.style.SpaceAfter)
	return e.author.err
}
