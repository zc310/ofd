package layout

import (
	"fmt"
	"strings"
	"testing"
)

// benchDocument 构造一份覆盖段落、代码块和表格的文档，用于观察排版阶段的
// 时间与分配。参数固定，构建结果可重复。
func benchDocument(paragraphs, linesPerParagraph int) *Document {
	blocks := make([]Block, 0, paragraphs+2)
	for index := 0; index < paragraphs; index++ {
		inlines := make([]Inline, 0, linesPerParagraph)
		for line := 0; line < linesPerParagraph; line++ {
			inlines = append(inlines, Inline{Text: fmt.Sprintf("第%d行：这是一段用于排版性能测试的中文文本内容。", line)})
		}
		blocks = append(blocks, Block{Kind: KindParagraph, Inlines: inlines})
	}
	blocks = append(blocks, Block{Kind: KindCode, Code: strings.Repeat("code line with some width\n", 200)})
	row := make([]Cell, 0, 6)
	for index := 0; index < 6; index++ {
		row = append(row, Cell{{Text: "单元格内容"}})
	}
	rows := make([][]Cell, 0, 50)
	for index := 0; index < 50; index++ {
		rows = append(rows, row)
	}
	blocks = append(blocks, Block{Kind: KindTable, Table: &Table{Header: rows[0], Rows: rows[1:]}})
	return &Document{
		Letterhead: &Letterhead{Org: "测试"},
		Footer:     &Footer{PageNumber: true, Size: 14},
		Blocks:     blocks,
	}
}

func BenchmarkBuildGBT(b *testing.B) {
	document := benchDocument(200, 8)
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := Build(document, GBTOptions()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuildDefault(b *testing.B) {
	document := benchDocument(200, 8)
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := Build(document, DefaultOptions()); err != nil {
			b.Fatal(err)
		}
	}
}
