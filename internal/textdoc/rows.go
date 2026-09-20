package textdoc

import (
	"math"
	"sort"
	"strings"
)

// Rows 按 Y 坐标把文字条目聚成行，行内按 X 排序。
func Rows(entries []Entry) [][]Entry {
	valid := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if entry.Text == "" || !Finite(entry.X) || !Finite(entry.Y) {
			continue
		}
		valid = append(valid, entry)
	}
	if len(valid) == 0 {
		return nil
	}
	sortByYThenX(valid)

	rows := make([][]Entry, 0)
	for start := 0; start < len(valid); {
		rowY := valid[start].Y
		rowSize := valid[start].Size
		end := start + 1
		for end < len(valid) {
			if valid[end].Y-rowY > RowTolerance(rowSize, valid[end].Size, 1) {
				break
			}
			if valid[end].Size > rowSize {
				rowSize = valid[end].Size
			}
			end++
		}
		row := valid[start:end]
		sort.SliceStable(row, func(i, j int) bool { return row[i].X < row[j].X })
		rows = append(rows, row)
		start = end
	}
	return rows
}

// JoinText 用空格连接一行中的所有文字。
func JoinText(row []Entry) string {
	var builder strings.Builder
	for i, entry := range row {
		if i > 0 {
			builder.WriteByte(' ')
		}
		builder.WriteString(entry.Text)
	}
	return builder.String()
}

// RowTop 返回一行的最小 Y 坐标。
func RowTop(row []Entry) float64 {
	top := math.Inf(1)
	for _, entry := range row {
		if entry.Y < top {
			top = entry.Y
		}
	}
	return top
}

// RowLeft 返回一行的最小 X 坐标。
func RowLeft(row []Entry) float64 {
	left := math.Inf(1)
	for _, entry := range row {
		if entry.X < left {
			left = entry.X
		}
	}
	return left
}

// RowRight 返回一行的最大右边界。
func RowRight(row []Entry) float64 {
	right := math.Inf(-1)
	for _, entry := range row {
		if end := entry.X + entry.Width; end > right {
			right = end
		}
	}
	return right
}

// RowSize 返回一行中的最大字号。
func RowSize(row []Entry) float64 {
	size := 0.0
	for _, entry := range row {
		if entry.Size > size {
			size = entry.Size
		}
	}
	return size
}

// RowTolerance 返回聚行时允许的纵向容差。
func RowTolerance(a, b, unit float64) float64 {
	size := math.Max(a, b)
	if !Finite(size) || size <= 0 {
		return unit
	}
	return size * 0.5
}
