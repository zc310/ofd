package textdoc

import (
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestBodyFontSizeUsesMostFrequentSizeAndStableTieBreak(t *testing.T) {
	for _, test := range []struct {
		name  string
		sizes []float64
		want  float64
	}{
		{"empty", nil, 6},
		{"invalid sizes", []float64{0, -1}, 6},
		{"most frequent", []float64{8, 5, 5, 6}, 5},
		{"tie uses smaller", []float64{8, 5, 8, 5}, 5},
	} {
		t.Run(test.name, func(t *testing.T) {
			entries := make([]Entry, len(test.sizes))
			for i, size := range test.sizes {
				entries[i].Size = size
			}
			if got := BodyFontSize(entries); got != test.want {
				t.Fatalf("正文字号=%g，期望 %g", got, test.want)
			}
		})
	}
}

func TestDetectHeadingLevelUsesSizeAndRejectsBodyText(t *testing.T) {
	for _, test := range []struct {
		text       string
		size, body float64
		want       int
	}{
		{"Overview", 12, 6, 1}, {"Overview", 10.5, 6, 2}, {"Overview", 9, 6, 3},
		{"Overview", 8, 6, 4}, {"Overview", 7, 6, 5}, {"Overview", 6, 6, 6},
		{"Overview", 4, 6, 0}, {"Overview", 12, 0, 0}, {"", 12, 6, 0}, {"  ", 12, 6, 0},
		{"lowercase body", 12, 6, 0}, {"(body)", 12, 6, 0}, {"Body sentence.", 12, 6, 0},
		{"Body sentence。", 12, 6, 0}, {strings.Repeat("A", 81), 12, 6, 0},
		{"12", 12, 6, 0}, {"— 12 —", 12, 6, 0},
		{"1. Introduction", 4, 6, 3}, {"**Overview**", 4, 6, 4},
		{"**12 items**", 4, 6, 0}, {"**Heading.**", 4, 6, 0}, {"**Heading\ncontinued**", 4, 6, 0},
	} {
		t.Run(test.text, func(t *testing.T) {
			if got := DetectHeadingLevel(test.text, test.size, test.body); got != test.want {
				t.Fatalf("标题级别=%d，期望 %d", got, test.want)
			}
		})
	}
}

func TestPageNumberRowsDependOnPagePosition(t *testing.T) {
	for _, test := range []struct {
		text      string
		y, height float64
		want      bool
	}{
		{"12", 5, 100, true}, {"— 12 —", 95, 100, true}, {"12", 50, 100, false},
		{"body", 95, 100, false}, {"12", 50, 0, true}, {"12", 50, math.NaN(), true}, {"", 5, 100, false},
	} {
		row := []Entry{{Text: test.text, Y: test.y}}
		if got := IsPageNumberRow(row, test.height); got != test.want {
			t.Errorf("页码判断(%q, y=%g, height=%g)=%t，期望 %t", test.text, test.y, test.height, got, test.want)
		}
	}
}

func TestParagraphStartsRecognizesIndentMarkersAndVerticalGaps(t *testing.T) {
	rows := [][]Entry{
		{{Text: "body one", X: 10, Y: 10, Width: 90, Size: 5}},
		{{Text: "body two", X: 10, Y: 20, Width: 90, Size: 5}},
		{{Text: "indented", X: 16, Y: 30, Width: 84, Size: 5}},
		{{Text: "第二条 内容", X: 10, Y: 40, Width: 90, Size: 5}},
		{{Text: "after gap", X: 10, Y: 70, Width: 90, Size: 5}},
	}
	infos := make([]RowInfo, len(rows))
	for i, row := range rows {
		infos[i] = RowInfo{Text: JoinText(row), MaxSize: 5}
	}
	got := ParagraphStarts(rows, make([]bool, len(rows)), infos)
	if want := []bool{false, false, true, true, true}; !slices.Equal(got, want) {
		t.Fatalf("段落起点=%v，期望 %v", got, want)
	}
	infos[2].IsTitle = true
	inTable := []bool{false, false, false, true, false}
	got = ParagraphStarts(rows, inTable, infos)
	if got[2] || got[3] || got[4] {
		t.Fatalf("标题和表格应重置段间距判定，实际 %v", got)
	}
}

func TestRowsFilterInvalidCoordinatesWithoutMutatingInput(t *testing.T) {
	entries := []Entry{{Text: "right", X: 20, Y: 10, Size: 4, Width: 3}, {Text: "left", X: 3, Y: 11, Size: 6, Width: 12}, {Text: "", X: 1, Y: 1}, {Text: "NaN", X: math.NaN()}, {Text: "Inf", Y: math.Inf(1)}}
	before := slices.Clone(entries)
	rows := Rows(entries)
	if len(rows) != 1 || JoinText(rows[0]) != "left right" {
		t.Fatalf("非法位置过滤或行内排序错误: %+v", rows)
	}
	if RowLeft(rows[0]) != 3 || RowRight(rows[0]) != 23 || RowTop(rows[0]) != 10 || RowSize(rows[0]) != 6 {
		t.Fatal("行边界或最大字号错误")
	}
	// NaN 不等于自身，分别比较有限条目并检查 NaN 位置仍保留。
	if !reflect.DeepEqual(entries[:3], before[:3]) || !math.IsNaN(entries[3].X) || !math.IsInf(entries[4].Y, 1) {
		t.Fatal("聚行不应修改输入")
	}
	if got := RowTolerance(math.NaN(), 1, 2); got != 2 {
		t.Fatalf("无效字号应使用默认容差，实际 %g", got)
	}
	if got := Rows([]Entry{{Text: "bad", X: math.Inf(1)}}); len(got) != 0 {
		t.Fatal("没有有效坐标时不应生成行")
	}
}
