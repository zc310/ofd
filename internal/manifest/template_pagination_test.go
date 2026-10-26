package manifest

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"go.yaml.in/yaml/v3"
)

func detailTestManifest() Manifest {
	text := func(path string, x, y float64) Item {
		return Item{Type: "text", X: x, Y: y, Width: 80, Height: 8, Size: 3.5, Bind: &Binding{Path: path}}
	}
	sum := text("amount", 100, 105)
	sum.Bind.Aggregate = "record_sum"
	variant := func(label string, height float64, total bool) *PageVariant {
		v := &PageVariant{DetailArea: &Box{X: 10, Y: 40, Width: 100, Height: height}, Items: []Item{{Type: "text", X: 10, Y: 25, Width: 100, Height: 8, Value: label}}}
		if total {
			v.Items = append(v.Items, sum)
		}
		return v
	}
	subtotal := text("amount", 100, 90)
	subtotal.Bind.Aggregate = "page_sum"
	padding := 1.0
	return Manifest{Version: 1, Document: Document{ID: "detail-template"}, Pages: []Page{{
		Repeat: "records",
		Paginate: &DetailPagination{Path: "lines", Area: Box{X: 10, Y: 40, Width: 100, Height: 32}, Size: 3.5, RowHeight: 8, HeaderHeight: 8, Padding: &padding, Columns: []DetailColumn{
			{Title: "#", Width: 10, Bind: Binding{Path: "@number"}},
			{Title: "Item", Width: 60, Bind: Binding{Path: "name"}},
			{Title: "Amount", Width: 30, Bind: Binding{Path: "amount", Align: "right"}},
		}},
		Variants: &PageVariants{First: variant("FIRST", 32, false), Continuation: variant("CONTINUATION", 24, false), Last: variant("LAST", 24, true), Single: variant("SINGLE", 32, true)},
		Items:    []Item{text("buyer", 10, 10), text("@detail_page", 10, 140), text("@detail_pages", 30, 140), subtotal},
	}}}
}

func detailTextAt(page creator.Page, x, y float64) string {
	var result string
	var visit func([]creator.Item)
	visit = func(items []creator.Item) {
		for _, item := range items {
			switch value := item.(type) {
			case creator.Text:
				if value.X == x && value.Y == y {
					result += value.Value
					for _, code := range value.TextCodes {
						result += code.Value
					}
				}
			case creator.PageBlock:
				visit(value.Items)
			}
		}
	}
	visit(page.Items)
	for _, layer := range page.Layers {
		visit(layer.Items)
	}
	return result
}

