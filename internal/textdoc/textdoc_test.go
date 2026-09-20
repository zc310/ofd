package textdoc

import (
	"testing"
	"unicode/utf8"
)

func TestEnsureUTF8DecodesGBKBytes(t *testing.T) {
	// "中文" 的 GBK 字节不是合法 UTF-8，应被解码为 UTF-8 中文。
	gbk := []byte{0xD6, 0xD0, 0xCE, 0xC4}
	if got := EnsureUTF8(string(gbk)); got != "中文" {
		t.Fatalf("EnsureUTF8(GBK) = %q, want %q", got, "中文")
	}
	// 已经是合法 UTF-8 的文本原样返回。
	if got := EnsureUTF8("中文"); got != "中文" {
		t.Fatalf("valid utf8 changed: %q", got)
	}
	// 无法按 GBK 解码的字节也必须得到合法 UTF-8，不能输出非法字节。
	if got := EnsureUTF8("\xff\xfe\xff"); !utf8.ValidString(got) {
		t.Fatalf("result not valid utf8: %q", got)
	}
}

func TestDisplayWidthCountsWideCharactersAsTwo(t *testing.T) {
	if got := DisplayWidth("中"); got != 2 {
		t.Fatalf("DisplayWidth(中) = %d, want 2", got)
	}
	if got := DisplayWidth("a中b"); got != 4 {
		t.Fatalf("DisplayWidth(a中b) = %d, want 4", got)
	}
}

func TestRowsGroupsByYAndOrdersByX(t *testing.T) {
	entries := []Entry{
		{Text: "B", X: 60, Y: 10, Size: 5, Width: 5},
		{Text: "A", X: 10, Y: 10, Size: 5, Width: 5},
		{Text: "C", X: 10, Y: 30, Size: 5, Width: 5},
	}
	rows := Rows(entries)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if JoinText(rows[0]) != "A B" {
		t.Fatalf("row0 = %q, want %q", JoinText(rows[0]), "A B")
	}
	if JoinText(rows[1]) != "C" {
		t.Fatalf("row1 = %q, want %q", JoinText(rows[1]), "C")
	}
}

func TestDetectTablesBorderless(t *testing.T) {
	rows := [][]Entry{
		{
			{Text: "序号", X: 30, Y: 52, Size: 5.2, Width: 10.75},
			{Text: "编号", X: 49, Y: 52, Size: 5.2, Width: 10.75},
			{Text: "项目名称", X: 83, Y: 52, Size: 5.2, Width: 21.84},
		},
		{
			{Text: "1", X: 30, Y: 63, Size: 5.2, Width: 5.2},
			{Text: "A-1", X: 49, Y: 63, Size: 5.2, Width: 17.7},
			{Text: "船拳", X: 83, Y: 63, Size: 5.2, Width: 10.75},
		},
	}
	tables := DetectTables(rows)
	if len(tables) != 1 {
		t.Fatalf("tables = %d, want 1", len(tables))
	}
	table := tables[0]
	if len(table.Header) != 3 || table.Header[0] != "序号" || table.Header[2] != "项目名称" {
		t.Fatalf("header = %v", table.Header)
	}
	if len(table.Rows) != 1 || table.Rows[0][2] != "船拳" {
		t.Fatalf("rows = %v", table.Rows)
	}
}

func TestDetectTablesIgnoresSingleColumn(t *testing.T) {
	rows := [][]Entry{
		{{Text: "段落一", X: 30, Y: 50, Size: 5.2, Width: 30}},
		{{Text: "段落二", X: 30, Y: 60, Size: 5.2, Width: 30}},
		{{Text: "段落三", X: 30, Y: 70, Size: 5.2, Width: 30}},
	}
	if tables := DetectTables(rows); len(tables) != 0 {
		t.Fatalf("single-column rows detected as table: %v", tables)
	}
}

func TestCountAndCollect(t *testing.T) {
	if got := Count(nil); got != 0 {
		t.Fatalf("Count(nil) = %d, want 0", got)
	}
	if pages := Collect(nil, 0, 0); len(pages) != 0 {
		t.Fatalf("Collect(nil) = %d pages, want 0", len(pages))
	}
}
