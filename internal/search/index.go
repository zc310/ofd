// Package search 提供基于 Unicode 字符的文本索引与匹配能力。
package search

import "strings"

// Index 保存规范化后的文本段，并记录每个字符出现在哪些文本段中。
type Index struct {
	runs   [][]rune
	byRune map[rune][]int
}

// Builder 构建 Index，并避免为每个文本段单独分配临时 map。
type Builder struct {
	runs   [][]rune
	byRune map[rune][]int
	seen   map[rune]int
}

// NewBuilder 创建一个可容纳 runCapacity 个文本段的构建器。
func NewBuilder(runCapacity int) *Builder {
	if runCapacity < 0 {
		runCapacity = 0
	}
	return &Builder{
		runs:   make([][]rune, 0, runCapacity),
		byRune: make(map[rune][]int),
		seen:   make(map[rune]int, 128),
	}
}

// Add 添加一个文本段，并将其转为小写后纳入索引。
func (builder *Builder) Add(text string) {
	runIndex := len(builder.runs)
	runes := []rune(strings.ToLower(text))
	builder.runs = append(builder.runs, runes)
	generation := runIndex + 1
	for _, value := range runes {
		if builder.seen[value] == generation {
			continue
		}
		builder.seen[value] = generation
		builder.byRune[value] = append(builder.byRune[value], runIndex)
	}
}

// Build 返回完成后的索引；去重表仅保留在构建器中，不进入索引缓存。
func (builder *Builder) Build() *Index {
	return &Index{runs: builder.runs, byRune: builder.byRune}
}

// Len 返回索引中的文本段数量。
func (index *Index) Len() int { return len(index.runs) }

// Run 返回指定文本段的小写字符序列。
func (index *Index) Run(run int) []rune { return index.runs[run] }

// Candidates 返回包含指定字符的文本段索引。
func (index *Index) Candidates(value rune) []int { return index.byRune[value] }

// Weight 估算索引占用的内存，用于带权重的缓存。
func (index *Index) Weight() int64 {
	var total int64
	for _, run := range index.runs {
		total += 24 + int64(len(run))*4
	}
	for _, candidates := range index.byRune {
		total += 8 + int64(len(candidates))*4
	}
	return total
}

// RuneSearchTable 为 needle 构造 KMP 最长前缀表。
func RuneSearchTable(needle []rune) []int {
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
	return lps
}

// IndexOfRunes 返回 needle 在 text 中首次出现的位置；未找到时返回 -1。
func IndexOfRunes(text, needle []rune, lps []int) int {
	if len(needle) == 0 {
		return 0
	}
	if len(needle) > len(text) {
		return -1
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
