package textdoc

import (
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	// tableGapMinMM 判定同一行内两个单元格之间的最小水平间距（毫米）。
	// 小于该间距的文字视为同一单元格（如“共计 22 项”）。
	tableGapMinMM = 3.0
	// tableMinRows 构成表格所需的最少行数（含表头）。
	tableMinRows = 2
	// tableMinColumns 构成表格所需的最少列数。
	tableMinColumns = 2
	// tableContinuationRatio 续行文字与所属表格行允许的最大纵向距离，
	// 以字号为倍数；用于把跨行的单元格内容合并到对应行。
	tableContinuationRatio = 1.6
)

// Table 描述一个已识别的表格。Start/End 是页内行索引（含），Header 为表头
// 单元格文本，Rows 为数据行。
type Table struct {
	Start  int
	End    int
	Header []string
	Rows   [][]string
}

// tableCell 是一行内按水平间距合并后的单元格。
type tableCell struct {
	entries []Entry
	start   float64
	end     float64
}

// tableRowMeta 保存一行的几何信息与切分后的单元格。
type tableRowMeta struct {
	top   float64
	size  float64
	cells []tableCell
}

// DetectTables 在已按行分组的文字条目中识别表格。算法：
//  1. 行内按水平间距切分单元格，含两个及以上单元格的行为候选网格行；
//  2. 用候选网格行的单元格区间求并，间隔（白槽）处切分得到列；
//  3. 每个候选行的单元格必须各自落在唯一列内且覆盖至少两列，才认定为表格行；
//  4. 只有一个单元格且落在唯一列内的行作为续行，合并到最近的表格行；
//  5. 被非表格行（标题、页脚等）隔开的多行网格行组成一个表格。
func DetectTables(rows [][]Entry) []Table {
	if len(rows) < tableMinRows {
		return nil
	}
	metas := make([]tableRowMeta, len(rows))
	for i, row := range rows {
		metas[i] = tableRowMeta{
			top:   RowTop(row),
			size:  RowSize(row),
			cells: splitCells(row),
		}
	}

	intervals := make([][2]float64, 0, len(rows)*2)
	for _, meta := range metas {
		if len(meta.cells) < tableMinColumns {
			continue
		}
		for _, cell := range meta.cells {
			intervals = append(intervals, [2]float64{cell.start, cell.end})
		}
	}
	columns := mergeIntervals(intervals, tableGapMinMM)
	if len(columns) < tableMinColumns {
		return nil
	}

	const (
		rowBarrier = iota
		rowGrid
		rowContinuation
	)
	kinds := make([]int, len(metas))
	for i, meta := range metas {
		indices := make([]int, 0, len(meta.cells))
		aligned := true
		for _, cell := range meta.cells {
			column := columnIndex(cell, columns)
			if column < 0 {
				aligned = false
				break
			}
			indices = append(indices, column)
		}
		switch {
		case aligned && len(meta.cells) >= tableMinColumns && uniqueInts(indices) >= tableMinColumns:
			kinds[i] = rowGrid
		case len(meta.cells) == 1 && aligned:
			kinds[i] = rowContinuation
		default:
			kinds[i] = rowBarrier
		}
	}

	// 把续行合并到最近的表格行；无法归并的续行退化为分隔行。
	type continuation struct {
		row  int
		cell tableCell
		col  int
	}
	continuations := map[int][]continuation{}
	for i, meta := range metas {
		if kinds[i] != rowContinuation {
			continue
		}
		column := columnIndex(meta.cells[0], columns)
		best, bestDistance := -1, math.Inf(1)
		for j := range metas {
			if kinds[j] != rowGrid {
				continue
			}
			if distance := math.Abs(metas[j].top - meta.top); distance < bestDistance {
				best, bestDistance = j, distance
			}
		}
		if best < 0 {
			kinds[i] = rowBarrier
			continue
		}
		limit := tableContinuationRatio * math.Max(meta.size, metas[best].size)
		if bestDistance > limit {
			kinds[i] = rowBarrier
			continue
		}
		continuations[best] = append(continuations[best], continuation{row: i, cell: meta.cells[0], col: column})
	}

	// 收集被非表格行隔开的网格行段。
	var runs [][]int
	current := make([]int, 0)
	for i := range metas {
		switch kinds[i] {
		case rowGrid:
			current = append(current, i)
		case rowContinuation:
			// 续行不打断表格。
		default:
			if len(current) > 0 {
				runs = append(runs, current)
				current = nil
			}
		}
	}
	if len(current) > 0 {
		runs = append(runs, current)
	}

	tables := make([]Table, 0, len(runs))
	for _, run := range runs {
		if len(run) < tableMinRows {
			continue
		}
		table := Table{Start: run[0], End: run[len(run)-1]}
		for position, rowIndex := range run {
			byColumn := map[int][]tableCell{}
			for _, cell := range metas[rowIndex].cells {
				column := columnIndex(cell, columns)
				if column >= 0 {
					byColumn[column] = append(byColumn[column], cell)
				}
			}
			for _, extra := range continuations[rowIndex] {
				byColumn[extra.col] = append(byColumn[extra.col], extra.cell)
				table.Start = min(table.Start, extra.row)
				table.End = max(table.End, extra.row)
			}
			cells := make([]string, len(columns))
			for column, cellList := range byColumn {
				cells[column] = joinCells(cellList)
			}
			if position == 0 {
				table.Header = cells
			} else {
				table.Rows = append(table.Rows, cells)
			}
		}
		tables = append(tables, table)
	}
	return tables
}

