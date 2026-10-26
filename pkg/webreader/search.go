package webreader

import (
	"context"
	"errors"
	"fmt"
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

// SearchOptions 控制单次搜索的工作量。MaxPages、MaxResults 为 0 时不限制。
type SearchOptions struct {
	// MaxPages 是最多扫描的页面数。
	MaxPages int
	// MaxResults 是最多返回的命中数。
	MaxResults int
}

// SearchReport 描述一次有界搜索的结果。
type SearchReport struct {
	// Results 是按页面和文字绘制顺序排列的命中。
	Results []SearchResult
	// Scanned 是实际扫描的页面数。
	Scanned int
	// Truncated 表示仍有页面或命中未返回。
	Truncated bool
}

// Search 在所有页面的文字对象中查找 query，匹配不区分大小写，并支持取消与工作量上限。
// Truncated 表示仍有页面未扫描，或达到结果上限后提前停止。
func (r *Reader) Search(ctx context.Context, query string, options SearchOptions) (SearchReport, error) {
	if r == nil {
		return SearchReport{}, errors.New("文档引擎为空")
	}
	if ctx == nil {
		return SearchReport{}, errors.New("搜索上下文为空")
	}
	if options.MaxPages < 0 || options.MaxResults < 0 {
		return SearchReport{}, errors.New("搜索页数和结果上限不能为负数")
	}
	query = strings.TrimSpace(query)
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return SearchReport{}, errors.New("文档引擎已经关闭")
	}
	if err := ctx.Err(); err != nil {
		return SearchReport{}, err
	}
	if query == "" {
		return SearchReport{Results: []SearchResult{}}, nil
	}
	needle := strings.ToLower(query)
	needleRunes := []rune(needle)
	searchTable := search.RuneSearchTable(needleRunes)
	report := SearchReport{Results: make([]SearchResult, 0)}
	type match struct {
		layoutIndex int
		start       int
		end         int
	}
	pageLimit := len(r.pages)
	if options.MaxPages > 0 && pageLimit > options.MaxPages {
		pageLimit = options.MaxPages
		report.Truncated = true
	}
	for pageIndex := 0; pageIndex < pageLimit; pageIndex++ {
		if err := ctx.Err(); err != nil {
			return SearchReport{}, err
		}
		indexed, err := r.searchAt(pageIndex)
		if err != nil {
			return SearchReport{}, fmt.Errorf("读取第 %d 页搜索索引失败: %w", pageIndex, err)
		}
		report.Scanned = pageIndex + 1
		var matches []match
		var layoutRunIndices []int
		resultLimitReached := false
	searchPage:
		for candidateIndex, runIndex := range indexed.Candidates(needleRunes[0]) {
			if candidateIndex%256 == 0 {
				if err := ctx.Err(); err != nil {
					return SearchReport{}, err
				}
			}
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
				if options.MaxResults > 0 && len(report.Results)+len(matches) >= options.MaxResults {
					report.Truncated = true
					resultLimitReached = true
					break searchPage
				}
				start = end
				if start >= len(text) {
					break
				}
			}
		}
		if len(matches) == 0 {
			continue
		}
		layouts, err := r.pages[pageIndex].document.TextLayoutsForRunsWithVisibilityE(r.pages[pageIndex].page, layoutRunIndices, r.hiddenLayers[pageIndex])
		if err != nil {
			return SearchReport{}, fmt.Errorf("读取第 %d 页搜索布局失败: %w", pageIndex, err)
		}
		for _, found := range matches {
			if found.layoutIndex < 0 || found.layoutIndex >= len(layouts) {
				continue
			}
			layout := layouts[found.layoutIndex]
			result := SearchResult{
				Page: pageIndex, Run: layoutRunIndices[found.layoutIndex], Text: layout.Text,
				Start: found.start, End: found.end,
			}
			// 禁止导出的页面仍可定位命中，但不借搜索结果返回整个文字对象。
			if !documentPermissions(r.pages[pageIndex].fontScope, r.pages[pageIndex].document.Permissions).Export {
				result.Text = query
				result.Start, result.End = 0, len([]rune(query))
			}
			glyphStart := minInt(found.start, len(layout.Glyphs))
			glyphEnd := minInt(found.end, len(layout.Glyphs))
			for _, glyph := range layout.Glyphs[glyphStart:glyphEnd] {
				result.Rects = append(result.Rects, Rect{X: glyph.X, Y: glyph.Y, Width: glyph.Width, Height: glyph.Height, Angle: glyph.Angle})
			}
			report.Results = append(report.Results, result)
		}
		if resultLimitReached {
			return report, nil
		}
	}
	return report, nil
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func (r *Reader) searchAt(index int) (*search.Index, error) {
	if indexed, ok := r.search.Get(index); ok {
		return indexed, nil
	}
	var builder *search.Builder
	if runs, ok := r.text.Get(index); ok {
		builder = search.NewBuilder(len(runs))
		for _, run := range runs {
			builder.Add(run.Text)
		}
	} else {
		ref := r.pages[index]
		texts, err := ref.document.TextValuesWithVisibilityE(ref.page, r.hiddenLayers[index])
		if err != nil {
			return nil, err
		}
		builder = search.NewBuilder(len(texts))
		for _, text := range texts {
			builder.Add(text)
		}
	}
	indexed := builder.Build()
	r.search.AddWeighted(index, indexed, indexed.Weight())
	return indexed, nil
}
