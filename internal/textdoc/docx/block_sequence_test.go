package docx

import (
	"bytes"
	"errors"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/textdoc"
)

func TestBlockSequencePreservesHeadingsListsAndParagraphRuns(t *testing.T) {
	entries := []textdoc.Entry{
		{Text: "Overview", X: 10, Y: 20, Size: 9, Width: 60, FontName: "Demo Font", Bold: true, Italic: true},
		{Text: "first body.", X: 10, Y: 40, Size: 6, Width: 90},
		{Text: "second body.", X: 10, Y: 50, Size: 6, Width: 90},
		{Text: "1. first", X: 10, Y: 70, Size: 6, Width: 50},
		{Text: "2. second", X: 10, Y: 80, Size: 6, Width: 50},
		{Text: "watermark", X: 10, Y: 90, Size: 6, Width: 50, Source: textdoc.EntrySourceAnnotation},
		{Text: "12", X: 50, Y: 195, Size: 6, Width: 5},
	}
	before := slices.Clone(entries)
	blocks := BlockSequence(entries, 200, PageOptions{}, nil)
	if len(blocks) != 4 || blocks[0].Kind != BlockHeading || blocks[1].Kind != BlockParagraph {
		t.Fatalf("应生成标题、正文和两个列表段落，实际 %+v", blocks)
	}
	heading := blocks[0].Paragraph()
	if heading.Properties == nil || heading.Properties.Style.Value != "Heading1" || len(heading.Runs) != 1 {
		t.Fatalf("标题层级应从 Heading1 开始: %+v", heading)
	}
	run := heading.Runs[0]
	if run.Text.Value != "Overview" || run.Properties.Fonts.ASCII != "Demo Font" || run.Properties.Fonts.EastAsia != "Demo Font" || run.Properties.Size.Value != "51" || run.Properties.Bold.Value != "1" || run.Properties.Italic.Value != "1" {
		t.Fatalf("标题文字格式丢失: %+v", run)
	}
	paragraph := blocks[1].Paragraph()
	if paragraph.Properties != nil || len(paragraph.Runs) != 2 || paragraph.Runs[0].Text.Value != "first body." || paragraph.Runs[1].Text.Value != "second body." {
		t.Fatalf("同段的多行应合并为两个文字 run: %+v", paragraph)
	}
	for i, want := range []string{"first", "second"} {
		paragraph := blocks[i+2].Paragraph()
		if paragraph.Properties == nil || paragraph.Properties.Numbering == nil || paragraph.Properties.Numbering.NumID.Value != "1" || len(paragraph.Runs) != 1 || paragraph.Runs[0].Text.Value != want {
			t.Fatalf("列表应使用自动编号并移除原序号: %+v", paragraph)
		}
	}
	withAnnotations := BlockSequence(entries, 200, PageOptions{Annotations: true}, nil)
	last := withAnnotations[len(withAnnotations)-1].Paragraph()
	if len(last.Runs) != 1 || last.Runs[0].Text.Value != "watermark" {
		t.Fatal("开启批注输出后应保留水印文字")
	}
	if !slices.Equal(entries, before) {
		t.Fatal("块编排不应修改原始条目")
	}
}

func TestBlockSequenceTableWritesConsistentOOXMLGrid(t *testing.T) {
	entries := []textdoc.Entry{
		{Text: "名称", X: 10, Y: 20, Size: 5, Width: 10}, {Text: "数量", X: 50, Y: 20, Size: 5, Width: 10},
		{Text: "项目", X: 10, Y: 30, Size: 5, Width: 10}, {Text: "12", X: 50, Y: 30, Size: 5, Width: 10},
	}
	blocks := BlockSequence(entries, 200, PageOptions{Tables: true}, nil)
	if len(blocks) != 1 || blocks[0].Kind != BlockTable {
		t.Fatalf("应识别为单个表格块: %+v", blocks)
	}
	table := blocks[0].Table()
	if len(table.Grid.Columns) != 2 || len(table.Rows) != 2 || len(table.Rows[0].Cells) != 2 {
		t.Fatalf("表格应保持 2x2 网格: %+v", table)
	}
	if table.Properties.Width.Value != 1134 || table.Grid.Columns[0].Width != 567 || table.Rows[0].Properties.TableHeader.Value != "1" {
		t.Fatal("表格应使用实际列宽并将首行标记为重复表头")
	}
	var output bytes.Buffer
	if err := Write(&output, Options{}, func(w *Writer) error { return w.AddTable(table) }); err != nil {
		t.Fatal(err)
	}
	document := partText(t, output.Bytes(), partDocument)
	for _, want := range []string{`<w:tblLayout w:type="fixed"`, `<w:gridCol w:w="567"`, `<w:tblHeader w:val="1"`, "名称", "数量", "项目", "12"} {
		if !strings.Contains(document, want) {
			t.Errorf("DOCX 表格缺少 %q", want)
		}
	}
}

func TestBlockSequencePlacesImagesBeforeTextAtSameHeight(t *testing.T) {
	entries := []textdoc.Entry{{Text: "body.", Y: 20, Size: 6, Width: 30}}
	images := []textdoc.Image{{Y: 20, Width: 10, Height: 10}, {Y: 50, Width: 10, Height: 10}}
	blocks := BlockSequence(entries, 200, PageOptions{}, images)
	if len(blocks) != 3 || blocks[0].Kind != BlockImage || blocks[1].Kind != BlockParagraph || blocks[2].Kind != BlockImage {
		t.Fatalf("图片应按纵向位置插入并在同高度文字之前: %+v", blocks)
	}
	if blocks := BlockSequence(nil, 200, PageOptions{}, nil); len(blocks) != 0 {
		t.Fatal("空页面不应生成块")
	}
}

func TestFontSizeConversionRejectsInvalidValuesAndRoundsSmallSizes(t *testing.T) {
	for _, size := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		if got := fontSizeHalfPoints(size); got != 0 {
			t.Errorf("无效字号 %g 不应输出 w:sz，实际 %d", size, got)
		}
	}
	if fontSizeHalfPoints(0.01) != 1 || Twips(25.4) != 1440 || TwipsFromPoints(12) != 240 {
		t.Fatal("毫米、磅或最小字号换算错误")
	}
}

func TestWritePreservesBuildAndOutputErrors(t *testing.T) {
	buildError := errors.New("文档构建失败")
	var output bytes.Buffer
	err := Write(&output, Options{}, func(w *Writer) error {
		if err := w.AddParagraph(&Paragraph{Runs: []Run{{Text: NewText("partial content")}}}); err != nil {
			return err
		}
		return buildError
	})
	if !errors.Is(err, buildError) {
		t.Fatalf("应保留构建错误，实际 %v", err)
	}
	if got := partText(t, output.Bytes(), partDocument); !strings.Contains(got, "partial content") || !strings.Contains(got, "</w:document>") {
		t.Fatal("构建失败后仍应正确关闭已开始的 DOCX 包和文档元素")
	}
	writeError := errors.New("输出不可写")
	if err := Write(docxErrorWriter{writeError}, Options{}, func(*Writer) error { return nil }); !errors.Is(err, writeError) {
		t.Fatalf("应保留底层输出错误，实际 %v", err)
	}
	if err := Write(nil, Options{}, func(*Writer) error { return nil }); err == nil {
		t.Fatal("未设置输出时应返回错误")
	}
}

type docxErrorWriter struct{ err error }

func (w docxErrorWriter) Write([]byte) (int, error) { return 0, w.err }
