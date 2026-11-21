package flow

import (
	"context"
	"fmt"
	"math"
	"strings"
	"unicode"
)

const footnoteGap = 4.0

// 正文引用采用 75% 字号、上移 0.3em；页底编号仍使用注释正文的字号和基线。
const footnoteScale = 0.75
const footnoteRise = 0.3

type footnoteKey struct {
	target string
	width  float64
}

type preparedFootnote struct {
	target      string
	style       TextStyle
	marker      textLine
	markerWidth float64
	lines       []textLine
	height      float64
}

type pageFootnotes struct {
	notes  []*preparedFootnote
	height float64
}

func footnoteTarget(number int) string { return fmt.Sprintf("%snote_%d", reservedIDPrefix, number) }
func footnotePageTarget(target string, page, column int) string {
	return fmt.Sprintf("%s_page_%d_column_%d", target, page, column)
}
func footnoteBackTarget(target string, page, column int) string {
	return fmt.Sprintf("%s_ref_%d_column_%d", target, page, column)
}

func prepareFootnotes(ctx context.Context, blocks []Block, opts Options) (Options, error) {
	definitions := make(map[string]Footnote, len(opts.Footnotes))
	opts.noteNumbers, opts.noteTargets = make(map[string]int), make(map[string]Footnote)
	for i, note := range opts.Footnotes {
		if err := ctx.Err(); err != nil {
			return opts, err
		}
		if note.ID == "" || strings.IndexFunc(note.ID, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
			return opts, fmt.Errorf("footnotes[%d].id: 脚注 ID 不能为空或包含空白、控制字符", i)
		}
		if err := ValidateText(note.ID); err != nil {
			return opts, fmt.Errorf("footnotes[%d].id: %w", i, err)
		}
		if _, exists := definitions[note.ID]; exists {
			return opts, fmt.Errorf("footnotes[%d]: 脚注 ID %q 重复", i, note.ID)
		}
		if note.Text != "" && len(note.Runs) > 0 {
			return opts, fmt.Errorf("footnotes[%d]: text 与 runs 互斥", i)
		}
		if strings.TrimSpace(note.Text) == "" && len(note.Runs) == 0 {
			return opts, fmt.Errorf("footnotes[%d]: 脚注内容不能为空", i)
		}
		if err := ValidateText(note.Text); err != nil {
			return opts, fmt.Errorf("footnotes[%d]: %w", i, err)
		}
		for _, run := range note.Runs {
			if run.Footnote != "" {
				return opts, fmt.Errorf("footnotes[%d]: 不能嵌套脚注", i)
			}
			if err := ValidateText(run.Text); err != nil {
				return opts, fmt.Errorf("footnotes[%d]: %w", i, err)
			}
		}
		definitions[note.ID] = note
	}
	register := func(runs []Run) error {
		for _, run := range runs {
			if run.Footnote == "" {
				continue
			}
			note, exists := definitions[run.Footnote]
			if !exists {
				return fmt.Errorf("脚注 %q 不存在", run.Footnote)
			}
			if opts.noteNumbers[run.Footnote] == 0 {
				number := len(opts.noteNumbers) + 1
				opts.noteNumbers[run.Footnote] = number
				opts.noteTargets[footnoteTarget(number)] = note
			}
		}
		return nil
	}
	for i, block := range blocks {
		if err := ctx.Err(); err != nil {
			return opts, err
		}
		if err := register(block.Runs); err != nil {
			return opts, fmt.Errorf("blocks[%d]: %w", i, err)
		}
		if block.Table == nil {
			continue
		}
		for r, row := range block.Table.Grid {
			for c, cell := range row {
				if err := register(cell.Runs); err != nil {
					return opts, fmt.Errorf("blocks[%d].table.grid[%d][%d]: %w", i, r, c, err)
				}
			}
		}
	}
	return opts, nil
}

