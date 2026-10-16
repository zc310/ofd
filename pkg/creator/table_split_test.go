package creator

import (
	"archive/zip"
	"bytes"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

// splitTestTable 造一张可跨页的表：每行文字带行号，便于确认既不丢行也不重复。
func splitTestTable(rows int, y float64) Table {
	table := Table{
		X: 20, Y: y, Width: 170,
		HeaderRows:          1,
		Caption:             "按销售方汇总",
		ContinuationCaption: "{caption}（续）",
		Style: TableStyle{
			HorizontalLines: true,
			VerticalLines:   true,
			LineWidth:       0.2,
			OuterLineWidth:  0.4,
			HeaderLineWidth: 0.35,
		},
		Rows: []TableRow{{Cells: []TableCell{
			{Value: "项目", Size: 4.2, Align: "center"},
			{Value: "金额", Size: 4.2, Align: "right"},
		}}},
	}
	for i := 1; i <= rows; i++ {
		table.Rows = append(table.Rows, TableRow{Cells: []TableCell{
			{Value: "行" + strconv.Itoa(i) + "号", Size: 4.2},
			{Value: "12,300.00", Size: 4.2, Align: "right"},
		}})
	}
	return table
}

// pageContents 读出包内各页的 Content.xml，下标即页序。
func pageContents(t *testing.T, doc Document) []string {
	t.Helper()
	var buf bytes.Buffer
	if err := Create(doc, &buf); err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("读取生成的 OFD 失败: %v", err)
	}
	pages := map[string]string{}
	for _, file := range zr.File {
		if !strings.HasSuffix(file.Name, "/Content.xml") {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body := new(bytes.Buffer)
		if _, err := body.ReadFrom(rc); err != nil {
			t.Fatal(err)
		}
		_ = rc.Close()
		pages[file.Name] = body.String()
	}
	// 按包内路径排序，不能按内容排：内容相近的页序换了会看不出问题。
	names := make([]string, 0, len(pages))
	for name := range pages {
		names = append(names, name)
	}
	sort.Strings(names)
	ordered := make([]string, 0, len(names))
	for _, name := range names {
		ordered = append(ordered, pages[name])
	}
	return ordered
}

var splitRowRe = regexp.MustCompile(`行[0-9]+号`)

func splitTestDoc(pages ...Page) Document {
	return Document{
		ID:       "table-split",
		PageSize: A4,
		Fonts:    []Font{{Name: "SimHei", FamilyName: "SimHei", Charset: "prc"}},
		Pages:    pages,
	}
}

// 放得下的表格不该被拆，页数与内容都保持原样。
func TestSplittingPagesKeepsFittingTableOnOnePage(t *testing.T) {
	pages := pageContents(t, splitTestDoc(Page{Items: []Item{splitTestTable(5, 30)}}))
	if len(pages) != 1 {
		t.Fatalf("5 行表格应仍为 1 页，实际 %d 页", len(pages))
	}
	if got := len(splitRowRe.FindAllString(pages[0], -1)); got != 5 {
		t.Errorf("首页含 %d 个数据行，期望 5", got)
	}
}

// 行数增多时页数跟着增长，每一行恰好出现一次，表头每页一次，续页有续表题。
func TestSplittingPagesSplitsWithoutLosingRows(t *testing.T) {
	prev := 0
	for _, rows := range []int{40, 120} {
		pages := pageContents(t, splitTestDoc(Page{Items: []Item{splitTestTable(rows, 30)}}))
		if len(pages) <= prev {
			t.Fatalf("%d 行生成 %d 页，未超过上一次的 %d 页", rows, len(pages), prev)
		}
		prev = len(pages)

		seen := 0
		for index, xml := range pages {
			seen += len(splitRowRe.FindAllString(xml, -1))
			if got := strings.Count(xml, ">项目<"); got != 1 {
				t.Errorf("%d 行第 %d 页表头出现 %d 次，期望 1", rows, index+1, got)
			}
			if index > 0 && !strings.Contains(xml, "续") {
				t.Errorf("%d 行第 %d 页缺少续表题", rows, index+1)
			}
		}
		if seen != rows {
			t.Errorf("%d 行拆页后共 %d 个数据行文字，期望 %d（既不丢行也不重复）", rows, seen, rows)
		}
	}
}

// 拆出的每一页都必须完整落在页面内：曾经的 bug 是超出的行被画到页底之外。
func TestSplittingPagesKeepsEveryItemInsidePage(t *testing.T) {
	boundary := regexp.MustCompile(`Boundary="([\d.]+) ([\d.]+) ([\d.]+) ([\d.]+)"`)
	for _, xml := range pageContents(t, splitTestDoc(Page{Items: []Item{splitTestTable(120, 30)}})) {
		for _, m := range boundary.FindAllStringSubmatch(xml, -1) {
			box := make([]float64, 4)
			for i := range box {
				box[i], _ = strconv.ParseFloat(m[i+1], 64)
			}
			if box[1]+box[3] > A4.Height+0.01 {
				t.Errorf("图元底边 %.2fmm 超出页高 %.0fmm", box[1]+box[3], A4.Height)
			}
			if box[0]+box[2] > A4.Width+0.01 {
				t.Errorf("图元右边 %.2fmm 超出页宽 %.0fmm", box[0]+box[2], A4.Width)
			}
		}
	}
}

// 页内的其它图元只出现在首片那一页，不能跟着每一页重复。
func TestSplittingPagesKeepsOtherItemsOnFirstPageOnly(t *testing.T) {
	title := Text{X: 20, Y: 12, Width: 100, Height: 6, Value: "分页续表验证", Size: 5, Font: "SimHei"}
	pages := pageContents(t, splitTestDoc(Page{Items: []Item{title, splitTestTable(120, 30)}}))
	if len(pages) < 2 {
		t.Fatalf("期望至少 2 页，实际 %d 页", len(pages))
	}
	if !strings.Contains(pages[0], "分页续表验证") {
		t.Error("首页缺少标题文字")
	}
	for index, xml := range pages[1:] {
		if strings.Contains(xml, "分页续表验证") {
			t.Errorf("第 %d 页重复出现了标题文字", index+2)
		}
	}
}

// 一页里两张表各自跨页时，输出页数取各表片数的最大值，短的那张不重复出现。
func TestSplittingPagesTakesMaxFragmentCount(t *testing.T) {
	doc := splitTestDoc(Page{Items: []Item{splitTestTable(120, 30), splitTestTable(5, 30)}})
	pages := pageContents(t, doc)
	if len(pages) < 2 {
		t.Fatalf("期望至少 2 页，实际 %d 页", len(pages))
	}
	// 短表放得下，只应出现在首片那一页。
	first := strings.Count(pages[0], "行1号") + strings.Count(pages[0], "行5号")
	if first < 2 {
		t.Error("首片缺少短表的数据行")
	}
	for index, xml := range pages[1:] {
		if strings.Count(xml, "行5号") > 0 && strings.Count(xml, "行120号") > 0 {
			t.Errorf("第 %d 页同时含长短两表的末行，说明短表被拉长", index+2)
		}
	}
}

// 表格位于不能跨页的上下文（这里是模板页）时必须明确报错，
// 而不是把行画到页底之外让人以为表丢了。
func TestExpandTableRejectsOverflowInNestedContext(t *testing.T) {
	doc := splitTestDoc(Page{})
	doc.Templates = []TemplatePage{{
		ID: 1, Name: "tpl", ZOrder: spec.ZOrderBackground,
		Items: []Item{splitTestTable(120, 5)},
	}}
	doc.Pages[0].Templates = []TemplateRef{{ID: 1, ZOrder: spec.ZOrderBackground}}
	if _, err := Marshal(doc); err == nil {
		t.Fatal("模板页内放不下的表格应当报错")
	} else if !strings.Contains(err.Error(), "不能跨页") {
		t.Errorf("错误信息未说明该上下文不能跨页: %v", err)
	}
}
