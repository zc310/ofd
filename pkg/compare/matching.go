package compare

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// PagePair 指定基准和目标中从 1 开始的原始全局页码。
type PagePair struct {
	Before int `json:"before"`
	After  int `json:"after"`
}

// ParsePageMap 解析 "2:3,4:5" 格式的手工页映射，拒绝重复或无效页码。
func ParsePageMap(spec string) ([]PagePair, error) {
	var pairs []PagePair
	left, right := map[int]bool{}, map[int]bool{}
	if strings.TrimSpace(spec) == "" {
		return pairs, nil
	}
	for _, item := range strings.Split(spec, ",") {
		a, b, ok := strings.Cut(item, ":")
		x, xe := strconv.Atoi(strings.TrimSpace(a))
		y, ye := strconv.Atoi(strings.TrimSpace(b))
		if !ok || xe != nil || ye != nil || x < 1 || y < 1 || left[x] || right[y] {
			return nil, fmt.Errorf("无效或重复的手工页映射 %q", item)
		}
		left[x], right[y] = true, true
		pairs = append(pairs, PagePair{x, y})
	}
	return pairs, nil
}

func pageNumber(p Page, index int) int {
	if p.Number > 0 {
		return p.Number
	}
	return index + 1
}

type pagePairMatch struct {
	Before, After int
	Info          PageMatchInfo
}

func matchPages(ctx context.Context, a, b []Page, options Options) ([]pagePairMatch, error) {
	matches, used := map[int]int{}, map[int]bool{}
	info := map[int]PageMatchInfo{}
	searchLimited := false
	manual, err := ParsePageMap(options.PageMap)
	if err != nil {
		return nil, err
	}
	leftNumbers, rightNumbers := map[int]int{}, map[int]int{}
	for i, p := range a {
		leftNumbers[pageNumber(p, i)] = i
	}
	for i, p := range b {
		rightNumbers[pageNumber(p, i)] = i
	}
	for _, pair := range manual {
		i, iok := leftNumbers[pair.Before]
		j, jok := rightNumbers[pair.After]
		if !iok || !jok {
			return nil, fmt.Errorf("手工页映射 %d:%d 不属于两侧所选页面", pair.Before, pair.After)
		}
		matches[i], used[j] = j, true
		info[i] = PageMatchInfo{Method: "manual"}
	}
	if options.PageMatch == "auto" {
		for _, textOnly := range []bool{false, true} {
			left, right := map[string][]int{}, map[string][]int{}
			key := func(p Page) string {
				value := p.ContentSHA256
				if textOnly {
					value = p.Text
				}
				if value == "" {
					return ""
				}
				return strconv.Itoa(p.Document) + ":" + value
			}
			for i, p := range a {
				if _, ok := matches[i]; !ok {
					if k := key(p); k != "" {
						left[k] = append(left[k], i)
					}
				}
			}
			for i, p := range b {
				if !used[i] {
					if k := key(p); k != "" {
						right[k] = append(right[k], i)
					}
				}
			}
			for k, li := range left {
				ri := right[k]
				if textOnly && (len(li) != 1 || len(ri) != 1) {
					continue
				}
				for n := 0; n < len(li) && n < len(ri); n++ {
					matches[li[n]], used[ri[n]] = ri[n], true
					method := "content"
					if textOnly {
						method = "text"
					}
					info[li[n]] = PageMatchInfo{Method: method}
				}
			}
		}
		var err error
		if searchLimited, err = matchSimilarPages(ctx, a, b, matches, used, info); err != nil {
			return nil, err
		}
	}
	// 定位点之间按顺序补齐；文档体分别维护可用队列，避免跨文档体误配。
	lo, hi := matchWindows(len(a), len(b), matches)
	type queue struct {
		ids  []int
		next []int
	}
	queues := map[int]*queue{}
	for j, p := range b {
		if !used[j] {
			doc := p.Document
			if options.PageMatch == "number" {
				doc = 0
			}
			q := queues[doc]
			if q == nil {
				q = &queue{}
				queues[doc] = q
			}
			q.ids = append(q.ids, j)
		}
	}
	var find func(*queue, int) int
	find = func(q *queue, k int) int {
		root := k
		for q.next[root] != root {
			root = q.next[root]
		}
		for q.next[k] != k {
			n := q.next[k]
			q.next[k] = root
			k = n
		}
		return root
	}
	for _, q := range queues {
		q.next = make([]int, len(q.ids)+1)
		for k := range q.next {
			q.next[k] = k
		}
	}
	for i, p := range a {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, ok := matches[i]; ok {
			continue
		}
		doc := p.Document
		if options.PageMatch == "number" {
			doc = 0
		}
		q := queues[doc]
		if q == nil {
			continue
		}
		lower, upper := 0, len(b)
		if options.PageMatch == "auto" {
			lower, upper = lo[i], hi[i]
		}
		k := find(q, sort.SearchInts(q.ids, lower))
		if k < len(q.ids) && q.ids[k] < upper {
			j := q.ids[k]
			matches[i], used[j] = j, true
			method := "sequence"
			if options.PageMatch == "number" {
				method = "number"
			}
			info[i] = PageMatchInfo{Method: method, SearchLimited: searchLimited}
			q.next[k] = find(q, k+1)
		}
	}
	out := make([]pagePairMatch, 0, len(a)+len(b))
	for i, p := range a {
		after := 0
		basis := PageMatchInfo{Method: "unmatched", SearchLimited: searchLimited}
		if j, ok := matches[i]; ok {
			after = pageNumber(b[j], j)
			basis = info[i]
		}
		out = append(out, pagePairMatch{Before: pageNumber(p, i), After: after, Info: basis})
	}
	for i, p := range b {
		if !used[i] {
			out = append(out, pagePairMatch{After: pageNumber(p, i), Info: PageMatchInfo{Method: "unmatched", SearchLimited: searchLimited}})
		}
	}
	return out, ctx.Err()
}