func (e *engine) prepareFootnote(target string) (*preparedFootnote, error) {
	key := footnoteKey{target: target, width: e.contentWidth}
	if note := e.state.footnoteCache[key]; note != nil {
		return note, nil
	}
	definition := e.state.opts.noteTargets[target]
	style := e.state.opts.Theme.Body
	style.Size, style.LineHeight, style.SpaceBefore, style.SpaceAfter, style.FirstLineIndent, style.Align = 9, 1.3, 0, 1, 0, "left"
	if e.state.opts.FootnoteStyle != nil {
		style = mergeTextStyle(style, *e.state.opts.FootnoteStyle)
	}
	if definition.Style != nil {
		style = mergeTextStyle(style, *definition.Style)
	}
	if err := validateTextStyle(style); err != nil {
		return nil, fmt.Errorf("脚注 %q: %w", definition.ID, err)
	}
	if e.state.faces[style.Font] == nil {
		return nil, fmt.Errorf("脚注 %q: 字体 %q 未注册", definition.ID, style.Font)
	}
	number := e.state.opts.noteNumbers[definition.ID]
	marker := e.breakLines([]Run{{Text: fmt.Sprintf("[%d]", number)}}, style, e.contentWidth, 0)
	if len(marker) != 1 {
		return nil, fmt.Errorf("脚注 %q: 编号无法放入一行", definition.ID)
	}
	note := &preparedFootnote{target: target, style: style, marker: marker[0], markerWidth: e.lineWidth(marker[0].atoms) + ptToMM(style.Size)*0.5}
	width := e.contentWidth - note.markerWidth
	if width <= style.FirstLineIndent {
		return nil, fmt.Errorf("脚注 %q: 编号或缩进超过行宽", definition.ID)
	}
	runs := definition.Runs
	if definition.Text != "" {
		runs = []Run{{Text: definition.Text}}
	}
	note.lines = e.breakLines(runs, style, width, style.FirstLineIndent)
	visible := false
	for _, line := range note.lines {
		for _, atom := range line.atoms {
			visible = visible || strings.TrimSpace(atom.text) != ""
		}
	}
	if len(note.lines) == 0 || !visible {
		return nil, fmt.Errorf("脚注 %q: 没有可显示的文字", definition.ID)
	}
	note.lines[0].ascent = math.Max(note.lines[0].ascent, note.marker.ascent)
	note.lines[0].descent = math.Max(note.lines[0].descent, note.marker.descent)
	note.lines[0].height = math.Max(note.lines[0].height, note.lines[0].ascent+note.lines[0].descent)
	note.height = style.SpaceBefore + style.SpaceAfter
	for _, line := range note.lines {
		if e.lineWidth(line.atoms)+line.indent > width+0.001 {
			return nil, fmt.Errorf("脚注 %q: 不可断文本超过行宽", definition.ID)
		}
		note.height += line.height
	}
	if e.state.err != nil {
		return nil, fmt.Errorf("脚注 %q: %w", definition.ID, e.state.err)
	}
	e.state.footnoteCache[key] = note
	return note, nil
}

// 只试算，不改动页底空间；孤行控制缩减正文行数后会重新试算。
func (e *engine) planFootnotes(lines []textLine, existing pageFootnotes) (pageFootnotes, error) {
	plan := pageFootnotes{notes: append([]*preparedFootnote(nil), existing.notes...), height: existing.height}
	seen := make(map[string]bool, len(plan.notes))
	for _, note := range plan.notes {
		seen[note.target] = true
	}
	for _, line := range lines {
		for _, atom := range line.atoms {
			if _, exists := e.state.opts.noteTargets[atom.target]; !exists || seen[atom.target] {
				continue
			}
			note, err := e.prepareFootnote(atom.target)
			if err != nil {
				return plan, err
			}
			if len(plan.notes) == 0 {
				plan.height = footnoteGap
			}
			plan.notes = append(plan.notes, note)
			plan.height += note.height
			seen[atom.target] = true
		}
	}
	return plan, nil
}

func (e *engine) currentFootnotes() pageFootnotes {
	return e.state.pageFootnotes[e.pageIndex][e.state.columnIndex]
}

