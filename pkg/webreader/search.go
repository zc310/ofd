package webreader

import (
	"errors"
	"strings"
)

// SearchResult 描述一个页面文字命中。
type SearchResult struct {
	Page  int
	Run   int
	Text  string
	Start int
	End   int
	Rects []Rect
}

// Rect 描述一个搜索命中的字符区域，坐标单位为毫米。
type Rect struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
	Angle  float64
}

type searchPage struct {
	runs   [][]rune
	byRune map[rune][]int
}

// Search 在所有页面的文字对象中查找 query，匹配不区分大小写。
func (r *Reader) Search(query string) ([]SearchResult, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	query = strings.TrimSpace(query)
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.cacheMu.RLock()
	defer r.cacheMu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	if query == "" {
		return []SearchResult{}, nil
	}
	needle := strings.ToLower(query)
	needleRunes := []rune(needle)
	results := make([]SearchResult, 0)
	for pageIndex := range r.pages {
		runs := r.textAt(pageIndex)
		indexed := r.searchAt(pageIndex, runs)
		for _, runIndex := range indexed.byRune[needleRunes[0]] {
			text := indexed.runs[runIndex]
			run := runs[runIndex]
			start := 0
			for {
				found := indexRunes(text[start:], needleRunes)
				if found < 0 {
					break
				}
				found += start
				end := found + len(needleRunes)
				result := SearchResult{Page: pageIndex, Run: runIndex, Text: run.Text, Start: found, End: end}
				glyphStart := minInt(found, len(run.Glyphs))
				glyphEnd := minInt(end, len(run.Glyphs))
				for _, glyph := range run.Glyphs[glyphStart:glyphEnd] {
					result.Rects = append(result.Rects, Rect{X: glyph.X, Y: glyph.Y, Width: glyph.Width, Height: glyph.Height, Angle: glyph.Angle})
				}
				results = append(results, result)
				start = end
				if start >= len(text) {
					break
				}
			}
		}
	}
	return results, nil
}

func indexRunes(text, needle []rune) int {
	if len(needle) == 0 {
		return 0
	}
	if len(needle) > len(text) {
		return -1
	}
	lps := make([]int, len(needle))
	length := 0
	for i := 1; i < len(needle); {
		if needle[i] == needle[length] {
			length++
			lps[i] = length
			i++
		} else if length > 0 {
			length = lps[length-1]
		} else {
			lps[i] = 0
			i++
		}
	}
	for i, j := 0, 0; i < len(text); {
		if text[i] == needle[j] {
			i++
			j++
			if j == len(needle) {
				return i - j
			}
		} else if j > 0 {
			j = lps[j-1]
		} else {
			i++
		}
	}
	return -1
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func (r *Reader) searchAt(index int, runs []TextRun) searchPage {
	if indexed, ok := r.search.Get(index); ok {
		return indexed
	}
	indexed := buildSearchIndex(runs)
	r.search.AddWeighted(index, indexed, searchPageWeight(indexed))
	return indexed
}

// searchPageWeight 估算一页搜索索引的内存占用。
//
// 两个分量都要算：runs 是每字符一个 rune（4 字节）外加每个 run 一个切片头，
// byRune 是 map 的桶与每个字符的候选 run 列表。实测密集页约 9.8 KB、稀疏页
// 约 0.1 KB。
const (
	// searchPageRunOverhead 是每个 run 在索引里占的固定字节（切片头等）。
	searchPageRunOverhead = 24
	// searchPageEntryOverhead 是 byRune 里每个 key 的估算开销（桶与键本身）。
	searchPageEntryOverhead = 8
)

func searchPageWeight(page searchPage) int64 {
	var total int64
	for _, run := range page.runs {
		total += searchPageRunOverhead + int64(len(run))*4
	}
	for _, candidates := range page.byRune {
		total += searchPageEntryOverhead + int64(len(candidates))*4
	}
	return total
}

// buildSearchIndex 建立一页的搜索索引。
//
// byRune 把"首字 -> 可能命中的 run 序号"记下来，查询时先按首字筛候选，再逐个
// 精确匹配，避免对整页做逐位比较。索引按小写建，与 Search 里对 needle 的
// 处理一致——否则 "Hello" 与 "hello" 会各占一个 key，而查询只走一边。
func buildSearchIndex(runs []TextRun) searchPage {
	indexed := searchPage{
		runs:   make([][]rune, len(runs)),
		byRune: make(map[rune][]int),
	}
	// seen 记的是"这个字符最后出现在哪个 run"，整页只建一次。
	//
	// 此前每个 run 新建一个 map[rune]struct{}，一个 500 行的页面就是 500 次
	// map 分配，而这页各 run 的用字高度重叠——同一页汉字的用字表基本一致，
	// 分配出来的东西大半是重复的。
	//
	// 用 runIndex+1 当代次标记，省掉逐次 clear：值相同才说明本 run 已登记过。
	// 不用 0 是因为首个 run 的代次就是 1，0 恰好适合表示"没登记过"。
	seen := make(map[rune]int, 128)
	for runIndex, run := range runs {
		indexed.runs[runIndex] = []rune(strings.ToLower(run.Text))
		generation := runIndex + 1
		for _, value := range indexed.runs[runIndex] {
			if seen[value] == generation {
				continue
			}
			seen[value] = generation
			indexed.byRune[value] = append(indexed.byRune[value], runIndex)
		}
	}
	return indexed
}
