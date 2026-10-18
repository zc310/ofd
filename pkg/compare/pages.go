package compare

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// PageRange 是从 1 开始的全局页码闭区间。
type PageRange struct{ First, Last int }

// ParsePageRanges 将 "1-3,5" 解析为排序并合并后的区间，空值表示全部页面。
// 区间不展开为逐页列表，避免极大端点造成无界分配或页码循环溢出。
func ParsePageRanges(spec string) ([]PageRange, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, nil
	}
	ranges := []PageRange{}
	for _, part := range strings.Split(spec, ",") {
		firstText, lastText, hasRange := strings.Cut(strings.TrimSpace(part), "-")
		first, err := strconv.Atoi(strings.TrimSpace(firstText))
		if err != nil || first < 1 {
			return nil, fmt.Errorf("页码无效: %q", part)
		}
		last := first
		if hasRange {
			last, err = strconv.Atoi(strings.TrimSpace(lastText))
			if err != nil || last < first {
				return nil, fmt.Errorf("页码范围无效: %q", part)
			}
		}
		ranges = append(ranges, PageRange{first, last})
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].First == ranges[j].First {
			return ranges[i].Last < ranges[j].Last
		}
		return ranges[i].First < ranges[j].First
	})
	merged := ranges[:0]
	for _, r := range ranges {
		if len(merged) > 0 {
			last := &merged[len(merged)-1]
			if r.First <= last.Last || r.First-1 == last.Last {
				last.Last = max(last.Last, r.Last)
				continue
			}
		}
		merged = append(merged, r)
	}
	return merged, nil
}

func pageSelected(ranges []PageRange, number int) bool {
	if len(ranges) == 0 {
		return true
	}
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i].Last >= number })
	return i < len(ranges) && ranges[i].First <= number
}

func selectedPages(pages []Page, spec string) []Page {
	if strings.TrimSpace(spec) == "" {
		return pages
	}
	selected := []Page{}
	for _, p := range pages {
		if p.Selected {
			selected = append(selected, p)
		}
	}
	return selected
}