func (e *engine) commitFootnotes(plan pageFootnotes) {
	e.state.pageFootnotes[e.pageIndex][e.state.columnIndex] = plan
	e.contentBottom = e.flowBottom() + plan.height
}

func (e *engine) fitLines(lines []textLine, gap float64) (int, error) {
	plan := e.currentFootnotes()
	height, count := 0.0, 0
	for _, line := range lines {
		next, err := e.planFootnotes([]textLine{line}, plan)
		if err != nil {
			return 0, err
		}
		if e.y-gap-height-line.height < e.flowBottom()+next.height-0.001 {
			break
		}
		plan, height, count = next, height+line.height, count+1
	}
	return count, nil
}

// 标题跟随与整块同页规则，仅在完整一页能够容纳内容及脚注时执行。
func (e *engine) ensureHeight(height float64, lines []textLine) error {
	fresh, err := e.planFootnotes(lines, pageFootnotes{})
	if err != nil {
		return err
	}
	if height+fresh.height > e.contentHeight+0.001 {
		return nil
	}
	plan, err := e.planFootnotes(lines, e.currentFootnotes())
	if err != nil {
		return err
	}
	if e.y-height < e.flowBottom()+plan.height-0.001 && e.hasContent() {
		e.nextColumn()
	}
	if height+fresh.height > e.contentTop-e.flowBottom()+0.001 {
		return fmt.Errorf("整体块或跟随链超过平衡栏高")
	}
	return nil
}

func (p *preparedBlock) minimumNoteLines() []textLine {
	if p.table != nil {
		if p.table.splitRows && len(p.table.groups) > 0 {
			return append(p.table.noteLines(0, p.table.headerRows), p.table.minimumRowFragment(p.table.groups[0].start).noteLines()...)
		}
		end := p.table.headerRows
		if len(p.table.groups) > 0 {
			end = p.table.groups[0].end
		}
		return p.table.noteLines(0, end)
	}
	return p.lines[:p.minimumLineCount()]
}

func (t *preparedTable) noteLines(start, end int) []textLine {
	var lines []textLine
	for _, cell := range t.cells[t.rowCells[start]:t.rowCells[end]] {
		lines = append(lines, cell.lines...)
	}
	return lines
}

// 页底注释仍由标准文字、路径及 Goto/Dest 动作表达（GB/T 33190—2016 §9.1、§11.2、§14.2）。
func (e *engine) emitFootnotes() error {
	for page, columns := range e.state.pageFootnotes {
		for column, plan := range columns {
			if len(plan.notes) == 0 {
				continue
			}
			if err := e.state.ctx.Err(); err != nil {
				return err
			}
			e.pageIndex = page
			e.state.columnIndex = column
			e.setSectionArea(e.state.pageSettings[page].section, column)
			e.y = e.geometry.MarginBottom + plan.height
			e.fillRect(e.contentLeft, e.y-1, math.Min(40, e.contentWidth), 0.15, e.state.opts.Theme.BorderColor)
			e.y -= footnoteGap
			for _, note := range plan.notes {
				e.y -= note.style.SpaceBefore
				top := e.geometry.PageHeight - e.y
				destination := e.destination(top)
				e.state.anchors[footnotePageTarget(note.target, page, column)] = destination
				marker := note.marker
				marker.atoms = append([]atom(nil), marker.atoms...)
				for i := range marker.atoms {
					marker.atoms[i].target = footnoteBackTarget(note.target, page, column)
				}
				for i, line := range note.lines {
					if i == 0 {
						marker.height = line.height
						marker.ascent, marker.descent = line.ascent, line.descent
						e.drawLine(marker, note.style, e.contentLeft, note.markerWidth, e.y)
					}
					e.drawLine(line, note.style, e.contentLeft+note.markerWidth, e.contentWidth-note.markerWidth, e.y)
					e.y -= line.height
				}
				e.y -= note.style.SpaceAfter
			}
		}
	}
	return e.state.err
}
