package flow

import (
	"strconv"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
)

// 用实际嵌入字体覆盖段落、代码与表格，基准只计量现用流式排版入口。
func BenchmarkComposeReport(b *testing.B) {
	blocks := make([]Block, 0, 202)
	for range 200 {
		blocks = append(blocks, Block{Text: strings.Repeat("Paragraph words for layout measurements. ", 8)})
	}
	blocks = append(blocks, Block{Kind: "code", Text: strings.Repeat("code line with some width\n", 200)})
	rows := make([][]string, 50)
	for i := range rows {
		rows[i] = []string{strconv.Itoa(i), "Measured cell", "Final value"}
	}
	blocks = append(blocks, Block{Kind: "table", Table: &Table{Header: []string{"No.", "Description", "Value"}, Rows: rows}})
	opts := Options{
		PageSize: creator.A4, MarginTop: 20, MarginRight: 20, MarginBottom: 20, MarginLeft: 20,
		Theme: MaterialTheme(), BoldFont: "Flow-Bold", CodeFont: "Flow-Mono",
		Fonts: []creator.Font{
			{Name: "Flow-Regular", Data: goregular.TTF},
			{Name: "Flow-Bold", Data: gobold.TTF, Bold: true},
			{Name: "Flow-Mono", Data: gomono.TTF, FixedWidth: true},
		},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := Compose(b.Context(), blocks, opts); err != nil {
			b.Fatal(err)
		}
	}
}