// splitCells 按水平间距把一行文字切成单元格。间距小于
// max(tableGapMinMM, 0.6*字号) 的相邻文字并入同一单元格。
func splitCells(row []Entry) []tableCell {
	cells := make([]tableCell, 0, len(row))
	for _, entry := range row {
		start := entry.X
		end := entry.X + entry.Width
		if len(cells) > 0 {
			last := &cells[len(cells)-1]
			if start <= last.end+cellGap(entry.Size) {
				last.entries = append(last.entries, entry)
				if end > last.end {
					last.end = end
				}
				continue
			}
		}
		cells = append(cells, tableCell{entries: []Entry{entry}, start: start, end: end})
	}
	return cells
}

func cellGap(size float64) float64 {
	if !Finite(size) || size <= 0 {
		return tableGapMinMM
	}
	return math.Max(tableGapMinMM, size*0.6)
}

// mergeIntervals 合并重叠或间距小于 gap 的区间，得到候选列。
func mergeIntervals(intervals [][2]float64, gap float64) [][2]float64 {
	if len(intervals) == 0 {
		return nil
	}
	sorted := make([][2]float64, len(intervals))
	copy(sorted, intervals)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i][0] < sorted[j][0] })

	merged := make([][2]float64, 0, len(sorted))
	merged = append(merged, sorted[0])
	for _, interval := range sorted[1:] {
		last := &merged[len(merged)-1]
		if interval[0] <= last[1]+gap {
			if interval[1] > last[1] {
				last[1] = interval[1]
			}
			continue
		}
		merged = append(merged, interval)
	}
	return merged
}

// columnIndex 返回单元格所属列；若跨越列边界（合并单元格）返回 -1。
func columnIndex(cell tableCell, columns [][2]float64) int {
	for i := 0; i < len(columns)-1; i++ {
		boundary := (columns[i][1] + columns[i+1][0]) / 2
		if cell.start < boundary && cell.end > boundary {
			return -1
		}
	}
	center := (cell.start + cell.end) / 2
	best, bestDistance := -1, math.Inf(1)
	for i, column := range columns {
		if center >= column[0] && center <= column[1] {
			return i
		}
		if distance := math.Abs(center - (column[0]+column[1])/2); distance < bestDistance {
			best, bestDistance = i, distance
		}
	}
	return best
}

func uniqueInts(values []int) int {
	seen := map[int]struct{}{}
	for _, value := range values {
		seen[value] = struct{}{}
	}
	return len(seen)
}

// joinCells 按阅读顺序拼接同一单元格内的文字对象。同一视觉行内不同文字对象的
// y 会有零点几毫米的基线差异，需要按字号容差先聚行再按 x 排序；中文之间不加
// 空格，仅相邻的 ASCII 字母数字之间补一个空格。
func joinCells(cells []tableCell) string {
	type piece struct {
		y    float64
		x    float64
		size float64
		text string
	}
	pieces := make([]piece, 0, len(cells))
	maxSize := 0.0
	for _, cell := range cells {
		for _, entry := range cell.entries {
			pieces = append(pieces, piece{y: entry.Y, x: entry.X, size: entry.Size, text: entry.Text})
			if entry.Size > maxSize {
				maxSize = entry.Size
			}
		}
	}
	if len(pieces) == 0 {
		return ""
	}
	sort.SliceStable(pieces, func(i, j int) bool { return pieces[i].y < pieces[j].y })

	tolerance := 0.5 * maxSize
	if !Finite(tolerance) || tolerance <= 0 {
		tolerance = 1
	}
	lines := make([]int, len(pieces))
	line, lineY := 0, pieces[0].y
	for i := range pieces {
		if pieces[i].y-lineY > tolerance {
			line++
			lineY = pieces[i].y
		}
		lines[i] = line
	}
	order := make([]int, len(pieces))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		left, right := order[i], order[j]
		if lines[left] != lines[right] {
			return lines[left] < lines[right]
		}
		return pieces[left].x < pieces[right].x
	})

	var builder strings.Builder
	for position, index := range order {
		if position > 0 && needSpace(builder.String(), pieces[index].text) {
			builder.WriteByte(' ')
		}
		builder.WriteString(pieces[index].text)
	}
	return strings.TrimSpace(builder.String())
}

func needSpace(previous, next string) bool {
	if previous == "" || next == "" {
		return false
	}
	prevRune, _ := utf8.DecodeLastRuneInString(previous)
	nextRune, _ := utf8.DecodeRuneInString(next)
	return asciiNum(prevRune) && asciiNum(nextRune)
}

func asciiNum(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}
