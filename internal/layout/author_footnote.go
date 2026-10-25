package layout

import (
	"context"
	"fmt"
	"math"
	"strings"
	"unicode"
)

const authorFootnoteGap = 4.0

// 正文引用采用 75% 字号、上移 0.3em；页底编号仍使用注释正文的字号和基线。
const authorFootnoteScale = 0.75
const authorFootnoteRise = 0.3

type authorFootnoteKey struct {
	target string
	width  float64
}

type authorFootnote struct {
	target      string
	style       TextStyle
	marker      authorLine
	markerWidth float64
	lines       []authorLine
	height      float64
}

type authorPageFootnotes struct {
	notes  []*authorFootnote
	height float64
}

func authorFootnoteTarget(number int) string { return fmt.Sprintf("%snote_%d", authorIDPrefix, number) }
func authorFootnotePageTarget(target string, page, column int) string {
	return fmt.Sprintf("%s_page_%d_column_%d", target, page, column)
}
func authorFootnoteBackTarget(target string, page, column int) string {
	return fmt.Sprintf("%s_ref_%d_column_%d", target, page, column)
}

func prepareAuthorFootnotes(ctx context.Context, blocks []FlowBlock, opts FlowOptions) (FlowOptions, error) {
	definitions := make(map[string]FlowFootnote, len(opts.Footnotes))
	opts.noteNumbers, opts.noteTargets = make(map[string]int), make(map[string]FlowFootnote)
	for i, note := range opts.Footnotes {
		if err := ctx.Err(); err != nil {
			return opts, err
		}
		if note.ID == "" || strings.IndexFunc(note.ID, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
			return opts, fmt.Errorf("footnotes[%d].id: 脚注 ID 不能为空或包含空白、控制字符", i)
		}
		if err := ValidateFlowText(note.ID); err != nil {
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
		if err := ValidateFlowText(note.Text); err != nil {
			return opts, fmt.Errorf("footnotes[%d]: %w", i, err)
		}
		for _, run := range note.Runs {
			if run.Footnote != "" {
				return opts, fmt.Errorf("footnotes[%d]: 不能嵌套脚注", i)
			}
			if err := ValidateFlowText(run.Text); err != nil {
				return opts, fmt.Errorf("footnotes[%d]: %w", i, err)
			}
		}
		definitions[note.ID] = note
	}
	register := func(runs []Inline) error {
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
				opts.noteTargets[authorFootnoteTarget(number)] = note
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

func (e *engine) prepareAuthorFootnote(target string) (*authorFootnote, error) {
	key := authorFootnoteKey{target: target, width: e.contentWidth}
	if note := e.author.footnoteCache[key]; note != nil {
		return note, nil
	}
	definition := e.author.opts.noteTargets[target]
	style := e.author.opts.Theme.Body
	style.Size, style.LineHeight, style.SpaceBefore, style.SpaceAfter, style.FirstLineIndent, style.Align = 9, 1.3, 0, 1, 0, "left"
	if e.author.opts.FootnoteStyle != nil {
		style = mergeTextStyle(style, *e.author.opts.FootnoteStyle)
	}
	if definition.Style != nil {
		style = mergeTextStyle(style, *definition.Style)
	}
	if err := validateTextStyle(style); err != nil {
		return nil, fmt.Errorf("脚注 %q: %w", definition.ID, err)
	}
	if e.author.faces[style.Font] == nil {
		return nil, fmt.Errorf("脚注 %q: 字体 %q 未注册", definition.ID, style.Font)
	}
	number := e.author.opts.noteNumbers[definition.ID]
	marker := e.authorLines([]Inline{{Text: fmt.Sprintf("[%d]", number)}}, style, e.contentWidth, 0)
	if len(marker) != 1 {
		return nil, fmt.Errorf("脚注 %q: 编号无法放入一行", definition.ID)
	}
	note := &authorFootnote{target: target, style: style, marker: marker[0], markerWidth: e.lineWidth(marker[0].atoms) + ptToMM(style.Size)*0.5}
	width := e.contentWidth - note.markerWidth
	if width <= style.FirstLineIndent {
		return nil, fmt.Errorf("脚注 %q: 编号或缩进超过行宽", definition.ID)
	}
	runs := definition.Runs
	if definition.Text != "" {
		runs = []Inline{{Text: definition.Text}}
	}
	note.lines = e.authorLines(runs, style, width, style.FirstLineIndent)
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
	if e.author.err != nil {
		return nil, fmt.Errorf("脚注 %q: %w", definition.ID, e.author.err)
	}
	e.author.footnoteCache[key] = note
	return note, nil
}

// 只试算，不改动页底空间；孤行控制缩减正文行数后会重新试算。
func (e *engine) planAuthorFootnotes(lines []authorLine, existing authorPageFootnotes) (authorPageFootnotes, error) {
	plan := authorPageFootnotes{notes: append([]*authorFootnote(nil), existing.notes...), height: existing.height}
	seen := make(map[string]bool, len(plan.notes))
	for _, note := range plan.notes {
		seen[note.target] = true
	}
	for _, line := range lines {
		for _, atom := range line.atoms {
			if _, exists := e.author.opts.noteTargets[atom.target]; !exists || seen[atom.target] {
				continue
			}
			note, err := e.prepareAuthorFootnote(atom.target)
			if err != nil {
				return plan, err
			}
			if len(plan.notes) == 0 {
				plan.height = authorFootnoteGap
			}
			plan.notes = append(plan.notes, note)
			plan.height += note.height
			seen[atom.target] = true
		}
	}
	return plan, nil
}

func (e *engine) currentAuthorFootnotes() authorPageFootnotes {
	return e.author.pageFootnotes[e.pageIndex][e.author.columnIndex]
}

func (e *engine) commitAuthorFootnotes(plan authorPageFootnotes) {
	e.author.pageFootnotes[e.pageIndex][e.author.columnIndex] = plan
	e.contentBottom = e.authorFlowBottom() + plan.height
}

func (e *engine) fitAuthorLines(lines []authorLine, gap float64) (int, error) {
	plan := e.currentAuthorFootnotes()
	height, count := 0.0, 0
	for _, line := range lines {
		next, err := e.planAuthorFootnotes([]authorLine{line}, plan)
		if err != nil {
			return 0, err
		}
		if e.y-gap-height-line.height < e.authorFlowBottom()+next.height-0.001 {
			break
		}
		plan, height, count = next, height+line.height, count+1
	}
	return count, nil
}

// 标题跟随与整块同页规则，仅在完整一页能够容纳内容及脚注时执行。
func (e *engine) ensureAuthorHeight(height float64, lines []authorLine) error {
	fresh, err := e.planAuthorFootnotes(lines, authorPageFootnotes{})
	if err != nil {
		return err
	}
	if height+fresh.height > e.contentHeight+0.001 {
		return nil
	}
	plan, err := e.planAuthorFootnotes(lines, e.currentAuthorFootnotes())
	if err != nil {
		return err
	}
	if e.y-height < e.authorFlowBottom()+plan.height-0.001 && e.authorHasContent() {
		e.nextAuthorColumn()
	}
	if height+fresh.height > e.contentTop-e.authorFlowBottom()+0.001 {
		return fmt.Errorf("整体块或跟随链超过平衡栏高")
	}
	return nil
}

func (p *authorBlock) minimumNoteLines() []authorLine {
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

func (t *authorTable) noteLines(start, end int) []authorLine {
	var lines []authorLine
	for _, cell := range t.cells[t.rowCells[start]:t.rowCells[end]] {
		lines = append(lines, cell.lines...)
	}
	return lines
}

// 页底注释仍由标准文字、路径及 Goto/Dest 动作表达（GB/T 33190—2016 §9.1、§11.2、§14.2）。
func (e *engine) emitAuthorFootnotes() error {
	for page, columns := range e.author.pageFootnotes {
		for column, plan := range columns {
			if len(plan.notes) == 0 {
				continue
			}
			if err := e.author.ctx.Err(); err != nil {
				return err
			}
			e.pageIndex = page
			e.author.columnIndex = column
			e.setAuthorSectionArea(e.author.pageSettings[page].section, column)
			e.y = e.opts.MarginBottom + plan.height
			e.fillRect(e.contentLeft, e.y-1, math.Min(40, e.contentWidth), 0.15, e.author.opts.Theme.BorderColor)
			e.y -= authorFootnoteGap
			for _, note := range plan.notes {
				e.y -= note.style.SpaceBefore
				top := e.opts.PageHeight - e.y
				destination := e.authorDestination(top)
				e.author.anchors[authorFootnotePageTarget(note.target, page, column)] = destination
				marker := note.marker
				marker.atoms = append([]atom(nil), marker.atoms...)
				for i := range marker.atoms {
					marker.atoms[i].target = authorFootnoteBackTarget(note.target, page, column)
				}
				for i, line := range note.lines {
					if i == 0 {
						marker.height = line.height
						marker.ascent, marker.descent = line.ascent, line.descent
						e.drawAuthorLine(marker, note.style, e.contentLeft, note.markerWidth, e.y)
					}
					e.drawAuthorLine(line, note.style, e.contentLeft+note.markerWidth, e.contentWidth-note.markerWidth, e.y)
					e.y -= line.height
				}
				e.y -= note.style.SpaceAfter
			}
		}
	}
	return e.author.err
}
