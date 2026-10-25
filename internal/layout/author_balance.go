package layout

import (
	"context"
	"maps"
	"math"
	"reflect"

	"github.com/zc310/ofd/pkg/creator"
)

// 平衡仅改变试排的可用高度，页面版心与脚注实际位置仍使用原几何。
// 每次试排重新生成图元、脚注、锚点和链接，不移动已输出的文字。
type authorBalancePass struct {
	limits  map[int]float64
	columns map[[2]int]float64
	fonts   *authorFontSnapshot
	pages   []authorBalancePage
}

type authorBalancePage struct {
	settings authorPageSettings
	items    []creator.Item
	hard     bool
	heights  []float64
}

func (e *engine) recordAuthorColumnHeight() {
	if e.pageIndex < 0 || e.author.opts.balance == nil {
		return
	}
	a := e.author
	s := a.pageSettings[e.pageIndex].section.geometry
	h := s.PageHeight - s.MarginTop - e.y + a.pageFootnotes[e.pageIndex][a.columnIndex].height
	a.columnHeights[e.pageIndex][a.columnIndex] = math.Max(a.columnHeights[e.pageIndex][a.columnIndex], h)
}

func (b *authorBalancePass) capture(e *engine) {
	e.recordAuthorColumnHeight()
	a := e.author
	b.fonts = &authorFontSnapshot{faces: a.faces, fonts: a.fonts, fontResources: a.fontResources, fontFamilies: a.fontFamilies}
	b.pages = make([]authorBalancePage, len(e.pages))
	for i, page := range e.pages {
		// 在导航回填前保留图元副本；链接目的地可因目标栏变化而合法改变。
		b.pages[i] = authorBalancePage{settings: e.author.pageSettings[i], items: append([]creator.Item(nil), page.Items...), hard: e.author.balanceHardPages[i], heights: append([]float64(nil), a.columnHeights[i]...)}
	}
}

func (e *engine) authorFlowBottom() float64 {
	bottom := e.opts.MarginBottom
	if b := e.author.opts.balance; b != nil {
		if height, ok := b.limits[e.pageIndex]; ok {
			bottom = math.Max(bottom, e.contentTop-height)
		}
		if height, ok := b.columns[[2]int{e.pageIndex, e.author.columnIndex}]; ok {
			bottom = math.Max(bottom, e.contentTop-height)
		}
	}
	if bottom-e.opts.MarginBottom < 0.000001 {
		return e.opts.MarginBottom
	}
	return bottom
}

func authorWantsBalance(blocks []FlowBlock, opts FlowOptions) bool {
	if opts.Columns.Balance {
		return true
	}
	for _, b := range blocks {
		if b.Section != nil && b.Section.Columns != nil && b.Section.Columns.Balance {
			return true
		}
	}
	return false
}

// 首先取得自然分页，再只压缩各节末页的可用栏高。
// 有限次数二分试排，以仍能装入原页数且不改变其他页面内容的结果为上界。
// 每轮导航回填重新确定末页，字体解析快照在全部试排之间共享。
func composePassOnce(ctx context.Context, blocks []FlowBlock, opts FlowOptions) (*creator.Document, error) {
	if !authorWantsBalance(blocks, opts) {
		return composeLayoutPass(ctx, blocks, opts)
	}
	trace := &authorBalancePass{}
	opts.balance = trace
	result, err := composeLayoutPass(ctx, blocks, opts)
	if err != nil {
		return nil, err
	}
	last := make(map[int]int)
	for i, page := range trace.pages {
		last[page.settings.sectionIndex] = i
	}
	limits := make(map[int]float64)
	columns := make(map[[2]int]float64)
	for page, settings := range trace.pages {
		s := settings.settings.section
		if last[settings.settings.sectionIndex] != page || !s.columns.Balance || s.columns.Count < 2 || settings.hard || len(settings.items) == 0 {
			continue
		}
		low := 0.0
		high := s.geometry.PageHeight - s.geometry.MarginTop - s.geometry.MarginBottom
		// 保留前面已经平衡的页；其他页面在图元与显式边界上必须完全一致。
		baseline := trace
		bestLabels := maps.Clone(opts.resolvedLabels)
		for attempt := 0; attempt < 24 && high-low > 0.0001; attempt++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			height := (low + high) / 2
			trial := &authorBalancePass{limits: maps.Clone(limits), columns: maps.Clone(columns), fonts: baseline.fonts}
			trial.limits[page] = height
			trialOpts := opts
			trialOpts.balance = trial
			trialOpts.resolvedLabels = make(map[string]string)
			candidate, err := composeLayoutPass(ctx, blocks, trialOpts)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if err != nil || !authorBalancePreservesPages(baseline, trial, page) {
				low = height
				continue
			}
			high, result, trace = height, candidate, trial
			bestLabels = trialOpts.resolvedLabels
		}
		limits[page] = high
		// 同一上限可能得到 4/4/2 等分布。再按当前平均占用高度缩短前面的栏，
		// 仅接受方差不增的合法结果，使余数分散到后续栏，避免多栏末尾大片空栏。
		// 同方差时仍可收紧该栏上限，让后续栏的调整越过离散文字行造成的平台。
		for sweep := 0; sweep < 2; sweep++ {
			for col := 0; col < s.columns.Count-1; col++ {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				used := trace.pages[page].heights
				mean := 0.0
				for _, h := range used {
					mean += h
				}
				mean /= float64(len(used))
				key := [2]int{page, col}
				cap := high
				if h, ok := columns[key]; ok {
					cap = h
				}
				if mean >= cap-0.001 {
					continue
				}
				trial := &authorBalancePass{limits: maps.Clone(limits), columns: maps.Clone(columns), fonts: trace.fonts}
				trial.columns[key] = mean
				trialOpts := opts
				trialOpts.balance = trial
				trialOpts.resolvedLabels = make(map[string]string)
				candidate, err := composeLayoutPass(ctx, blocks, trialOpts)
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				if err != nil || !authorBalancePreservesPages(trace, trial, page) || authorColumnVariance(trial.pages[page].heights) > authorColumnVariance(used)+0.000001 {
					continue
				}
				columns[key], result, trace = mean, candidate, trial
				bestLabels = trialOpts.resolvedLabels
			}
		}
		clear(opts.resolvedLabels)
		maps.Copy(opts.resolvedLabels, bestLabels)
	}
	return result, nil
}

func authorColumnVariance(heights []float64) float64 {
	mean := 0.0
	for _, h := range heights {
		mean += h
	}
	mean /= float64(len(heights))
	variance := 0.0
	for _, h := range heights {
		variance += (h - mean) * (h - mean)
	}
	return variance
}

func authorBalancePreservesPages(before, after *authorBalancePass, target int) bool {
	if len(before.pages) != len(after.pages) {
		return false
	}
	for i, page := range before.pages {
		other := after.pages[i]
		if page.settings.sectionIndex != other.settings.sectionIndex || page.settings.sectionPage != other.settings.sectionPage || page.hard != other.hard {
			return false
		}
		if i != target && !reflect.DeepEqual(page.items, other.items) {
			return false
		}
	}
	return true
}
