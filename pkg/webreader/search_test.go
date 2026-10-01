package webreader

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSearchAvoidsFullTextCache(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	ref := reader.pages[0]
	values := ref.document.TextValues(ref.page)
	if len(values) == 0 || values[0] == "" {
		t.Fatal("页面没有可搜索文字")
	}

	results, err := reader.Search("不存在的查询内容")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("无匹配查询返回了 %d 个结果", len(results))
	}
	if got := reader.text.Len(); got != 0 {
		t.Fatalf("无命中搜索仍缓存了 %d 页完整字形布局", got)
	}
	query := string([]rune(values[0])[:1])
	results, err = reader.Search(query)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatalf("搜索页面文字首字符 %q 未命中", query)
	}
	if len(results[0].Rects) == 0 {
		t.Fatalf("命中结果没有高亮矩形: %+v", results[0])
	}
	if got := reader.text.Len(); got != 0 {
		t.Fatalf("搜索命中仍缓存了 %d 页完整字形布局", got)
	}
}

func TestTextValuesMatchTextLayoutOrder(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "ano.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	ref := reader.pages[0]
	values := ref.document.TextValues(ref.page)
	runs, err := reader.Text(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != len(runs) {
		t.Fatalf("TextValues 返回 %d 项，Text 返回 %d 项", len(values), len(runs))
	}
	for index := range values {
		if values[index] != runs[index].Text {
			t.Fatalf("第 %d 项文字不一致: TextValues=%q, Text=%q", index, values[index], runs[index].Text)
		}
	}
}

func TestTextLayoutsForRunsPreservesPageOrder(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "ano.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	ref := reader.pages[0]
	all := ref.document.TextLayouts(ref.page)
	selected := ref.document.TextLayoutsForRuns(ref.page, []int{2, 0, 2, -1, len(all) + 1})
	if len(all) < 3 {
		t.Fatalf("测试文档只有 %d 个文字 run", len(all))
	}
	if len(selected) != 2 {
		t.Fatalf("选中布局数 = %d，期望 2", len(selected))
	}
	if selected[0].Text != all[0].Text || selected[1].Text != all[2].Text {
		t.Fatalf("筛选布局顺序或内容不匹配: got [%q %q], want [%q %q]", selected[0].Text, selected[1].Text, all[0].Text, all[2].Text)
	}
}

func TestSearchResultsMatchTextSnapshots(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "ano.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	firstPage := reader.pages[0]
	values := firstPage.document.TextValues(firstPage.page)
	if len(values) == 0 {
		t.Fatal("测试文档首页没有文字")
	}
	var trimmed string
	for _, value := range values {
		trimmed = strings.TrimSpace(value)
		if trimmed != "" {
			break
		}
	}
	if trimmed == "" {
		t.Fatal("首页首个文字 run 只有空白")
	}
	query := string([]rune(trimmed)[:1])
	got, err := reader.Search(query)
	if err != nil {
		t.Fatal(err)
	}
	if reader.text.Len() != 0 {
		t.Fatalf("未预热搜索在建立索引时缓存了 %d 页完整文字布局", reader.text.Len())
	}

	needle := []rune(strings.ToLower(query))
	table := buildRuneSearchTable(needle)
	want := make([]SearchResult, 0)
	for pageIndex := range reader.pages {
		runs, textErr := reader.Text(pageIndex)
		if textErr != nil {
			t.Fatalf("读取第 %d 页文字失败: %v", pageIndex, textErr)
		}
		for runIndex, run := range runs {
			text := []rune(strings.ToLower(run.Text))
			start := 0
			for {
				found := indexRunesWithTable(text[start:], needle, table)
				if found < 0 {
					break
				}
				found += start
				end := found + len(needle)
				result := SearchResult{Page: pageIndex, Run: runIndex, Text: run.Text, Start: found, End: end}
				for _, glyph := range run.Glyphs[minInt(found, len(run.Glyphs)):minInt(end, len(run.Glyphs))] {
					result.Rects = append(result.Rects, Rect{X: glyph.X, Y: glyph.Y, Width: glyph.Width, Height: glyph.Height, Angle: glyph.Angle})
				}
				want = append(want, result)
				start = end
				if start >= len(text) {
					break
				}
			}
		}
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("搜索结果与文字快照不一致：got %d hits, want %d hits", len(got), len(want))
	}
}

func TestSearchCacheCapacityTracksPageCount(t *testing.T) {
	cache := newSearchCache(3)
	for index := 0; index < 3; index++ {
		cache.Add(index, searchPage{})
	}
	if got := cache.Len(); got != 3 {
		t.Fatalf("三页文档的搜索缓存保留 %d 项，期望 3", got)
	}

	cache = newSearchCache(searchCacheCapacity + 1)
	for index := 0; index <= searchCacheCapacity; index++ {
		cache.Add(index, searchPage{})
	}
	if got := cache.Len(); got != searchCacheCapacity {
		t.Fatalf("超大文档的搜索缓存保留 %d 项，期望上限 %d", got, searchCacheCapacity)
	}
}