// 相邻定位点界定修改页范围；移页形成交叉定位点时退回同文档体范围。
func matchWindows(n, m int, matches map[int]int) ([]int, []int) {
	lo, hi := make([]int, n), make([]int, n)
	last := -1
	for i := 0; i < n; i++ {
		lo[i] = last + 1
		if j, ok := matches[i]; ok {
			last = j
		}
	}
	next := m
	for i := n - 1; i >= 0; i-- {
		hi[i] = next
		if j, ok := matches[i]; ok {
			next = j
		}
		if lo[i] > hi[i] {
			lo[i], hi[i] = 0, m
		}
	}
	return lo, hi
}

// 使用前 4096 个非空白字符的相邻字符对，限制长页的匹配工作量。
func textFingerprint(text string) map[uint64]int {
	f := map[uint64]int{}
	var previous rune
	count := 0
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		if count > 0 {
			f[uint64(previous)<<32|uint64(r)]++
		}
		previous = r
		count++
		if count >= 4096 {
			break
		}
	}
	return f
}

func fingerprintSimilarity(a, b map[uint64]int) float64 {
	common, total := 0, 0
	for k, n := range a {
		total += n
		common += min(n, b[k])
	}
	for _, n := range b {
		total += n
	}
	if total == 0 {
		return 0
	}
	return 2 * float64(common) / float64(total)
}

func matchSimilarPages(ctx context.Context, a, b []Page, matches map[int]int, used map[int]bool, info map[int]PageMatchInfo) (bool, error) {
	lo, hi := matchWindows(len(a), len(b), matches)
	type best struct {
		index         int
		score, second float64
	}
	left, right := make([]best, len(a)), make([]best, len(b))
	bf := map[int]map[uint64]int{}
	update := func(v *best, index int, score float64) {
		if score > v.score {
			v.second = v.score
			v.score = score
			v.index = index
		} else if score > v.second {
			v.second = score
		}
	}
	work := 0
	for i, p := range a {
		if _, ok := matches[i]; ok || p.Text == "" {
			continue
		}
		af := textFingerprint(p.Text)
		for j := lo[i]; j < hi[i]; j++ {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			work++
			if work > 4000000 {
				return true, nil
			}
			if used[j] || b[j].Document != p.Document || b[j].Text == "" {
				continue
			}
			if bf[j] == nil {
				if len(bf) >= 64 {
					clear(bf)
				}
				bf[j] = textFingerprint(b[j].Text)
				work += 4096
			}
			work += len(af) + len(bf[j])
			if work > 4000000 {
				return true, nil
			}
			score := fingerprintSimilarity(af, bf[j])
			update(&left[i], j, score)
			update(&right[j], i, score)
		}
	}
	for i, v := range left {
		if v.score < 0.6 || v.score-v.second < 0.1 {
			continue
		}
		r := right[v.index]
		if r.index == i && r.score-r.second >= 0.1 {
			matches[i], used[v.index] = v.index, true
			info[i] = PageMatchInfo{Method: "text_similarity", TextSimilarity: v.score}
		}
	}
	return false, ctx.Err()
}
