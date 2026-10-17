package webreader

import (
	"errors"
	"strings"

	"github.com/zc310/ofd/internal/search"
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
	searchTable := search.RuneSearchTable(needleRunes)
	results := make([]SearchResult, 0)
	type match struct {
		layoutIndex int
		start       int
		end         int
	}
	for pageIndex := range r.pages {
		indexed := r.searchAt(pageIndex)
		var matches []match
		var layoutRunIndices []int
		for _, runIndex := range indexed.Candidates(needleRunes[0]) {
			if runIndex < 0 || runIndex >= indexed.Len() {
				continue
			}
			text := indexed.Run(runIndex)
			start := 0
			layoutIndex := -1
			for {
				found := search.IndexOfRunes(text[start:], needleRunes, searchTable)
				if found < 0 {
					break
				}
				found += start
				end := found + len(needleRunes)
				if layoutIndex < 0 {
					layoutIndex = len(layoutRunIndices)
					layoutRunIndices = append(layoutRunIndices, runIndex)
				}
				matches = append(matches, match{layoutIndex: layoutIndex, start: found, end: end})
				start = end
				if start >= len(text) {
					break
				}
			}
		}
		if len(matches) == 0 {
			continue
		}
		layouts := r.pages[pageIndex].document.TextLayoutsForRunsWithVisibility(r.pages[pageIndex].page, layoutRunIndices, r.hiddenLayers[pageIndex])
		for _, found := range matches {
			if found.layoutIndex < 0 || found.layoutIndex >= len(layouts) {
				continue
			}
			layout := layouts[found.layoutIndex]
			result := SearchResult{
				Page: pageIndex, Run: layoutRunIndices[found.layoutIndex], Text: layout.Text,
				Start: found.start, End: found.end,
			}
			glyphStart := minInt(found.start, len(layout.Glyphs))
			glyphEnd := minInt(found.end, len(layout.Glyphs))
			for _, glyph := range layout.Glyphs[glyphStart:glyphEnd] {
				result.Rects = append(result.Rects, Rect{X: glyph.X, Y: glyph.Y, Width: glyph.Width, Height: glyph.Height, Angle: glyph.Angle})
			}
			results = append(results, result)
		}
	}
	return results, nil
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func (r *Reader) searchAt(index int) *search.Index {
	if indexed, ok := r.search.Get(index); ok {
		return indexed
	}
	var builder *search.Builder
	if runs, ok := r.text.Get(index); ok {
		builder = search.NewBuilder(len(runs))
		for _, run := range runs {
			builder.Add(run.Text)
		}
	} else {
		ref := r.pages[index]
		texts := ref.document.TextValuesWithVisibility(ref.page, r.hiddenLayers[index])
		builder = search.NewBuilder(len(texts))
		for _, text := range texts {
			builder.Add(text)
		}
	}
	indexed := builder.Build()
	r.search.AddWeighted(index, indexed, indexed.Weight())
	return indexed
}