func TestTemplateDetailPaginationSelectsLayoutsAndKeepsRecordTotalsSeparate(t *testing.T) {
	m := detailTestManifest()
	// 续页使用独立图层，保护公共 items 与附加 layers 组合后的页面内容。
	continuation := m.Pages[0].Variants.Continuation
	continuation.Layers = []Layer{{Type: "Body", Items: continuation.Items}}
	continuation.Items = nil
	data := `{"records":[{"buyer":"Alpha","lines":[{"name":"A1","amount":"0.10"},{"name":"A2","amount":"0.20"}]},{"buyer":"Beta","lines":[{"name":"B1","amount":"0.10"},{"name":"B2","amount":"0.20"},{"name":"B3","amount":"0.30"},{"name":"B4","amount":"0.40"},{"name":"B5","amount":"0.50"},{"name":"B6","amount":"0.60"},{"name":"B7","amount":"0.70"}]}]}`
	for _, format := range []string{"yaml", "json"} {
		t.Run(format, func(t *testing.T) {
			encoded, err := json.Marshal(m)
			if format == "yaml" {
				encoded, err = yaml.Marshal(m)
			}
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := Parse(encoded, format)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := loaded.RenderTemplate(strings.NewReader(data), "", "", TemplateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(doc.Pages) != 4 {
				t.Fatalf("预期 4 页，得到 %d", len(doc.Pages))
			}
			for p, label := range []string{"SINGLE", "FIRST", "CONTINUATION", "LAST"} {
				if got := detailTextAt(doc.Pages[p], 10, 25); got != label {
					t.Fatalf("第 %d 页版式 %q != %q", p, got, label)
				}
				buyer, number, pages := "Beta", fmt.Sprint(p), "3"
				if p == 0 {
					buyer, number, pages = "Alpha", "1", "1"
				}
				if detailTextAt(doc.Pages[p], 10, 10) != buyer || detailTextAt(doc.Pages[p], 10, 140) != number || detailTextAt(doc.Pages[p], 30, 140) != pages {
					t.Fatalf("第 %d 页单据或分页上下文错误", p)
				}
			}
			for p, subtotal := range []string{"0.30", "0.60", "0.90", "1.30"} {
				if got := detailTextAt(doc.Pages[p], 100, 90); got != subtotal {
					t.Fatalf("第 %d 页小计 %q != %q", p, got, subtotal)
				}
			}
			for p, total := range []string{"0.30", "", "", "2.80"} {
				if got := detailTextAt(doc.Pages[p], 100, 105); got != total {
					t.Fatalf("第 %d 页合计 %q != %q", p, got, total)
				}
			}
			// 行序号跨单据页连续，而不同单据重新从 1 开始。
			if detailTextAt(doc.Pages[2], 11, 49) != "4" || detailTextAt(doc.Pages[3], 11, 49) != "6" {
				t.Fatal("明细行序号未跨页连续")
			}
			if _, err := creator.Marshal(doc); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTemplateDetailRowWrapKeepsWholeRowInsideRegion(t *testing.T) {
	m := detailTestManifest()
	m.Pages[0].Variants = nil
	data := `{"records":[{"buyer":"Alpha","lines":[{"name":"Line one\nLine two\nLine three\nLine four","amount":"1.00"},{"name":"Short","amount":"2.00"}]}]}`
	doc, err := m.RenderTemplate(strings.NewReader(data), "", "", TemplateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages) != 2 {
		t.Fatalf("长行应整体转页，得到 %d 页", len(doc.Pages))
	}
	if got := detailTextAt(doc.Pages[0], 100, 90); got != "1.00" {
		t.Fatalf("长行小计错误: %s", got)
	}
	if got := detailTextAt(doc.Pages[1], 100, 90); got != "2.00" {
		t.Fatalf("后续行小计错误: %s", got)
	}
	for _, page := range doc.Pages {
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok && text.Y >= 40 && text.Y < 72 && text.Y+text.Height > 72+1e-7 {
				t.Fatalf("明细文字超出区域: %+v", text)
			}
		}
	}
}

func TestTemplateDetailsEmptyRowsAndFooterOnlyLastPage(t *testing.T) {
	m := detailTestManifest()
	doc, err := m.RenderTemplate(strings.NewReader(`{"records":[{"buyer":"Empty","lines":[]}]}`), "", "", TemplateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages) != 1 || detailTextAt(doc.Pages[0], 100, 105) != "0.00" {
		t.Fatal("空明细应保留一页并显示零合计")
	}
	m.Pages[0].Variants.Single.DetailArea.Height = 8
	m.Pages[0].Variants.Last.DetailArea.Height = 8
	doc, err = m.RenderTemplate(strings.NewReader(`{"records":[{"buyer":"Footer","lines":[{"name":"One","amount":"0.10"}]}]}`), "", "", TemplateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages) != 2 || detailTextAt(doc.Pages[1], 10, 25) != "LAST" || detailTextAt(doc.Pages[1], 100, 90) != "0.00" || detailTextAt(doc.Pages[1], 100, 105) != "0.10" {
		t.Fatal("末页仅容纳表头及合计时，未正确生成独立合计页")
	}
}

func TestTemplateDetailPaginationRejectsInvalidLayoutsAndRows(t *testing.T) {
	tests := []struct {
		name, match string
		edit        func(*Manifest)
	}{
		{"missing-path", "需要明细数组", func(m *Manifest) { m.Pages[0].Paginate.Path = "missing" }},
		{"width", "列宽之和", func(m *Manifest) { m.Pages[0].Paginate.Columns[0].Width++ }},
		{"padding", "两倍 padding", func(m *Manifest) { p := 10.0; m.Pages[0].Paginate.Padding = &p }},
		{"variant-width", "detail_area.width", func(m *Manifest) { m.Pages[0].Variants.Last.DetailArea.Width = 90 }},
		{"header", "表头", func(m *Manifest) { m.Pages[0].Paginate.HeaderHeight = 1 }},
		{"oversize-row", "明细行 0", func(m *Manifest) { m.Pages[0].Paginate.RowHeight = 100 }},
		{"column-meta", "列内仅支持", func(m *Manifest) { m.Pages[0].Paginate.Columns[0].Bind.Path = "@pages" }},
		{"column-aggregate", "列内不能", func(m *Manifest) { m.Pages[0].Paginate.Columns[2].Bind.Aggregate = "record_sum" }},
		{"unpaired-variants", "variants 必须", func(m *Manifest) { m.Pages[0].Paginate = nil }},
		{"decimal-places", "decimal_places", func(m *Manifest) { p := 19; m.Pages[0].Items[3].Bind.DecimalPlaces = &p }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := detailTestManifest()
			tt.edit(&m)
			_, err := m.RenderTemplate(strings.NewReader(`{"records":[{"buyer":"Test","lines":[{"name":"One","amount":"0.10"}]}]}`), "", "", TemplateOptions{})
			if err == nil || !strings.Contains(err.Error(), tt.match) {
				t.Fatalf("预期 %q，得到 %v", tt.match, err)
			}
		})
	}
	m := detailTestManifest()
	_, err := m.RenderTemplate(strings.NewReader(`{"records":[{"buyer":"Test","lines":[{"name":"One","amount":"0.10"},{"name":"Two","amount":"0.20"},{"name":"Three","amount":"0.30"},{"name":"Four","amount":"0.40"}]}]}`), "", "", TemplateOptions{MaxPages: 1})
	if err == nil || !strings.Contains(err.Error(), "max_pages") {
		t.Fatalf("分页未计入 max_pages: %v", err)
	}
}

func TestTemplateDecimalAggregationIsExactAndRoundsAfterSummation(t *testing.T) {
	for _, tt := range []struct {
		values, expected string
		places           int
	}{
		{`"0.1",0.2`, "0.30", 2},
		{`"9007199254740993.10","0.20"`, "9007199254740993.30", 2},
		{`"0.004","0.004"`, "0.01", 2},
		{`"-0.005"`, "-0.01", 2},
		{`"-0.004"`, "0.00", 2},
		{`1e-1,2e-1`, "0.300", 3},
		{`"1.5"`, "2", 0},
		{`"010","008"`, "18.00", 2},
	} {
		t.Run(tt.values, func(t *testing.T) {
			var values []any
			decoder := json.NewDecoder(strings.NewReader("[" + tt.values + "]"))
			decoder.UseNumber()
			if err := decoder.Decode(&values); err != nil {
				t.Fatal(err)
			}
			var rows []any
			for _, value := range values {
				rows = append(rows, map[string]any{"amount": value})
			}
			ctx := templateContext{detail: &templateDetailPage{group: &templateDetailGroup{data: rows, sums: map[string]string{}}, end: len(rows)}}
			got, err := aggregateTemplateRows(Binding{Path: "amount", Aggregate: "record_sum", DecimalPlaces: &tt.places}, ctx)
			if err != nil || got != tt.expected {
				t.Fatalf("求和 %q != %q: %v", got, tt.expected, err)
			}
		})
	}
	for _, value := range []any{"NaN", "1/3", "0x10", "1,234.50", "1e1001", true, ""} {
		if _, err := parseTemplateDecimal(value); err == nil {
			t.Fatalf("未拒绝非十进制数据 %v", value)
		}
	}
}

func TestTemplatePaginatedReferencesAnnotationsAndOriginalRemainCorrect(t *testing.T) {
	m := detailTestManifest()
	m.Pages[0].Items[0].Actions = []Action{{Goto: &GotoAction{Page: 0}}}
	m.Pages = append(m.Pages, Page{})
	m.Document.Bookmarks = []Bookmark{{Name: "tail", Goto: GotoAction{Page: 1}}}
	m.Document.Annotations = []AnnotationPage{{Page: 0, Items: []Annotation{{ID: 800, Type: "Stamp", Creator: "test", LastModDate: "2026-10-26", Items: []Item{{Type: "text", Width: 30, Height: 8, Bind: &Binding{Path: "@detail_page"}}}}}}}
	before, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]string
	for i := 0; i < 7; i++ {
		rows = append(rows, map[string]string{"name": fmt.Sprint(i), "amount": "0.10"})
	}
	data, err := json.Marshal(map[string]any{"records": []any{map[string]any{"buyer": "Alpha", "lines": rows}}})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := m.RenderTemplate(strings.NewReader(string(data)), "", "", TemplateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages) != 4 || doc.Bookmarks[0].Goto.Page != 3 || len(doc.Annotations) != 3 {
		t.Fatalf("分页后的目标或批注错误: %+v", doc)
	}
	seen := map[uint64]bool{}
	for i, annotation := range doc.Annotations {
		if annotation.Page != i || seen[annotation.Items[0].ID] {
			t.Fatalf("批注位置或 ID 重复: %+v", annotation)
		}
		seen[annotation.Items[0].ID] = true
		if got := annotation.Items[0].Items[0].(creator.Text).TextCodes[0].Value; got != fmt.Sprint(i+1) {
			t.Fatalf("批注单据页码错误: %s", got)
		}
		if got := doc.Pages[i].Items[0].(creator.Text).Actions[0].Goto.Page; got != i {
			t.Fatalf("第 %d 页自链接跳到 %d", i, got)
		}
	}
	after, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("明细分页修改了原模板")
	}
	if _, err := creator.Marshal(doc); err != nil {
		t.Fatal(err)
	}
}

func TestTemplateAggregateMissingValuesAndOptionalDefaults(t *testing.T) {
	rows := []any{map[string]any{"amount": "0.10"}, map[string]any{}}
	ctx := templateContext{detail: &templateDetailPage{group: &templateDetailGroup{data: rows, sums: map[string]string{}}, end: len(rows)}}
	b := Binding{Path: "amount", Aggregate: "record_sum"}
	if _, err := aggregateTemplateRows(b, ctx); err == nil {
		t.Fatal("缺失金额应定位报错")
	}
	optional := false
	b.Required = &optional
	if got, err := aggregateTemplateRows(b, ctx); err != nil || got != "0.10" {
		t.Fatalf("可选金额 %s: %v", got, err)
	}
	value := "0.20"
	b.Default = &value
	if got, err := aggregateTemplateRows(b, ctx); err != nil || got != "0.30" {
		t.Fatalf("默认金额 %s: %v", got, err)
	}
	if _, err := aggregateTemplateRows(b, templateContext{}); err == nil {
		t.Fatal("普通页面不能使用明细汇总")
	}
}
