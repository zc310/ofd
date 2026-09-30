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
	indexed := searchPage{
		runs:   make([][]rune, len(runs)),
		byRune: make(map[rune][]int),
	}
	for runIndex, run := range runs {
		indexed.runs[runIndex] = []rune(strings.ToLower(run.Text))
		seen := make(map[rune]struct{})
		for _, value := range indexed.runs[runIndex] {
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			indexed.byRune[value] = append(indexed.byRune[value], runIndex)
		}
	}
	r.search.Add(index, indexed)
	return indexed
}
